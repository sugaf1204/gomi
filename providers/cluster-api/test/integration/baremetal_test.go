package integration

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/sugaf1204/gomi/internal/auth"
	"github.com/sugaf1204/gomi/internal/baremetal"
	"github.com/sugaf1204/gomi/internal/cloudinit"
	serverapi "github.com/sugaf1204/gomi/internal/infra/api"
	infrasql "github.com/sugaf1204/gomi/internal/infra/sql"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/osimage"
	"github.com/sugaf1204/gomi/internal/power"
	infrav1 "github.com/sugaf1204/gomi/providers/cluster-api/api/v1alpha1"
	"github.com/sugaf1204/gomi/providers/cluster-api/internal/controllers"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type bareMetalPower struct{}

func (bareMetalPower) Execute(context.Context, power.MachineInfo, power.Action) error { return nil }
func (bareMetalPower) CheckStatus(context.Context, power.MachineInfo) (power.PowerState, error) {
	return power.PowerStateStopped, nil
}
func (bareMetalPower) ConfigureBootOrder(context.Context, power.MachineInfo, power.BootOrder) error {
	return nil
}

// Uses the real Kubernetes API, SQL backend, GOMI HTTP API and controller. Guest
// completion is injected explicitly; this is not proof of a physical OS boot.
func runBareMetalLifecycle(t *testing.T, kube client.Client) {
	ctx := context.Background()
	namespace := "capi-physical"
	must(t, kube.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}))
	backend, err := infrasql.New("sqlite", filepath.Join(t.TempDir(), "gomi.db"))
	must(t, err)
	t.Cleanup(func() { backend.Close() })
	must(t, backend.Migrate())
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	must(t, err)
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	must(t, err)
	public := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	original := machine.Machine{Name: "node1", Hostname: "node1", MAC: "00:11:22:33:44:55", Arch: "amd64", Firmware: machine.FirmwareUEFI, IP: "192.0.2.101", Phase: machine.PhaseReady, OSPreset: machine.OSPreset{Family: machine.OSTypeUbuntu, Version: "22.04", ImageRef: "prepared"}, Power: power.PowerConfig{Type: power.PowerTypeWebhook, Webhook: &power.WebhookConfig{PowerOnURL: "http://power.test/on", PowerOffURL: "http://power.test/off"}}}
	must(t, backend.Machines().Upsert(ctx, original))
	_, err = backend.BareMetal().Register(ctx, "node1", "pool", public, "/dev/nvme0n1")
	must(t, err)
	must(t, backend.OSImages().Upsert(ctx, osimage.OSImage{Name: "prepared", OSFamily: "ubuntu", OSVersion: "22.04", Format: osimage.FormatSquashFS, Arch: "amd64", Ready: true, Manifest: &osimage.Manifest{Root: osimage.RootArtifact{Path: "rootfs.squashfs", Format: osimage.FormatSquashFS}}}))
	token := "gomi_sa_integration-physical"
	digest := sha256.Sum256([]byte(token))
	must(t, backend.Auth().UpsertServiceAccount(ctx, auth.ServiceAccount{Name: "capi", Role: auth.RoleOperator, TokenHash: hex.EncodeToString(digest[:]), CreatedAt: time.Now()}))
	srv := serverapi.NewServer(serverapi.ServerConfig{Machines: machine.NewService(backend.Machines()), BareMetal: backend.BareMetal(), PowerExecutor: bareMetalPower{}, AuthStore: backend.Auth(), AuthService: serverapi.NewAuthService(backend.Auth(), time.Hour), OSImages: osimage.NewService(backend.OSImages()), CloudInits: cloudinit.NewService(backend.CloudInits())})
	server := httptest.NewServer(srv.Echo())
	t.Cleanup(server.Close)
	cluster := &clusterv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster", Namespace: namespace}, Spec: clusterv1.ClusterSpec{InfrastructureRef: clusterv1.ContractVersionedObjectReference{APIGroup: infrav1.GroupVersion.Group, Kind: "GomiCluster", Name: "infra"}}}
	must(t, kube.Create(ctx, cluster))
	must(t, kube.Create(ctx, &infrav1.GomiCluster{ObjectMeta: metav1.ObjectMeta{Name: "infra", Namespace: namespace}, Spec: infrav1.GomiClusterSpec{Server: server.URL, CredentialsRef: infrav1.SecretReference{Name: "credentials"}, ControlPlaneEndpoint: infrav1.Endpoint{Host: "192.0.2.1", Port: 6443}}}))
	must(t, kube.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "credentials", Namespace: namespace}, Data: map[string][]byte{"token": []byte(token)}}))
	bootstrap := "bootstrap"
	owner := &clusterv1.Machine{ObjectMeta: metav1.ObjectMeta{Name: "owner", Namespace: namespace}, Spec: clusterv1.MachineSpec{ClusterName: cluster.Name, Bootstrap: clusterv1.Bootstrap{DataSecretName: &bootstrap}, InfrastructureRef: clusterv1.ContractVersionedObjectReference{APIGroup: infrav1.GroupVersion.Group, Kind: "GomiMachine", Name: "physical"}}}
	must(t, kube.Create(ctx, owner))
	infra := &infrav1.GomiMachine{ObjectMeta: metav1.ObjectMeta{Name: "physical", Namespace: namespace, OwnerReferences: []metav1.OwnerReference{{APIVersion: clusterv1.GroupVersion.String(), Kind: "Machine", Name: owner.Name, UID: owner.UID}}}, Spec: infrav1.GomiMachineSpec{Kind: "BareMetal", BareMetal: &infrav1.BareMetalSpec{Pool: "pool", OSImageRef: "prepared"}}}
	must(t, kube.Create(ctx, infra))
	data := []byte("## template: jinja\n#cloud-config\nwrite_files:\n- path: /root/ca.key\n  content: PRIVATE-CA-SECRET\nruncmd:\n- kubeadm init\n")
	must(t, kube.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: bootstrap, Namespace: namespace}, Data: map[string][]byte{"value": data, "format": []byte("cloud-config")}}))
	reconciler := &controllers.MachineReconciler{Client: kube}
	objectKey := client.ObjectKeyFromObject(infra)
	step := func() {
		t.Helper()
		_, e := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: objectKey})
		must(t, e)
	}
	step()
	step()
	step()
	must(t, kube.Get(ctx, objectKey, infra))
	id := infra.Spec.InstanceID
	h, err := backend.BareMetal().FindOwner(ctx, id)
	must(t, err)
	if h.State != baremetal.Deploying {
		t.Fatalf("not deploying: %s", h.State)
	}
	deployed, err := backend.Machines().Get(ctx, "node1")
	must(t, err)
	if deployed.SealedBootstrap == nil || bytes.Contains(deployed.SealedBootstrap.Envelope, []byte("PRIVATE-CA-SECRET")) {
		t.Fatal("bootstrap not sealed")
	}
	firstAttempt := deployed.Provision.AttemptID
	step() // Re-observation cannot create a second attempt.
	observed, err := backend.Machines().Get(ctx, "node1")
	must(t, err)
	if observed.Provision.AttemptID != firstAttempt {
		t.Fatal("retry reinstalled the host")
	}
	if err := backend.Machines().Upsert(ctx, original); err != baremetal.ErrConflict {
		t.Fatalf("stale machine snapshot overwrote claim: %v", err)
	}
	// Deleting while the installer runs must not reboot it and lose its RAM key.
	request, err := http.NewRequest("DELETE", server.URL+"/api/v1/bare-metal-claims/"+id, nil)
	must(t, err)
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	must(t, err)
	response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("delete during install: %d", response.StatusCode)
	}
	observed, err = backend.Machines().Get(ctx, "node1")
	must(t, err)
	if observed.Provision.AttemptID != firstAttempt {
		t.Fatal("delete interrupted active deployment")
	}
	now := time.Now()
	deployed.Phase = machine.PhaseReady
	deployed.Provision.Active = false
	deployed.Provision.CompletedAt = &now
	// Completion follows curtin restoring the enrolled identity to disk.
	deployed.Provision.Artifacts = map[string]string{"imageApplied": "true"}
	must(t, backend.Machines().Upsert(ctx, deployed))
	step()
	must(t, kube.Get(ctx, objectKey, infra))
	if infra.Spec.ProviderID == nil || *infra.Spec.ProviderID != "gomi:///"+id || infra.Status.Initialization.Provisioned == nil || len(infra.Status.Addresses) != 1 {
		t.Fatal("physical provider status missing")
	}
	invalid := infra.DeepCopy()
	invalid.Spec.BareMetal.Pool = "other"
	if err := kube.Update(ctx, invalid); !apierrors.IsInvalid(err) {
		t.Fatalf("pool mutation accepted: %v", err)
	}
	must(t, kube.Delete(ctx, infra))
	step()
	cleanup, err := backend.Machines().Get(ctx, "node1")
	must(t, err)
	if cleanup.SealedBootstrap == nil || !cleanup.SealedBootstrap.Cleanup || cleanup.Provision.AttemptID == firstAttempt {
		t.Fatal("cleanup not started as separate fenced attempt")
	}
	h, err = backend.BareMetal().FindOwner(ctx, id)
	must(t, err)
	if h.State != baremetal.Releasing {
		t.Fatal("host freed before cleanup")
	}
	if _, err = backend.BareMetal().Acquire(ctx, "pool", "capi-other"); err != baremetal.ErrCapacity {
		t.Fatalf("unclean host was reallocated: %v", err)
	}
	cleanup.Phase = machine.PhaseReady
	cleanup.Provision.Active = false
	cleanup.Provision.CompletedAt = &now
	must(t, backend.Machines().Upsert(ctx, cleanup))
	step()
	if err := kube.Get(ctx, objectKey, infra); !apierrors.IsNotFound(err) {
		t.Fatalf("finalizer still present: %v", err)
	}
	h, err = backend.BareMetal().Get(ctx, "node1")
	must(t, err)
	if h.State != baremetal.Available || h.Owner != "" {
		t.Fatal("cleanup did not release host")
	}
	if _, err = backend.Machines().Get(ctx, "node1"); err != nil {
		t.Fatal("physical inventory was deleted")
	}
}

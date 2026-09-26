package integration

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sugaf1204/gomi/internal/auth"
	"github.com/sugaf1204/gomi/internal/cloudinit"
	"github.com/sugaf1204/gomi/internal/hypervisor"
	serverapi "github.com/sugaf1204/gomi/internal/infra/api"
	"github.com/sugaf1204/gomi/internal/infra/memory"
	"github.com/sugaf1204/gomi/internal/osimage"
	"github.com/sugaf1204/gomi/internal/vm"
	infrav1 "github.com/sugaf1204/gomi/providers/cluster-api/api/v1alpha1"
	"github.com/sugaf1204/gomi/providers/cluster-api/internal/controllers"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func TestRealAPIsLifecycle(t *testing.T) {
	crds := os.Getenv("CAPI_CRD_DIR")
	if crds == "" || os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Fatal("run make integration to provide pinned CAPI CRDs and envtest assets")
	}
	env := &envtest.Environment{CRDDirectoryPaths: []string{filepath.Join("..", "..", "config", "crd"), crds, filepath.Join(crds, "../../../..", "bootstrap/kubeadm/config/crd/bases"), filepath.Join(crds, "../../../..", "controlplane/kubeadm/config/crd/bases")}, ErrorIfCRDPathMissing: true}
	config, err := env.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := env.Stop(); err != nil {
			t.Error(err)
		}
	})
	scheme := runtime.NewScheme()
	must(t, corev1.AddToScheme(scheme))
	must(t, clusterv1.AddToScheme(scheme))
	must(t, infrav1.AddToScheme(scheme))
	kube, err := client.New(config, client.Options{Scheme: scheme})
	must(t, err)
	t.Run("sample-schema", func(t *testing.T) { validateSample(t, kube) })
	for _, family := range []string{"ubuntu", "fedora"} {
		t.Run(family, func(t *testing.T) { runLifecycle(t, kube, family) })
	}
	t.Run("manager-watch", func(t *testing.T) { testManagerWatches(t, config, kube) })
}
func runLifecycle(t *testing.T, kube client.Client, family string) {
	ctx := context.Background()
	namespace := "capi-" + family
	must(t, kube.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}))
	backend := memory.New()
	must(t, backend.Auth().UpsertUser(ctx, auth.User{Username: "controller", Role: auth.RoleOperator}))
	must(t, backend.Auth().CreateSession(ctx, auth.Session{Token: "integration-token", Username: "controller", ExpiresAt: time.Now().Add(time.Hour)}))
	must(t, backend.Hypervisors().Upsert(ctx, hypervisor.Hypervisor{Name: "test-hv", Connection: hypervisor.ConnectionSpec{Type: hypervisor.ConnectionTCP, Host: "192.0.2.1"}, Phase: hypervisor.PhaseReady}))
	must(t, backend.OSImages().Upsert(ctx, osimage.OSImage{Name: "prepared", OSFamily: family, Format: osimage.FormatQCOW2, Variant: osimage.VariantCloud, Arch: "amd64", Ready: true}))
	deleteCalled := false
	srv := serverapi.NewServer(serverapi.ServerConfig{
		AuthStore: backend.Auth(), AuthService: serverapi.NewAuthService(backend.Auth(), time.Hour),
		VMs: vm.NewService(backend.VMs()), Hypervisors: hypervisor.NewService(backend.Hypervisors(), backend.HypervisorTokens(), backend.AgentTokens()),
		OSImages: osimage.NewService(backend.OSImages()), CloudInits: cloudinit.NewService(backend.CloudInits()),
		VMRuntimeDeleter: func(context.Context, vm.VirtualMachine) error { deleteCalled = true; return nil },
	})
	httpServer := httptest.NewServer(srv.Echo())
	t.Cleanup(httpServer.Close)
	cluster := &clusterv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster", Namespace: namespace}, Spec: clusterv1.ClusterSpec{InfrastructureRef: clusterv1.ContractVersionedObjectReference{APIGroup: infrav1.GroupVersion.Group, Kind: "GomiCluster", Name: "infra"}}}
	must(t, kube.Create(ctx, cluster))
	infraCluster := &infrav1.GomiCluster{ObjectMeta: metav1.ObjectMeta{Name: "infra", Namespace: namespace, OwnerReferences: []metav1.OwnerReference{{APIVersion: clusterv1.GroupVersion.String(), Kind: "Cluster", Name: cluster.Name, UID: cluster.UID}}}, Spec: infrav1.GomiClusterSpec{Server: httpServer.URL, CredentialsRef: infrav1.SecretReference{Name: "credentials"}, ControlPlaneEndpoint: infrav1.Endpoint{Host: "192.0.2.10", Port: 6443}}}
	must(t, kube.Create(ctx, infraCluster))
	must(t, kube.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "credentials", Namespace: namespace}, Data: map[string][]byte{"token": []byte("integration-token")}}))
	cr := &controllers.ClusterReconciler{Client: kube}
	_, err := cr.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(infraCluster)})
	must(t, err)
	must(t, kube.Get(ctx, client.ObjectKeyFromObject(infraCluster), infraCluster))
	if infraCluster.Status.Initialization.Provisioned == nil || !*infraCluster.Status.Initialization.Provisioned {
		t.Fatal("cluster not provisioned")
	}
	bootstrap := "bootstrap"
	owner := &clusterv1.Machine{ObjectMeta: metav1.ObjectMeta{Name: "owner", Namespace: namespace}, Spec: clusterv1.MachineSpec{ClusterName: cluster.Name, InfrastructureRef: clusterv1.ContractVersionedObjectReference{APIGroup: infrav1.GroupVersion.Group, Kind: "GomiMachine", Name: "machine"}, Bootstrap: clusterv1.Bootstrap{DataSecretName: &bootstrap}}}
	must(t, kube.Create(ctx, owner))
	machine := &infrav1.GomiMachine{ObjectMeta: metav1.ObjectMeta{Name: "machine", Namespace: namespace, OwnerReferences: []metav1.OwnerReference{{APIVersion: clusterv1.GroupVersion.String(), Kind: "Machine", Name: owner.Name, UID: owner.UID}}}, Spec: infrav1.GomiMachineSpec{VirtualMachine: infrav1.VirtualMachineSpec{OSImageRef: "prepared", HypervisorRef: "test-hv", Resources: infrav1.Resources{CPUCores: 2, MemoryMB: 2048, DiskGB: 20}}}}
	must(t, kube.Create(ctx, machine))
	data := []byte("## template: jinja\n#cloud-config\nwrite_files:\n- path: /run/kubeadm/kubeadm.yaml\n  content: |\n    apiVersion: kubeadm.k8s.io/v1beta4\n    kind: JoinConfiguration\n    nodeRegistration:\n      kubeletExtraArgs:\n      - name: provider-id\n        value: gomi:///{{ v1.local_hostname }}\nruncmd:\n- kubeadm join --config /run/kubeadm/kubeadm.yaml\n")
	must(t, kube.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: bootstrap, Namespace: namespace}, Data: map[string][]byte{"value": data, "format": []byte("cloud-config")}}))
	reconciler := &controllers.MachineReconciler{Client: kube}
	key := types.NamespacedName{Namespace: namespace, Name: machine.Name}
	step := func() {
		t.Helper()
		_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		must(t, err)
	}
	step()
	step()
	step()
	must(t, kube.Get(ctx, key, machine))
	id := machine.Spec.InstanceID
	list, err := backend.VMs().List(ctx)
	must(t, err)
	if len(list) != 1 {
		t.Fatalf("expected one VM, got %d", len(list))
	}
	tpl, err := backend.CloudInits().Get(ctx, id)
	must(t, err)
	if tpl.UserData != string(data) {
		t.Fatal("bootstrap data changed")
	}
	// Model the external guest's completion signal. No libvirt/guest boot is
	// simulated as end-to-end proof; the real GOMI HTTP and K8s API are exercised.
	v, err := backend.VMs().Get(ctx, id)
	must(t, err)
	now := time.Now()
	v.Phase = vm.PhaseRunning
	v.Provisioning.Active = false
	v.Provisioning.CompletedAt = &now
	v.IPAddresses = []string{"192.0.2.20"}
	must(t, backend.VMs().Upsert(ctx, v))
	step()
	must(t, kube.Get(ctx, key, machine))
	if machine.Spec.ProviderID == nil || *machine.Spec.ProviderID != "gomi:///"+id || machine.Status.Initialization.Provisioned == nil {
		t.Fatalf("provider status not persisted: spec=%+v status=%+v", machine.Spec, machine.Status)
	}
	// Test real CEL transition validation, not just the fake Kubernetes client.
	modified := machine.DeepCopy()
	modified.Spec.InstanceID = "capi-other"
	if err := kube.Update(ctx, modified); !apierrors.IsInvalid(err) {
		t.Fatalf("identity mutation accepted: %v", err)
	}
	modified = machine.DeepCopy()
	modified.Spec.InstanceID = ""
	if err := kube.Update(ctx, modified); !apierrors.IsInvalid(err) {
		t.Fatalf("identity removal accepted: %v", err)
	}
	modified = machine.DeepCopy()
	modified.Spec.VirtualMachine.OSImageRef = "other"
	if err := kube.Update(ctx, modified); !apierrors.IsInvalid(err) {
		t.Fatalf("machine mutation accepted: %v", err)
	}
	invalid := machine.DeepCopy()
	invalid.ResourceVersion = ""
	invalid.UID = ""
	invalid.Name = "baremetal"
	invalid.Spec.Kind = "BareMetal"
	if err := kube.Create(ctx, invalid); !apierrors.IsInvalid(err) {
		t.Fatalf("unsupported kind accepted: %v", err)
	}
	must(t, kube.Delete(ctx, machine))
	step()
	if !deleteCalled {
		t.Fatal("VM runtime teardown not called")
	}
	list, err = backend.VMs().List(ctx)
	must(t, err)
	templates, err := backend.CloudInits().List(ctx)
	must(t, err)
	if len(list) != 0 || len(templates) != 0 {
		t.Fatal("external resource leak")
	}
	if err := kube.Get(ctx, key, &infrav1.GomiMachine{}); !apierrors.IsNotFound(err) {
		t.Fatalf("finalizer not removed: %v", err)
	}
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

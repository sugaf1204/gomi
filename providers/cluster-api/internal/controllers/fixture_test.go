package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	infrav1 "github.com/sugaf1204/gomi/providers/cluster-api/api/v1alpha1"
	"github.com/sugaf1204/gomi/providers/cluster-api/internal/gomi"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type fixture struct {
	t                                       *testing.T
	r                                       *MachineReconciler
	key                                     types.NamespacedName
	vm                                      *gomi.VM
	template                                *gomi.Template
	creates, templateCreates, deletes       int
	failDelete, failGet, lostCreateResponse bool
	unsupportedSeed                         bool
	requests                                []string
	url                                     string
}

func setup(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{t: t, key: types.NamespacedName{Namespace: "test", Name: "machine"}}
	server := httptest.NewServer(http.HandlerFunc(f.serve))
	f.url = server.URL
	t.Cleanup(server.Close)
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = clusterv1.AddToScheme(scheme)
	_ = infrav1.AddToScheme(scheme)
	bootstrap := "bootstrap"
	objects := []client.Object{
		&infrav1.GomiMachine{ObjectMeta: metav1.ObjectMeta{Namespace: "test", Name: "machine", UID: "machine-uid", OwnerReferences: []metav1.OwnerReference{{APIVersion: clusterv1.GroupVersion.String(), Kind: "Machine", Name: "owner"}}}, Spec: infrav1.GomiMachineSpec{VirtualMachine: &infrav1.VirtualMachineSpec{OSImageRef: "prepared-fedora", Resources: infrav1.Resources{CPUCores: 2, MemoryMB: 2048, DiskGB: 20}}}},
		&clusterv1.Machine{ObjectMeta: metav1.ObjectMeta{Namespace: "test", Name: "owner"}, Spec: clusterv1.MachineSpec{ClusterName: "cluster", Bootstrap: clusterv1.Bootstrap{DataSecretName: &bootstrap}}},
		&clusterv1.Cluster{ObjectMeta: metav1.ObjectMeta{Namespace: "test", Name: "cluster"}, Spec: clusterv1.ClusterSpec{InfrastructureRef: clusterv1.ContractVersionedObjectReference{APIGroup: infrav1.GroupVersion.Group, Kind: "GomiCluster", Name: "infra"}}},
		&infrav1.GomiCluster{ObjectMeta: metav1.ObjectMeta{Namespace: "test", Name: "infra"}, Spec: infrav1.GomiClusterSpec{Server: server.URL, CredentialsRef: infrav1.SecretReference{Name: "credentials"}}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "test", Name: "credentials"}, Data: map[string][]byte{"token": []byte("test-token")}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "test", Name: bootstrap}, Data: map[string][]byte{"value": []byte("## template: jinja\n#cloud-config\nruncmd:\n- kubeadm join\n"), "format": []byte("cloud-config")}},
	}
	f.r = &MachineReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&infrav1.GomiMachine{}, &infrav1.GomiCluster{}).WithObjects(objects...).Build()}
	return f
}
func (f *fixture) serve(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer test-token" {
		f.t.Error("missing authentication")
		w.WriteHeader(401)
		return
	}
	f.requests = append(f.requests, r.Method+" "+r.URL.Path)
	switch r.Method + " " + r.URL.Path {
	case "GET /api/v1/capabilities":
		if f.unsupportedSeed {
			w.WriteHeader(404)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"vmSeedTemplates": true, "bareMetalProvisioningTemplates": true})
	case "GET /api/v1/virtual-machines/capi-machine-uid":
		if f.failGet {
			w.WriteHeader(503)
			return
		}
		if f.vm == nil {
			w.WriteHeader(404)
			return
		}
		_ = json.NewEncoder(w).Encode(f.vm)
	case "GET /api/v1/cloud-init-templates/capi-machine-uid":
		if f.template == nil {
			w.WriteHeader(404)
			return
		}
		_ = json.NewEncoder(w).Encode(f.template)
	case "POST /api/v1/cloud-init-templates":
		f.templateCreates++
		f.template = &gomi.Template{}
		_ = json.NewDecoder(r.Body).Decode(f.template)
		w.WriteHeader(201)
	case "POST /api/v1/virtual-machines":
		f.creates++
		var body struct {
			ID string         `json:"virtualMachineId"`
			VM map[string]any `json:"virtualMachine"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.ID != "capi-machine-uid" || body.VM["cloudInitRef"] != body.ID || body.VM["powerControlMethod"] != "libvirt" || body.VM["osImageRef"] != "prepared-fedora" {
			f.t.Errorf("bad create request: %+v", body)
		}
		f.vm = &gomi.VM{Name: "virtualMachines/" + body.ID, CloudInitRef: "cloudInitTemplates/" + body.ID, Phase: "Provisioning"}
		if f.lostCreateResponse {
			w.WriteHeader(504)
			return
		}
		w.WriteHeader(201)
	case "DELETE /api/v1/virtual-machines/capi-machine-uid":
		if f.failDelete {
			w.WriteHeader(502)
			return
		}
		f.deletes++
		f.vm = nil
		w.WriteHeader(204)
	case "DELETE /api/v1/cloud-init-templates/capi-machine-uid":
		if f.vm != nil {
			f.t.Error("bootstrap deleted before VM")
		}
		f.template = nil
		w.WriteHeader(204)
	default:
		f.t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		w.WriteHeader(500)
	}
}
func (f *fixture) serverURL() string { return f.url }
func (f *fixture) step() error {
	_, err := f.r.Reconcile(context.Background(), ctrl.Request{NamespacedName: f.key})
	return err
}
func (f *fixture) ok() {
	f.t.Helper()
	if err := f.step(); err != nil {
		f.t.Fatal(err)
	}
}
func (f *fixture) get() *infrav1.GomiMachine {
	f.t.Helper()
	m := &infrav1.GomiMachine{}
	if err := f.r.Get(context.Background(), f.key, m); err != nil {
		f.t.Fatal(err)
	}
	return m
}
func (f *fixture) update(o client.Object) {
	f.t.Helper()
	if err := f.r.Update(context.Background(), o); err != nil {
		f.t.Fatal(err)
	}
}

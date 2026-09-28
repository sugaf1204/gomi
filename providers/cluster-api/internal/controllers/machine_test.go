package controllers

import (
	"context"
	infrav1 "github.com/sugaf1204/gomi/providers/cluster-api/api/v1alpha1"
	"github.com/sugaf1204/gomi/providers/cluster-api/internal/gomi"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"testing"
	"time"
)

func TestLifecycleAndMove(t *testing.T) {
	f := setup(t)
	f.ok()
	if len(f.requests) != 0 {
		t.Fatal("external write before identity persisted")
	}
	f.ok()
	f.ok()
	if f.creates != 1 || f.templateCreates != 1 {
		t.Fatalf("duplicate create: %d %d", f.creates, f.templateCreates)
	}
	now := time.Now()
	f.vm.Phase = "Running"
	f.vm.Provisioning.CompletedAt = &now
	f.vm.IPAddresses = []string{"192.0.2.12"}
	f.ok()
	m := f.get()
	if m.Spec.ProviderID == nil || *m.Spec.ProviderID != "gomi:///capi-machine-uid" || m.Status.Initialization.Provisioned == nil || !*m.Status.Initialization.Provisioned || len(m.Status.Addresses) != 1 {
		t.Fatalf("not provisioned: %+v", m)
	}
	// Simulate move: UID and status are replaced, but spec survives.
	m.UID = "different-management-cluster-uid"
	f.update(m)
	m = f.get()
	m.Status = infrav1.GomiMachineStatus{}
	if err := f.r.Status().Update(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	f.ok()
	if f.creates != 1 || f.get().Status.Initialization.Provisioned == nil {
		t.Fatal("move did not rediscover instance")
	}
	if err := f.r.Delete(context.Background(), f.get()); err != nil {
		t.Fatal(err)
	}
	f.failDelete = true
	if f.step() == nil {
		t.Fatal("expected failed deletion")
	}
	if f.template == nil || len(f.get().Finalizers) == 0 {
		t.Fatal("cleanup state lost")
	}
	f.failDelete = false
	f.ok()
	if f.vm != nil || f.template != nil || f.deletes != 1 {
		t.Fatal("external resources leaked")
	}
	if err := f.r.Get(context.Background(), f.key, &infrav1.GomiMachine{}); err == nil {
		t.Fatal("finalizer not released")
	}
}
func TestUnknownCreateResponse(t *testing.T) {
	f := setup(t)
	f.ok()
	f.lostCreateResponse = true
	if f.step() == nil {
		t.Fatal("expected transport failure")
	}
	f.ok()
	if f.creates != 1 {
		t.Fatal("created duplicate VM")
	}
}
func TestOutageDoesNotCreate(t *testing.T) {
	f := setup(t)
	f.ok()
	f.failGet = true
	if f.step() == nil {
		t.Fatal("expected error")
	}
	if f.creates != 0 || f.templateCreates != 0 {
		t.Fatal("write after failed read")
	}
}
func TestOwnershipConflict(t *testing.T) {
	f := setup(t)
	f.ok()
	f.vm = &gomi.VM{CloudInitRef: "someone-else", Phase: "Running"}
	f.ok()
	if f.get().Status.Conditions[0].Reason != "OwnershipConflict" {
		t.Fatal("adopted foreign VM")
	}
	if err := f.r.Delete(context.Background(), f.get()); err != nil {
		t.Fatal(err)
	}
	if f.step() == nil || f.deletes != 0 {
		t.Fatal("deleted foreign VM")
	}
}
func TestMissingInstanceNotRecreated(t *testing.T) {
	f := setup(t)
	f.ok()
	m := f.get()
	id := "gomi:///capi-machine-uid"
	m.Spec.ProviderID = &id
	f.update(m)
	f.ok()
	if f.creates != 0 || f.get().Status.Conditions[0].Reason != "InstanceMissing" {
		t.Fatal("missing instance recreated")
	}
}
func TestPausedCluster(t *testing.T) {
	f := setup(t)
	var c clusterv1.Cluster
	_ = f.r.Get(context.Background(), types.NamespacedName{Namespace: "test", Name: "cluster"}, &c)
	b := true
	c.Spec.Paused = &b
	f.update(&c)
	f.ok()
	if len(f.requests) != 0 || f.get().Spec.InstanceID != "" {
		t.Fatal("paused cluster reconciled")
	}
}
func TestUnsupportedKind(t *testing.T) {
	f := setup(t)
	m := f.get()
	m.Spec.Kind = "Unknown"
	f.update(m)
	f.ok()
	if len(f.requests) != 0 || f.get().Status.Conditions[0].Reason != "UnsupportedKind" {
		t.Fatal("bare metal accepted")
	}
}
func TestBootstrapValidation(t *testing.T) {
	for _, tt := range []struct {
		name, data, format string
		ok                 bool
	}{
		{"cloud-config", "#cloud-config\nruncmd: []\n", "cloud-config", true},
		{"jinja", "## template: jinja\n#cloud-config\nhostname: '{{ v1.local_hostname }}'\n", "", true},
		{"ignition", "{}", "ignition", false}, {"shell", "#!/bin/sh\necho hi", "", false}, {"empty", "", "", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if (validateBootstrap(tt.data, tt.format) == nil) != tt.ok {
				t.Fatal("wrong bootstrap validation")
			}
		})
	}
}

func TestDeclarativeCloudInitValidation(t *testing.T) {
	valid := "#cloud-config\npackages: [kubelet]\nwrite_files:\n- path: /etc/example\n  content: value\n"
	if err := validateDeclarativeCloudInit(valid); err != nil {
		t.Fatalf("valid declarative config rejected: %v", err)
	}
	for _, key := range []string{"bootcmd", "runcmd"} {
		if err := validateDeclarativeCloudInit("#cloud-config\n" + key + ": [echo no]\n"); err == nil {
			t.Fatalf("imperative %s accepted", key)
		}
	}
}

func TestBareMetalCloudInitRequiresImmutableConfigMap(t *testing.T) {
	f := setup(t)
	immutable := true
	config := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Namespace: "test", Name: "node-packages"},
		Immutable:  &immutable,
		Data:       map[string]string{"user-data": "#cloud-config\npackages: [kubelet]\n"},
	}
	if err := f.r.Create(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	got, err := f.r.bareMetalCloudInit(context.Background(), "test", &infrav1.ConfigMapKeyReference{Name: config.Name, Key: "user-data"})
	if err != nil || got != config.Data["user-data"] {
		t.Fatalf("cloud-init was not resolved: %q, %v", got, err)
	}
	mutable := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: "test", Name: "mutable"}, Data: config.Data}
	if err := f.r.Create(context.Background(), mutable); err != nil {
		t.Fatal(err)
	}
	if _, err := f.r.bareMetalCloudInit(context.Background(), "test", &infrav1.ConfigMapKeyReference{Name: mutable.Name, Key: "user-data"}); err == nil {
		t.Fatal("mutable provisioning config accepted")
	}
}

func TestBootstrapPending(t *testing.T) {
	f := setup(t)
	f.ok()
	var m clusterv1.Machine
	if err := f.r.Get(context.Background(), types.NamespacedName{Namespace: "test", Name: "owner"}, &m); err != nil {
		t.Fatal(err)
	}
	m.Spec.Bootstrap.DataSecretName = nil
	f.update(&m)
	f.ok()
	if f.creates != 0 || f.templateCreates != 0 || f.get().Status.Conditions[0].Reason != "WaitingForBootstrap" {
		t.Fatal("created before bootstrap")
	}
}
func TestTemplateConflict(t *testing.T) {
	f := setup(t)
	f.ok()
	f.template = &gomi.Template{Description: "foreign", UserData: "secret"}
	if f.step() == nil || f.creates != 0 || f.templateCreates != 0 {
		t.Fatal("overwrote foreign template")
	}
}

func TestProvisioningTemplateOwnershipAndIdempotency(t *testing.T) {
	f := setup(t)
	api, err := gomi.New(f.serverURL(), "test-token")
	if err != nil {
		t.Fatal(err)
	}
	data := "#cloud-config\npackages: [kubelet]\n"
	if err := ensureProvisioningTemplate(context.Background(), api, "capi-machine-uid", data); err != nil {
		t.Fatal(err)
	}
	if f.templateCreates != 1 || f.template == nil || f.template.DeliveryMode != "" || f.template.UserData != data {
		t.Fatalf("ordinary provisioning template not created: %+v", f.template)
	}
	if err := ensureProvisioningTemplate(context.Background(), api, "capi-machine-uid", data); err != nil || f.templateCreates != 1 {
		t.Fatalf("idempotent ensure failed: creates=%d err=%v", f.templateCreates, err)
	}
	f.template.UserData = "changed"
	if err := ensureProvisioningTemplate(context.Background(), api, "capi-machine-uid", data); err == nil {
		t.Fatal("changed provisioning template was adopted")
	}
}
func TestVMRunningIsNotBootstrapCompletion(t *testing.T) {
	f := setup(t)
	f.ok()
	f.ok()
	f.vm.Phase = "Running"
	f.ok()
	if f.get().Status.Initialization.Provisioned != nil {
		t.Fatal("reported ready before provisioning completion")
	}
}
func TestUnsupportedBootstrapHasNoWrites(t *testing.T) {
	f := setup(t)
	f.ok()
	var s corev1.Secret
	_ = f.r.Get(context.Background(), types.NamespacedName{Namespace: "test", Name: "bootstrap"}, &s)
	s.Data["format"] = []byte("ignition")
	f.update(&s)
	f.ok()
	if f.creates != 0 || f.templateCreates != 0 {
		t.Fatal("wrote unsupported bootstrap")
	}
}
func TestProvisionedLatch(t *testing.T) {
	f := setup(t)
	f.ok()
	f.ok()
	now := time.Now()
	f.vm.Phase = "Running"
	f.vm.Provisioning.CompletedAt = &now
	f.ok()
	f.vm.Phase = "Stopped"
	f.ok()
	m := f.get()
	if m.Status.Initialization.Provisioned == nil || !*m.Status.Initialization.Provisioned || m.Status.Conditions[0].Status != "False" {
		t.Fatal("initialization latch lost during outage")
	}
}

func TestMoveRestoresProvisionedWhileUnavailable(t *testing.T) {
	for _, state := range []string{"Stopped", "Migrating", "Error", "Missing", "api-outage", "deleted"} {
		t.Run(state, func(t *testing.T) {
			f := setup(t)
			f.ok()
			f.ok()
			now := time.Now()
			f.vm.Phase = "Running"
			f.vm.Provisioning.CompletedAt = &now
			f.ok()
			m := f.get()
			m.UID = "moved"
			f.update(m)
			m = f.get()
			m.Status = infrav1.GomiMachineStatus{}
			if err := f.r.Status().Update(context.Background(), m); err != nil {
				t.Fatal(err)
			}
			f.vm.Phase = state
			if state == "api-outage" {
				f.failGet = true
			}
			if state == "deleted" {
				f.vm = nil
			}
			err := f.step()
			if (err != nil) != (state == "api-outage") {
				t.Fatalf("unexpected error: %v", err)
			}
			m = f.get()
			if m.Status.Initialization.Provisioned == nil || !*m.Status.Initialization.Provisioned || m.Status.Conditions[0].Status != "False" {
				t.Fatalf("initialization/ready state incorrect: %+v", m.Status)
			}
			if f.creates != 1 {
				t.Fatal("recreated moved instance")
			}
		})
	}
}

func TestCompletedStoppedVMReconstructsProviderID(t *testing.T) {
	f := setup(t)
	f.ok()
	f.ok()
	now := time.Now()
	f.vm.Phase = "Stopped"
	f.vm.Provisioning.CompletedAt = &now
	f.ok()
	m := f.get()
	if m.Spec.ProviderID == nil || m.Status.Initialization.Provisioned == nil || !*m.Status.Initialization.Provisioned || m.Status.Conditions[0].Status != "False" {
		t.Fatalf("completion not reconstructed: %+v", m)
	}
}

func TestOldGomiNeverReceivesBootstrapSecret(t *testing.T) {
	f := setup(t)
	f.ok()
	f.unsupportedSeed = true
	if f.step() == nil {
		t.Fatal("accepted server without protected delivery")
	}
	if f.creates != 0 || f.templateCreates != 0 {
		t.Fatal("sent bootstrap to unsupported server")
	}
}

func TestBootstrapRequiresSeedOnlyTemplate(t *testing.T) {
	f := setup(t)
	f.ok()
	f.ok()
	if f.template.DeliveryMode != "vm-seed" {
		t.Fatal("bootstrap is publicly accessible")
	}
	f.vm = nil
	f.template.DeliveryMode = ""
	if f.step() == nil || f.creates != 1 {
		t.Fatal("reused publicly accessible bootstrap")
	}
}

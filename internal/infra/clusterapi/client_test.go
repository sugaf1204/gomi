package clusterapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/rest"
	clienttesting "k8s.io/client-go/testing"
)

func object(kind, name string, spec map[string]interface{}) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "cluster.x-k8s.io/v1beta2", "kind": kind,
		"metadata": map[string]interface{}{"name": name, "namespace": "lab", "uid": "uid-" + name, "resourceVersion": "7"}, "spec": spec,
	}}
}
func fixture() (*Service, *fake.FakeDynamicClient, *unstructured.Unstructured, *unstructured.Unstructured) {
	cluster := object("Cluster", "demo", map[string]interface{}{})
	md := object("MachineDeployment", "workers", map[string]interface{}{"clusterName": "demo", "replicas": int64(2)})
	client := fake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		gvr("clusters", "v1beta2"): "ClusterList", gvr("machinedeployments", "v1beta2"): "MachineDeploymentList", gvr("machines", "v1beta2"): "MachineList", gvr("kubeadmcontrolplanes", "v1beta2"): "KubeadmControlPlaneList",
	}, cluster, md)
	return &Service{client: client, namespace: "lab", context: "test"}, client, cluster, md
}
func TestSnapshotSanitizesAndGroups(t *testing.T) {
	svc, client, _, md := fixture()
	md.Object["status"] = map[string]interface{}{"phase": "ScalingUp", "readyReplicas": int64(1), "v1beta2": map[string]interface{}{"conditions": []interface{}{map[string]interface{}{"type": "Available", "status": "False", "reason": "ScalingUp", "message": "waiting for nodes"}}}}
	md.Object["spec"].(map[string]interface{})["secret"] = "must-not-leak"
	_, err := client.Resource(gvr("machinedeployments", "v1beta2")).Namespace("lab").Update(context.Background(), md, metav1.UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Resources) != 2 || result.Context != "test" {
		t.Fatalf("%+v", result)
	}
	r := result.Resources[1]
	if r.ClusterName != "demo" || *r.ReadyReplicas != 1 || len(r.Conditions) != 1 || r.Conditions[0].Reason != "ScalingUp" {
		t.Fatalf("%+v", r)
	}
	data, _ := json.Marshal(result)
	if strings.Contains(string(data), "must-not-leak") {
		t.Fatal("raw spec exposed")
	}
}
func TestScaleAndPausePatches(t *testing.T) {
	svc, client, _, _ := fixture()
	for _, tc := range []struct {
		resource, name string
		change         Change
		path           string
		value          interface{}
	}{
		{"machinedeployments", "workers", Change{APIVersion: "cluster.x-k8s.io/v1beta2", UID: "uid-workers", ResourceVersion: "7", Replicas: ptr(int64(0))}, "/spec/replicas", float64(0)},
		{"clusters", "demo", Change{APIVersion: "cluster.x-k8s.io/v1beta2", UID: "uid-demo", ResourceVersion: "7", Paused: ptr(true)}, "/spec/paused", true},
	} {
		client.PrependReactor("patch", tc.resource, func(action clienttesting.Action) (bool, runtime.Object, error) {
			var ops []patchOp
			if err := json.Unmarshal(action.(clienttesting.PatchAction).GetPatch(), &ops); err != nil {
				t.Fatal(err)
			}
			if len(ops) != 3 || ops[0].Path != "/metadata/uid" || ops[1].Path != "/metadata/resourceVersion" || ops[2].Path != tc.path || ops[2].Value != tc.value {
				t.Fatalf("unexpected patch: %+v", ops)
			}
			return false, nil, nil
		})
		if _, err := svc.Change(context.Background(), "lab", tc.resource, tc.name, tc.change); err != nil {
			t.Fatal(err)
		}
	}
}
func ptr[T any](value T) *T { return &value }
func TestScaleRejectsUnsafeOrStaleChanges(t *testing.T) {
	for _, tc := range []string{"uid", "version", "namespace", "negative", "topology", "autoscaler", "paused-cluster", "paused-worker", "deleting", "both", "unsupported", "cluster-replicas"} {
		t.Run(tc, func(t *testing.T) {
			svc, client, cluster, md := fixture()
			namespace, resource, name := "lab", "machinedeployments", "workers"
			change := Change{APIVersion: "cluster.x-k8s.io/v1beta2", UID: "uid-workers", ResourceVersion: "7", Replicas: ptr(int64(3))}
			switch tc {
			case "uid":
				change.UID = "old"
			case "version":
				change.ResourceVersion = "6"
			case "namespace":
				namespace = "other"
			case "negative":
				change.Replicas = ptr(int64(-1))
			case "topology":
				cluster.Object["spec"].(map[string]interface{})["topology"] = map[string]interface{}{"class": "demo"}
			case "autoscaler":
				md.SetAnnotations(map[string]string{"cluster.x-k8s.io/cluster-api-autoscaler-node-group-min-size": "1"})
			case "paused-cluster":
				cluster.SetAnnotations(map[string]string{pausedAnnotation: ""})
			case "paused-worker":
				md.Object["spec"].(map[string]interface{})["paused"] = true
			case "deleting":
				now := metav1.Now()
				md.SetDeletionTimestamp(&now)
			case "both":
				change.Paused = ptr(true)
			case "unsupported":
				resource = "secrets"
			case "cluster-replicas":
				resource = "clusters"
				name = "demo"
				change.UID = "uid-demo"
			}
			for _, u := range []*unstructured.Unstructured{cluster, md} {
				r := "clusters"
				if u.GetKind() == "MachineDeployment" {
					r = "machinedeployments"
				}
				if _, err := client.Resource(gvr(r, "v1beta2")).Namespace("lab").Update(context.Background(), u, metav1.UpdateOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			_, err := svc.Change(context.Background(), namespace, resource, name, change)
			if !errors.Is(err, ErrInvalid) && !errors.Is(err, ErrConflict) {
				t.Fatalf("expected rejection: %v", err)
			}
			for _, action := range client.Actions() {
				if action.GetVerb() == "patch" {
					t.Fatal("unsafe operation reached patch")
				}
			}
		})
	}
}
func TestResumeRemovesLegacyPause(t *testing.T) {
	svc, client, cluster, _ := fixture()
	cluster.SetAnnotations(map[string]string{pausedAnnotation: "", "other": "keep"})
	if _, err := client.Resource(gvr("clusters", "v1beta2")).Namespace("lab").Update(context.Background(), cluster, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	result, err := svc.Change(context.Background(), "lab", "clusters", "demo", Change{APIVersion: "cluster.x-k8s.io/v1beta2", UID: "uid-demo", ResourceVersion: "7", Paused: ptr(false)})
	if err != nil || result.Paused {
		t.Fatalf("%+v %v", result, err)
	}
	updated, _ := client.Resource(gvr("clusters", "v1beta2")).Namespace("lab").Get(context.Background(), "demo", metav1.GetOptions{})
	if updated.GetAnnotations()["other"] != "keep" {
		t.Fatal("unrelated annotation lost")
	}
}
func TestListVersionFallbackAndPagination(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.String())
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "v1beta2") {
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"kind":"Status","apiVersion":"v1","reason":"NotFound","code":404}`))
			return
		}
		token, name := "next", "one"
		if r.URL.Query().Get("continue") == "next" {
			token = ""
			name = "two"
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"apiVersion": "cluster.x-k8s.io/v1beta1", "kind": "ClusterList", "metadata": map[string]interface{}{"continue": token}, "items": []interface{}{object("Cluster", name, map[string]interface{}{}).Object}})
	}))
	defer server.Close()
	client, err := dynamic.NewForConfig(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{client: client, namespace: "lab"}
	result, err := svc.list(context.Background(), "clusters")
	if err != nil || len(result) != 2 || len(requests) != 3 {
		t.Fatalf("%+v %v %v", result, err, requests)
	}
}
func TestDisabledDoesNotLoadAmbientKubeconfig(t *testing.T) {
	t.Setenv("KUBECONFIG", "/does/not/exist")
	svc, err := New("", "", "")
	if svc != nil || err != nil {
		t.Fatalf("%+v %v", svc, err)
	}
}

func TestConcurrentPatchFailureBecomesConflict(t *testing.T) {
	svc, client, _, md := fixture()
	client.PrependReactor("patch", "machinedeployments", func(clienttesting.Action) (bool, runtime.Object, error) {
		md.SetResourceVersion("8")
		if err := client.Tracker().Update(gvr("machinedeployments", "v1beta2"), md, "lab"); err != nil {
			t.Fatal(err)
		}
		return true, nil, apierrors.NewInvalid(schema.GroupKind{Group: "cluster.x-k8s.io", Kind: "MachineDeployment"}, "workers", nil)
	})
	_, err := svc.Change(context.Background(), "lab", "machinedeployments", "workers", Change{APIVersion: "cluster.x-k8s.io/v1beta2", UID: "uid-workers", ResourceVersion: "7", Replicas: ptr(int64(3))})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected conflict after concurrent update: %v", err)
	}
}

func TestBeta1ScaleAndConditionProjection(t *testing.T) {
	cluster := object("Cluster", "demo", map[string]interface{}{})
	md := object("MachineDeployment", "workers", map[string]interface{}{"clusterName": "demo", "replicas": int64(2)})
	cluster.SetAPIVersion("cluster.x-k8s.io/v1beta1")
	md.SetAPIVersion("cluster.x-k8s.io/v1beta1")
	md.Object["status"] = map[string]interface{}{
		"readyReplicas": int64(2),
		"conditions":    []interface{}{map[string]interface{}{"type": "Ready", "status": "True"}},
		"v1beta2": map[string]interface{}{
			"readyReplicas": int64(1),
			"conditions":    []interface{}{map[string]interface{}{"type": "Available", "status": "False", "observedGeneration": int64(1)}},
		},
	}
	client := fake.NewSimpleDynamicClient(runtime.NewScheme(), cluster, md)
	svc := &Service{client: client, namespace: "lab"}
	result, err := svc.Change(context.Background(), "lab", "machinedeployments", "workers", Change{APIVersion: "cluster.x-k8s.io/v1beta1", UID: "uid-workers", ResourceVersion: "7", Replicas: ptr(int64(3))})
	if err != nil {
		t.Fatal(err)
	}
	if result.Replicas == nil || *result.Replicas != 3 || result.ReadyReplicas == nil || *result.ReadyReplicas != 1 || len(result.Conditions) != 1 || result.Conditions[0].Type != "Available" {
		t.Fatalf("projection: %+v", result)
	}
}

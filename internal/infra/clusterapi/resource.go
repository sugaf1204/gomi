package clusterapi

import (
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type Snapshot struct {
	Configured bool       `json:"configured"`
	Context    string     `json:"context,omitempty"`
	Namespace  string     `json:"namespace,omitempty"`
	ObservedAt time.Time  `json:"observedAt"`
	Resources  []Resource `json:"resources"`
}

type Condition struct {
	ObservedGeneration *int64 `json:"observedGeneration,omitempty"`
	Type               string `json:"type"`
	Status             string `json:"status"`
	Reason             string `json:"reason,omitempty"`
	Message            string `json:"message,omitempty"`
}

type Resource struct {
	Generation         int64       `json:"generation"`
	Resource           string      `json:"resource"`
	APIVersion         string      `json:"apiVersion"`
	Namespace          string      `json:"namespace"`
	Name               string      `json:"name"`
	UID                string      `json:"uid"`
	ResourceVersion    string      `json:"resourceVersion"`
	ClusterName        string      `json:"clusterName,omitempty"`
	Phase              string      `json:"phase,omitempty"`
	Version            string      `json:"version,omitempty"`
	Replicas           *int64      `json:"replicas,omitempty"`
	ReadyReplicas      *int64      `json:"readyReplicas,omitempty"`
	Paused             bool        `json:"paused"`
	Deleting           bool        `json:"deleting"`
	Topology           bool        `json:"topology"`
	Autoscaled         bool        `json:"autoscaled"`
	NodeName           string      `json:"nodeName,omitempty"`
	ProviderID         string      `json:"providerID,omitempty"`
	InfrastructureName string      `json:"infrastructureName,omitempty"`
	Conditions         []Condition `json:"conditions"`
}

const clusterLabel = "cluster.x-k8s.io/cluster-name"
const pausedAnnotation = "cluster.x-k8s.io/paused"

func textAt(u *unstructured.Unstructured, fields ...string) string {
	value, _, _ := unstructured.NestedString(u.Object, fields...)
	return value
}
func boolAt(u *unstructured.Unstructured, fields ...string) bool {
	value, _, _ := unstructured.NestedBool(u.Object, fields...)
	return value
}
func numberAt(u *unstructured.Unstructured, fields ...string) *int64 {
	value, ok, _ := unstructured.NestedInt64(u.Object, fields...)
	if !ok {
		return nil
	}
	return &value
}
func hasTopology(u *unstructured.Unstructured) bool {
	_, ok, _ := unstructured.NestedMap(u.Object, "spec", "topology")
	return ok
}
func autoscaled(u *unstructured.Unstructured) bool {
	for _, key := range []string{"cluster.x-k8s.io/cluster-api-autoscaler-node-group-min-size", "cluster.x-k8s.io/cluster-api-autoscaler-node-group-max-size"} {
		if _, ok := u.GetAnnotations()[key]; ok {
			return true
		}
	}
	return false
}
func paused(u *unstructured.Unstructured) bool {
	_, annotated := u.GetAnnotations()[pausedAnnotation]
	return annotated || boolAt(u, "spec", "paused")
}
func summarize(resource string, u *unstructured.Unstructured) Resource {
	r := Resource{
		Generation:         u.GetGeneration(),
		Resource:           resource,
		APIVersion:         u.GetAPIVersion(),
		Namespace:          u.GetNamespace(),
		Name:               u.GetName(),
		UID:                string(u.GetUID()),
		ResourceVersion:    u.GetResourceVersion(),
		ClusterName:        textAt(u, "spec", "clusterName"),
		Phase:              textAt(u, "status", "phase"),
		Version:            textAt(u, "spec", "version"),
		Replicas:           numberAt(u, "spec", "replicas"),
		ReadyReplicas:      numberAt(u, "status", "readyReplicas"),
		Paused:             paused(u),
		Deleting:           u.GetDeletionTimestamp() != nil,
		Topology:           hasTopology(u),
		Autoscaled:         autoscaled(u),
		NodeName:           textAt(u, "status", "nodeRef", "name"),
		ProviderID:         textAt(u, "spec", "providerID"),
		InfrastructureName: textAt(u, "spec", "infrastructureRef", "name"),
		Conditions:         []Condition{},
	}
	if r.ClusterName == "" {
		r.ClusterName = u.GetLabels()[clusterLabel]
	}
	if resource == "clusters" {
		r.ClusterName = r.Name
	}
	if resource == "machinedeployments" {
		r.Version = textAt(u, "spec", "template", "spec", "version")
	}
	if modern := numberAt(u, "status", "v1beta2", "readyReplicas"); modern != nil {
		r.ReadyReplicas = modern
	}
	conditions, ok, _ := unstructured.NestedSlice(u.Object, "status", "v1beta2", "conditions")
	if !ok {
		conditions, _, _ = unstructured.NestedSlice(u.Object, "status", "conditions")
	}
	for _, item := range conditions {
		if value, ok := item.(map[string]interface{}); ok {
			c := &unstructured.Unstructured{Object: value}
			r.Conditions = append(r.Conditions, Condition{
				ObservedGeneration: numberAt(c, "observedGeneration"),
				Type:               textAt(c, "type"), Status: textAt(c, "status"),
				Reason: textAt(c, "reason"), Message: textAt(c, "message"),
			})
		}
	}
	return r
}

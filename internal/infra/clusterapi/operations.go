package clusterapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

var ErrConflict = errors.New("resource changed; refresh before retrying")
var ErrInvalid = errors.New("invalid Cluster API operation")
var validName = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$`)

type Change struct {
	APIVersion      string `json:"apiVersion"`
	UID             string `json:"uid"`
	ResourceVersion string `json:"resourceVersion"`
	Replicas        *int64 `json:"replicas,omitempty"`
	Paused          *bool  `json:"paused,omitempty"`
}

type patchOp struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

func (s *Service) Change(ctx context.Context, namespace, resource, name string, change Change) (Resource, error) {
	if !validName.MatchString(namespace) || !validName.MatchString(name) || len(namespace) > 63 || len(name) > 253 || (s.namespace != "" && s.namespace != namespace) {
		return Resource{}, fmt.Errorf("%w: invalid namespace or name", ErrInvalid)
	}
	if resource != "clusters" && resource != "machinedeployments" {
		return Resource{}, fmt.Errorf("%w: unsupported resource", ErrInvalid)
	}
	version, err := schema.ParseGroupVersion(change.APIVersion)
	if err != nil || version.Group != "cluster.x-k8s.io" || (version.Version != "v1beta2" && version.Version != "v1beta1") || change.UID == "" || change.ResourceVersion == "" {
		return Resource{}, fmt.Errorf("%w: API version, UID and resourceVersion are required", ErrInvalid)
	}
	if (change.Paused == nil) == (change.Replicas == nil) {
		return Resource{}, fmt.Errorf("%w: supply exactly one of paused or replicas", ErrInvalid)
	}
	if change.Paused != nil && resource != "clusters" {
		return Resource{}, fmt.Errorf("%w: pause and resume are supported on Clusters", ErrInvalid)
	}
	if change.Replicas != nil && (resource != "machinedeployments" || *change.Replicas < 0 || *change.Replicas > 2147483647) {
		return Resource{}, fmt.Errorf("%w: replicas must be a non-negative int32 on a MachineDeployment", ErrInvalid)
	}
	endpoint := s.client.Resource(gvr(resource, version.Version)).Namespace(namespace)
	current, err := endpoint.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return Resource{}, err
	}
	if string(current.GetUID()) != change.UID || current.GetResourceVersion() != change.ResourceVersion {
		return Resource{}, ErrConflict
	}
	if current.GetDeletionTimestamp() != nil {
		return Resource{}, fmt.Errorf("%w: resource is deleting", ErrInvalid)
	}
	if change.Replicas != nil {
		if err := s.allowScale(ctx, current, version.Version); err != nil {
			return Resource{}, err
		}
	}
	ops := []patchOp{{Op: "test", Path: "/metadata/uid", Value: change.UID}, {Op: "test", Path: "/metadata/resourceVersion", Value: change.ResourceVersion}}
	if change.Replicas != nil {
		ops = append(ops, patchOp{Op: "add", Path: "/spec/replicas", Value: *change.Replicas})
	}
	if change.Paused != nil {
		// spec.paused is the CAPI contract. Remove a legacy pause annotation on
		// resume as well, otherwise the UI would report a successful no-op.
		ops = append(ops, patchOp{Op: "add", Path: "/spec/paused", Value: *change.Paused})
		if !*change.Paused {
			if _, ok := current.GetAnnotations()[pausedAnnotation]; ok {
				ops = append(ops, patchOp{Op: "remove", Path: "/metadata/annotations/cluster.x-k8s.io~1paused"})
			}
		}
	}
	payload, err := json.Marshal(ops)
	if err != nil {
		return Resource{}, err
	}
	updated, err := endpoint.Patch(ctx, name, types.JSONPatchType, payload, metav1.PatchOptions{})
	if err != nil {
		// A JSON Patch test can return Invalid (422) if a controller changed
		// the object between GET and PATCH. Read back identity/version so this
		// race has the same conflict contract as a stale UI observation.
		if apierrors.IsInvalid(err) {
			latest, readErr := endpoint.Get(ctx, name, metav1.GetOptions{})
			if readErr == nil && (string(latest.GetUID()) != change.UID || latest.GetResourceVersion() != change.ResourceVersion) {
				return Resource{}, ErrConflict
			}
		}
		return Resource{}, err
	}
	return summarize(resource, updated), nil
}

func (s *Service) allowScale(ctx context.Context, md *unstructured.Unstructured, version string) error {
	if paused(md) || autoscaled(md) {
		return fmt.Errorf("%w: worker group is paused or managed by an autoscaler", ErrInvalid)
	}
	clusterName := textAt(md, "spec", "clusterName")
	if clusterName == "" {
		return fmt.Errorf("%w: missing clusterName", ErrInvalid)
	}
	cluster, err := s.client.Resource(gvr("clusters", version)).Namespace(md.GetNamespace()).Get(ctx, clusterName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if paused(cluster) || cluster.GetDeletionTimestamp() != nil {
		return fmt.Errorf("%w: cluster is paused or deleting", ErrInvalid)
	}
	if hasTopology(cluster) {
		return fmt.Errorf("%w: scale this worker group through Cluster topology", ErrInvalid)
	}
	return nil
}

// Package clusterapi exposes a deliberately bounded management-cluster interface.
// Kubernetes credentials and arbitrary resource bodies never cross the GOMI API.
package clusterapi

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
)

type Service struct {
	client    dynamic.Interface
	namespace string
	context   string
}

// New requires an explicit file; it never falls back to the host's current
// kubeconfig or in-cluster credentials. The kubeconfig is trusted server input.
func New(path, contextName, namespace string) (*Service, error) {
	if path == "" {
		return nil, nil
	}
	rules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: path}
	overrides := &clientcmd.ConfigOverrides{CurrentContext: contextName}
	loader := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides)
	raw, err := loader.RawConfig()
	if err != nil {
		return nil, fmt.Errorf("load Cluster API kubeconfig: %w", err)
	}
	if contextName == "" {
		contextName = raw.CurrentContext
	}
	cfg, err := loader.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("configure Cluster API client: %w", err)
	}
	cfg.Timeout = 15 * time.Second
	cfg.UserAgent = "gomi-cluster-api-ui"
	client, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &Service{client: client, namespace: namespace, context: contextName}, nil
}

func gvr(resource, version string) schema.GroupVersionResource {
	group := "cluster.x-k8s.io"
	if resource == "kubeadmcontrolplanes" {
		group = "controlplane.cluster.x-k8s.io"
	}
	return schema.GroupVersionResource{Group: group, Version: version, Resource: resource}
}

func (s *Service) list(ctx context.Context, resource string) ([]unstructured.Unstructured, error) {
	for _, version := range []string{"v1beta2", "v1beta1"} {
		var result []unstructured.Unstructured
		token := ""
		for {
			page, err := s.client.Resource(gvr(resource, version)).Namespace(s.namespace).List(ctx, metav1.ListOptions{Limit: 500, Continue: token})
			if apierrors.IsNotFound(err) {
				break
			}
			if err != nil {
				return nil, fmt.Errorf("list %s: %w", resource, err)
			}
			// Some dynamic test clients omit list item GVK; real apiservers provide it.
			for i := range page.Items {
				if page.Items[i].GetAPIVersion() == "" {
					page.Items[i].SetAPIVersion(gvr(resource, version).GroupVersion().String())
				}
			}
			result = append(result, page.Items...)
			token = page.GetContinue()
			if token == "" {
				return result, nil
			}
		}
	}
	return nil, apierrors.NewNotFound(gvr(resource, "v1beta2").GroupResource(), "")
}

func (s *Service) Snapshot(ctx context.Context) (Snapshot, error) {
	result := Snapshot{Configured: true, Context: s.context, Namespace: s.namespace, Resources: []Resource{}, ObservedAt: time.Now().UTC()}
	for _, kind := range []string{"clusters", "machinedeployments", "machines", "kubeadmcontrolplanes"} {
		items, err := s.list(ctx, kind)
		if err != nil {
			if kind == "kubeadmcontrolplanes" && apierrors.IsNotFound(err) {
				continue
			}
			return Snapshot{}, err
		}
		for i := range items {
			result.Resources = append(result.Resources, summarize(kind, &items[i]))
		}
	}
	return result, nil
}

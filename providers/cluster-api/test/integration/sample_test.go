package integration

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func validateSample(t *testing.T, kube client.Client) {
	ctx := context.Background()
	must(t, kube.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "sample"}}))
	data, err := os.ReadFile("../../samples/cluster-template.yaml")
	must(t, err)
	rendered := strings.NewReplacer(
		"${CLUSTER_NAME}", "sample-cluster", "${NAMESPACE}", "sample", "${GOMI_TOKEN}", "dummy",
		"${KUBERNETES_VERSION}", "v1.36.2", "${GOMI_ENDPOINT}", "https://gomi.example.test",
		"${GOMI_HYPERVISOR}", "hv", "${GOMI_OS_IMAGE}", "prepared", "${GOMI_BRIDGE}", "br0",
		"${GOMI_SUBNET}", "subnet", "${CONTROL_PLANE_ENDPOINT}", "192.0.2.10",
		"${CONTROL_PLANE_MACHINE_COUNT:=1}", "1", "${WORKER_MACHINE_COUNT:=1}", "1",
	).Replace(string(data))
	if strings.Contains(rendered, "${") {
		t.Fatal("unexpanded sample variable")
	}
	decoder := yamlutil.NewYAMLOrJSONDecoder(strings.NewReader(rendered), 4096)
	for {
		object := &unstructured.Unstructured{}
		err := decoder.Decode(object)
		if err == io.EOF {
			break
		}
		must(t, err)
		if object.GetKind() == "" {
			continue
		}
		must(t, kube.Create(ctx, object, &client.CreateOptions{FieldValidation: "Strict"}))
	}
}

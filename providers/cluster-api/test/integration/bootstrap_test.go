package integration

import (
	"context"
	"strings"
	"testing"

	"github.com/sugaf1204/gomi/internal/cloudinit"
	"github.com/sugaf1204/gomi/internal/infra/memory"
	"github.com/sugaf1204/gomi/internal/infra/pxehttp"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/osimage"
	"github.com/sugaf1204/gomi/internal/vm"
	capicloudinit "sigs.k8s.io/cluster-api/bootstrap/kubeadm/pkg/cloudinit"
	"sigs.k8s.io/yaml"
)

func TestKubeadmBootstrapThroughGomi(t *testing.T) {
	// Use the actual CABPK generator rather than a mock cloud-config document.
	config := "apiVersion: kubeadm.k8s.io/v1beta4\nkind: JoinConfiguration\nnodeRegistration:\n  name: '{{ v1.local_hostname }}'\n  kubeletExtraArgs:\n  - name: provider-id\n    value: 'gomi:///{{ v1.local_hostname }}'\n"
	for _, mode := range []string{"worker", "control-plane-init", "control-plane-join"} {
		t.Run(mode, func(t *testing.T) {
			var data []byte
			var err error
			switch mode {
			case "worker":
				data, err = capicloudinit.NewNode(&capicloudinit.NodeInput{JoinConfiguration: config})
			case "control-plane-init":
				data, err = capicloudinit.NewInitControlPlane(&capicloudinit.ControlPlaneInput{InitConfiguration: strings.Replace(config, "JoinConfiguration", "InitConfiguration", 1), ClusterConfiguration: "apiVersion: kubeadm.k8s.io/v1beta4\nkind: ClusterConfiguration\n"})
			case "control-plane-join":
				data, err = capicloudinit.NewJoinControlPlane(&capicloudinit.ControlPlaneJoinInput{JoinConfiguration: config})
			}
			must(t, err)
			for _, family := range []string{"ubuntu", "fedora"} {
				t.Run(family, func(t *testing.T) {
					ctx := context.Background()
					b := memory.New()
					must(t, b.CloudInits().Upsert(ctx, cloudinit.CloudInitTemplate{Name: "bootstrap", UserData: string(data), DeliveryMode: cloudinit.DeliveryVMSeed}))
					must(t, b.OSImages().Upsert(ctx, osimage.OSImage{Name: "image", OSFamily: family, Format: osimage.FormatQCOW2, Variant: osimage.VariantCloud}))
					must(t, b.VMs().Upsert(ctx, vm.VirtualMachine{Name: "capi-test", OSImageRef: "image", CloudInitRefs: []string{"bootstrap"}, InstallCfg: &vm.InstallConfig{Type: vm.InstallConfigCurtin}, Network: []vm.NetworkInterface{{Name: "eth0", MAC: "52:54:00:44:55:66"}}, Provisioning: vm.ProvisioningStatus{Active: true, CompletionToken: "test-completion"}}))
					handler := pxehttp.NewHandler(pxehttp.Config{Machines: machine.NewService(b.Machines()), VMs: vm.NewService(b.VMs()), OSImages: osimage.NewService(b.OSImages()), CloudInits: cloudinit.NewService(b.CloudInits())})
					stored, err := b.VMs().Get(ctx, "capi-test")
					must(t, err)
					files, err := handler.RenderVMSeed(ctx, stored, "http://gomi.test/pxe")
					must(t, err)
					body := files["user-data"]
					if !strings.HasPrefix(body, "## template: jinja\n#cloud-config") {
						t.Fatal("lost Jinja header")
					}
					var rendered map[string]any
					must(t, yaml.Unmarshal([]byte(body), &rendered))
					if rendered["hostname"] != "capi-test" {
						t.Fatalf("incorrect hostname: %v", rendered["hostname"])
					}
					for _, part := range []string{"gomi:///{{ v1.local_hostname }}", "kubeadm ", "bootstrap-success.complete", "install-complete"} {
						if !strings.Contains(body, part) {
							t.Errorf("lost %s", part)
						}
					}
					var original map[string]any
					must(t, yaml.Unmarshal(data, &original))
					before := original["runcmd"].([]any)
					after := rendered["runcmd"].([]any)
					// GOMI correctly prepends OS-family network setup. CABPK's own
					// commands must survive intact and retain their relative order.
					next := 0
					for _, command := range after {
						if next < len(before) && command == before[next] {
							next++
						}
					}
					if next != len(before) {
						t.Fatal("kubeadm commands changed or reordered")
					}

				})
			}
		})
	}
}

package api_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sugaf1204/gomi/internal/auth"
	"github.com/sugaf1204/gomi/internal/cloudinit"
	infraapi "github.com/sugaf1204/gomi/internal/infra/api"
	"github.com/sugaf1204/gomi/internal/infra/memory"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/osimage"
	"github.com/sugaf1204/gomi/internal/vm"
)

func TestProtectedSeedNeverServedPublicly(t *testing.T) {
	for _, family := range []string{"ubuntu", "fedora", "debian"} {
		t.Run(family, func(t *testing.T) {
			ctx := context.Background()
			b := memory.New()
			const secret = "TEST_CLUSTER_CA_PRIVATE_KEY"
			const token = "private-completion-token"
			check := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			check(b.CloudInits().Upsert(ctx, cloudinit.CloudInitTemplate{Name: "bootstrap", DeliveryMode: cloudinit.DeliveryVMSeed, UserData: "#cloud-config\nwrite_files:\n- path: /etc/kubernetes/pki/ca.key\n  content: " + secret + "\n"}))
			check(b.OSImages().Upsert(ctx, osimage.OSImage{Name: "image", OSFamily: family, Format: osimage.FormatQCOW2, Variant: osimage.VariantCloud}))
			v := vm.VirtualMachine{Name: "private", OSImageRef: "image", CloudInitRefs: []string{"bootstrap"}, InstallCfg: &vm.InstallConfig{Type: vm.InstallConfigCurtin}, Network: []vm.NetworkInterface{{Name: "eth0", MAC: "52:54:00:44:55:66"}}, Phase: vm.PhaseProvisioning, Provisioning: vm.ProvisioningStatus{Active: true, CompletionToken: token}}
			check(b.VMs().Upsert(ctx, v))
			d := &vm.Deployer{}
			s := infraapi.NewServer(infraapi.ServerConfig{Machines: machine.NewService(b.Machines()), VMs: vm.NewService(b.VMs()), CloudInits: cloudinit.NewService(b.CloudInits()), OSImages: osimage.NewService(b.OSImages()), VMDeployer: d})
			if d.RenderNoCloudSeed == nil {
				t.Fatal("internal seed renderer not wired")
			}
			// The internal renderer must preserve the established public
			// NoCloud rendering for ordinary VMs across OS families.
			publicTemplate := cloudinit.CloudInitTemplate{Name: "ordinary", UserData: "#cloud-config\npackages: [curl]\n"}
			check(b.CloudInits().Upsert(ctx, publicTemplate))
			v.CloudInitRefs = []string{"ordinary"}
			check(b.VMs().Upsert(ctx, v))
			ordinary, err := d.RenderNoCloudSeed(ctx, v, "http://gomi.test/pxe")
			check(err)
			for name, content := range ordinary {
				rec := doRequest(s.Echo(), http.MethodGet, "http://gomi.test/pxe/nocloud/525400445566/"+name, nil, "")
				requireStatus(t, rec, http.StatusOK)
				if rec.Body.String() != content {
					t.Fatalf("ordinary %s changed under internal delivery", name)
				}
			}
			v.CloudInitRefs = []string{"bootstrap"}
			check(b.VMs().Upsert(ctx, v))
			files, err := d.RenderNoCloudSeed(ctx, v, "http://gomi.test/pxe")
			check(err)
			if !strings.Contains(files["user-data"], secret) || !strings.Contains(files["user-data"], token) || !strings.Contains(files["meta-data"], "private") || files["network-config"] == "" {
				t.Fatal("internal seed lost data")
			}
			for _, completed := range []bool{false, true} {
				if completed {
					now := time.Now()
					v.Phase = vm.PhaseRunning
					v.Provisioning.Active = false
					v.Provisioning.CompletedAt = &now
					check(b.VMs().Upsert(ctx, v))
				}
				for _, path := range []string{"/pxe/nocloud/525400445566/user-data", "/pxe/nocloud/525400445566/meta-data", "/pxe/nocloud/525400445566/vendor-data", "/pxe/nocloud/525400445566/network-config", "/pxe/boot.ipxe?mac=52:54:00:44:55:66", "/pxe/preseed.cfg?mac=52:54:00:44:55:66", "/pxe/curtin-config?mac=52:54:00:44:55:66&token=" + token} {
					rec := doRequest(s.Echo(), http.MethodGet, path, nil, "")
					requireStatus(t, rec, http.StatusForbidden)
					if strings.Contains(rec.Body.String(), secret) || strings.Contains(rec.Body.String(), token) {
						t.Fatal("public response disclosed protected data")
					}
				}
			}
			if _, err := d.RenderNoCloudSeed(ctx, v, "http://gomi.test/pxe"); err == nil {
				t.Fatal("rendered protected data after completion")
			}
		})
	}
}

func TestProtectedTemplateAPI(t *testing.T) {
	env := setupTestEnv(t)
	const secret = "PRIVATE_BOOTSTRAP_MARKER"
	body := map[string]any{"name": "private", "deliveryMode": "vm-seed", "userData": "#cloud-config\nsecret: " + secret, "networkConfig": secret, "metadataTemplate": secret}
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/cloud-init-templates", body, env.token)
	requireStatus(t, rec, http.StatusCreated)
	if !strings.Contains(rec.Body.String(), `"deliveryMode":"vm-seed"`) {
		t.Fatal("delivery mode missing from response")
	}
	createUser(t, env.authStore, "reader", "password", auth.RoleViewer)
	viewer := createSession(t, env.authStore, "reader")
	requireStatus(t, doRequest(env.echo, http.MethodGet, "/api/v1/cloud-init-templates/private", nil, ""), http.StatusUnauthorized)
	requireStatus(t, doRequest(env.echo, http.MethodGet, "/api/v1/cloud-init-templates/private", nil, viewer), http.StatusForbidden)
	rec = doRequest(env.echo, http.MethodGet, "/api/v1/cloud-init-templates", nil, viewer)
	requireStatus(t, rec, http.StatusOK)
	if strings.Contains(rec.Body.String(), secret) {
		t.Fatal("viewer list exposed bootstrap")
	}
	rec = doRequest(env.echo, http.MethodGet, "/api/v1/cloud-init-templates/private", nil, env.token)
	requireStatus(t, rec, http.StatusOK)
	if !strings.Contains(rec.Body.String(), secret) {
		t.Fatal("writer cannot read owned bootstrap")
	}
	// Older clients omit the new field on edits: protection must be sticky.
	delete(body, "deliveryMode")
	for _, method := range []string{http.MethodPut, http.MethodPost} {
		path := "/api/v1/cloud-init-templates/private"
		status := http.StatusOK
		if method == http.MethodPost {
			path = "/api/v1/cloud-init-templates"
			status = http.StatusCreated
		}
		rec = doRequest(env.echo, method, path, body, env.token)
		requireStatus(t, rec, status)
		if !strings.Contains(rec.Body.String(), `"deliveryMode":"vm-seed"`) {
			t.Fatal("template protection downgraded")
		}
	}
}

package api_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sugaf1204/gomi/internal/auth"
	"github.com/sugaf1204/gomi/internal/baremetal"
	"github.com/sugaf1204/gomi/internal/cloudinit"
	infraapi "github.com/sugaf1204/gomi/internal/infra/api"
	infrasql "github.com/sugaf1204/gomi/internal/infra/sql"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/osimage"
	"github.com/sugaf1204/gomi/internal/power"
)

func TestBareMetalDeployUsesOrdinaryCloudInitTemplate(t *testing.T) {
	ctx := context.Background()
	b, err := infrasql.New("sqlite", filepath.Join(t.TempDir(), "gomi.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if err := b.Migrate(); err != nil {
		t.Fatal(err)
	}
	publicKey, err := os.ReadFile("../../baremetal/testdata/host-public.pem")
	if err != nil {
		t.Fatal(err)
	}
	m := machine.Machine{
		Name: "node1", Hostname: "node1", MAC: "00:11:22:33:44:55", Arch: "amd64",
		Firmware: machine.FirmwareBIOS, TargetDisk: "/dev/vda",
		Power: power.PowerConfig{Type: power.PowerTypeWebhook, Webhook: &power.WebhookConfig{
			PowerOnURL: "http://127.0.0.1:1/on", PowerOffURL: "http://127.0.0.1:1/off",
		}},
	}
	if err := b.Machines().Upsert(ctx, m); err != nil {
		t.Fatal(err)
	}
	if _, err := b.BareMetal().Register(ctx, m.Name, "pool", string(publicKey), m.TargetDisk); err != nil {
		t.Fatal(err)
	}
	host, err := b.BareMetal().Acquire(ctx, "pool", "capi-owner")
	if err != nil {
		t.Fatal(err)
	}
	if err := b.OSImages().Upsert(ctx, osimage.OSImage{Name: "image", Arch: "amd64", OSFamily: "ubuntu", Format: osimage.FormatSquashFS, Variant: osimage.VariantBareMetal, Ready: true, Manifest: &osimage.Manifest{Root: osimage.RootArtifact{Format: osimage.FormatSquashFS, Path: "rootfs.squashfs"}}}); err != nil {
		t.Fatal(err)
	}
	template := cloudinit.CloudInitTemplate{Name: "capi-owner", UserData: "#cloud-config\npackages: [kubelet]\n"}
	if err := b.CloudInits().Upsert(ctx, template); err != nil {
		t.Fatal(err)
	}
	fingerprint, err := baremetal.EnrollmentFingerprint(host.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	encode := base64.StdEncoding.EncodeToString
	envelope, err := json.Marshal(map[string]any{
		"version": 1, "algorithm": "RSA-OAEP-SHA256+A256GCM", "host": host.Name,
		"owner": host.Owner, "keyFingerprint": fingerprint,
		"wrappedKey": encode(make([]byte, 256)), "nonce": encode(make([]byte, 12)), "ciphertext": encode(make([]byte, 17)),
	})
	if err != nil {
		t.Fatal(err)
	}
	createUser(t, b.Auth(), "operator", "password", auth.RoleOperator)
	token := createSession(t, b.Auth(), "operator")
	e := infraapi.NewServer(infraapi.ServerConfig{
		BareMetal: b.BareMetal(), Machines: machine.NewService(b.Machines()),
		CloudInits: cloudinit.NewService(b.CloudInits()), OSImages: osimage.NewService(b.OSImages()),
		AuthStore: b.Auth(), AuthService: infraapi.NewAuthService(b.Auth(), time.Hour),
	}).Echo()
	body := map[string]any{"osImageRef": "image", "cloudInitRef": template.Name, "envelope": json.RawMessage(envelope)}
	rec := doRequest(e, http.MethodPost, "/api/v1/bare-metal-claims/capi-owner/deploy", body, token)
	requireStatus(t, rec, http.StatusAccepted)
	saved, err := b.Machines().Get(ctx, m.Name)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.CloudInitRefs) != 1 || saved.CloudInitRefs[0] != template.Name || saved.LastDeployedCloudInitRef != template.Name {
		t.Fatalf("provisioning template was not attached: %+v", saved)
	}
	// Retries must match the template as well as the image and sealed bootstrap.
	requireStatus(t, doRequest(e, http.MethodPost, "/api/v1/bare-metal-claims/capi-owner/deploy", body, token), http.StatusAccepted)
}

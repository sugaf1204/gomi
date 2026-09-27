package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sugaf1204/gomi/internal/auth"
	"github.com/sugaf1204/gomi/internal/baremetal"
	infraapi "github.com/sugaf1204/gomi/internal/infra/api"
	infrasql "github.com/sugaf1204/gomi/internal/infra/sql"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/osimage"
	"github.com/sugaf1204/gomi/internal/power"
)

func TestBareMetalRetryRequiresFailedAttemptAndAdmin(t *testing.T) {
	for _, tc := range []struct {
		name    string
		active  bool
		role    auth.Role
		state   baremetal.State
		attempt string
		want    int
	}{
		{"delete-failed", false, auth.RoleOperator, baremetal.Failed, "old", http.StatusAccepted},
		{"delete-failed-before-restore", false, auth.RoleOperator, baremetal.Failed, "old", http.StatusConflict},
		{"failed-before-restore", false, auth.RoleAdmin, baremetal.Failed, "old", http.StatusConflict},
		{"delete-post-install", true, auth.RoleOperator, baremetal.Deploying, "old", http.StatusAccepted},
		{"delete-installer", true, auth.RoleOperator, baremetal.Deploying, "old", http.StatusAccepted},
		{"delete-cleanup-failed", false, auth.RoleOperator, baremetal.Failed, "old", http.StatusConflict},
		{"post-install", true, auth.RoleAdmin, baremetal.Deploying, "old", http.StatusAccepted},
		{"retry", false, auth.RoleAdmin, baremetal.Failed, "old", http.StatusAccepted},
		{"running", true, auth.RoleAdmin, baremetal.Failed, "old", http.StatusConflict},
		{"deploying", true, auth.RoleAdmin, baremetal.Deploying, "old", http.StatusConflict},
		{"stale", false, auth.RoleAdmin, baremetal.Failed, "stale", http.StatusConflict},
		{"operator", false, auth.RoleOperator, baremetal.Failed, "old", http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			b, err := infrasql.New("sqlite", filepath.Join(t.TempDir(), "gomi.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer b.Close()
			if err = b.Migrate(); err != nil {
				t.Fatal(err)
			}
			key, err := os.ReadFile("../../baremetal/testdata/host-public.pem")
			if err != nil {
				t.Fatal(err)
			}
			m := machine.Machine{Name: "node1", Hostname: "node1", MAC: "00:11:22:33:44:55", Arch: "amd64", Firmware: machine.FirmwareBIOS,
				TargetDisk: "/dev/vda", OSPreset: machine.OSPreset{Family: machine.OSTypeUbuntu, Version: "22.04", ImageRef: "image"}, Phase: machine.PhaseError,
				Power:           power.PowerConfig{Type: power.PowerTypeWebhook, Webhook: &power.WebhookConfig{PowerOnURL: "http://127.0.0.1:1/on", PowerOffURL: "http://127.0.0.1:1/off"}},
				Provision:       &machine.ProvisionProgress{Active: tc.active, AttemptID: "old"},
				SealedBootstrap: &machine.SealedBootstrap{Owner: "capi-owner", Envelope: json.RawMessage(`{"sealed":"unchanged"}`)}}
			if tc.state == baremetal.Deploying {
				m.Phase = machine.PhaseProvisioning
			}
			if strings.HasSuffix(tc.name, "post-install") || tc.name == "retry" || tc.name == "delete-failed" || tc.name == "delete-cleanup-failed" || tc.name == "stale" {
				m.Provision.Artifacts = map[string]string{"imageApplied": "true"}
			}
			if tc.name == "delete-cleanup-failed" {
				m.SealedBootstrap.Cleanup = true
			}
			if err = b.Machines().Upsert(ctx, m); err != nil {
				t.Fatal(err)
			}
			s := b.BareMetal()
			if _, err = s.Register(ctx, m.Name, "pool", string(key), m.TargetDisk); err != nil {
				t.Fatal(err)
			}
			h, err := s.Acquire(ctx, "pool", "capi-owner")
			if err != nil {
				t.Fatal(err)
			}
			h, err = s.Transition(ctx, h, baremetal.Deploying, "old")
			if err != nil {
				t.Fatal(err)
			}
			if tc.state == baremetal.Failed {
				h, err = s.Transition(ctx, h, baremetal.Failed, "old")
				if err != nil {
					t.Fatal(err)
				}
			}
			if err = b.OSImages().Upsert(ctx, osimage.OSImage{Name: "image", Arch: "amd64", OSFamily: "ubuntu", Format: osimage.FormatSquashFS, Variant: osimage.VariantBareMetal, Ready: true, Manifest: &osimage.Manifest{Root: osimage.RootArtifact{Format: osimage.FormatSquashFS, Path: "rootfs.squashfs"}}}); err != nil {
				t.Fatal(err)
			}
			if err = b.OSImages().Upsert(ctx, osimage.OSImage{Name: "replacement", Arch: "amd64", OSFamily: "ubuntu", OSVersion: "24.04", Format: osimage.FormatSquashFS, Variant: osimage.VariantBareMetal, Ready: true, Manifest: &osimage.Manifest{Root: osimage.RootArtifact{Format: osimage.FormatSquashFS, Path: "rootfs.squashfs"}}}); err != nil {
				t.Fatal(err)
			}
			createUser(t, b.Auth(), "actor", "password", tc.role)
			token := createSession(t, b.Auth(), "actor")
			e := infraapi.NewServer(infraapi.ServerConfig{Machines: machine.NewService(b.Machines()), OSImages: osimage.NewService(b.OSImages()), AuthStore: b.Auth(), AuthService: infraapi.NewAuthService(b.Auth(), time.Hour), BareMetal: s}).Echo()
			method, route := http.MethodPost, "/api/v1/bare-metal-claims/capi-owner/retry"
			deleting := strings.HasPrefix(tc.name, "delete-")
			if deleting {
				method, route = http.MethodDelete, "/api/v1/bare-metal-claims/capi-owner"
			}
			request := map[string]any{"attemptID": tc.attempt}
			if tc.name == "post-install" {
				request["osImageRef"] = "replacement"
				request["powerCycle"] = false
			}
			rec := doRequest(e, method, route, request, token)
			requireStatus(t, rec, tc.want)
			updated, err := s.FindOwner(ctx, h.Owner)
			if err != nil {
				t.Fatal(err)
			}
			saved, err := b.Machines().Get(ctx, m.Name)
			if err != nil {
				t.Fatal(err)
			}
			if tc.want == http.StatusAccepted && tc.name != "delete-installer" {
				next := baremetal.Deploying
				if deleting {
					next = baremetal.Releasing
				}
				if updated.State != next || updated.AttemptID == "old" || saved.Provision.AttemptID != updated.AttemptID {
					t.Fatal("new attempt not atomically fenced")
				}
				if saved.TargetDisk != m.TargetDisk || (!deleting && string(saved.SealedBootstrap.Envelope) != string(m.SealedBootstrap.Envelope)) || (deleting && (!saved.SealedBootstrap.Cleanup || (len(saved.SealedBootstrap.Envelope) != 0 && string(saved.SealedBootstrap.Envelope) != "null"))) {
					t.Fatal("retry changed enrollment inputs")
				}
				if tc.name == "post-install" && (saved.OSPreset.ImageRef != "replacement" || saved.OSPreset.Version != "24.04") {
					t.Fatalf("retry did not switch to requested image: %+v", saved.OSPreset)
				}
				if deleting {
					requireStatus(t, doRequest(e, http.MethodDelete, "/api/v1/bare-metal-claims/capi-owner", nil, token), http.StatusAccepted)
					again, err := s.FindOwner(ctx, h.Owner)
					if err != nil || again != updated {
						t.Fatal("repeat deletion restarted cleanup")
					}
				} else {
					requireStatus(t, doRequest(e, http.MethodPost, "/api/v1/bare-metal-claims/capi-owner/retry", map[string]string{"attemptID": "old"}, token), http.StatusConflict)
				}
				if err = b.Machines().Upsert(ctx, m); err == nil {
					t.Fatal("late previous attempt overwrote retry")
				}
			} else if updated != h || saved.Provision.AttemptID != "old" {
				t.Fatal("rejected retry changed state")
			}
		})
	}
}

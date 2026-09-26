package pxehttp

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/sugaf1204/gomi/internal/hwinfo"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/osimage"
	"gopkg.in/yaml.v3"
)

func TestSealedBootstrapCurtinFamiliesAndOrdinaryPath(t *testing.T) {
	for _, family := range []string{"ubuntu", "debian", "fedora"} {
		for _, mode := range []string{"ordinary", "sealed", "cleanup"} {
			t.Run(family+"/"+mode, func(t *testing.T) {
				m := &machine.Machine{Name: "node1", Hostname: "node1", MAC: "00:11:22:33:44:55", Firmware: machine.FirmwareUEFI, Provision: &machine.ProvisionProgress{Active: true, AttemptID: "attempt"}}
				if mode != "ordinary" {
					m.SealedBootstrap = &machine.SealedBootstrap{Owner: "capi-owner", KeyFingerprint: strings.Repeat("a", 64), Envelope: json.RawMessage(`{"ciphertext":"encrypted"}`), Cleanup: mode == "cleanup"}
				}
				img := osimage.OSImage{Name: "image", OSFamily: family, Ready: true, Format: osimage.FormatSquashFS, LocalPath: "/var/lib/gomi/data/images/image", Manifest: &osimage.Manifest{Root: osimage.RootArtifact{Path: "rootfs.squashfs", Format: osimage.FormatSquashFS}}}
				info := &hwinfo.HardwareInfo{Disks: []hwinfo.DiskInfo{{Name: "nvme0n1", Path: "/dev/nvme0n1", Type: "disk", SizeMB: 65536}}}
				request := httptest.NewRequest("GET", "http://gomi/pxe/curtin-config?attempt_id=attempt", nil)
				ctx := echo.New().NewContext(request, httptest.NewRecorder())
				rendered, err := (&Handler{}).buildCurtinInstallConfig(context.Background(), ctx, m, img, info)
				if err != nil {
					t.Fatal(err)
				}
				var cfg curtinConfig
				if err = yaml.Unmarshal([]byte(rendered), &cfg); err != nil {
					t.Fatal(err)
				}
				if mode == "ordinary" {
					if len(cfg.EarlyCommands) != 0 || strings.Contains(rendered, "gomi-sealed") {
						t.Fatal("ordinary deployment changed")
					}
					return
				}
				early := cfg.EarlyCommands["00-gomi-preserve-bootstrap"][2]
				if !strings.Contains(early, "--disk '/dev/nvme0n1'") || strings.Contains(early, "%!s") {
					t.Fatalf("bad early command: %s", early)
				}
				if strings.Contains(early, "gomi-unseal-bootstrap") != (mode == "sealed") {
					t.Fatal("incorrect sealed/cleanup branch")
				}
				for _, command := range []string{early, cfg.LateCommands["99-gomi-sealed-bootstrap"][2]} {
					cmd := exec.Command("sh", "-n")
					cmd.Stdin = strings.NewReader(command)
					if output, err := cmd.CombinedOutput(); err != nil {
						t.Fatalf("invalid shell: %v %s", err, output)
					}
				}
			})
		}
	}
}
func TestSealedCompletionMarkerSurvivesExtraCommands(t *testing.T) {
	body, err := injectCloudConfigCompletion("#cloud-config\nruncmd:\n- prepare\n", "http://gomi/pxe/install-complete?token=t&type=curtin", "node1", 1)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err = yaml.Unmarshal([]byte(body), &cfg); err != nil {
		t.Fatal(err)
	}
	cfg["runcmd"] = append(cfg["runcmd"].([]any), "install-wol-agent")
	body, err = renderCloudConfig(body, cfg)
	if err != nil {
		t.Fatal(err)
	}
	result, err := markSealedCompletion(body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "# gomi-capi-completion") || !strings.Contains(result, "bootstrap-success.complete || exit 1") || !strings.Contains(result, "install-wol-agent") {
		t.Fatal("lost bootstrap/completion ordering marker")
	}
}

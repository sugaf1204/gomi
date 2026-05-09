package build

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sugaf1204/gomi/bootenv/internal/spec"
)

func TestSelectDebianLiveISO(t *testing.T) {
	name, sum, ok := selectDebianLiveISO(`
111  debian-live-12.9.0-amd64-standard.iso
222  debian-live-13.0.0-amd64-standard.iso
333  debian-live-13.0.0-amd64-gnome.iso
`, "amd64", "standard")
	if !ok {
		t.Fatal("expected match")
	}
	if name != "debian-live-13.0.0-amd64-standard.iso" || sum != "222" {
		t.Fatalf("selected %s/%s", name, sum)
	}
}

func TestSelectUbuntuCloudSquashFS(t *testing.T) {
	name, sum, ok := selectUbuntuCloudSquashFS(`
111  ubuntu-24.04-minimal-cloudimg-amd64.img
222  ubuntu-24.04-minimal-cloudimg-amd64.squashfs
333  ubuntu-24.04-minimal-cloudimg-arm64.squashfs
`, "amd64")
	if !ok {
		t.Fatal("expected match")
	}
	if name != "ubuntu-24.04-minimal-cloudimg-amd64.squashfs" || sum != "222" {
		t.Fatalf("selected %s/%s", name, sum)
	}
}

func TestBuildRootFSPackagesAddsCloudInitramfsSupport(t *testing.T) {
	doc := spec.Document{
		Spec: spec.Spec{
			Kernel:    spec.Kernel{Package: "linux-image-generic"},
			Initramfs: spec.Initramfs{Packages: []string{"cloud-initramfs-rooturl"}},
			RootFS: spec.RootFS{
				Source:   spec.RootFSSource{Type: "ubuntu-cloud-squashfs"},
				Packages: []string{"curtin", "curl", "rsync"},
			},
		},
	}

	got := strings.Join(buildRootFSPackages(doc), " ")
	for _, want := range []string{
		"curtin",
		"curl",
		"rsync",
		"linux-image-generic",
		"initramfs-tools",
		"cloud-initramfs-rooturl",
		"cloud-initramfs-copymods",
		"overlayroot",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in %q", want, got)
		}
	}
}

func TestApplyDeclarativeRootFSContent(t *testing.T) {
	rootfs := t.TempDir()
	err := applyFiles(rootfs, []spec.File{{Path: "/etc/gomi/agent.env", Mode: "0640", Contents: "A=B\n"}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(rootfs, "etc", "gomi", "agent.env")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "A=B\n" {
		t.Fatalf("body = %q", body)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
}

func TestInstallRootFSPackagesUsesChrootApt(t *testing.T) {
	rootfs := t.TempDir()
	runner := &recordingRunner{}

	err := installRootFSPackages(context.Background(), io.Discard, runner, rootfs, []string{"curtin", "curl"})
	if err != nil {
		t.Fatal(err)
	}

	got := strings.Join(runner.calls, "\n")
	for _, want := range []string{
		"mount -t proc proc " + filepath.Join(rootfs, "proc"),
		"mount -t sysfs sysfs " + filepath.Join(rootfs, "sys"),
		"mount --bind /dev " + filepath.Join(rootfs, "dev"),
		"chroot " + rootfs + " apt-get -o Acquire::Languages=none update",
		"chroot " + rootfs + " env DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends curtin curl",
		"chroot " + rootfs + " apt-get clean",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected command %q in calls:\n%s", want, got)
		}
	}
}

type recordingRunner struct {
	calls []string
}

func (r *recordingRunner) Run(_ context.Context, _ io.Writer, name string, args ...string) error {
	if name == "sudo" && len(args) > 0 {
		name, args = args[0], args[1:]
	}
	r.calls = append(r.calls, strings.Join(append([]string{name}, args...), " "))
	return nil
}

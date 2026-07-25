package pxehttp

import (
	"strings"
	"testing"
)

// The Jinja header must survive every stage that rebuilds the cloud-config
// document. yaml.Unmarshal drops comments, so a renderer that re-marshals has
// to re-attach it; otherwise cloud-init treats the expressions as literal text.
func TestInjectCloudConfigCompletion_PreservesJinjaHeader(t *testing.T) {
	tests := []struct {
		name     string
		userData string
	}{
		{
			name:     "quoted jinja expression",
			userData: "## template: jinja\n#cloud-config\nfqdn: \"{{ v1.local_hostname }}.lab\"\n",
		},
		{
			name:     "jinja control block without expressions",
			userData: "## template: jinja\n#cloud-config\npackages:\n  - curl\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := injectCloudConfigCompletion(tt.userData, "", "vm1", 0)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.HasPrefix(strings.TrimSpace(got), jinjaTemplateHeader) {
				t.Errorf("jinja header was stripped\ninput:\n%s\ngot:\n%s", tt.userData, got)
			}
			if strings.Count(got, jinjaTemplateHeader) != 1 {
				t.Errorf("expected exactly one jinja header, got %d\n%s", strings.Count(got, jinjaTemplateHeader), got)
			}
		})
	}
}

// A document that only becomes valid YAML after cloud-init renders it cannot
// receive the install-complete callback, so serving it would strand the target
// in Provisioning. It must fail loudly instead.
func TestInjectCloudConfigCompletion_RejectsUnparseableUserData(t *testing.T) {
	userData := "## template: jinja\n#cloud-config\nhostname: {{ v1.local_hostname }}\n"

	_, err := injectCloudConfigCompletion(userData, "http://gomi/complete?token=t&type=vm", "vm1", 60)
	if err == nil {
		t.Fatal("expected an error for user-data that is not valid YAML")
	}
	if !strings.Contains(err.Error(), "not valid YAML") {
		t.Errorf("error should say the user-data is unparseable: %v", err)
	}
	if !strings.Contains(err.Error(), "quote it") {
		t.Errorf("error should hint at quoting the expression: %v", err)
	}
}

// The header must survive the whole PXE chain, not just the first renderer:
// injectSSHKeysAndLoginUser, the hypervisor, WoL and network injectors all
// unmarshal and re-marshal the document.
func TestDeployPipeline_PreservesJinjaHeader(t *testing.T) {
	userData := "## template: jinja\n#cloud-config\nfqdn: \"{{ v1.local_hostname }}.lab\"\n"

	rendered, err := injectCloudConfigCompletion(userData, "", "vm1", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// A representative intermediate re-marshaller from the chain, then the
	// final defaults pass. Both must carry the header through.
	rendered = injectHypervisorSetup(rendered, "http://gomi", "hv1", "token", "ubuntu")
	final := withDeployCloudInitDefaults(rendered, true)

	if !strings.HasPrefix(strings.TrimSpace(final), jinjaTemplateHeader) {
		t.Errorf("jinja header lost in the full pipeline:\n%s", final)
	}
	if strings.Count(final, jinjaTemplateHeader) != 1 {
		t.Errorf("expected exactly one jinja header, got %d\n%s", strings.Count(final, jinjaTemplateHeader), final)
	}
	if !strings.Contains(final, "{{ v1.local_hostname }}") {
		t.Errorf("jinja expression did not survive rendering:\n%s", final)
	}
}

// A document without the header must not acquire one at any stage.
func TestPipeline_PlainConfigKeepsNoJinjaHeader(t *testing.T) {
	got, err := injectCloudConfigCompletion("#cloud-config\npackages:\n  - curl\n", "", "vm1", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got = injectHypervisorSetup(got, "http://gomi", "hv1", "token", "ubuntu")
	got = withDeployCloudInitDefaults(got, false)

	if strings.Contains(got, jinjaTemplateHeader) {
		t.Errorf("plain cloud-config gained a jinja header:\n%s", got)
	}
	if !strings.HasPrefix(strings.TrimSpace(got), "#cloud-config") {
		t.Errorf("expected a #cloud-config header:\n%s", got)
	}
}

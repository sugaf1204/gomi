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
			// Quoted expressions are valid YAML, so this reaches the re-marshal
			// path where the header was previously dropped.
			name:     "valid yaml with quoted jinja expression",
			userData: "## template: jinja\n#cloud-config\nfqdn: \"{{ v1.local_hostname }}.lab\"\n",
		},
		{
			// A bare {{ ... }} is a YAML flow mapping and fails to unmarshal, so
			// this exercises the passthrough path instead.
			name:     "unquoted jinja expression",
			userData: "## template: jinja\n#cloud-config\nhostname: {{ v1.local_hostname }}\n",
		},
		{
			name:     "jinja control block without expressions",
			userData: "## template: jinja\n#cloud-config\npackages:\n  - curl\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := injectCloudConfigCompletion(tt.userData, "", "vm1", 0)
			if !strings.HasPrefix(strings.TrimSpace(got), jinjaTemplateHeader) {
				t.Errorf("jinja header was stripped\ninput:\n%s\ngot:\n%s", tt.userData, got)
			}
			if strings.Count(got, jinjaTemplateHeader) != 1 {
				t.Errorf("expected exactly one jinja header, got %d\n%s", strings.Count(got, jinjaTemplateHeader), got)
			}
		})
	}
}

// The header must also survive the full PXE pipeline, where
// withDeployCloudInitDefaults runs after injectCloudConfigCompletion.
func TestDeployPipeline_PreservesJinjaHeader(t *testing.T) {
	userData := "## template: jinja\n#cloud-config\nfqdn: \"{{ v1.local_hostname }}.lab\"\n"

	rendered := injectCloudConfigCompletion(userData, "", "vm1", 0)
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

// A document without the header must not acquire one.
func TestInjectCloudConfigCompletion_PlainConfigKeepsNoJinjaHeader(t *testing.T) {
	got := injectCloudConfigCompletion("#cloud-config\npackages:\n  - curl\n", "", "vm1", 0)

	if strings.Contains(got, jinjaTemplateHeader) {
		t.Errorf("plain cloud-config gained a jinja header:\n%s", got)
	}
	if !strings.HasPrefix(strings.TrimSpace(got), "#cloud-config") {
		t.Errorf("expected a #cloud-config header:\n%s", got)
	}
}

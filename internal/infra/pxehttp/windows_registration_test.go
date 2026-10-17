package pxehttp

import (
	"encoding/base64"
	"encoding/xml"
	"strings"
	"testing"
)

func TestWindowsCompletionRegistrationHasNoWallClockBoundary(t *testing.T) {
	start := strings.Index(windowsRegisterScript, "<Task ")
	end := strings.Index(windowsRegisterScript, "</Task>")
	if start < 0 || end < start {
		t.Fatal("missing task XML")
	}
	var task struct {
		Triggers struct {
			Items []struct {
				XMLName    xml.Name
				Enabled    bool `xml:"Enabled"`
				Repetition struct {
					Interval string `xml:"Interval"`
				} `xml:"Repetition"`
				Start string `xml:"StartBoundary"`
				End   string `xml:"EndBoundary"`
			} `xml:",any"`
		} `xml:"Triggers"`
	}
	if err := xml.Unmarshal([]byte(windowsRegisterScript[start:end+len("</Task>")]), &task); err != nil {
		t.Fatal(err)
	}
	expected := map[string]bool{"RegistrationTrigger": false, "BootTrigger": false, "LogonTrigger": false}
	for _, trigger := range task.Triggers.Items {
		if _, ok := expected[trigger.XMLName.Local]; !ok {
			t.Fatalf("unexpected trigger %s", trigger.XMLName.Local)
		}
		if !trigger.Enabled || trigger.Repetition.Interval != "PT1M" || trigger.Start != "" || trigger.End != "" {
			t.Fatalf("unsafe trigger: %+v", trigger)
		}
		expected[trigger.XMLName.Local] = true
	}
	for name, found := range expected {
		if !found {
			t.Fatalf("missing %s", name)
		}
	}
	rendered := renderWindowsInstall([]byte(`{}`))
	if strings.Contains(rendered, "__GOMI_REGISTER__") || !strings.Contains(rendered, base64.StdEncoding.EncodeToString([]byte(windowsRegisterScript))) {
		t.Fatal("installer does not embed registration script")
	}
}

func TestWindowsCompletionExpiryUsesServerResponse(t *testing.T) {
	if strings.Contains(windowsCompleteScript, "$config.deadline") || strings.Contains(windowsCompleteScript, "[DateTimeOffset]::UtcNow") {
		t.Fatal("guest clock must not expire provisioning credentials")
	}
	expiry := strings.Index(windowsCompleteScript, "StatusCode -in @(404, 410)")
	if expiry < 0 || !strings.Contains(windowsCompleteScript[expiry:], "Remove-GomiTask") {
		t.Fatal("server expiry must clean task and credentials")
	}
}

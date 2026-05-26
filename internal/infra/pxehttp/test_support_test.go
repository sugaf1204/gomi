package pxehttp

import (
	"context"
	"github.com/sugaf1204/gomi/internal/power"
	"gopkg.in/yaml.v3"
	"strings"
	"testing"
)

type bootOrderCall struct {
	machine power.MachineInfo
	order   power.BootOrder
}

type stubPowerExecutor struct {
	calls []bootOrderCall
	err   error
}

func (s *stubPowerExecutor) ConfigureBootOrder(_ context.Context, m power.MachineInfo, order power.BootOrder) error {
	copied := append(power.BootOrder(nil), order...)
	s.calls = append(s.calls, bootOrderCall{machine: m, order: copied})
	return s.err
}

func parseCloudConfigBody(t *testing.T, raw string) map[string]any {
	t.Helper()
	raw = strings.TrimPrefix(raw, "#cloud-config\n")
	var cfg map[string]any
	if err := yaml.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("yaml.Unmarshal: %v\n%s", err, raw)
	}
	return cfg
}

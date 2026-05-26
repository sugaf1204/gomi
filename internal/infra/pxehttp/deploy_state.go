package pxehttp

import (
	"context"
	"fmt"
	"github.com/sugaf1204/gomi/internal/hwinfo"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/resource"
	"net/url"
	"strings"
	"time"
)

func (h *Handler) hardwareInfo(ctx context.Context, machineName string) (*hwinfo.HardwareInfo, error) {
	if h.hwinfo == nil {
		return nil, resource.ErrNotFound
	}
	info, err := h.hwinfo.Get(ctx, machineName)
	if err != nil {
		return nil, err
	}
	return &info, nil
}

func (h *Handler) requireProvisioningMachine(ctx context.Context, token string) (*machine.Machine, error) {
	target, err := h.findMachineByProvisionToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if target == nil || target.Provision == nil || strings.TrimSpace(target.Provision.CompletionToken) != strings.TrimSpace(token) {
		return nil, resource.ErrNotFound
	}
	if !target.Provision.Active {
		return nil, resource.ErrNotFound
	}
	return target, nil
}

func (h *Handler) updateProvisionProgress(ctx context.Context, name string, fn func(*machine.Machine)) error {
	if h.machines == nil {
		return fmt.Errorf("machine service not available")
	}
	m, err := h.machines.Get(ctx, name)
	if err != nil {
		return err
	}
	fn(&m)
	m.UpdatedAt = time.Now().UTC()
	return h.machines.Store().Upsert(ctx, m)
}

func buildPXECurtinConfigURL(base, token, attemptID string) string {
	q := url.Values{}
	q.Set("token", token)
	if attemptID != "" {
		q.Set("attempt_id", attemptID)
	}
	return strings.TrimRight(base, "/") + "/curtin-config?" + q.Encode()
}

func buildPXEDeployEventsURL(base, token, attemptID string) string {
	q := url.Values{}
	q.Set("token", token)
	if attemptID != "" {
		q.Set("attempt_id", attemptID)
	}
	return strings.TrimRight(base, "/") + "/deploy-events?" + q.Encode()
}

func machineImageApplied(m *machine.Machine) bool {
	if m == nil || m.Provision == nil || m.Provision.Artifacts == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(m.Provision.Artifacts[provisionArtifactImageApplied]), "true")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func timePtr(t time.Time) *time.Time {
	return &t
}

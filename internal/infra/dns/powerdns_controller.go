package dns

import (
	"context"
	"fmt"

	"github.com/sugaf1204/gomi/internal/machine"
)

var _ Controller = (*PowerDNSController)(nil)

type PowerDNSController struct {
	client   *PowerDNSClient
	machines machine.Store
}

func NewPowerDNSController(client *PowerDNSClient, machines machine.Store) *PowerDNSController {
	return &PowerDNSController{client: client, machines: machines}
}

func (p *PowerDNSController) Start(ctx context.Context) error {
	if err := p.Sync(ctx); err != nil {
		return err
	}
	<-ctx.Done()
	return nil
}

func (p *PowerDNSController) Sync(ctx context.Context) error {
	if p.client == nil || !p.client.Enabled() {
		return nil
	}
	machines, err := p.machines.List(ctx)
	if err != nil {
		return fmt.Errorf("powerdns list machines: %w", err)
	}
	for _, m := range machines {
		if err := p.client.UpsertMachineRecord(ctx, m); err != nil {
			return fmt.Errorf("powerdns upsert machine %s: %w", m.Name, err)
		}
	}
	return nil
}

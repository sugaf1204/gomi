package api

import (
	"testing"

	"github.com/sugaf1204/gomi/internal/machine"
)

func TestProvisionProgressResponseIncludesTimings(t *testing.T) {
	resp := provisionProgressResponse(&machine.ProvisionProgress{
		Timings: []machine.ProvisionTiming{
			{
				Source:         "server",
				Name:           "server.inventory.store",
				DurationMillis: 8,
			},
		},
	})
	if resp == nil || len(resp.Timings) != 1 {
		t.Fatalf("expected timings in response, got %#v", resp)
	}
	if resp.Timings[0].Name != "server.inventory.store" {
		t.Fatalf("unexpected timing response: %#v", resp.Timings)
	}
}

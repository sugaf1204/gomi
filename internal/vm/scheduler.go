package vm

import (
	"math/rand/v2"

	"github.com/sugaf1204/gomi/internal/hypervisor"
)

func SelectHypervisor(hypervisors []hypervisor.Hypervisor) string {
	type candidate struct {
		name   string
		weight int64
	}

	var candidates []candidate
	for _, h := range hypervisors {
		if h.Phase != hypervisor.PhaseReady {
			continue
		}
		avail := AvailableMemory(h)
		if avail > 0 {
			candidates = append(candidates, candidate{name: h.Name, weight: avail})
		}
	}
	if len(candidates) == 0 {
		for _, h := range hypervisors {
			avail := AvailableMemory(h)
			if avail > 0 {
				candidates = append(candidates, candidate{name: h.Name, weight: avail})
			}
		}
	}
	if len(candidates) == 0 {
		return ""
	}

	var totalWeight int64
	for _, c := range candidates {
		totalWeight += c.weight
	}
	pick := rand.Int64N(totalWeight)
	var cumulative int64
	for _, c := range candidates {
		cumulative += c.weight
		if pick < cumulative {
			return c.name
		}
	}
	return candidates[len(candidates)-1].name
}

func AvailableMemory(h hypervisor.Hypervisor) int64 {
	if h.Capacity == nil {
		return 0
	}
	total := h.Capacity.MemoryMB
	if h.Used != nil {
		return total - h.Used.MemoryUsedMB
	}
	return total
}

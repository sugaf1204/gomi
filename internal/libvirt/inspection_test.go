package libvirt

import "testing"

func TestMapDomainState(t *testing.T) {
	// Verify that mapDomainState covers expected libvirt state constants.
	tests := []struct {
		name  string
		input uint8
		want  DomainState
	}{
		{"running", 1, StateRunning},
		{"shutoff", 5, StateShutoff},
		{"paused", 3, StatePaused},
		{"crashed", 6, StateCrashed},
		{"unknown", 255, StateUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapDomainState(tt.input)
			if got != tt.want {
				t.Errorf("mapDomainState(%d) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

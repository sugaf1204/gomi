package machine

var validTransitions = map[Phase][]Phase{
	PhaseDiscovered:   {PhaseReady, PhaseProvisioning, PhaseError},
	PhaseReady:        {PhaseProvisioning, PhaseError},
	PhaseProvisioning: {PhaseReady, PhaseError},
	PhaseError:        {PhaseReady, PhaseProvisioning, PhaseDiscovered},
}

func CanTransition(from, to Phase) bool {
	targets, ok := validTransitions[from]
	if !ok {
		return false
	}
	for _, t := range targets {
		if t == to {
			return true
		}
	}
	return false
}

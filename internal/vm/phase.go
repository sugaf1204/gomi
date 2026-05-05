package vm

var validTransitions = map[Phase][]Phase{
	PhasePending:      {PhaseCreating, PhaseProvisioning, PhaseError},
	PhaseCreating:     {PhaseRunning, PhaseProvisioning, PhaseStopped, PhaseError},
	PhaseRunning:      {PhaseStopped, PhaseMigrating, PhaseProvisioning, PhaseError, PhaseDeleting},
	PhaseStopped:      {PhaseRunning, PhaseProvisioning, PhaseError, PhaseDeleting},
	PhaseProvisioning: {PhaseRunning, PhaseStopped, PhaseError},
	PhaseError:        {PhaseRunning, PhaseStopped, PhaseProvisioning, PhaseDeleting},
	PhaseMigrating:    {PhaseRunning, PhaseError},
	PhaseDeleting:     {},
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

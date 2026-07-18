package vm

var validTransitions = map[Phase][]Phase{
	PhasePending:      {PhaseCreating, PhaseProvisioning, PhaseError},
	PhaseCreating:     {PhaseRunning, PhaseProvisioning, PhaseStopped, PhaseError},
	PhaseRunning:      {PhaseStopped, PhaseMigrating, PhaseProvisioning, PhaseError, PhaseDeleting, PhaseMissing},
	PhaseStopped:      {PhaseRunning, PhaseProvisioning, PhaseError, PhaseDeleting, PhaseMissing},
	PhaseProvisioning: {PhaseRunning, PhaseStopped, PhaseError, PhaseMissing},
	PhaseError:        {PhaseRunning, PhaseStopped, PhaseProvisioning, PhaseDeleting, PhaseMissing},
	PhaseMigrating:    {PhaseRunning, PhaseError},
	PhaseDeleting:     {},
	PhaseMissing:      {PhaseRunning, PhaseStopped, PhaseDeleting},
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

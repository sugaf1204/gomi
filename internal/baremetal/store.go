// Package baremetal owns physical-host allocation independently of Kubernetes
// object UIDs. Claims must survive controller restarts and clusterctl move.
package baremetal

import (
	"context"
	"errors"
	"fmt"
	"github.com/sugaf1204/gomi/internal/machine"
	"regexp"
	"strings"
)

var (
	ErrConflict = errors.New("bare-metal host ownership or revision conflict")
	ErrCapacity = errors.New("no available bare-metal hosts in pool")
	ErrNotFound = errors.New("bare-metal host not found")
)

type State string

const (
	Available State = "Available"
	Claimed   State = "Claimed"
	Deploying State = "Deploying"
	Ready     State = "Ready"
	Releasing State = "Releasing"
	Failed    State = "Failed"
)

type Host struct {
	TargetDisk string `json:"targetDisk"`
	IPAddress  string `json:"ipAddress,omitempty"`
	PublicKey  string `json:"publicKey"`
	Name       string `json:"name"`
	Pool       string `json:"pool"`
	Owner      string `json:"owner,omitempty"`
	State      State  `json:"state"`
	Revision   int64  `json:"revision"`
	// AttemptID fences the current deployment. Explicit failed-attempt recovery
	// atomically replaces it; callbacks from an older attempt are rejected.
	AttemptID string `json:"attemptID,omitempty"`
}

// Store implementations must enforce ownership and revision checks atomically.
// Register is insert-only: re-registering a host never resets an active claim.
type Store interface {
	Register(context.Context, string, string, string, string) (Host, error)
	UpdatePool(context.Context, string, string, int64) (Host, error)
	Get(context.Context, string) (Host, error)
	FindOwner(context.Context, string) (Host, error)
	Acquire(context.Context, string, string) (Host, error)
	Transition(context.Context, Host, State, string) (Host, error)
	CompleteRelease(context.Context, Host) (Host, error)
	CommitDeployment(context.Context, Host, machine.Machine) (Host, error)
}

func ValidateRegistration(name, pool string) error {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(pool) == "" {
		return fmt.Errorf("host name and pool are required")
	}
	return nil
}

var ownerPattern = regexp.MustCompile(`^capi-[a-z0-9-]{1,123}$`)

func ValidateAcquire(pool, owner string) error {
	if strings.TrimSpace(pool) == "" || !ownerPattern.MatchString(owner) {
		return fmt.Errorf("pool and stable capi- owner identity are required")
	}
	return nil
}
func ValidateTransition(h Host, next State, attempt string) error {
	if h.Owner == "" {
		return ErrConflict
	}
	if h.AttemptID != "" && attempt != h.AttemptID {
		return fmt.Errorf("deployment attempt is immutable within a claim")
	}
	allowed := false
	switch next {
	case Deploying:
		allowed = h.State == Claimed && attempt != ""
	case Ready:
		allowed = h.State == Deploying && attempt != ""
	case Failed:
		allowed = h.State == Claimed || h.State == Deploying || h.State == Releasing
	case Releasing:
		allowed = h.State == Claimed || h.State == Deploying || h.State == Ready || h.State == Failed
	}
	if !allowed {
		return fmt.Errorf("invalid bare-metal transition %s -> %s", h.State, next)
	}
	return nil
}

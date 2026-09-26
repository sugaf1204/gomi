package baremetal

import "testing"

func TestTransitionRejectsUnsafeLifecycle(t *testing.T) {
	for _, tc := range []struct {
		from, to                 State
		attempt, previous, owner string
	}{
		{Available, Deploying, "attempt", "", ""},
		{Claimed, Ready, "attempt", "", "capi-owner"},
		{Claimed, Deploying, "", "", "capi-owner"},
		{Ready, Deploying, "attempt", "attempt", "capi-owner"},
		{Failed, Deploying, "new", "old", "capi-owner"},
		{Releasing, Available, "attempt", "attempt", "capi-owner"},
		{Deploying, Ready, "new", "old", "capi-owner"},
	} {
		h := Host{State: tc.from, AttemptID: tc.previous, Owner: tc.owner}
		if err := ValidateTransition(h, tc.to, tc.attempt); err == nil {
			t.Errorf("accepted unsafe transition: %+v", tc)
		}
	}
}
func TestStableOwnerValidation(t *testing.T) {
	for _, owner := range []string{"", "capi-", "capi-../name", "capi-owner/name", "capi-owner\n", "other-123"} {
		if err := ValidateAcquire("pool", owner); err == nil {
			t.Errorf("accepted owner %q", owner)
		}
	}
	if err := ValidateAcquire("pool", "capi-123-abc"); err != nil {
		t.Fatal(err)
	}
}

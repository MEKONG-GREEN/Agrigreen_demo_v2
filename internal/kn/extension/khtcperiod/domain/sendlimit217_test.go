package domain

import "testing"

func TestThirdSubmissionAutoLocks217(t *testing.T) {
	v := FormStateOf(FormFacts{Versions: 3, Max: 3})
	if v.State != FormLocked || v.CanConfirm || v.CanRequestSupplement || !v.CountsAsSent {
		t.Fatalf("third submission must be locked, complete and unreviewable: %+v", v)
	}
}

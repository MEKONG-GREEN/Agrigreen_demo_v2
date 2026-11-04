package port

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func operation212() PeriodOperationRequest {
	tenant, period := uuid.New(), uuid.New()
	return PeriodOperationRequest{OperationID: uuid.New(), Profile: "receipt_fixture", PeriodID: period, Action: PeriodOperationChangeDue,
		Expected: PeriodRevision{TenantID: tenant, PeriodID: period, Profile: "receipt_fixture", EventSeq: 1}, DueAt: time.Date(2026, 11, 4, 3, 0, 0, 0, time.UTC), Reason: "  original request  "}
}

func TestPeriodOperation212CanonicalInputAndBinding(t *testing.T) {
	req := operation212()
	got, err := PeriodOperationDigest(req)
	if err != nil {
		t.Fatal(err)
	}
	equivalent := req
	equivalent.Reason = "original request"
	equivalent.DueAt = req.DueAt.In(time.FixedZone("VN", 7*3600))
	same, err := PeriodOperationDigest(equivalent)
	if err != nil || same != got {
		t.Fatalf("canonical equivalent changed: %v", err)
	}
	for name, change := range map[string]func(*PeriodOperationRequest){
		"body":      func(r *PeriodOperationRequest) { r.Reason = "different request" },
		"expected":  func(r *PeriodOperationRequest) { r.Expected.DueSeq++ },
		"operation": func(r *PeriodOperationRequest) { r.OperationID = uuid.New() },
		"due":       func(r *PeriodOperationRequest) { r.DueAt = r.DueAt.Add(time.Hour) },
	} {
		t.Run(name, func(t *testing.T) {
			altered := req
			change(&altered)
			sum, e := PeriodOperationDigest(altered)
			if e != nil || sum == got {
				t.Fatalf("changed input was not bound: %v", e)
			}
		})
	}
	if req.Reason != "  original request  " {
		t.Fatal("normalization mutated original intent")
	}
}

func TestPeriodOperation212RejectsIgnoredOrUnboundedInputs(t *testing.T) {
	for name, change := range map[string]func(*PeriodOperationRequest){
		"zero_operation":     func(r *PeriodOperationRequest) { r.OperationID = uuid.Nil },
		"wrong_resource":     func(r *PeriodOperationRequest) { r.Expected.PeriodID = uuid.New() },
		"wrong_profile":      func(r *PeriodOperationRequest) { r.Expected.Profile = "other" },
		"invalid_revision":   func(r *PeriodOperationRequest) { r.Expected.EventSeq = 0 },
		"oversize_reason":    func(r *PeriodOperationRequest) { r.Reason = strings.Repeat("x", 100000) },
		"ignored_close_body": func(r *PeriodOperationRequest) { r.Action = PeriodOperationClose; r.Reason = "" },
		"unknown_action":     func(r *PeriodOperationRequest) { r.Action = "unconditional" },
	} {
		t.Run(name, func(t *testing.T) {
			req := operation212()
			change(&req)
			if _, err := PeriodOperationDigest(req); err == nil {
				t.Fatal("invalid request accepted")
			}
		})
	}
}

func TestPeriodOperation212MissingCapabilityIsClosed(t *testing.T) {
	if _, err := ExecutePeriodOperation(context.Background(), nil, operation212()); !errors.Is(err, ErrPeriodOperationUnavailable) {
		t.Fatalf("missing capability: %v", err)
	}
}

func TestPeriodOperationDigest212_RejectsMalformedProfile(t *testing.T) {
	request := PeriodOperationRequest{OperationID: uuid.New(), Profile: string([]byte{'f', 'x', 0xff}), PeriodID: uuid.New(), Action: PeriodOperationClose}
	request.Expected = PeriodRevision{TenantID: uuid.New(), Profile: request.Profile, PeriodID: request.PeriodID, EventSeq: 1}
	if _, err := PeriodOperationDigest(request); err == nil {
		t.Fatal("malformed UTF8 profile was silently canonicalized")
	}
}

func TestPeriodOperationDigest212_RevisionNumericWireBoundary(t *testing.T) {
	original, err := json.Marshal(operation212())
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []struct{ name, old string }{{"event_seq", "1"}, {"due_seq", "0"}} {
		for _, token := range []string{"1e100000", "1e-100000", strings.Repeat("9", 100000), "NaN", "Infinity"} {
			t.Run(field.name+"_"+token[:min(len(token), 12)], func(t *testing.T) {
				wire := strings.Replace(string(original), `"`+field.name+`":`+field.old, `"`+field.name+`":`+token, 1)
				var parsed PeriodOperationRequest
				if err := json.Unmarshal([]byte(wire), &parsed); err == nil {
					t.Fatal("unbounded/noninteger revision accepted")
				}
			})
		}
	}
}

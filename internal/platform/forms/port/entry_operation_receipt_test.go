package port

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestReviewOperation288_CanonicalDigestAndValidation(t *testing.T) {
	req := ReviewOperationRequest{OperationID: ids.New(), Profile: "kn_khtc", PeriodID: ids.New(), Unit: "trong_trot", SubmissionID: ids.New(), ExpectedVersion: 1, Decision: "returned", Note: "Bổ sung"}
	a, err := ReviewOperationDigest(req)
	if err != nil {
		t.Fatal(err)
	}
	same := req
	same.Note = " " + req.Note + " "
	same.Decision = " returned "
	same.Unit = " trong_trot "
	b, err := ReviewOperationDigest(same)
	if err != nil || a != b || len(a) != 64 {
		t.Fatalf("canonical digest mismatch: %s %s %v", a, b, err)
	}
	for _, field := range []string{"operation", "period", "unit", "submission", "version", "decision", "note", "profile"} {
		changed := req
		switch field {
		case "operation":
			changed.OperationID = ids.New()
		case "period":
			changed.PeriodID = ids.New()
		case "unit":
			changed.Unit = "other"
		case "submission":
			changed.SubmissionID = ids.New()
		case "version":
			changed.ExpectedVersion++
		case "decision":
			changed.Decision = "accepted"
		case "note":
			changed.Note = "Khác"
		case "profile":
			changed.Profile = "kn"
		}
		c, e := ReviewOperationDigest(changed)
		if e != nil || c == a {
			t.Fatalf("request field %s not bound", field)
		}
	}
	for _, bad := range []ReviewOperationRequest{
		{}, {OperationID: uuid.New()},
		func() ReviewOperationRequest { x := req; x.ExpectedVersion = 0; return x }(),
		func() ReviewOperationRequest { x := req; x.Note = " "; return x }(),
		func() ReviewOperationRequest { x := req; x.Note = strings.Repeat("a", 1001); return x }(),
		func() ReviewOperationRequest { x := req; x.Note = string([]byte{255}); return x }(),
		func() ReviewOperationRequest { x := req; x.Unit = "\x00"; return x }(),
		func() ReviewOperationRequest { x := req; x.Profile = " kn_khtc"; return x }(),
	} {
		if _, e := NormalizeReviewOperation(bad); e == nil {
			t.Fatalf("malformed request accepted: %+v", bad)
		}
	}
}

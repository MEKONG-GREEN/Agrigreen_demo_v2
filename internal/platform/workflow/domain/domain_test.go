package domain

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/workflow/port"
)

func TestFourEyes_W61(t *testing.T) {
	req, ben, other := uuid.New(), uuid.New(), uuid.New()
	cases := []struct {
		name        string
		beneficiary *uuid.UUID
		decider     uuid.UUID
		outcome     port.Outcome
		want        error
	}{
		{"người khác duyệt", &ben, other, port.OutcomeApproved, nil},
		{"người khác từ chối", nil, other, port.OutcomeRejected, nil},
		{"tự duyệt", nil, req, port.OutcomeApproved, ErrSelfDecision},
		{"tự từ chối", &ben, req, port.OutcomeRejected, ErrSelfDecision},
		{"người hưởng duyệt", &ben, ben, port.OutcomeApproved, ErrBeneficiary},
		{"người hưởng từ chối", &ben, ben, port.OutcomeRejected, ErrBeneficiary},
		{"người lập tự hủy", &ben, req, port.OutcomeCancelled, nil},
		{"thiếu người quyết định", nil, uuid.Nil, port.OutcomeApproved, errEmptyDecision},
	}
	for _, c := range cases {
		if err := FourEyes(req, c.beneficiary, c.decider, c.outcome); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, muốn %v", c.name, err, c.want)
		}
	}
	if !IsFourEyes(ErrSelfDecision) || !IsFourEyes(ErrBeneficiary) || IsFourEyes(ErrKind) {
		t.Error("IsFourEyes")
	}
}

func TestValidate_W61(t *testing.T) {
	for kind, ok := range map[string]bool{
		"masterdata.param_version": true, "party.share_grant": true, "a.b": true,
		"": false, "masterdata": false, "Masterdata.x": false, "a.b.c": false, "1a.b": false, "a.b-c": false,
		strings.Repeat("a", 41) + ".b": false,
	} {
		if err := ValidateKind(kind); (err == nil) != ok {
			t.Errorf("kind %q: %v", kind, err)
		}
	}
	base := port.SubmitInput{Kind: "x.y", Subject: uuid.New(), Requester: uuid.New()}
	if err := ValidateSubmit(base); err != nil {
		t.Fatal(err)
	}
	for name, mut := range map[string]func(*port.SubmitInput){
		"subject":   func(in *port.SubmitInput) { in.Subject = uuid.Nil },
		"requester": func(in *port.SubmitInput) { in.Requester = uuid.Nil },
		"summary":   func(in *port.SubmitInput) { in.Summary = strings.Repeat("x", 201) },
	} {
		in := base
		mut(&in)
		if ValidateSubmit(in) == nil {
			t.Errorf("submit %s phải lỗi", name)
		}
	}
	d := port.DecideInput{Kind: "x.y", Subject: uuid.New(), Requester: uuid.New(), Decider: uuid.New(), Outcome: port.OutcomeApproved}
	if err := ValidateDecide(d); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		mut  func(*port.DecideInput)
		want error
	}{
		"outcome lạ":       {func(in *port.DecideInput) { in.Outcome = "maybe" }, ErrOutcome},
		"từ chối thiếu lý": {func(in *port.DecideInput) { in.Outcome, in.Note = port.OutcomeRejected, "  " }, ErrNoteRequired},
		"hủy thiếu lý":     {func(in *port.DecideInput) { in.Outcome = port.OutcomeCancelled }, ErrNoteRequired},
		"ghi chú dài":      {func(in *port.DecideInput) { in.Note = strings.Repeat("x", 1001) }, ErrNoteTooLong},
		"tự duyệt":         {func(in *port.DecideInput) { in.Decider = in.Requester }, ErrSelfDecision},
		"thiếu người lập":  {func(in *port.DecideInput) { in.Requester = uuid.Nil }, ErrPerson},
	} {
		in := d
		c.mut(&in)
		if err := ValidateDecide(in); !errors.Is(err, c.want) {
			t.Errorf("decide %s: %v, muốn %v", name, err, c.want)
		}
	}
}

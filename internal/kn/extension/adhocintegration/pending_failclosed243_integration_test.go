package adhocintegration_test

import (
	"bytes"
	"context"
	"net/http"
	"testing"

	forms "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/google/uuid"
)

type pendingProbe243 struct {
	forms.PeriodControl
	change func(*forms.PeriodProgress)
	deny   bool
	calls  int
}

func (p *pendingProbe243) Progress(ctx context.Context, profile string, id uuid.UUID) (forms.PeriodProgress, error) {
	p.calls++
	if p.deny {
		return forms.PeriodProgress{}, problem.Forbidden
	}
	v, err := p.PeriodControl.Progress(ctx, profile, id)
	if err == nil && p.change != nil {
		v.Units = append([]forms.UnitProgress(nil), v.Units...)
		p.change(&v)
	}
	return v, err
}

func TestKHTCPendingInbox243_FailClosedNativeFactsAndCurrentGrant(t *testing.T) {
	var probe *pendingProbe243
	f := newKHTCFormFxWith(t, func(port forms.PeriodControl) forms.PeriodControl {
		probe = &pendingProbe243{PeriodControl: port}
		return probe
	})
	p := f.open(41, dTT)
	code, body := f.send(p.ID, dTT, "submit", 0, ttSheet(f.item, "25"))
	f.ok(code, body, nil)
	if out := readInbox243(t, f, ""); len(out.Rows) != 1 {
		t.Fatalf("control needs actual native submission: %+v", out)
	}
	for _, tc := range []struct {
		name   string
		change func(*forms.PeriodProgress)
	}{
		{"period", func(v *forms.PeriodProgress) { v.ID = uuid.New() }},
		{"profile", func(v *forms.PeriodProgress) { v.Profile = "other" }},
		{"status", func(v *forms.PeriodProgress) { v.Status = "unknown" }},
		{"missing submission", func(v *forms.PeriodProgress) { v.Units[0].LastSubmissionID = nil }},
		{"missing time", func(v *forms.PeriodProgress) { v.Units[0].LastSubmittedAt = nil }},
		{"negative version", func(v *forms.PeriodProgress) { v.Units[0].Versions = -1 }},
		{"unknown decision", func(v *forms.PeriodProgress) { v.Units[0].Decision = "unverified" }},
		{"duplicate unit", func(v *forms.PeriodProgress) { v.Units = append(v.Units, v.Units[0]) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			probe.change = tc.change
			code, body := f.khtc(http.MethodGet, "/pending-review", f.khtcUser, "", nil)
			if code != http.StatusServiceUnavailable || bytes.Contains(body, []byte(`"rows"`)) {
				t.Fatalf("inconsistent native facts exposed pending data: HTTP%d %s", code, body)
			}
		})
	}
	probe.change = nil
	probe.deny = true
	code, body = f.khtc(http.MethodGet, "/pending-review", f.khtcUser, "", nil)
	if code != http.StatusForbidden || bytes.Contains(body, []byte(`"rows"`)) {
		t.Fatalf("native authority denial became empty/partial page: HTTP%d %s", code, body)
	}
	probe.deny = false
	probe.change = func(v *forms.PeriodProgress) { v.Status = "closed" }
	if out := readInbox243(t, f, ""); len(out.Rows) != 0 {
		t.Fatalf("native closed with unresolved KHTC metadata stayed pending: %+v", out)
	}
	probe.change = nil
	if out := readInbox243(t, f, ""); len(out.Rows) != 1 {
		t.Fatalf("restored authority must read fresh facts: %+v", out)
	}
}

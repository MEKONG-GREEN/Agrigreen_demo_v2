package knaudit_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/knaudit"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	repport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

func exportCodec83(t *testing.T) *knaudit.ExportFilterCodec {
	t.Helper()
	reg, err := accport.NewAuditRegistry((&knaudit.Service{}).Sources()...)
	if err != nil {
		t.Fatal(err)
	}
	codec, err := knaudit.NewExportFilterCodec(reg)
	if err != nil {
		t.Fatal(err)
	}
	return codec
}

func freezeRequest83() repport.SourceFreezeRequest {
	return repport.SourceFreezeRequest{Source: knaudit.ExportSource, TemplateKey: knaudit.ExportTemplate, Format: "xlsx",
		TenantID: ids.New(), JobID: ids.New(), RequestedBy: ids.New(), RequestedAt: time.Date(2026, 11, 7, 2, 3, 4, 123456000, time.FixedZone("ICT", 7*60*60)),
		SourceArgs: json.RawMessage(`{"from":"2026-11-01T00:00:00+07:00","to":"2026-11-07T00:00:00+07:00"}`)}
}

func access83(r repport.SourceFreezeRequest, raw json.RawMessage) repport.SourceJobAccess {
	return repport.SourceJobAccess{Source: r.Source, TemplateKey: r.TemplateKey, Format: r.Format, TenantID: r.TenantID,
		JobID: r.JobID, RequestedBy: r.RequestedBy, RequestedAt: r.RequestedAt, ArgsVersion: knaudit.ExportArgsVersion, SourceArgs: raw}
}

func TestExportFilterCanonicalReplay83(t *testing.T) {
	c := exportCodec83(t)
	r := freezeRequest83()
	r.SourceArgs = json.RawMessage(`{"to":"2026-11-06T17:00:00Z","sources":["kn.review_event","kn.period_event"],"from":"2026-10-31T17:00:00Z","module":"kn.report"}`)
	one, err := c.Freeze(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	r.SourceArgs = json.RawMessage(`{"module":"kn.report","from":"2026-11-01T00:00:00+07:00","to":"2026-11-07T00:00:00+07:00","sources":["kn.period_event","kn.review_event"]}`)
	two, err := c.Freeze(context.Background(), r)
	if err != nil || !bytes.Equal(one, two) {
		t.Fatalf("canonical replay differs: %v", err)
	}
	f, err := c.Decode(access83(r, one))
	if err != nil || !f.AsOf.Equal(r.RequestedAt) || f.AsOf.Location() != time.UTC || len(f.Sources) != 2 || f.Sources[0] != "kn.period_event" {
		t.Fatalf("invalid frozen filter: %+v, %v", f, err)
	}
	if strings.Contains(string(one), "+07:00") {
		t.Fatal("snapshot must use UTC")
	}
	// PostgreSQL jsonb can reorder object keys; decoding must not depend on the wire bytes.
	var object map[string]json.RawMessage
	if err := json.Unmarshal(one, &object); err != nil {
		t.Fatal(err)
	}
	object["version"] = json.RawMessage("1")
	reencoded, err := json.MarshalIndent(object, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Decode(access83(r, reencoded)); err != nil {
		t.Fatalf("jsonb-compatible snapshot rejected: %v", err)
	}
}

func TestExportFilterStrictBounds83(t *testing.T) {
	c := exportCodec83(t)
	base := `"from":"2026-11-01T00:00:00Z","to":"2026-11-07T00:00:00Z"`
	cases := []string{
		`null`, `[]`, `{}`, `{` + base + `,"from":"2026-11-02T00:00:00Z"}`,
		`{` + base + `,"From":"2026-11-01T00:00:00Z"}`,
		`{` + base + `,"as_of":"2026-11-07T00:00:00Z"}`,
		`{` + base + `,"tenant_id":"` + ids.New().String() + `"}`,
		`{` + base + `,"payload":"private-marker"}`,
		`{` + base + `,"actor_ref":"0"}`, `{` + base + `,"actor_ref":null}`,
		`{` + base + `,"actor_ref":"00000000-0000-0000-0000-000000000000"}`,
		`{` + base + `,"department":"=HYPERLINK(private-marker)"}`,
		`{` + base + `,"module":"kn.unregistered"}`, `{` + base + `,"action":"private marker"}`,
		`{` + base + `,"sources":[]}`, `{` + base + `,"sources":null}`,
		`{` + base + `,"sources":["kn.period_event","kn.period_event"]}`,
		`{` + base + `,"sources":["kn.unknown"]}`,
		`{` + base + `,"module":"kn.report","sources":["kn.producer"]}`,
		`{` + base + `,"sources":[{"value":"kn.producer","value":"kn.review_event"}]}`,
		`{"from":"2026-01-01T00:00:00Z","to":"2026-04-05T00:00:00Z"}`,
		`{"from":"2026-11-07T00:00:00Z","to":"2026-11-01T00:00:00Z"}`,
		`{` + base + `} {}`, `{` + base + `,"from":1e100000}`, `{` + base + `,"from":1e-100000}`,
		`{` + base + `,"actor_ref":"` + strings.Repeat("1", 100000) + `"}`,
	}
	for i, raw := range cases {
		r := freezeRequest83()
		r.SourceArgs = json.RawMessage(raw)
		if _, err := c.Freeze(context.Background(), r); err == nil || strings.Contains(err.Error(), "private-marker") {
			t.Errorf("case %d accepted or disclosed input: %v", i, err)
		}
	}
	r := freezeRequest83()
	r.SourceArgs = json.RawMessage(`{"from":"2026-01-01T00:00:00Z","to":"2026-04-04T00:00:00Z"}`)
	if _, err := c.Freeze(context.Background(), r); err != nil {
		t.Fatalf("exact 93-day interval rejected: %v", err)
	}
}

func TestExportFilterBindingsAndTampering83(t *testing.T) {
	c := exportCodec83(t)
	r := freezeRequest83()
	raw, err := c.Freeze(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	a := access83(r, raw)
	changes := []func(*repport.SourceJobAccess){
		func(a *repport.SourceJobAccess) { a.ArgsVersion++ },
		func(a *repport.SourceJobAccess) { a.TenantID = ids.New() },
		func(a *repport.SourceJobAccess) { a.JobID = ids.New() },
		func(a *repport.SourceJobAccess) { a.RequestedBy = ids.New() },
		func(a *repport.SourceJobAccess) { a.RequestedAt = a.RequestedAt.Add(time.Microsecond) },
		func(a *repport.SourceJobAccess) { a.Source = "kn.other" },
		func(a *repport.SourceJobAccess) { a.TemplateKey = "kn.other" },
		func(a *repport.SourceJobAccess) { a.Format = "pdf" },
		func(a *repport.SourceJobAccess) { a.Scope.All = true },
		func(a *repport.SourceJobAccess) { a.Scope.Units = []string{"01"} },
		func(a *repport.SourceJobAccess) {
			a.SourceArgs = bytes.ReplaceAll(raw, []byte(`"version":1`), []byte(`"version":1e100000`))
		},
		func(a *repport.SourceJobAccess) {
			a.SourceArgs = bytes.ReplaceAll(raw, []byte(`"version":1`), []byte(`"version":1e-100000`))
		},
		func(a *repport.SourceJobAccess) {
			a.SourceArgs = append(raw[:len(raw)-1:len(raw)-1], []byte(`,"version":1}`)...)
		},
	}
	for i, change := range changes {
		bad := a
		change(&bad)
		if _, err := c.Decode(bad); err == nil {
			t.Errorf("tampered binding %d accepted", i)
		}
	}
	if _, err := knaudit.NewExportFilterCodec(nil); err == nil {
		t.Fatal("missing registry accepted")
	}
}

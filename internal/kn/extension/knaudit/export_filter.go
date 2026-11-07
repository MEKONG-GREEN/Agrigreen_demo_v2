package knaudit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"time"
	"unicode/utf8"

	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	repport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/google/uuid"
)

const (
	ExportSource      = "kn.audit_export"
	ExportTemplate    = "kn.audit_log"
	ExportArgsVersion = 1
	maxExportArgs     = 8 * 1024
)

// ExportFilterCodec uses the same immutable source registry as the audit viewer.
// It validates filters and server bindings; it does not grant job authority.
type ExportFilterCodec struct{ registry *accport.AuditRegistry }

func NewExportFilterCodec(registry *accport.AuditRegistry) (*ExportFilterCodec, error) {
	if len(registry.Sources()) == 0 {
		return nil, badExportFilter()
	}
	return &ExportFilterCodec{registry: registry}, nil
}

// FrozenExportFilter contains only bounded code/ref filters and server metadata.
// Reports persists it in the private job envelope, never in a public Params DTO.
type FrozenExportFilter struct {
	Version     int       `json:"version"`
	Source      string    `json:"source"`
	TemplateKey string    `json:"template_key"`
	Format      string    `json:"format"`
	TenantID    uuid.UUID `json:"tenant_id"`
	JobID       uuid.UUID `json:"job_id"`
	RequestedBy uuid.UUID `json:"requested_by"`
	AsOf        time.Time `json:"as_of"`
	From        time.Time `json:"from"`
	To          time.Time `json:"to"`
	ActorRef    uuid.UUID `json:"actor_ref"`
	Department  string    `json:"department,omitempty"`
	Module      string    `json:"module,omitempty"`
	Action      string    `json:"action,omitempty"`
	Sources     []string  `json:"sources"`
}

func badExportFilter() error {
	return problem.Validation.WithField("source_args", "bộ lọc nhật ký không hợp lệ")
}

// exportObject rejects duplicate keys, null fields and unknown/case-folded keys
// before typed decoding. This schema permits no nested objects or numeric input.
func exportObject(raw []byte, fields ...string) (map[string]json.RawMessage, error) {
	if len(raw) == 0 || len(raw) > maxExportArgs || !utf8.Valid(raw) {
		return nil, badExportFilter()
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	tok, err := d.Token()
	if err != nil || tok != json.Delim('{') {
		return nil, badExportFilter()
	}
	out := map[string]json.RawMessage{}
	for d.More() {
		tok, err := d.Token()
		if err != nil {
			return nil, badExportFilter()
		}
		key, ok := tok.(string)
		if !ok || !slices.Contains(fields, key) {
			return nil, badExportFilter()
		}
		if _, exists := out[key]; exists {
			return nil, badExportFilter()
		}
		var value json.RawMessage
		if err = d.Decode(&value); err != nil || bytes.Equal(value, []byte("null")) {
			return nil, badExportFilter()
		}
		out[key] = value
	}
	if tok, err = d.Token(); err != nil || tok != json.Delim('}') {
		return nil, badExportFilter()
	}
	if _, err = d.Token(); !errors.Is(err, io.EOF) {
		return nil, badExportFilter()
	}
	return out, nil
}

// Freeze accepts only the viewer's filter fields. All job identity and AsOf
// values come from the server, including when reports replays an existing job.
func (c *ExportFilterCodec) Freeze(_ context.Context, r repport.SourceFreezeRequest) (json.RawMessage, error) {
	if c == nil || c.registry == nil || r.Source != ExportSource || r.TemplateKey != ExportTemplate || r.Format != "xlsx" ||
		r.TenantID == uuid.Nil || r.JobID == uuid.Nil || r.RequestedBy == uuid.Nil || r.RequestedAt.IsZero() {
		return nil, badExportFilter()
	}
	fields, err := exportObject(r.SourceArgs, "from", "to", "actor_ref", "department", "module", "action", "sources")
	if err != nil {
		return nil, err
	}
	var in struct {
		From, To                   time.Time
		ActorRef                   string `json:"actor_ref"`
		Department, Module, Action string
		Sources                    []string
	}
	if err = json.Unmarshal(r.SourceArgs, &in); err != nil {
		return nil, badExportFilter()
	}
	actor := uuid.Nil
	if _, supplied := fields["actor_ref"]; supplied {
		if len(in.ActorRef) != 36 {
			return nil, badExportFilter()
		}
		if actor, err = uuid.Parse(in.ActorRef); err != nil || actor == uuid.Nil {
			return nil, badExportFilter()
		}
	}
	f := FrozenExportFilter{Version: ExportArgsVersion, Source: r.Source, TemplateKey: r.TemplateKey, Format: r.Format,
		TenantID: r.TenantID, JobID: r.JobID, RequestedBy: r.RequestedBy, AsOf: r.RequestedAt.UTC().Truncate(time.Microsecond),
		From: in.From.UTC().Truncate(time.Microsecond), To: in.To.UTC().Truncate(time.Microsecond), ActorRef: actor,
		Department: in.Department, Module: in.Module, Action: in.Action, Sources: in.Sources}
	_, supplied := fields["sources"]
	if err = c.normalize(&f, !supplied); err != nil {
		return nil, err
	}
	frozen, err := json.Marshal(f)
	if err != nil || len(frozen) > maxExportArgs {
		return nil, badExportFilter()
	}
	return frozen, nil
}

func (c *ExportFilterCodec) normalize(f *FrozenExportFilter, defaultSources bool) error {
	if c == nil || c.registry == nil || f.From.IsZero() || f.To.IsZero() || !f.To.After(f.From) || f.To.Sub(f.From) > maxWindow {
		return badExportFilter()
	}
	for _, value := range []string{f.Department, f.Module, f.Action} {
		if value != "" && !code.MatchString(value) {
			return badExportFilter()
		}
	}
	allowed := []string{}
	for _, source := range c.registry.Sources() {
		if f.Module == "" || f.Module == source.Module {
			allowed = append(allowed, source.Key)
		}
	}
	if len(allowed) == 0 {
		return badExportFilter()
	}
	if defaultSources {
		f.Sources = allowed
	}
	if len(f.Sources) == 0 || len(f.Sources) > 32 {
		return badExportFilter()
	}
	f.Sources = slices.Clone(f.Sources)
	slices.Sort(f.Sources)
	for i, source := range f.Sources {
		if !slices.Contains(allowed, source) || (i > 0 && source == f.Sources[i-1]) {
			return badExportFilter()
		}
	}
	return nil
}

// Decode rechecks the private snapshot against the server's verified job.
// Tenant mode requires an empty geographic scope, never a synthetic Scope.All.
func (c *ExportFilterCodec) Decode(a repport.SourceJobAccess) (FrozenExportFilter, error) {
	var f FrozenExportFilter
	if a.ArgsVersion != ExportArgsVersion || a.Source != ExportSource || a.TemplateKey != ExportTemplate || a.Format != "xlsx" ||
		a.TenantID == uuid.Nil || a.JobID == uuid.Nil || a.RequestedBy == uuid.Nil || a.RequestedAt.IsZero() || a.Scope.All || len(a.Scope.Units) != 0 {
		return f, badExportFilter()
	}
	fields, err := exportObject(a.SourceArgs, "version", "source", "template_key", "format", "tenant_id", "job_id", "requested_by", "as_of",
		"from", "to", "actor_ref", "department", "module", "action", "sources")
	if err != nil {
		return f, err
	}
	for _, required := range []string{"version", "source", "template_key", "format", "tenant_id", "job_id", "requested_by", "as_of", "from", "to", "actor_ref", "sources"} {
		if _, present := fields[required]; !present {
			return f, badExportFilter()
		}
	}
	if err := json.Unmarshal(a.SourceArgs, &f); err != nil || f.Version != ExportArgsVersion || f.Source != a.Source || f.TemplateKey != a.TemplateKey ||
		f.Format != a.Format || f.TenantID != a.TenantID || f.JobID != a.JobID || f.RequestedBy != a.RequestedBy ||
		!f.AsOf.Equal(a.RequestedAt.UTC().Truncate(time.Microsecond)) || !f.AsOf.Equal(f.AsOf.Truncate(time.Microsecond)) ||
		!f.From.Equal(f.From.Truncate(time.Microsecond)) || !f.To.Equal(f.To.Truncate(time.Microsecond)) {
		return FrozenExportFilter{}, badExportFilter()
	}
	if err := c.normalize(&f, false); err != nil {
		return FrozenExportFilter{}, err
	}
	f.From, f.To, f.AsOf = f.From.UTC(), f.To.UTC(), f.AsOf.UTC()
	return f, nil
}

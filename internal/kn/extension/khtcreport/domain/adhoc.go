package domain

import (
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
)

// AdhocSelection stores identities, never user-supplied numeric values. Available values
// remain in the frozen input snapshot; the selection is versioned with the draft/export.
type AdhocSelection struct {
	Rows    []string `json:"rows"`
	Metrics []string `json:"metrics"`
}

const MaxAdhocFigures = 200

func (s *AdhocSelection) Clone() *AdhocSelection {
	if s == nil {
		return nil
	}
	return &AdhocSelection{Rows: append([]string{}, s.Rows...), Metrics: append([]string{}, s.Metrics...)}
}
func (s AdhocSelection) Validate() error {
	if len(s.Rows)+len(s.Metrics) == 0 || len(s.Rows)+len(s.Metrics) > MaxAdhocFigures {
		return fmt.Errorf("%w: chọn 1–200 chỉ tiêu", ErrInput)
	}
	seen := map[string]bool{}
	for _, id := range s.Rows {
		if len(id) != 36 {
			return ErrInput
		}
		if _, err := uuid.Parse(id); err != nil || seen["row:"+id] {
			return ErrInput
		}
		seen["row:"+id] = true
	}
	for _, key := range s.Metrics {
		if len(key) == 0 || len(key) > 512 || strings.ContainsRune(key, 0) || seen["metric:"+key] {
			return ErrInput
		}
		seen["metric:"+key] = true
	}
	return nil
}
func (s AdhocSelection) ValidateData(d Data) error {
	if d.Template != "adhoc" {
		return ErrInput
	}
	if err := s.Validate(); err != nil {
		return err
	}
	rows := map[string]bool{}
	metrics := map[string]bool{}
	for _, r := range d.Rows {
		if r.Kind == "item" {
			rows[r.Ref.String()] = true
		}
	}
	for _, m := range d.Metrics {
		metrics[m.Key] = true
	}
	for _, id := range s.Rows {
		if !rows[id] {
			return fmt.Errorf("%w: hạng mục không còn trong ảnh chụp", ErrInput)
		}
	}
	for _, key := range s.Metrics {
		if !metrics[key] {
			return fmt.Errorf("%w: chỉ tiêu không có nguồn trong ảnh chụp", ErrInput)
		}
	}
	return nil
}

// AdhocData is the report view of a frozen draft. It never alters the source snapshot
// or retains unselected external figures/text from the fixed five report layouts.
func AdhocData(d Draft) Data {
	out := d.Data
	out.Rows = []ProgramRow{}
	out.Metrics = []SourceMetric{}
	out.External = nil
	out.Provisional = []string{}
	for _, r := range out.Supplements {
		if r.State == "intent" || r.State == "issued" {
			out.Provisional = append(out.Provisional, "Yêu cầu bổ sung chưa hoàn tất: "+r.Department)
		}
	}
	if d.Adhoc == nil {
		out.Provisional = []string{"Chưa chọn chỉ tiêu"}
		return out
	}
	for _, r := range d.Data.Rows {
		if slices.Contains(d.Adhoc.Rows, r.Ref.String()) {
			out.Rows = append(out.Rows, r)
			if r.Missing {
				out.Provisional = append(out.Provisional, "Hạng mục chưa có số liệu: "+r.Name)
			}
		}
	}
	for _, m := range d.Data.Metrics {
		if slices.Contains(d.Adhoc.Metrics, m.Key) {
			out.Metrics = append(out.Metrics, m)
			if m.Value.Provisional() {
				out.Provisional = append(out.Provisional, "Nguồn chưa chốt: "+m.Label)
			}
		}
	}
	return out
}

type AdhocFigure struct {
	Name, Unit, Value, Source string
	Literal                   bool
}

func AdhocFigures(d Data) []AdhocFigure {
	out := make([]AdhocFigure, 0, len(d.Rows)+len(d.Metrics))
	for _, r := range d.Rows {
		out = append(out, AdhocFigure{r.Name, r.Unit, r.InPeriod, fmt.Sprintf("Phiếu đã gửi · %s · kế hoạch %d/%s", r.Department, d.Plan.Year, d.Plan.VersionID), false})
	}
	for _, m := range d.Metrics {
		value := "—"
		v := m.Value
		switch {
		case m.Text != "":
			value = m.Text
		case v.Min != nil && v.Max != nil:
			value = v.Min.String() + " – " + v.Max.String()
		case m.Stock && v.Cumulative != nil:
			value = v.Cumulative.String()
		case v.Increment != nil:
			value = v.Increment.String()
		case v.Sum != nil:
			value = v.Sum.String()
		case v.Cumulative != nil:
			value = v.Cumulative.String()
		}
		out = append(out, AdhocFigure{m.Label, m.Unit, value, m.Department + " · " + strings.Join(v.SourceKeys, ", "), m.Text != ""})
	}
	return out
}

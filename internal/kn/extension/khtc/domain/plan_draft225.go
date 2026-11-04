package domain

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"strings"
)

// Bound resource work before EntriesHash invokes Decimal.String, even for direct callers.
func CheckDraftResource225(es []Entry) error {
	if len(es) > MaxEntries {
		return Errors{{Field: "entries", Reason: "tối đa 5000 dòng"}}
	}
	var bad Errors
	for i, e := range es {
		f := func(name string) string { return fmt.Sprintf("entries.%d.%s", i, name) }
		for _, n := range []struct {
			name         string
			value        *decimal.Decimal
			scale, power int32
		}{{"planned_qty", e.PlannedQty, 4, 16}, {"budget", e.Budget, 2, 18}} {
			v := n.value
			if v == nil {
				continue
			}
			if v.Exponent() < -32 || v.Exponent() > 32 || v.Coefficient().BitLen() > 128 {
				bad.add(f(n.name), "số thập phân vượt giới hạn")
				continue
			}
			// Existing NormalizePlan rules, after the cheap exponent/coefficient guard.
			if v.IsNegative() || !v.Equal(v.Round(n.scale)) || v.GreaterThanOrEqual(decimal.New(1, n.power)) {
				bad.add(f(n.name), "số không âm, đúng độ chính xác và giới hạn nghiệp vụ")
			}
		}
		if e.SortOrder < 0 || e.SortOrder > 100000 {
			bad.add(f("sort_order"), "0–100000")
		}
		if e.PointsCount != nil && (*e.PointsCount < 0 || *e.PointsCount > 100000) {
			bad.add(f("points_count"), "0–100000")
		}
		for _, t := range []struct {
			name, value string
			max         int
		}{{"code", e.Code, 160}, {"name", e.Name, 4000}, {"unit_code", e.UnitCode, 160}, {"scale_note", e.ScaleNote, 4000}, {"funding_code", e.FundingCode, 160}, {"field_code", e.FieldCode, 160}, {"department_code", e.Department, 160}, {"data_source", e.DataSource, 40}, {"source_key", e.SourceKey, 400}, {"stop_decision", e.StopDecision, 400}, {"note", e.Note, 8000}} {
			if strings.IndexByte(t.value, 0) >= 0 {
				bad.add(f(t.name), "không nhận ký tự NUL")
			} else if len(t.value) > t.max {
				bad.add(f(t.name), "nội dung vượt giới hạn")
			}
		}
		if len(e.Communes) > 500 {
			bad.add(f("communes"), "tối đa 500 xã")
		}
		for _, c := range e.Communes {
			if strings.IndexByte(c, 0) >= 0 || len(c) > 40 {
				bad.add(f("communes"), "mã xã vượt giới hạn")
				break
			}
		}
	}
	if len(bad) > 0 {
		return bad
	}
	return nil
}
func DraftRequestHash225(year int, generation uuid.UUID, revision int64, action, note string, es []Entry) ([]byte, error) {
	if len(note) > 4000 || strings.IndexByte(note, 0) >= 0 {
		return nil, errors.New("draft note resource bound")
	}
	if err := CheckDraftResource225(es); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(struct {
		Year         int
		Generation   uuid.UUID
		Revision     int64
		Action, Note string
		EntriesHash  []byte
	}{year, generation, revision, action, note, EntriesHash(es)})
	if err != nil {
		return nil, err
	}
	h := sha256.Sum256(raw)
	return h[:], nil
}

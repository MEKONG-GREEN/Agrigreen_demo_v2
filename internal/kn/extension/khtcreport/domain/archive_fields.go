package domain

import (
	"bytes"
	"encoding/json"
	"regexp"
	"slices"
	"sort"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/google/uuid"
)

const maxArchiveMetadataBytes = 32 << 20

var archiveFieldCode = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,39}$`)

type ArchiveFields struct {
	Codes  []string `json:"field_codes"`
	Status string   `json:"field_status"`
	// AllRows counts the frozen unfiltered array, exactly the existing egress
	// contract. Unknown is explicit and must never be substituted with zero.
	AllRows       int  `json:"-"`
	RowCountKnown bool `json:"-"`
}
type ArchiveFieldSelector struct{ Code, Status string }

func (f ArchiveFieldSelector) Valid() bool {
	return (f.Code == "" || (len(f.Code) <= 40 && archiveFieldCode.MatchString(f.Code))) && slices.Contains([]string{"", "complete", "partial", "unknown"}, f.Status)
}
func (f ArchiveFieldSelector) Matches(meta ArchiveFields) bool {
	return (f.Code == "" || slices.Contains(meta.Codes, f.Code)) && (f.Status == "" || f.Status == meta.Status)
}

// These projections intentionally do not declare money, quantity, metrics,
// PlannedQty, Budget, dates or any other unused numeric field. Legacy numeric
// lexemes are scanned as JSON and never passed to a decimal/float parser.
type archivePlanNode struct {
	Ref             uuid.UUID
	ParentRef       *uuid.UUID
	Kind, FieldCode string
}
type archiveRowNode struct {
	Ref        uuid.UUID  `json:"ref"`
	ParentRef  *uuid.UUID `json:"parent_ref"`
	ProgramRef *uuid.UUID `json:"program_ref"`
	Kind       string     `json:"kind"`
	Department string     `json:"department"`
	Level      string     `json:"level"`
	Communes   []string   `json:"communes"`
}

func ProjectArchiveFields(payload, filterJSON []byte) ArchiveFields {
	out := ArchiveFields{Codes: []string{}, Status: "unknown"}
	if len(payload) > maxArchiveMetadataBytes || len(filterJSON) > 4096 {
		return out
	}
	var frozen struct {
		Data *struct {
			Template string                             `json:"template"`
			Rows     json.RawMessage                    `json:"rows"`
			Plan     *struct{ Entries json.RawMessage } `json:"plan"`
		} `json:"data"`
		Adhoc *AdhocSelection `json:"adhoc"`
	}
	if json.Unmarshal(payload, &frozen) != nil || frozen.Data == nil {
		return out
	}
	var rawRows []json.RawMessage
	if len(frozen.Data.Rows) == 0 || bytes.Equal(bytes.TrimSpace(frozen.Data.Rows), []byte("null")) || json.Unmarshal(frozen.Data.Rows, &rawRows) != nil {
		return out
	}
	out.AllRows = len(rawRows)
	out.RowCountKnown = true
	if frozen.Data.Plan == nil || len(frozen.Data.Plan.Entries) == 0 {
		return out
	}
	var rawPlan []json.RawMessage
	if json.Unmarshal(frozen.Data.Plan.Entries, &rawPlan) != nil {
		return out
	}
	entries := map[uuid.UUID]port.KHTCPlanEntry{}
	duplicates := map[uuid.UUID]bool{}
	for _, raw := range rawPlan {
		var e archivePlanNode
		if json.Unmarshal(raw, &e) != nil || e.Ref == uuid.Nil {
			continue
		}
		if _, exists := entries[e.Ref]; exists {
			duplicates[e.Ref] = true
		}
		entries[e.Ref] = port.KHTCPlanEntry{Ref: e.Ref, ParentRef: e.ParentRef, Kind: e.Kind, FieldCode: e.FieldCode}
	}
	for ref := range duplicates {
		delete(entries, ref)
	}
	rows := make([]ProgramRow, 0, len(rawRows))
	uncertain := false
	for _, raw := range rawRows {
		var row archiveRowNode
		if json.Unmarshal(raw, &row) != nil || row.Ref == uuid.Nil {
			uncertain = true
			continue
		}
		if row.Kind != "item" && row.Kind != "group" && row.Kind != "program" {
			uncertain = true
			continue
		}
		rows = append(rows, ProgramRow{Ref: row.Ref, ParentRef: row.ParentRef, ProgramRef: row.ProgramRef, Kind: row.Kind, Department: row.Department, Level: row.Level, Communes: row.Communes})
	}
	var filter Filter
	if json.Unmarshal(filterJSON, &filter) != nil || filter.Validate() != nil {
		return out
	}
	if frozen.Data.Template == "adhoc" {
		rows = AdhocData(Draft{Data: Data{Template: "adhoc", Rows: rows}, Adhoc: frozen.Adhoc}).Rows
	}
	selected := SelectRows(rows, filter)
	fields := map[string]bool{}
	items := 0
	for _, row := range selected {
		if row.Kind != "item" {
			continue
		}
		items++
		entry, ok := entries[row.Ref]
		if !ok || entry.Kind != "item" {
			uncertain = true
			continue
		}
		code := InheritedFieldCode(entry, entries)
		if len(code) > 40 || !archiveFieldCode.MatchString(code) {
			uncertain = true
			continue
		}
		fields[code] = true
	}
	for code := range fields {
		out.Codes = append(out.Codes, code)
	}
	sort.Strings(out.Codes)
	if len(out.Codes) > 0 {
		out.Status = "complete"
		if uncertain || items == 0 {
			out.Status = "partial"
		}
	}
	return out
}

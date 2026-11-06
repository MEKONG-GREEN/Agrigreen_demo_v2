package domain

import (
	"encoding/json"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Reuse the existing frozen-metadata byte bound; this reader never decodes the
// plan's decimal objects or the narrative/PII parts of a report.
const PNProgressPayloadBytes215 = maxArchiveMetadataBytes
const pnProgressRows215 = 10000

var ErrPNProgramScope215 = errors.New("programme outside the current department")

type PNProgressRow215 struct {
	Ref        uuid.UUID  `json:"ref"`
	ParentRef  *uuid.UUID `json:"parent_ref"`
	Kind       string     `json:"kind"`
	Roman      string     `json:"roman"`
	Name       string     `json:"name"`
	Department string     `json:"department,omitempty"`
	Unit       string     `json:"unit"`
	Planned    *string    `json:"planned"`
	YearToDate string     `json:"year_to_date"`
	Percent    *string    `json:"percent"`
	Level      string     `json:"level"`
	Missing    bool       `json:"missing"`
}

type PNProgram215 struct {
	Ref   uuid.UUID `json:"ref"`
	Name  string    `json:"name"`
	Roman string    `json:"roman"`
}

type PNProjection215 struct {
	Period     Period
	CapturedAt time.Time
	InputHash  string
	Rows       []PNProgressRow215
	Programs   []PNProgram215
}

func plainPNNumber215(raw string) bool {
	if len(raw) == 0 || len(raw) > 96 {
		return false
	}
	i := 0
	if raw[0] == '-' {
		i++
	}
	whole, fraction, dot := 0, 0, false
	for ; i < len(raw); i++ {
		if raw[i] == '.' && !dot && whole > 0 {
			dot = true
			continue
		}
		if raw[i] < '0' || raw[i] > '9' {
			return false
		}
		if dot {
			fraction++
		} else {
			whole++
		}
	}
	return whole > 0 && (!dot || fraction > 0)
}

// ProjectPNProgress215 retains only own items and the labels of their ancestors.
// Ancestor quantities, other departments, source payloads and actor data never
// become part of the response or the derived workbook.
func ProjectPNProgress215(payload []byte, department string, filter Filter) (PNProjection215, error) {
	out := PNProjection215{Rows: []PNProgressRow215{}, Programs: []PNProgram215{}}
	if department == "" || len(payload) > PNProgressPayloadBytes215 || filter.Validate() != nil {
		return out, ErrInput
	}
	var envelope struct {
		Data struct {
			Period     Period       `json:"period"`
			Template   string       `json:"template"`
			Rows       []ProgramRow `json:"rows"`
			CapturedAt time.Time    `json:"captured_at"`
			InputHash  string       `json:"input_hash"`
		} `json:"data"`
	}
	if json.Unmarshal(payload, &envelope) != nil {
		return out, ErrInput
	}
	d := envelope.Data
	if d.Template != "mau_5" || d.Period.Type != "program" || d.Period.ID == uuid.Nil || d.Period.Year < 2000 || d.Period.Year > 2100 || d.CapturedAt.IsZero() || len(d.Rows) > pnProgressRows215 {
		return out, ErrInput
	}
	seen := map[uuid.UUID]bool{}
	for _, r := range d.Rows {
		if r.Ref == uuid.Nil || seen[r.Ref] || !utf8.ValidString(r.Name) || len(r.Name) > 2048 {
			return out, ErrInput
		}
		seen[r.Ref] = true
	}
	for _, r := range SelectRows(d.Rows, Filter{Department: department}) {
		if r.Kind == "program" {
			out.Programs = append(out.Programs, PNProgram215{Ref: r.Ref, Name: r.Name, Roman: r.Roman})
		}
	}
	if filter.Program != "" {
		allowed := false
		for _, p := range out.Programs {
			if p.Ref.String() == filter.Program {
				allowed = true
				break
			}
		}
		if !allowed {
			return out, ErrPNProgramScope215
		}
	}
	filter.Department = department
	for _, r := range SelectRows(d.Rows, filter) {
		row := PNProgressRow215{Ref: r.Ref, ParentRef: r.ParentRef, Kind: r.Kind, Roman: r.Roman, Name: r.Name}
		if r.Kind == "item" {
			if r.Department != department {
				return out, ErrInput
			}
			if !plainPNNumber215(r.YearToDate) || r.Planned != nil && !plainPNNumber215(*r.Planned) || r.Percent != nil && !plainPNNumber215(*r.Percent) {
				return out, ErrInput
			}
			row.Department, row.Unit, row.Planned, row.YearToDate, row.Percent, row.Level, row.Missing = r.Department, r.Unit, r.Planned, r.YearToDate, r.Percent, r.Level, r.Missing
		}
		out.Rows = append(out.Rows, row)
	}
	out.Period, out.CapturedAt, out.InputHash = d.Period, d.CapturedAt, d.InputHash
	return out, nil
}

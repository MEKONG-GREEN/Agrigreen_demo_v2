// Package excel supplies the technical memory-only KHTC form XLSX codec.
// It has no published-schema producer, authorization or file-provenance owner.
package excel

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
)

var (
	ErrSnapshot          = errors.New("khtc form excel: invalid server schema snapshot")
	ErrWorkbook          = errors.New("khtc form excel: workbook does not match the pinned schema")
	ErrLimit             = errors.New("khtc form excel: workbook exceeds bounds")
	ErrPreview           = errors.New("khtc form excel: preview does not belong to this snapshot")
	ErrFormulaProjection = errors.New("khtc form excel: Excel expression projection is not supported")
)

// Limits are fixed technical bounds, not tenant configuration. Numeric values
// use the manual form's ordinary-decimal syntax (15 integer/6 fraction digits;
// integer fields 9 digits) before any decimal operation.
const (
	MaxWorkbookBytes = 5 << 20
	MaxUnzipBytes    = 32 << 20
	MaxPartBytes     = 16 << 20
	MaxZipFiles      = 512
	MaxSheets        = 32
	MaxColumns       = 64
	MaxRows          = 2000
	MaxAdjustments   = 200
	MaxCellBytes     = 32000
	MaxTextRunes     = 8000
	maxXMLRows       = MaxRows + MaxAdjustments + 16
	maxXMLCells      = (MaxRows + MaxAdjustments + MaxSheets*8) * (MaxColumns + 1)
	firstDataRow     = 4
	magic            = "AG-KHTC-FORM-EXCEL/1"
	adjustmentMarker = "AG-KHTC-ADJUSTMENTS/1"
	endMarker        = "AG-KHTC-END/1"
)

type sheetSpec struct {
	def        port.KHTCFormExcelSheet
	columns    map[string]port.KHTCFormExcelColumn
	rows       map[string]port.KHTCFormExcelRow
	lastColumn int
}

// Codec pins a deep copy of an explicit owner projection. It performs no lookup
// and never interprets an ID/hash as proof that a template was published.
type Codec struct {
	snapshot port.KHTCFormExcelSnapshot
	hash     string
	sheets   map[string]sheetSpec
	periods  map[uuid.UUID]bool
}

// New validates bounds before copying or hashing the supplied server projection.
// Formula columns must explicitly opt into the server_value subset; requests
// for expression fidelity fail rather than silently converting an expression.
func New(snapshot port.KHTCFormExcelSnapshot) (*Codec, error) {
	if err := validateSnapshot(snapshot); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return nil, ErrSnapshot
	}
	var copied port.KHTCFormExcelSnapshot
	if err := json.Unmarshal(raw, &copied); err != nil {
		return nil, ErrSnapshot
	}
	hash := sha256.Sum256(raw)
	c := &Codec{snapshot: copied, hash: hex.EncodeToString(hash[:]), sheets: map[string]sheetSpec{}, periods: map[uuid.UUID]bool{}}
	for _, p := range copied.Periods {
		c.periods[p.ID] = true
	}
	for _, sh := range copied.Sheets {
		spec := sheetSpec{def: sh, columns: map[string]port.KHTCFormExcelColumn{}, rows: map[string]port.KHTCFormExcelRow{}}
		for _, col := range sh.Columns {
			spec.columns[col.Letter] = col
			spec.lastColumn, _ = excelize.ColumnNameToNumber(col.Letter)
		}
		for _, row := range sh.Rows {
			spec.rows[row.Code] = row
		}
		c.sheets[sh.Key] = spec
	}
	return c, nil
}

func (c *Codec) SnapshotHash() string {
	if c == nil {
		return ""
	}
	return c.hash
}

func (c *Codec) initialized() bool {
	return c != nil && c.hash != "" && len(c.sheets) > 0
}

func validText(value string, maxRunes int) bool {
	if len(value) > MaxCellBytes || !utf8.ValidString(value) || utf8.RuneCountInString(value) > maxRunes {
		return false
	}
	for _, r := range value {
		if r < 32 && r != '\t' && r != '\r' && r != '\n' {
			return false
		}
	}
	return true
}

func identifier(value string) bool {
	return value != "" && value == strings.TrimSpace(value) && !strings.ContainsAny(value, "\r\n\t") && validText(value, 128)
}

func validateSnapshot(s port.KHTCFormExcelSnapshot) error {
	if s.ID == uuid.Nil || s.TenantID == uuid.Nil || s.PeriodID == uuid.Nil || !identifier(s.Department) || len(s.Sheets) < 1 || len(s.Sheets) > MaxSheets || len(s.Periods) > MaxAdjustments {
		return ErrSnapshot
	}
	budget, rows := 0, 0
	add := func(value string) bool {
		budget += len(value)
		return budget <= MaxPartBytes && validText(value, MaxTextRunes)
	}
	keys, names, periods := map[string]bool{}, map[string]bool{}, map[uuid.UUID]bool{}
	for _, p := range s.Periods {
		if p.ID == uuid.Nil || periods[p.ID] || !add(p.Name) {
			return ErrSnapshot
		}
		periods[p.ID] = true
	}
	for _, sh := range s.Sheets {
		lowerName := strings.ToLower(sh.Name)
		if !identifier(sh.Key) || keys[sh.Key] || names[lowerName] || !validText(sh.Name, 31) || sh.Name == "" || strings.ContainsAny(sh.Name, "[]:*?/\\") || strings.HasPrefix(sh.Name, "'") || strings.HasSuffix(sh.Name, "'") || strings.EqualFold(sh.Name, "History") || sh.TemplateID == uuid.Nil || sh.TemplateVersion < 1 || len(sh.TemplateHash) != 64 {
			return ErrSnapshot
		}
		if _, err := hex.DecodeString(sh.TemplateHash); err != nil || sh.TemplateHash != strings.ToLower(sh.TemplateHash) {
			return ErrSnapshot
		}
		if (sh.Kind != "grid" && sh.Kind != "narrative") || (sh.Kind == "narrative" && sh.AllowAdjustments) || len(sh.Columns) < 1 || len(sh.Columns) > MaxColumns {
			return ErrSnapshot
		}
		keys[sh.Key], names[lowerName] = true, true
		rows += len(sh.Rows)
		if rows > MaxRows {
			return ErrSnapshot
		}
		columns := map[string]port.KHTCFormExcelColumn{}
		last := 0
		for _, col := range sh.Columns {
			n, err := excelize.ColumnNameToNumber(col.Letter)
			if err != nil || n <= last || n > MaxColumns || col.Letter != strings.ToUpper(col.Letter) || col.Title == "" || !add(col.Title) || col.MaxLength < 0 || col.MaxLength > MaxTextRunes || len(col.Choices) > 100 {
				return ErrSnapshot
			}
			last = n
			if !slices.Contains([]string{"fixed", "from_plan", "prior_cumulative", "input", "formula"}, col.Kind) || !slices.Contains([]string{"number", "integer", "text"}, col.DataType) {
				return ErrSnapshot
			}
			if col.Kind == "formula" {
				if col.FormulaMode != "server_value" {
					return ErrFormulaProjection
				}
			} else if col.FormulaMode != "" {
				return ErrSnapshot
			}
			choices := map[string]bool{}
			for _, choice := range col.Choices {
				if choice == "" || choice != strings.TrimSpace(choice) || choices[choice] || !add(choice) {
					return ErrSnapshot
				}
				choices[choice] = true
			}
			columns[col.Letter] = col
		}
		codes := map[string]bool{}
		for _, row := range sh.Rows {
			if !identifier(row.Code) || codes[row.Code] || !add(row.Code) || len(row.SystemValues) > len(columns) || len(row.Locked) > len(columns) {
				return ErrSnapshot
			}
			codes[row.Code] = true
			locked := map[string]bool{}
			for _, letter := range row.Locked {
				col, ok := columns[letter]
				if !ok || col.Kind != "input" || locked[letter] {
					return ErrSnapshot
				}
				locked[letter] = true
			}
			for letter, value := range row.SystemValues {
				col, ok := columns[letter]
				if !ok || (col.Kind == "input" && !locked[letter]) || !add(value) {
					return ErrSnapshot
				}
				if col.Kind == "formula" && value != "" && ((col.DataType == "number" && !projectedNumber.MatchString(value)) || (col.DataType == "integer" && !projectedInteger.MatchString(value))) {
					return ErrFormulaProjection
				}
			}
			for letter, col := range columns {
				if col.Kind != "input" || locked[letter] {
					if _, ok := row.SystemValues[letter]; !ok {
						return ErrSnapshot
					}
				}
			}
		}
	}
	return nil
}

func editable(col port.KHTCFormExcelColumn, row port.KHTCFormExcelRow) bool {
	return col.Kind == "input" && !slices.Contains(row.Locked, col.Letter)
}

// validateContentShape bounds detached editor values without making a draft's
// currently invalid field values disappear. Preview applies the full field rules.
func (c *Codec) validateContentShape(content port.KHTCFormExcelContent) error {
	if len(content.Rows) > len(c.sheets) || len(content.Adjustments) > MaxAdjustments {
		return ErrLimit
	}
	rows, budget := 0, 0
	for key, values := range content.Rows {
		spec, ok := c.sheets[key]
		if !ok || len(values) > len(spec.rows) {
			return ErrSnapshot
		}
		rows += len(values)
		if rows > MaxRows {
			return ErrLimit
		}
		for code, cells := range values {
			row, ok := spec.rows[code]
			if !ok || len(cells) > len(spec.columns) {
				return ErrSnapshot
			}
			for letter, value := range cells {
				col, ok := spec.columns[letter]
				budget += len(value)
				if !ok || !editable(col, row) {
					return ErrSnapshot
				}
				if budget > MaxPartBytes || !validText(value, MaxTextRunes) {
					return ErrLimit
				}
			}
		}
	}
	for _, a := range content.Adjustments {
		spec, ok := c.sheets[a.Sheet]
		_, rowExists := spec.rows[a.ItemCode]
		if !ok || !spec.def.AllowAdjustments || !rowExists {
			return ErrSnapshot
		}
		if !validText(a.Amount, MaxTextRunes) || !validText(a.Reason, MaxTextRunes) {
			return ErrLimit
		}
	}
	return nil
}

var _ port.KHTCFormExcelCodec = (*Codec)(nil)

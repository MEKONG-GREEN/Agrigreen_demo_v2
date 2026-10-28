package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const MaxSourceArgsBytes = 16 * 1024

var ErrSourceArgs = errors.New("reports: invalid private source arguments")

// FrozenSourceArgs is stored with the job, never serialized in any public Job DTO.
// Binding prevents moving a valid owner's snapshot to another job or tenant.
type FrozenSourceArgs struct {
	TemplateKey     string          `json:"template_key"`
	TemplateVersion int             `json:"template_version"`
	Format          string          `json:"format"`
	Source          string          `json:"source"`
	Version         int             `json:"version"`
	JobID           uuid.UUID       `json:"job_id"`
	TenantID        uuid.UUID       `json:"tenant_id"`
	RequestedBy     uuid.UUID       `json:"requested_by"`
	RequestedAt     time.Time       `json:"requested_at"`
	Args            json.RawMessage `json:"args"`
}

func (j Job) ValidateSourceArgs() error {
	f := j.SourceArgs
	if f == nil {
		return nil
	}
	if f.Source == "" || f.Source != j.Def.Source || f.Version < 1 || f.JobID == uuid.Nil || f.JobID != j.ID ||
		f.TenantID == uuid.Nil || f.TenantID != j.TenantID || f.RequestedBy == uuid.Nil || f.RequestedBy != j.RequestedBy ||
		f.RequestedAt.IsZero() || !f.RequestedAt.Equal(j.RequestedAt) || f.TemplateKey != j.TemplateKey || f.TemplateVersion != j.TemplateVersion || f.Format != j.Format {
		return ErrSourceArgs
	}
	_, err := CanonicalSourceArgs(f.Args)
	return err
}

// CanonicalSourceArgs rejects duplicate keys recursively, reserved private envelopes,
// non-object roots, excessive depth and oversized values before the owner sees them.
func CanonicalSourceArgs(raw json.RawMessage) (json.RawMessage, error) {
	return canonicalObject(raw, MaxSourceArgsBytes, false)
}
func canonicalObject(raw []byte, limit int, allowReserved bool) (json.RawMessage, error) {
	return canonicalObjectBudget(raw, limit, limit, allowReserved)
}
func canonicalObjectBudget(raw []byte, inputLimit, limit int, allowReserved bool) (json.RawMessage, error) {
	if len(raw) == 0 || len(raw) > inputLimit || !utf8.Valid(raw) {
		return nil, ErrSourceArgs
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	expansionExtra := int64(0)
	var parse func(int) (any, error)
	parse = func(depth int) (any, error) {
		if depth > 16 {
			return nil, ErrSourceArgs
		}
		tok, err := d.Token()
		if err != nil {
			return nil, ErrSourceArgs
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			if number, isNumber := tok.(json.Number); isNumber {
				normalized, expanded, err := canonicalJSONNumber(number)
				if err != nil {
					return nil, err
				}
				if extra := expanded - int64(len(normalized)); extra > 0 {
					expansionExtra += extra
				}
				if expansionExtra > int64(limit) {
					return nil, ErrSourceArgs
				}
				return normalized, nil
			}
			return tok, nil
		}
		switch delim {
		case '{':
			out := map[string]any{}
			for d.More() {
				key, e := d.Token()
				if e != nil {
					return nil, ErrSourceArgs
				}
				k, ok := key.(string)
				if !ok || (!allowReserved && k == "_reports_source") {
					return nil, ErrSourceArgs
				}
				if _, exists := out[k]; exists {
					return nil, ErrSourceArgs
				}
				v, e := parse(depth + 1)
				if e != nil {
					return nil, e
				}
				out[k] = v
			}
			end, e := d.Token()
			if e != nil || end != json.Delim('}') {
				return nil, ErrSourceArgs
			}
			return out, nil
		case '[':
			out := []any{}
			for d.More() {
				v, e := parse(depth + 1)
				if e != nil {
					return nil, e
				}
				out = append(out, v)
			}
			end, e := d.Token()
			if e != nil || end != json.Delim(']') {
				return nil, ErrSourceArgs
			}
			return out, nil
		default:
			return nil, ErrSourceArgs
		}
	}
	v, err := parse(0)
	if err != nil {
		return nil, err
	}
	if _, ok := v.(map[string]any); !ok {
		return nil, ErrSourceArgs
	}
	if _, err = d.Token(); !errors.Is(err, io.EOF) {
		return nil, ErrSourceArgs
	}
	b, err := json.Marshal(v)
	if err != nil || int64(len(b))+expansionExtra > int64(limit) {
		return nil, ErrSourceArgs
	}
	return b, nil
}

// canonicalJSONNumber normalizes exact JSON decimal values without float64.
// It checks the expanded length before allocating a bounded plain decimal.
func canonicalJSONNumber(number json.Number) (json.Number, int64, error) {
	text := number.String()
	sign := ""
	if strings.HasPrefix(text, "-") {
		sign = "-"
		text = text[1:]
	}
	exponent := int64(0)
	if i := strings.IndexAny(text, "eE"); i >= 0 {
		value, err := strconv.ParseInt(text[i+1:], 10, 32)
		if err != nil {
			return "", 0, ErrSourceArgs
		}
		exponent = value
		text = text[:i]
	}
	if i := strings.IndexByte(text, '.'); i >= 0 {
		exponent -= int64(len(text) - i - 1)
		text = text[:i] + text[i+1:]
	}
	coefficient := strings.TrimLeft(text, "0")
	if coefficient == "" {
		return json.Number("0"), 1, nil
	}
	short := strings.TrimRight(coefficient, "0")
	exponent += int64(len(coefficient) - len(short))
	coefficient = short
	point := int64(len(coefficient)) + exponent
	expanded := int64(len(sign))
	switch {
	case point <= 0:
		expanded += 2 - point + int64(len(coefficient))
	case point < int64(len(coefficient)):
		expanded += int64(len(coefficient)) + 1
	default:
		expanded += point
	}
	// Reject an exponent that PostgreSQL would expand beyond the snapshot budget,
	// before allocating any expanded string.
	if expanded > MaxSourceArgsBytes {
		return "", 0, ErrSourceArgs
	}
	var normalized string
	switch {
	case point <= 0:
		normalized = sign + "0." + strings.Repeat("0", int(-point)) + coefficient
	case point < int64(len(coefficient)):
		normalized = sign + coefficient[:int(point)] + "." + coefficient[int(point):]
	default:
		normalized = sign + coefficient + strings.Repeat("0", int(exponent))
	}
	return json.Number(normalized), expanded, nil
}

// MarshalJobParams preserves the native Params encoding when no policy is used.
func MarshalJobParams(j Job) ([]byte, error) {
	if err := j.ValidateSourceArgs(); err != nil {
		return nil, err
	}
	b, err := json.Marshal(j.Params)
	if err != nil || j.SourceArgs == nil {
		return b, err
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(b, &fields); err != nil {
		return nil, err
	}
	fields["_reports_source"], err = json.Marshal(j.SourceArgs)
	if err != nil {
		return nil, err
	}
	return json.Marshal(fields)
}
func UnmarshalJobParams(raw []byte, j *Job) error {
	j.SourceArgs = nil
	if err := json.Unmarshal(raw, &j.Params); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	if frozen, ok := fields["_reports_source"]; ok {
		if _, err := canonicalObjectBudget(raw, 2*MaxSourceArgsBytes+4096, MaxSourceArgsBytes+4096, true); err != nil {
			return err
		}
		if _, err := canonicalObjectBudget(frozen, 2*MaxSourceArgsBytes+2048, MaxSourceArgsBytes+2048, false); err != nil {
			return err
		}
		d := json.NewDecoder(bytes.NewReader(frozen))
		d.DisallowUnknownFields()
		var f FrozenSourceArgs
		if err := d.Decode(&f); err != nil {
			return ErrSourceArgs
		}
		canonical, err := canonicalObjectBudget(f.Args, 2*MaxSourceArgsBytes, MaxSourceArgsBytes, false)
		if err != nil {
			return err
		}
		f.Args = canonical
		j.SourceArgs = &f
		return j.ValidateSourceArgs()
	}
	return nil
}

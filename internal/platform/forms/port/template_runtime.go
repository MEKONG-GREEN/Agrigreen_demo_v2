package port

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"time"
	"unicode/utf8"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/google/uuid"
)

const RuntimeDefinitionV1 = "forms.runtime/v1"
const TemplateIdentityV1 = "forms.template/v1"
const MaxRuntimeConfigBytes = 65536

// RuntimeDefinition is an immutable owner contract, not executable Forms policy.
// The registered definition policy must explicitly validate its contract and config.
// Config is bounded canonical object JSON; numeric values use ordinary decimals
// with at most 18 integer and 6 fractional digits. It never accepts exponents.
type RuntimeDefinition struct {
	Schema   string          `json:"schema"`
	Contract string          `json:"contract"`
	Config   json.RawMessage `json:"config"`
}

var runtimeContract = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}/v[1-9][0-9]{0,3}$`)
var runtimeNumber = regexp.MustCompile(`^-?[0-9]{1,18}(\.[0-9]{1,6})?$`)

func (r *RuntimeDefinition) UnmarshalJSON(raw []byte) error {
	type plain RuntimeDefinition
	var value plain
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&value); err != nil {
		return err
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("runtime: trailing JSON")
	}
	*r = RuntimeDefinition(value)
	return nil
}

// NormalizeRuntime returns a detached snapshot and rejects unsupported envelopes,
// duplicate keys, excessive nesting and oversized scalars before numeric parsing.
func NormalizeRuntime(in *RuntimeDefinition) (*RuntimeDefinition, error) {
	if in == nil {
		return nil, nil
	}
	fail := func() (*RuntimeDefinition, error) {
		return nil, ErrInvalidTemplate.WithField("runtime", "cấu hình runtime không hợp lệ hoặc vượt giới hạn")
	}
	if in.Schema != RuntimeDefinitionV1 || !runtimeContract.MatchString(in.Contract) || len(in.Config) > MaxRuntimeConfigBytes || !utf8.Valid(in.Config) {
		return fail()
	}
	d := json.NewDecoder(bytes.NewReader(in.Config))
	d.UseNumber()
	count := 0
	value, err := runtimeValue(d, 0, &count)
	if err != nil {
		return fail()
	}
	if _, ok := value.(map[string]any); !ok {
		return fail()
	}
	if d.Decode(new(any)) != io.EOF {
		return fail()
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > MaxRuntimeConfigBytes {
		return fail()
	}
	return &RuntimeDefinition{Schema: in.Schema, Contract: in.Contract, Config: raw}, nil
}
func runtimeValue(d *json.Decoder, depth int, count *int) (any, error) {
	if depth > 16 || *count >= 2048 {
		return nil, errors.New("runtime: complexity limit")
	}
	*count++
	tok, err := d.Token()
	if err != nil {
		return nil, err
	}
	switch v := tok.(type) {
	case json.Delim:
		switch v {
		case '{':
			out := map[string]any{}
			for d.More() {
				k, e := d.Token()
				if e != nil {
					return nil, e
				}
				key, ok := k.(string)
				if !ok || len(key) > 128 {
					return nil, errors.New("runtime: key")
				}
				if _, exists := out[key]; exists {
					return nil, errors.New("runtime: duplicate key")
				}
				value, e := runtimeValue(d, depth+1, count)
				if e != nil {
					return nil, e
				}
				out[key] = value
			}
			_, err = d.Token()
			return out, err
		case '[':
			out := []any{}
			for d.More() {
				value, e := runtimeValue(d, depth+1, count)
				if e != nil {
					return nil, e
				}
				out = append(out, value)
			}
			_, err = d.Token()
			return out, err
		}
	case json.Number:
		if len(v) > 26 || !runtimeNumber.MatchString(string(v)) {
			return nil, errors.New("runtime: decimal syntax or bounds")
		}
	case string:
		if len(v) > 4096 {
			return nil, errors.New("runtime: string limit")
		}
	}
	return tok, nil
}

// TemplateIdentity pins genuine server-derived tenant/version/schema/config identity.
// Digest includes this identity, title, fixture, schema and the complete runtime.
type TemplateIdentity struct {
	Schema        string    `json:"schema"`
	TenantID      uuid.UUID `json:"tenant_id"`
	ID            uuid.UUID `json:"id"`
	Profile       string    `json:"profile"`
	Code          string    `json:"code"`
	Version       int64     `json:"version"`
	SchemaDigest  string    `json:"schema_digest"`
	RuntimeDigest string    `json:"runtime_digest"`
	Digest        string    `json:"digest"`
}

// TemplateCopy copies the exact readable source snapshot. The destination uses
// its actual base; source and destination are reauthorized before every replay.
type TemplateCopy struct {
	Source      TemplateIdentity `json:"source"`
	Code        string           `json:"code"`
	Title       string           `json:"title"`
	BaseVersion int64            `json:"base_version"`
}

type TemplateCopies interface {
	CopyTemplate(context.Context, TemplateCopy, string) (TemplateVersion, error)
}

const (
	TemplateUseRuntime   = "runtime"
	TemplateUseData      = "data"
	TemplateUnused       = "unused"
	TemplateUsed         = "used"
	TemplateUsageUnknown = "unknown"
)

// TemplateUsage is authoritative for one exact version, never a paged inference.
// Missing pre-contract coverage is unknown even when native data is absent.
type TemplateUsage struct {
	Identity    TemplateIdentity `json:"identity"`
	State       string           `json:"state"`
	FirstUsedAt *time.Time       `json:"first_used_at,omitempty"`
	Native      bool             `json:"native"`
	HasData     *bool            `json:"has_data,omitempty"`
	FirstDataAt *time.Time       `json:"first_data_at,omitempty"`
	Runtime     bool             `json:"runtime"`
}

// TemplateUseRequest must identify a real owner resource. An unverified client
// ID or contract name does not establish use or confer permission.
type TemplateUseRequest struct {
	Identity  TemplateIdentity `json:"identity"`
	Runtime   string           `json:"runtime"`
	Reference uuid.UUID        `json:"reference"`
	Kind      string           `json:"kind"`
}

type TemplateUseReceipt struct {
	ID         uuid.UUID          `json:"id"`
	Request    TemplateUseRequest `json:"request"`
	ActorRef   uuid.UUID          `json:"actor_ref"`
	RecordedAt time.Time          `json:"recorded_at"`
	HasData    bool               `json:"has_data"`
}

type TemplateUsePolicyRequest struct {
	Principal authz.Principal
	Actor     ResolvedActor
	Template  TemplateVersion
	Request   TemplateUseRequest
}

// Binding is a bounded server-derived reference to current owner authorization.
// Allowed may be set only after validating the runtime resource, tenant, node,
// profile, pinned identity and current write permission outside the Forms TX.
type TemplateUseDecision struct {
	Allowed bool
	Binding string
	// HasData is verified by this registered server-side callback, never copied
	// from client input. A data intent requires positive owner data evidence.
	HasData bool
}
type TemplateUsePolicy func(context.Context, TemplateUsePolicyRequest) (TemplateUseDecision, error)

type TemplateUsages interface {
	TemplateUsage(context.Context, string, uuid.UUID) (TemplateUsage, error)
	RecordTemplateUse(context.Context, TemplateUseRequest, string) (TemplateUseReceipt, error)
}

var ErrTemplateInUse = problem.Conflict.WithDetail("biểu đã có dữ liệu hoặc thiếu chứng cứ cũ: chỉ được thêm cột")

package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/google/uuid"
)

const MaxAuditWindow = 93 * 24 * time.Hour

var auditQueryCode = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,79}$`)

type AuditInput struct {
	From, To                   time.Time
	ActorRef                   uuid.UUID
	Department, Module, Action string
	Limit                      int
	Cursor                     string
}
type AuditItem struct {
	Source string `json:"source"`
	Module string `json:"module"`
	port.AuditRecord
}
type AuditPage struct {
	Items      []AuditItem `json:"items"`
	Sources    []string    `json:"sources"`
	AsOf       time.Time   `json:"as_of"`
	NextCursor *string     `json:"next_cursor"`
}
type auditCursor struct {
	Version    int                `json:"v"`
	FilterHash string             `json:"filter_hash"`
	AsOf       time.Time          `json:"as_of"`
	Before     port.AuditPosition `json:"before"`
}

func auditBad(field string) error {
	return problem.Validation.WithField(field, "bộ lọc nhật ký không hợp lệ")
}
func auditUnavailable(err error) error {
	return ErrDependency.WithDetail("Chức năng nhật ký chưa sẵn sàng").WithCause(err)
}
func auditPosition(item AuditItem) port.AuditPosition {
	return port.AuditPosition{RecordedAt: item.RecordedAt, SourceKey: item.Source, EventRef: item.EventRef}
}

// auditCompare is ascending; the query and response use strictly descending order.
func auditCompare(a, b port.AuditPosition) int {
	if a.RecordedAt.Before(b.RecordedAt) {
		return -1
	}
	if a.RecordedAt.After(b.RecordedAt) {
		return 1
	}
	if c := strings.Compare(a.SourceKey, b.SourceKey); c != 0 {
		return c
	}
	return bytes.Compare(a.EventRef[:], b.EventRef[:])
}
func cloneAuditRecord(r port.AuditRecord) port.AuditRecord {
	r.Changes = slices.Clone(r.Changes)
	for i := range r.Changes {
		clone := func(v *port.AuditValue) *port.AuditValue {
			if v == nil {
				return nil
			}
			out := *v
			out.Codes = slices.Clone(v.Codes)
			if v.Boolean != nil {
				b := *v.Boolean
				out.Boolean = &b
			}
			if v.Ref != nil {
				id := *v.Ref
				out.Ref = &id
			}
			return &out
		}
		r.Changes[i].Before = clone(r.Changes[i].Before)
		r.Changes[i].After = clone(r.Changes[i].After)
	}
	if r.Changes == nil {
		r.Changes = []port.AuditChange{}
	}
	return r
}

// Audit queries owner-provided sources, then durably records this viewing fact
// before returning. It never mints identity, infers an Admin role or uses legacy
// session data. Authorize must enforce the application's actual Admin session.
func (s *Service) Audit(ctx context.Context, in AuditInput) (AuditPage, error) {
	p, ok := authz.FromContext(ctx)
	if !ok || p.ActorRef == uuid.Nil || p.TenantID == uuid.Nil {
		return AuditPage{}, problem.Unauthenticated
	}
	if !p.MFA {
		return AuditPage{}, problem.MFARequired
	}
	if tid, err := tenant.FromContext(ctx); err == nil && tid != p.TenantID {
		return AuditPage{}, problem.Forbidden
	}
	if s.AuditRegistry == nil || s.AuditAccess.Authorize == nil || s.AuditAccess.RecordRead == nil {
		return AuditPage{}, auditUnavailable(nil)
	}
	if err := s.AuditAccess.Authorize(ctx); err != nil {
		return AuditPage{}, err
	}
	// PostgreSQL stores timestamptz at microsecond precision. Canonicalize
	// before validation/hash/reader checks so accepted nanosecond input cannot
	// make SQL and the post-read domain guard disagree.
	in.From, in.To = in.From.UTC().Truncate(time.Microsecond), in.To.UTC().Truncate(time.Microsecond)
	if in.From.IsZero() || in.To.IsZero() || !in.To.After(in.From) || in.To.Sub(in.From) > MaxAuditWindow {
		return AuditPage{}, auditBad("from")
	}
	if in.Limit == 0 {
		in.Limit = 50
	}
	if in.Limit < 1 || in.Limit > 200 {
		return AuditPage{}, auditBad("limit")
	}
	for field, v := range map[string]string{"department": in.Department, "module": in.Module, "action": in.Action} {
		if v != "" && !auditQueryCode.MatchString(v) {
			return AuditPage{}, auditBad(field)
		}
	}
	sources := []port.AuditSource{}
	for _, source := range s.AuditRegistry.Sources() {
		if in.Module == "" || source.Module == in.Module {
			sources = append(sources, source)
		}
	}
	if len(sources) == 0 {
		return AuditPage{}, auditBad("module")
	}
	keys := make([]string, 0, len(sources))
	for _, source := range sources {
		keys = append(keys, source.Key)
	}
	// The hash is an integrity binding to this query/identity/registry, not an
	// authorization token. Authorization and tenant RLS are repeated on every page.
	normalized := struct {
		Tenant, Reader, Actor      uuid.UUID
		From, To                   time.Time
		Department, Module, Action string
		Limit                      int
		Sources                    []string
	}{p.TenantID, p.ActorRef, in.ActorRef, in.From, in.To, in.Department, in.Module, in.Action, in.Limit, keys}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return AuditPage{}, auditUnavailable(err)
	}
	sum := sha256.Sum256(encoded)
	hash := hex.EncodeToString(sum[:])
	now := s.now()
	asOf := now
	if in.To.Before(asOf) {
		asOf = in.To
	}
	var before *port.AuditPosition
	if in.Cursor != "" {
		if len(in.Cursor) > 2048 {
			return AuditPage{}, auditBad("cursor")
		}
		data, err := base64.RawURLEncoding.DecodeString(in.Cursor)
		if err != nil {
			return AuditPage{}, auditBad("cursor")
		}
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		var cur auditCursor
		if err = dec.Decode(&cur); err != nil {
			return AuditPage{}, auditBad("cursor")
		}
		var trailing any
		if err = dec.Decode(&trailing); !errors.Is(err, io.EOF) {
			return AuditPage{}, auditBad("cursor")
		}
		if cur.Version != 1 || cur.FilterHash != hash || cur.AsOf.IsZero() || cur.AsOf.After(asOf) || cur.Before.EventRef == uuid.Nil || !slices.Contains(keys, cur.Before.SourceKey) || cur.Before.RecordedAt.Before(in.From) || !cur.Before.RecordedAt.Before(in.To) || cur.Before.RecordedAt.After(cur.AsOf) {
			return AuditPage{}, auditBad("cursor")
		}
		asOf = cur.AsOf
		position := cur.Before
		before = &position
	}
	items := []AuditItem{}
	for _, source := range sources {
		q := port.AuditReadQuery{From: in.From, To: in.To, AsOf: asOf, ActorRef: in.ActorRef, Department: in.Department, Module: in.Module, Action: in.Action, Limit: in.Limit + 1}
		if before != nil {
			v := *before
			q.Before = &v
		}
		rows, err := source.Read(ctx, p.TenantID, q)
		if err != nil {
			return AuditPage{}, auditUnavailable(err)
		}
		if len(rows) > q.Limit {
			return AuditPage{}, auditUnavailable(port.ErrAuditRecord)
		}
		var prev *port.AuditPosition
		for _, row := range rows {
			if err := s.AuditRegistry.ValidateRecord(source.Key, row); err != nil {
				return AuditPage{}, auditUnavailable(err)
			}
			item := AuditItem{Source: source.Key, Module: source.Module, AuditRecord: cloneAuditRecord(row)}
			pos := auditPosition(item)
			if row.RecordedAt.Before(in.From) || !row.RecordedAt.Before(in.To) || row.RecordedAt.After(asOf) || (in.ActorRef != uuid.Nil && row.ActorRef != in.ActorRef) || (in.Department != "" && row.Department != in.Department) || (in.Action != "" && row.Action != in.Action) || (before != nil && auditCompare(pos, *before) >= 0) || (prev != nil && auditCompare(pos, *prev) >= 0) {
				return AuditPage{}, auditUnavailable(port.ErrAuditRecord)
			}
			prev = &pos
			items = append(items, item)
		}
	}
	slices.SortFunc(items, func(a, b AuditItem) int { return -auditCompare(auditPosition(a), auditPosition(b)) })
	page := AuditPage{Sources: slices.Clone(keys), AsOf: asOf}
	if len(items) > in.Limit {
		items = items[:in.Limit]
		cursor := auditCursor{Version: 1, FilterHash: hash, AsOf: asOf, Before: auditPosition(items[len(items)-1])}
		data, err := json.Marshal(cursor)
		if err != nil {
			return AuditPage{}, auditUnavailable(err)
		}
		value := base64.RawURLEncoding.EncodeToString(data)
		page.NextCursor = &value
	}
	page.Items = items
	receipt := port.AuditReadReceipt{RequestRef: ids.New(), TenantID: p.TenantID, ActorRef: p.ActorRef, ReadAt: now, FilterHash: hash, From: in.From, To: in.To, AsOf: asOf, FilterActorRef: in.ActorRef, Department: in.Department, Module: in.Module, Action: in.Action, Limit: in.Limit, PurposeCode: port.AuditPurposeView, Sources: slices.Clone(keys), Returned: []port.AuditPosition{}}
	for _, item := range items {
		receipt.Returned = append(receipt.Returned, auditPosition(item))
	}
	if err := s.AuditAccess.RecordRead(ctx, receipt); err != nil {
		return AuditPage{}, auditUnavailable(err)
	}
	return page, nil
}

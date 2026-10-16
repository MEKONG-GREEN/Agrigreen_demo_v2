package app

// Sinh, tự đóng cảnh báo và phát sự kiện cho pf/notify — trong giao dịch của bên gọi (consumer inbox, job).

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsalert/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsalert/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

// Raise là yêu cầu sinh một cảnh báo.
type Raise struct {
	Kind         string
	Subject      string // mã đơn vị (xã, cơ sở, hộ, điểm) hoặc mã ổ dịch
	SubjectKind  string // commune | facility | household | site | outbreak
	Scope        string // khóa kỳ / phạm vi chống trùng
	CommuneCode  string
	ProvinceCode string
	TemplateCode string
	PeriodKey    string
	PeriodRef    *uuid.UUID
	SourceRef    *uuid.UUID
	Title        string
	Findings     []domain.Finding
	Extra        map[string]any
	Recipients   []uuid.UUID
	At           time.Time
}

// settings đọc cấu hình xử lý của loại (mặc định nếu chưa cấu hình).
func (s *Service) alertSetting(ctx context.Context, tx pgx.Tx, kind string) domain.AlertSetting {
	cs, err := store.Configs(ctx, tx)
	if err == nil {
		for _, c := range cs {
			if c.Scope == domain.ScopeAlert && c.Code == kind {
				var v domain.AlertSetting
				if json.Unmarshal(c.Settings, &v) == nil && len(v.Normalize(kind)) == 0 {
					return v
				}
			}
		}
	}
	return domain.DefaultAlertSetting(kind)
}

// raise ghi cảnh báo mới nếu chưa có cảnh báo cùng khóa chống trùng (kể cả đã đóng — "mỗi đối tượng, loại, kỳ chỉ một
// cảnh báo"). Trả (cảnh báo, true) nếu vừa tạo.
func (s *Service) raise(ctx context.Context, tx pgx.Tx, tid uuid.UUID, r Raise) (*store.Alert, bool, error) {
	key := domain.DedupKey(r.Kind, r.Subject, r.Scope)
	if err := store.Lock(ctx, tx, key); err != nil {
		return nil, false, err
	}
	if cur, err := store.ByDedup(ctx, tx, key); err != nil || cur != nil {
		return cur, false, err
	}
	set := s.alertSetting(ctx, tx, r.Kind)
	detail := map[string]any{}
	for k, v := range r.Extra {
		detail[k] = v
	}
	if len(r.Findings) > 0 {
		detail["findings"] = r.Findings
		detail["summary"] = domain.Summary(r.Findings)
	}
	recips := dedupRefs(r.Recipients)
	a := store.Alert{Ref: newID(), DedupKey: key, Department: domain.Department, Kind: r.Kind, Level: domain.LevelFor(r.Kind, set),
		Status: domain.StatusNew, CommuneCode: r.CommuneCode, ProvinceCode: r.ProvinceCode, SubjectKind: r.SubjectKind, SubjectRef: r.Subject,
		TemplateCode: r.TemplateCode, PeriodKey: r.PeriodKey, PeriodRef: r.PeriodRef, SourceRef: r.SourceRef, Title: clip(r.Title, 300),
		Detail: detail, RecipientRefs: recips, RaisedAt: r.At, DueAt: r.At.Add(time.Duration(set.SLAHours) * time.Hour)}
	due := a.DueAt
	e := store.Event{ID: newID(), Action: domain.ActionRaised, StatusAfter: domain.StatusNew, DueAt: &due, RecipientRefs: recips,
		EventTime: r.At, ActorRef: SystemActor, IdemKey: "raise:" + key}
	if err := store.InsertEvent(ctx, tx, tid, a, &e); err != nil {
		return nil, false, err
	}
	a.LastEventID, a.LastSeq = e.ID, e.Seq
	if ok, err := store.InsertCurrent(ctx, tx, tid, a); err != nil || !ok {
		return nil, false, err
	}
	return &a, true, emitChanged(ctx, tx, tid, a, port.CNTSAlertRaised, recips, r.At)
}

// autoClose đóng tự động cảnh báo chưa đóng của khóa (số liệu sửa hết vi phạm — Mục V "tự đóng (ghi log)").
func (s *Service) autoClose(ctx context.Context, tx pgx.Tx, tid uuid.UUID, kind, subject, scope string, source *uuid.UUID, at time.Time) error {
	key := domain.DedupKey(kind, subject, scope)
	if err := store.Lock(ctx, tx, key); err != nil {
		return err
	}
	a, err := store.ByDedup(ctx, tx, key)
	if err != nil || a == nil || a.Status == domain.StatusClosed {
		return err
	}
	note := "Số liệu đã sửa hết vi phạm — hệ thống tự đóng"
	if source != nil {
		note += " (bài nộp " + source.String() + ")"
	}
	_, err = s.apply(ctx, tx, tid, a, domain.ActionAutoClosed, applyInput{Note: note, Actor: SystemActor, At: at,
		Idem: "auto:" + key + ":" + itoa(a.LastSeq)})
	return err
}

type applyInput struct {
	Assignee *uuid.UUID
	DueAt    *time.Time
	Note     string
	Actor    uuid.UUID
	OnBehalf *uuid.UUID
	Support  *string
	At       time.Time
	Idem     string
}

// apply ghi sự việc act lên cảnh báo a (đã khóa) và cập nhật bảng chiếu.
func (s *Service) apply(ctx context.Context, tx pgx.Tx, tid uuid.UUID, a *store.Alert, act string, in applyInput) (store.Event, error) {
	next, err := domain.Transition(a.Status, act)
	if err != nil {
		return store.Event{}, ErrAlertTransition
	}
	e := store.Event{ID: newID(), Action: act, StatusAfter: next, Note: in.Note, EventTime: in.At, ActorRef: in.Actor, OnBehalfOf: in.OnBehalf,
		SupportReason: in.Support, IdemKey: in.Idem, AssigneeRef: a.AssigneeRef}
	switch act {
	case domain.ActionAssigned:
		a.AssigneeRef, a.DueAt, a.OverdueAt = in.Assignee, *in.DueAt, nil
		e.AssigneeRef = in.Assignee
	case domain.ActionClosed, domain.ActionAutoClosed:
		at := in.At
		a.ClosedAt, a.Result, a.AutoClosed = &at, in.Note, act == domain.ActionAutoClosed
	case domain.ActionOverdue:
		at := in.At
		a.OverdueAt = &at
	}
	due := a.DueAt
	e.DueAt = &due
	a.Status = next
	if err := store.InsertEvent(ctx, tx, tid, *a, &e); err != nil {
		return e, err
	}
	a.LastEventID, a.LastSeq = e.ID, e.Seq
	return e, store.UpdateCurrent(ctx, tx, *a)
}

// emitChanged ghi pf.extension.cnts_alert.changed (pf/notify báo người nhận).
func emitChanged(ctx context.Context, tx pgx.Tx, tid uuid.UUID, a store.Alert, action string, recips []uuid.UUID, at time.Time) error {
	if recips == nil {
		recips = []uuid.UUID{}
	}
	p := port.CNTSAlertChangedV1{AlertRef: a.Ref, Action: action, Kind: a.Kind, Level: a.Level, Title: a.Title, CommuneCode: optStr(a.CommuneCode),
		ProvinceCode: optStr(a.ProvinceCode), TemplateCode: optStr(a.TemplateCode), PeriodKey: optStr(a.PeriodKey),
		DueAt: a.DueAt.UTC().Format(time.RFC3339), RecipientRefs: recips, Department: domain.Department, EventTime: at.UTC().Format(time.RFC3339)}
	return outbox.Enqueue(ctx, tx, outbox.Event{ID: ids.New(), TenantID: tid, Type: port.EventCNTSAlertChanged, SchemaVersion: 1, OccurredAt: at,
		TraceID: telemetry.TraceID(ctx), Producer: port.EventProducerName, Payload: p})
}

func optStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func dedupRefs(rs []uuid.UUID) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(rs))
	for _, r := range rs {
		if r != uuid.Nil && !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	return out
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

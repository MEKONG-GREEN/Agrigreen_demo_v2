package app

// KN v7.7 gói P1 — khung Log của màn Detail tài khoản (FN-AD-04.04, plan §1.5): hợp nhất sổ thao tác
// tài khoản (tạo, khóa, mở khóa, gửi lại liên kết), sổ gán vai trò (thêm/sửa/xóa vai trò, cũ → mới) và sổ
// chỉnh sửa thông tin (tên trường) theo thời điểm ghi. Định dạng như P2:
// {seq, event, actor_ref, actor_display, at, changes:[{field, old, new}], reason}.

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// LogEntry là một dòng khung Log.
type LogEntry struct {
	Seq          int64 // thứ tự trong Log hợp nhất của tài khoản (1, 2, …), ổn định vì các sổ chỉ ghi thêm
	Event        string
	EntityRef    uuid.UUID // lần gán vai trò (sự kiện assignment.*); Nil với sự kiện của tài khoản
	ActorRef     uuid.UUID
	ActorDisplay string
	At           time.Time
	RecordedAt   time.Time
	Changes      []domain.FieldChange
	Reason       *string
}

func sp(s string) *string { return &s }

func strOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func accountLog(e domain.Event) LogEntry {
	l := LogEntry{Event: "account." + string(e.Action), ActorRef: e.ActorRef, At: e.EventTime, RecordedAt: e.RecordedAt, Reason: strOrNil(e.Reason)}
	switch e.Action {
	case domain.ActionCreated:
		l.Changes = []domain.FieldChange{{Field: "roles", New: sp(strings.Join(e.Account.Roles, ","))}, {Field: "enabled", New: sp("true")}}
	case domain.ActionLocked:
		l.Changes = []domain.FieldChange{{Field: "enabled", Old: sp("true"), New: sp("false")}}
	case domain.ActionUnlocked:
		l.Changes = []domain.FieldChange{{Field: "enabled", Old: sp("false"), New: sp("true")}}
	case domain.ActionActionsSent:
		// "Gửi lại liên kết kích hoạt / đặt lại mật khẩu" — không có mật khẩu tạm (gap §4.4).
		l.Event = "account.activation_sent"
		acts, _ := e.Details["actions"].([]any)
		var parts []string
		for _, x := range acts {
			parts = append(parts, fmt.Sprint(x))
		}
		l.Changes = []domain.FieldChange{{Field: "required_actions", New: sp(strings.Join(parts, ","))}}
	case domain.ActionAssignmentChanged:
		prevRoles, _ := e.Details["prev_roles"].([]any)
		var pr []string
		for _, x := range prevRoles {
			pr = append(pr, fmt.Sprint(x))
		}
		l.Changes = []domain.FieldChange{{Field: "roles", Old: sp(strings.Join(pr, ",")), New: sp(strings.Join(e.Account.Roles, ","))}}
	}
	return l
}

// UserLog trả khung Log hợp nhất của tài khoản, phân trang theo seq (cursor = seq cuối trang trước).
func (s *Service) UserLog(ctx context.Context, user uuid.UUID, after int64, limit int) ([]LogEntry, error) {
	a, err := s.knCaller(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := s.target(ctx, a, user); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var out []LogEntry
	err = tenant.WithTenantTx(ctx, s.DB, a.p.TenantID, func(tx pgx.Tx) error {
		accs, err := store.AccountEvents(ctx, tx, user)
		if err != nil {
			return err
		}
		for _, e := range accs {
			out = append(out, accountLog(e))
		}
		asgs, err := store.AsgEventsOfUser(ctx, tx, user)
		if err != nil {
			return err
		}
		for _, e := range asgs {
			out = append(out, LogEntry{Event: "assignment." + e.Action, EntityRef: e.A.ID, ActorRef: e.ActorRef, At: e.EventTime,
				RecordedAt: e.RecordedAt, Changes: domain.Diff(e.Before, e.After), Reason: e.Reason})
		}
		// W11.07: thuộc tính phân quyền trong vai (sổ phụ) — cũ → mới như sổ gán vai.
		attrs, err := store.AttrEventsOfUser(ctx, tx, user)
		if err != nil {
			return err
		}
		for _, e := range attrs {
			out = append(out, LogEntry{Event: "assignment.attributes_changed", EntityRef: e.AssignmentID, ActorRef: e.ActorRef, At: e.EventTime,
				RecordedAt: e.RecordedAt, Changes: domain.Diff(e.Before, e.After), Reason: e.Reason})
		}
		pcs, err := store.ProfileChanges(ctx, tx, user)
		if err != nil {
			return err
		}
		for _, p := range pcs {
			l := LogEntry{Event: "account.profile_updated", ActorRef: p.ActorRef, At: p.EventTime, RecordedAt: p.RecordedAt, Reason: p.Reason}
			// RB-3: chỉ tên trường — giá trị cũ/mới nằm ở Keycloak, không chép vào sổ.
			for _, f := range p.Fields {
				l.Changes = append(l.Changes, domain.FieldChange{Field: f})
			}
			out = append(out, l)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].RecordedAt.Before(out[j].RecordedAt) })
	for i := range out {
		out[i].Seq = int64(i + 1)
	}
	start := min(int(max(after, 0)), len(out))
	page := slices.Clone(out[start:min(start+limit, len(out))])
	refs := make([]uuid.UUID, 0, len(page))
	for _, l := range page {
		refs = append(refs, l.ActorRef)
	}
	if names, err := s.DisplayNames(ctx, refs); err == nil {
		for i := range page {
			page[i].ActorDisplay = names[page[i].ActorRef]
		}
	}
	return page, nil
}

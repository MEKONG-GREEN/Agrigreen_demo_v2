// Package knaudit là phần chủ KN của nhật ký hệ thống (AUDIT-LOG-KN, #73) trên cổng truy vấn dùng chung #70
// (accounts/port.AuditRegistry, AuditAccess):
//
//   - Authorize: chỉ phiên Admin KN thật (vai ngữ cảnh "admin", không ở chế độ hỗ trợ) được xem; mọi lỗi là kết quả
//     cuối, không suy từ vai realm hay Principal.KN cũ. Lần bị từ chối được ghi sổ (outcome denied) rồi vẫn từ chối.
//   - RecordRead: ghi bền lượt xem vào sổ chỉ ghi thêm pf_extension.kn_audit_view TRƯỚC khi trang được trả; lỗi ghi →
//     lõi dùng chung trả 503, không trả dữ liệu.
//   - Sources: bộ đọc các sổ KN đã có (chỉ cột mã/ref an toàn, không lý do/ghi chú/payload/tên) và chính sổ lượt xem.
//
// Mỗi lần đọc/ghi là một giao dịch ngắn có ngữ cảnh tenant (RLS) trên vai r_extension; không gọi I/O khác trong giao
// dịch. Sổ lượt xem không có chính sách dọn (giữ vô thời hạn, ≥ 3 tháng — migration 20261029031500).
package knaudit

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	knport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// RuleVersion là phiên bản quy tắc ghi sổ lượt xem.
const RuleVersion = "kn-audit-view/1"

// Mã từ chối lưu ở cột denial_code.
const (
	DenialNoSession   = "no_session"   // chưa chọn ngữ cảnh phiên KN
	DenialNotAdmin    = "not_admin"    // ngữ cảnh không phải Admin
	DenialSupportMode = "support_mode" // Admin đang ở chế độ hỗ trợ cấp khác
	DenialInvalid     = "invalid_session"
)

// ErrReceipt: biên nhận lượt xem không hợp lệ (không khớp người gọi, thiếu trường) — không ghi, không trả dữ liệu.
var ErrReceipt = errors.New("knaudit: invalid audit read receipt")

// Service giữ pool đã gắn vai r_extension.
type Service struct {
	DB    tenant.Beginner
	Clock clock.Clock
}

// Access trả cặp callback chủ KN cho accounts.Module.UseAudit.
func (s *Service) Access() accport.AuditAccess {
	return accport.AuditAccess{Authorize: s.Authorize, RecordRead: s.RecordRead}
}

// Authorize chỉ cho phiên Admin KN (ngoài chế độ hỗ trợ). Lần bị từ chối được ghi sổ khi biết người gọi và tenant;
// ghi lỗi vẫn từ chối (đóng khi lỗi).
func (s *Service) Authorize(ctx context.Context) error {
	p, k, err := knport.RequireSession(ctx, knport.SessionRoleAdmin)
	code := ""
	switch {
	case err != nil && p.SelectedSession == nil:
		code = DenialNoSession
	case err != nil:
		code = DenialNotAdmin
		if _, rerr := knport.ReadSessionContext(p); rerr != nil {
			code = DenialInvalid
		}
	case k.Support != nil:
		code = DenialSupportMode
		err = problem.Forbidden.WithDetail("Admin đang ở chế độ hỗ trợ, thoát hỗ trợ để xem nhật ký hệ thống")
	}
	if code == "" {
		return nil
	}
	if p.ActorRef != uuid.Nil && p.TenantID != uuid.Nil {
		var session uuid.UUID
		if code != DenialNoSession && code != DenialInvalid {
			session = k.ID
		}
		// Lỗi ghi lượt bị từ chối không đổi kết quả: vẫn từ chối (log chỉ mang mã từ chối, không dữ liệu cá nhân).
		if rerr := s.insert(ctx, p.TenantID, viewRow{RequestRef: ids.New(), Outcome: "denied", DenialCode: code, SessionRef: session,
			EventTime: s.now(), ActorRef: p.ActorRef}); rerr != nil {
			slog.WarnContext(ctx, "knaudit: không ghi được lượt xem bị từ chối", "denial", code, "err", rerr)
		}
	}
	return err
}

// RecordRead ghi bền lượt xem (một trang, kể cả rỗng) trước khi lõi trả dữ liệu.
func (s *Service) RecordRead(ctx context.Context, r accport.AuditReadReceipt) error {
	p, ok := authz.FromContext(ctx)
	if !ok || p.ActorRef == uuid.Nil || p.ActorRef != r.ActorRef || p.TenantID != r.TenantID || r.TenantID == uuid.Nil {
		return ErrReceipt
	}
	if r.RequestRef == uuid.Nil || r.ReadAt.IsZero() || r.PurposeCode != accport.AuditPurposeView || len(r.FilterHash) != 64 ||
		r.From.IsZero() || r.To.IsZero() || r.AsOf.IsZero() || r.Limit < 1 || r.Limit > 200 || len(r.Sources) == 0 ||
		len(r.Sources) > 32 || len(r.Returned) > 200 {
		return ErrReceipt
	}
	row := viewRow{RequestRef: r.RequestRef, Outcome: "succeeded", FilterHash: r.FilterHash, From: r.From, To: r.To, AsOf: r.AsOf,
		FilterActor: r.FilterActorRef, Department: r.Department, Module: r.Module, Action: r.Action, Limit: r.Limit,
		Sources: slices.Clone(r.Sources), EventTime: r.ReadAt, ActorRef: r.ActorRef}
	if k, err := knport.ReadSessionContext(p); err == nil {
		row.SessionRef = k.ID
	}
	for _, pos := range r.Returned {
		row.ReturnedAt = append(row.ReturnedAt, pos.RecordedAt)
		row.ReturnedSources = append(row.ReturnedSources, pos.SourceKey)
		row.ReturnedRefs = append(row.ReturnedRefs, pos.EventRef)
	}
	return s.insert(ctx, r.TenantID, row)
}

func (s *Service) now() time.Time {
	if s.Clock == nil {
		return clock.System{}.Now()
	}
	return s.Clock.Now().UTC()
}

type viewRow struct {
	RequestRef                 uuid.UUID
	Outcome, DenialCode        string
	SessionRef                 uuid.UUID
	FilterHash                 string
	From, To, AsOf             time.Time
	FilterActor                uuid.UUID
	Department, Module, Action string
	Limit                      int
	Sources                    []string
	ReturnedAt                 []time.Time
	ReturnedSources            []string
	ReturnedRefs               []uuid.UUID
	EventTime                  time.Time
	ActorRef                   uuid.UUID
}

func nullText(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func nullRef(id uuid.UUID) any {
	if id == uuid.Nil {
		return nil
	}
	return id
}
func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC()
}
func nullInt(n int) any {
	if n == 0 {
		return nil
	}
	return n
}

// insert là một giao dịch ghi ngắn: một câu INSERT (DB tính seq, băm chuỗi, recorded_at).
func (s *Service) insert(ctx context.Context, tid uuid.UUID, r viewRow) error {
	if s.DB == nil {
		return ErrReceipt
	}
	// Mảng truyền dạng text[] rồi ép kiểu trong SQL: pool chế độ exec không mô tả câu lệnh nên pgx không tự mã hóa
	// []uuid.UUID, []time.Time.
	sources, srcs := append([]string{}, r.Sources...), append([]string{}, r.ReturnedSources...)
	at, refs := make([]string, 0, len(r.ReturnedAt)), make([]string, 0, len(r.ReturnedRefs))
	for _, t := range r.ReturnedAt {
		at = append(at, t.UTC().Format(time.RFC3339Nano))
	}
	for _, id := range r.ReturnedRefs {
		refs = append(refs, id.String())
	}
	return tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO pf_extension.kn_audit_view (id, tenant_id, stream_key, request_ref, outcome, denial_code,
  purpose_code, session_ref, filter_hash, filter_from, filter_to, as_of, filter_actor_ref, filter_department, filter_module,
  filter_action, page_limit, sources, returned_at, returned_sources, returned_refs, event_time, actor_ref, idem_key, rule_version)
VALUES ($1::uuid, $2::uuid, $3::text, $4::uuid, $5::text, $6::text, 'audit_view', $7::uuid, $8::text, $9::timestamptz,
  $10::timestamptz, $11::timestamptz, $12::uuid, $13::text, $14::text, $15::text, $16::integer, $17::text[], $18::text[]::timestamptz[],
  $19::text[], $20::text[]::uuid[], $21::timestamptz, $22::uuid, $23::text, $24::text)`,
			ids.New(), tid, "kn_audit_view:"+r.ActorRef.String(), r.RequestRef, r.Outcome, nullText(r.DenialCode), nullRef(r.SessionRef),
			nullText(r.FilterHash), nullTime(r.From), nullTime(r.To), nullTime(r.AsOf), nullRef(r.FilterActor), nullText(r.Department),
			nullText(r.Module), nullText(r.Action), nullInt(r.Limit), sources, at, srcs, refs, r.EventTime.UTC(), r.ActorRef,
			"view:"+r.RequestRef.String(), RuleVersion)
		return err
	})
}

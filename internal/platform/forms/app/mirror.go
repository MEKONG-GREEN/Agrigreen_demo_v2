package app

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

func errorsAs(err error, target any) bool { return errors.As(err, target) }

func hexOf(b []byte) string { return hex.EncodeToString(b) }

// Record thực hiện port.Freezer: ghi chốt/mở lại/hủy kỳ của bên dùng (origin = mirror) — và ảnh chụp đóng băng khi chốt —
// trong giao dịch của bên dùng (ctx mang giao dịch — db.JoinTx), qua hàm pf_forms.mirror. Gọi lặp cùng IdemKey trả
// lại kết quả cũ (Replayed). Chốt mới → sự kiện pf.forms.period.closed trong cùng giao dịch.
func (s *Service) Record(ctx context.Context, in port.MirrorInput) (port.MirrorResult, error) {
	tx, ok := db.JoinedTx(ctx)
	if !ok {
		return port.MirrorResult{}, port.ErrNoCallerTx
	}
	tid, err := currentTenant(ctx, tx)
	if err != nil {
		return port.MirrorResult{}, err
	}
	if _, ok := s.profiles[in.Profile]; !ok {
		return port.MirrorResult{}, port.ErrProfileNotFound
	}
	if in.PeriodRef == uuid.Nil || in.Actor == uuid.Nil || strings.TrimSpace(in.IdemKey) == "" {
		return port.MirrorResult{}, problem.Validation.WithDetail("period_ref, actor, idem_key bắt buộc")
	}
	if in.Mode == "" {
		in.Mode = port.ModeManual
	}
	if in.Mode != port.ModeManual && in.Mode != port.ModeAuto {
		return port.MirrorResult{}, problem.Validation.WithField("mode", "manual | auto")
	}
	var label, reason *string
	if l := strings.TrimSpace(in.Label); l != "" {
		if len([]rune(l)) > 200 {
			l = string([]rune(l)[:200])
		}
		label = &l
	}
	var content []byte
	switch in.Action {
	case port.ActionClosed:
		var obj map[string]json.RawMessage
		if len(in.Content) == 0 || json.Unmarshal(in.Content, &obj) != nil {
			return port.MirrorResult{}, problem.Validation.WithField("content", "chốt kỳ cần nội dung ảnh chụp là object JSON")
		}
		content = in.Content
	case port.ActionReopened, port.ActionCancelled:
		why, ok := domain.Reason(in.Reason)
		if !ok {
			return port.MirrorResult{}, problem.Validation.WithField("reason", "bắt buộc, 10–500 ký tự")
		}
		reason = &why
	default:
		return port.MirrorResult{}, problem.Validation.WithField("action", "closed | reopened | cancelled")
	}
	for _, u := range in.Units {
		if !domain.ValidateUnit(u) {
			return port.MirrorResult{}, problem.Validation.WithField("units", "mã đơn vị không hợp lệ: "+u)
		}
	}
	at := in.At
	if at.IsZero() {
		at = s.Clock.Now()
	}
	at = at.UTC()
	r, err := store.Mirror(ctx, tx, ids.New(), ids.New(), in.Profile, in.PeriodRef, label, in.Action, in.Mode, in.Units, reason, content, in.Actor, at,
		"mirror:"+in.IdemKey)
	if err != nil {
		return port.MirrorResult{}, classify(err)
	}
	out := port.MirrorResult{EventID: r.EventID, Seq: r.Seq, SnapshotID: r.SnapshotID, ContentHash: r.Hash, Replayed: r.Replayed}
	if r.SnapshotNo != nil {
		out.SnapshotNo = *r.SnapshotNo
	}
	if in.Action == port.ActionClosed && !r.Replayed && r.SnapshotID != nil {
		if err := enqueueClosed(ctx, tx, tid, in.PeriodRef, in.Profile, "mirror", in.Mode, *r.SnapshotID, out.SnapshotNo, r.Hash, in.Actor, at); err != nil {
			return port.MirrorResult{}, err
		}
	}
	return out, nil
}

// currentTenant: tenant của giao dịch bên dùng; chưa đặt → lỗi (pf_forms.mirror cũng từ chối, 42501).
func currentTenant(ctx context.Context, tx pgx.Tx) (uuid.UUID, error) {
	tid, err := store.CurrentTenant(ctx, tx)
	if err != nil {
		return uuid.Nil, err
	}
	if tid == nil {
		return uuid.Nil, errors.New("forms: giao dịch bên gọi chưa đặt app.tenant_id")
	}
	return *tid, nil
}

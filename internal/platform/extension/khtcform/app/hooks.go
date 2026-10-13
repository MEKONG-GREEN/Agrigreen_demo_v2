package app

// Móc nối với gói khác: W11.61 EntryUsage (hạng mục đã có số liệu chỉ ngừng), W11.62 trạng thái "Lưu nháp" ở bảng tiến độ
// kỳ, và quy tắc cap/forms của profile kn_khtc (bài nộp phải mang bảng số liệu đã niêm phong qua màn phiếu KHTC).

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/khtcform/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/khtcform/store"
	pdomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/khtcperiod/domain"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// EntryUsage là móc W11.61 (khtc/app.Service.EntryUsage): dòng kế hoạch nào của năm year đã có số liệu ở phiếu đã gửi
// (bảng niêm phong có ô nhập hoặc dòng điều chỉnh) — "đã có số liệu chỉ Ngừng theo dõi, không bỏ khỏi kế hoạch".
func (s *Service) EntryUsage(ctx context.Context, year int, refs []uuid.UUID) (map[uuid.UUID]bool, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return nil, problem.Unauthenticated
	}
	if s.DB == nil || len(refs) == 0 {
		return map[uuid.UUID]bool{}, nil
	}
	var out map[uuid.UUID]bool
	err := s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		out, e = store.UsedEntries(ctx, tx, year, refs)
		return e
	})
	return out, err
}

// Drafts là móc W11.62 (khtcperiod/app.Service.Drafts): phòng có nháp ở máy chủ của kỳ KHTC, kèm số lần gửi đã thấy khi
// lưu nháp (nháp sau lần gửi cuối mới là nháp hiện hành).
func (s *Service) Drafts(ctx context.Context, periodID uuid.UUID) (map[string]int64, error) {
	p, ok := authz.FromContext(ctx)
	if !ok || s.DB == nil {
		return nil, nil
	}
	out := map[string]int64{}
	err := s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		ds, e := store.DraftAttempts(ctx, tx, periodID)
		for d, n := range ds {
			out[d] = int64(n)
		}
		return e
	})
	return out, err
}

// Validator cài formsport.Validator cho profile kn_khtc: bài nộp biểu KHTC-PHIEU phải mang mã bảng số liệu đã niêm phong
// của tenant (gửi qua /v1/extension/khtc/periods/{id}/forms/{dept}/submit — đã kiểm 3 quy tắc). Bài nộp thẳng /v1/forms
// không có bảng → chặn.
type Validator struct{ Svc *Service }

var _ formsport.Validator = Validator{}

// Validate kiểm trường bang_so_lieu.
func (v Validator) Validate(ctx context.Context, templateCode string, values map[string]string) []formsport.Issue {
	if templateCode != pdomain.TemplateCode {
		return nil
	}
	bad := []formsport.Issue{{Field: domain.FieldSheet, Reason: "phiếu KHTC phải gửi từ màn Báo cáo cho KHTC (bảng số liệu đã kiểm tra)"}}
	id, err := uuid.Parse(values[domain.FieldSheet])
	if err != nil {
		return bad
	}
	p, ok := authz.FromContext(ctx)
	if !ok || v.Svc == nil || v.Svc.DB == nil {
		return bad
	}
	var sh *store.Sheet
	if err := v.Svc.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		sh, e = store.ByID(ctx, tx, id)
		return e
	}); err != nil || sh == nil || sh.Kind != store.KindSealed {
		return bad
	}
	return nil
}

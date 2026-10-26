package app

// W11.48: cảnh báo dòng 6–9 (môi trường nước, tăng trọng thấp, tỷ lệ chết, vị trí lần ghi) sinh NGAY khi lần ghi nhật ký
// BM-08/09/19 được gửi (FN-10 AC-02), trong giao dịch ghi của nhật ký (extension/cntsdiary). Người nhận do nhật ký giải
// (chủ nhiệm dự án, cán bộ theo dõi điểm). Mỗi lần ghi × loại một cảnh báo; số liệu sửa hết vi phạm → tự đóng.

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsalert/domain"
)

// LogAlert là kết quả kiểm tra một loại cảnh báo của một lần ghi nhật ký.
type LogAlert struct {
	Kind         string // domain.KindWaterQuality | KindLowGrowth | KindMortality | KindLogLocation
	Hit          bool
	HouseholdRef uuid.UUID
	EntryRef     uuid.UUID
	CommuneCode  string
	ProvinceCode string
	Form         string // BM-08 | BM-09 | BM-19
	Title        string
	Findings     []domain.Finding
	Recipients   []uuid.UUID
	At           time.Time
}

// LogVerdict sinh cảnh báo khi có vi phạm, tự đóng khi hết — trong giao dịch tx của bên gọi.
func (s *Service) LogVerdict(ctx context.Context, tx pgx.Tx, tid uuid.UUID, a LogAlert) error {
	scope := "entry:" + a.EntryRef.String()
	subject := a.HouseholdRef.String()
	src := a.EntryRef
	if !a.Hit {
		return s.autoClose(ctx, tx, tid, a.Kind, subject, scope, &src, a.At)
	}
	k, _ := domain.KindOf(a.Kind)
	title := k.Label
	if a.Title != "" {
		title += " · " + a.Title
	}
	_, _, err := s.raise(ctx, tx, tid, Raise{Kind: a.Kind, Subject: subject, SubjectKind: "household", Scope: scope,
		CommuneCode: a.CommuneCode, ProvinceCode: a.ProvinceCode, TemplateCode: a.Form, SourceRef: &src, Title: title,
		Findings: a.Findings, Extra: map[string]any{"entry_ref": a.EntryRef.String()}, Recipients: a.Recipients, At: a.At})
	return err
}

package port

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// W11.44: cổng đọc số liệu kỳ native cho tổng hợp của module nghiệp vụ (KN CNTS: BM-02 cộng BM-01 theo tuần KSGM/tháng,
// BM-18 Phụ lục 2 cộng BM-17 theo xã + lũy kế). Đọc theo lô (một giao dịch, vài câu SQL — không mở từng kỳ), chỉ kỳ có
// lịch (period_spec.period_key) của MỘT mã biểu, chưa hủy, người gọi phải thấy kỳ (như GET /v1/forms/periods/{id}).
// Trả CẢ bài nộp hiện hành (số "đã gửi" cho màn tổng hợp) LẪN ảnh chụp đóng băng mới nhất (số "đã chốt" cho hợp đồng
// closed_figures — D17, D18). Không trả người nộp/rà soát (R-14).
// ---------------------------------------------------------------------------

// UnitFigures là số liệu của một đơn vị trong kỳ.
type UnitFigures struct {
	Values    map[string]string
	NilReport bool   // báo "không phát sinh" (sổ phụ submission_meta)
	Version   int64  // phiên bản bài nộp (0 = trong ảnh chụp không có bài)
	Decision  string // rà soát của phiên bản: "" (chưa), accepted, returned
}

// PeriodFigures là một kỳ native có lịch kèm số liệu.
type PeriodFigures struct {
	ID           uuid.UUID
	TemplateCode string
	Label        string
	OwnerRef     *uuid.UUID
	UnitKind     string
	Units        []string
	Parents      map[string]string // đơn vị → mã xã cha (cơ sở/hộ); rỗng với xã
	Kind, Key    string            // loại kỳ, khóa kỳ đã lưu ("2026-10-05", "2026-W41")
	From, To     time.Time
	Rule         string // thẻ quy tắc chia kỳ (mang ";cal=<phòng>:v<n>" — D8)
	// Status: open | closed | reopened (kỳ đã hủy không có trong kết quả).
	Status string
	// Submitted: bài nộp cuối của từng đơn vị (mọi trạng thái rà soát).
	Submitted map[string]UnitFigures
	// ClosedNo, ClosedAt, Closed: ảnh chụp đóng băng mới nhất (ClosedNo = 0 → chưa chốt lần nào, Closed nil). Closed chỉ
	// gồm đơn vị CÓ bài trong ảnh chụp.
	ClosedNo int64
	ClosedAt *time.Time
	Closed   map[string]UnitFigures
}

// FiguresQuery là phạm vi đọc: profile, mã biểu, khoảng ngày (kỳ có period_to ≥ From và period_from ≤ To).
type FiguresQuery struct {
	Profile, TemplateCode string
	From, To              time.Time
}

// MaxFiguresPeriods là số kỳ tối đa một lần đọc (BM-01 một năm × vài lô).
const MaxFiguresPeriods = 2000

// FiguresReader là cổng đọc số liệu kỳ native theo lô (người gọi lấy từ ctx — kiểm như REST).
type FiguresReader interface {
	Figures(ctx context.Context, q FiguresQuery) ([]PeriodFigures, error)
}

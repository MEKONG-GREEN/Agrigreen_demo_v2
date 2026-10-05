package port

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

// Trạng thái căn cứ của một quan hệ kế thừa (ADR-0032 mục 3).
const (
	// BasisOfficial: căn cứ pháp lý chính thức (nghị quyết sắp xếp đơn vị hành chính).
	BasisOfficial = "official"
	// BasisPendingC9: chưa có nguồn chính thức (C-9 / H-16) — dữ liệu điền từ
	// successor_codes, seed, UAT. Dòng 'official' cùng cặp được ưu tiên.
	BasisPendingC9 = "pending_c9"
)

// Succession là một quan hệ kế thừa: phiên bản tiền thân đóng hiệu lực ngày
// EffectiveOn và (một phần) diện tích của nó thuộc về phiên bản kế thừa.
type Succession struct {
	PredecessorCode      string
	PredecessorValidFrom time.Time // ngày, 00:00 giờ Việt Nam
	SuccessorCode        string
	SuccessorValidFrom   time.Time
	EffectiveOn          time.Time // = ValidTo của tiền thân
	// AreaShare: tỉ lệ diện tích tiền thân chuyển sang kế thừa, (0, 1]; nil = chưa biết.
	AreaShare   *decimal.Decimal
	LegalBasis  string
	BasisStatus string // BasisOfficial | BasisPendingC9
}

// RolledUnit là một mã tại ngày đích mà một mã cũ ánh xạ tới (RollForward).
type RolledUnit struct {
	Code string
	// Share: phần diện tích của mã nguồn thuộc về Code (tích AreaShare dọc đường kế thừa,
	// cộng các đường cùng đích); nil = chưa biết (có bước tách mà chưa có tỉ lệ).
	Share *decimal.Decimal
	// Estimated: có bước tách một phần trên đường đi (Share < 1 hoặc chưa biết) — số liệu
	// chia theo Share là ƯỚC TÍNH, giao diện phải gắn cờ (ADR-0032 mục 5).
	Estimated bool
	// Pending: có bước dựa trên căn cứ BasisPendingC9 (chưa chính thức).
	Pending bool
}

// AdminUnitHistory tra đơn vị hành chính theo thời điểm và chuỗi kế thừa (R-DB-09,
// ADR-0032 mục 5). Thực hiện bởi adapter AdminUnits của pf/masterdata; module gọi
// lấy qua Module.AdminUnitHistory() hoặc type assertion trên AdminUnits (giữ tương
// thích với bản giả AdminUnits trong kiểm thử). Không có → lỗi bọc ErrUnitNotFound.
type AdminUnitHistory interface {
	// AdminUnitAt: phiên bản của code có hiệu lực ngày at.
	AdminUnitAt(ctx context.Context, code string, at time.Time) (AdminUnit, error)
	// SuccessorsAt: chuỗi kế thừa từ phiên bản của code có hiệu lực ngày from, gồm mọi
	// quan hệ có EffectiveOn ≤ to (theo EffectiveOn rồi mã), kèm AreaShare.
	SuccessorsAt(ctx context.Context, code string, from, to time.Time) ([]Succession, error)
	// RollForward: ánh xạ code tại ngày at sang các mã có hiệu lực ngày target ≥ at
	// (báo cáo so sánh nhiều kỳ qua một lần sắp xếp), theo mã.
	RollForward(ctx context.Context, code string, at, target time.Time) ([]RolledUnit, error)
}

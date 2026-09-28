package mechanization

import (
	"encoding/json"
	"fmt"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/domain"
)

// RuleVersion là phiên bản quy tắc cân đối hiện hành (QT-01/QT-02).
const RuleVersion = domain.BalanceRuleVersion

// RecomputeInput là đầu vào đã LƯU của một dòng kết quả cân đối — dùng để tính
// lại khi diễn tập phục hồi (mục 22: "chạy lại một mô phỏng mẫu", cmd/drill).
type RecomputeInput struct {
	AreaHa         *decimal.Decimal // area_ha đã lưu (nil = chưa có diện tích)
	NormValue      *decimal.Decimal // norm_value đã lưu (nil = chưa cấu hình định mức)
	Capacity       int              // số máy Hoạt động tại T
	MachinesTotal  int
	ThresholdsJSON []byte // cột balance_run.thresholds
}

// RecomputeResult là số liệu tính lại, cùng dạng cột của balance_result.
type RecomputeResult struct {
	Required      *decimal.Decimal
	RequiredWhole *int
	CoveragePct   *decimal.Decimal
	Gap           *int
	Status        string
}

// Recompute áp lại đúng hàm thuần của FN-09 (domain.ComputeBalance) trên đầu
// vào đã lưu. Kết quả phải trùng từng cột với dòng đã lưu; lệch nghĩa là dữ
// liệu phục hồi hỏng hoặc quy tắc đã đổi mà không nâng RuleVersion.
func Recompute(in RecomputeInput) (RecomputeResult, error) {
	var th domain.Thresholds
	if len(in.ThresholdsJSON) > 0 {
		if err := json.Unmarshal(in.ThresholdsJSON, &th); err != nil {
			return RecomputeResult{}, fmt.Errorf("mechanization: thresholds hỏng: %w", err)
		}
	}
	var norm *domain.ParamRef
	if in.NormValue != nil {
		norm = &domain.ParamRef{Value: *in.NormValue}
	}
	f := domain.ComputeBalance(domain.BalanceInput{
		AreaHa: in.AreaHa, Norm: norm, MachinesTotal: in.MachinesTotal, Capacity: in.Capacity, Thresholds: th,
	})
	return RecomputeResult{Required: f.Required, RequiredWhole: f.RequiredWhole, CoveragePct: f.CoveragePct, Gap: f.Gap, Status: string(f.Status)}, nil
}

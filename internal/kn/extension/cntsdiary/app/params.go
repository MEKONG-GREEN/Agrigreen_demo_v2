package app

import (
	"context"
	"errors"
	"time"

	"github.com/shopspring/decimal"

	knport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
)

// Hạn sửa lần ghi đã gửi chưa xác thực (Mục III.5) và hạn lưu tọa độ GPS lúc ghi (giả định tạm Q-005) là tham số có phiên bản
// của pf/masterdata (KN-PARAMS-CNTS-DIARY-OWNER #85; đăng ký ở extension.CNTSDiaryMasterdataParams). Hợp đồng đọc:
//   - Đọc phiên bản ĐÃ DUYỆT có hiệu lực ở NGÀY NGHIỆP VỤ (giờ Việt Nam, clock.BusinessDay) của thời điểm sự việc:
//     hạn sửa — NGÀY GHI của lần ghi (entry_date; PO 27/10: đổi tham số sau không đổi hạn sửa của lần ghi đã có);
//     hạn lưu GPS — ngày thiết bị ghi (captured_at). Hạn lưu đã tính được
//     lưu cùng tọa độ (purge_after) nên đổi phiên bản sau không đổi hạn của tọa độ đã ghi; phát lại (Idempotency-Key) trả
//     bản đã lưu.
//   - Đọc NGOÀI giao dịch KN (pf/masterdata mở giao dịch riêng — P0-POOL-KN): bên gọi tra trước khi mở giao dịch.
//   - Chưa có phiên bản đã duyệt (ErrParamNotFound — tenant chưa nạp dữ liệu nền) hoặc chưa nối Params → mặc định
//     EditDays/FixRetentionDays (bằng seed đăng ký).
//   - Lỗi khác (DB, ctx thiếu tenant) hoặc giá trị không phải số nguyên trong [min, max] của đăng ký → ErrDependency (503), không
//     âm thầm rơi về mặc định, không cắt/làm tròn.
const (
	ParamEditWindow   = knport.ParamCNTSDiaryEditWindowDays
	ParamFixRetention = knport.ParamCNTSDiaryGPSRetentionDays

	EditDays         = 7   // mặc định khi chưa có phiên bản đã duyệt
	FixRetentionDays = 365 // mặc định khi chưa có phiên bản đã duyệt

	minEditDays, maxEditDays                 = 0, 30
	minFixRetentionDays, maxFixRetentionDays = 1, 3650
)

// editDays trả hạn sửa (ngày) có hiệu lực ở ngày nghiệp vụ của at, cho tenant của ctx (phiên đã xác thực).
func (s *Service) editDays(ctx context.Context, at time.Time) (int, error) {
	return s.dayParam(ctx, ParamEditWindow, at, EditDays, minEditDays, maxEditDays)
}

// fixRetentionDays trả hạn lưu tọa độ GPS (ngày) có hiệu lực ở ngày nghiệp vụ của at, cho tenant của ctx.
func (s *Service) fixRetentionDays(ctx context.Context, at time.Time) (int, error) {
	return s.dayParam(ctx, ParamFixRetention, at, FixRetentionDays, minFixRetentionDays, maxFixRetentionDays)
}

func (s *Service) dayParam(ctx context.Context, key string, at time.Time, def, lo, hi int64) (int, error) {
	if s.Params == nil {
		return int(def), nil
	}
	v, err := s.Params.At(ctx, key, clock.BusinessDay(at))
	if errors.Is(err, params.ErrParamNotFound) {
		return int(def), nil
	}
	if err != nil {
		return 0, ErrDependency.WithDetail("không đọc được tham số " + key).WithCause(err)
	}
	if !v.Value.IsInteger() || v.Value.LessThan(decimal.NewFromInt(lo)) || v.Value.GreaterThan(decimal.NewFromInt(hi)) {
		return 0, ErrDependency.WithDetail("tham số " + key + " đã duyệt không hợp lệ (số nguyên " + decimal.NewFromInt(lo).String() +
			"–" + decimal.NewFromInt(hi).String() + " ngày)")
	}
	return int(v.Value.IntPart()), nil
}

package assembly

import (
	"context"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/price"
	priceport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// ---------------------------------------------------------------------------
// cap/price (W11.71, ADR-0049): khung module giá. Cấu hình theo bên dùng — tương đương mục `cap/price` của
// app/KN/app.yaml trong kiến trúc v4.1 §7.2 (app.yaml của repo chưa mang cấu hình module — W1.3). Pool đơn vị
// (app_platform → r_price) cho /v1/price/*.
// ---------------------------------------------------------------------------

// KNPriceProfile là cấu hình cap/price của App Khuyến nông (Báo giá nông sản v1.3): TW xem, Tỉnh (Phòng TTTT) quản lý,
// Xã (CTV báo giá) nhập. Admin KN ghi danh mục nền, xã báo giá, phân công; Cấp Tỉnh có cờ "Quản trị nghiệp vụ báo giá"
// (W11.07 qtnv_bao_gia) cấu hình lịch, ngưỡng, mặt hàng (W11.72).
// Department là giả định (mã phòng ban của tenant chưa chốt — Q-02; docs/kn/gia-dinh-brd-2026-10.md).
var KNPriceProfile = priceport.Profile{
	Name:       "kn",
	Department: "thong_tin_truyen_thong",
	Roles:      map[string]string{authz.KNTW: "TW", authz.KNTinh: "TINH", authz.KNXa: "XA"},
	ManageBy:   []string{authz.KNAdmin},
	// W11.72: cờ QTNV báo giá trên lần gán Cấp Tỉnh (KNContext.HasDuty — W11.07).
	StewardDuty: authz.DutyPriceAdmin,
	// W11.74 (BRD FN-AD-04.04…06): lịch T2–T6 (Q-086 chưa trả lời — giữ mẫu BRD), khóa 12:00, hạn phản hồi 13:30;
	// nhắc 07:30 / 11:00 là giả định (BRD không nêu giờ); ngưỡng 30 %, N = 7, đi ngang 1 %, phiếu điều tra 40 %.
	// Tenant đổi bằng /v1/price/calendar/schedule và /v1/price/thresholds (sổ có phiên bản); đây chỉ là mặc định.
	Schedule: priceport.ScheduleDefaults{
		Weekdays:     []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
		LockAt:       "12:00",
		RespondBy:    "13:30",
		Remind1:      "07:30",
		Remind2:      "11:00",
		Channels:     []string{priceport.ChannelInApp, priceport.ChannelEmail},
		ThresholdPct: decimal.NewFromInt(30),
		LookbackDays: 7,
		FlatPct:      decimal.NewFromInt(1),
		SurveyPct:    decimal.NewFromInt(40),
		PreviewDays:  30,
	},
}

// PriceProfiles là cấu hình cap/price của mọi bên dùng (api-platform, job price_calendar của cmd/worker).
func PriceProfiles() []priceport.Profile { return []priceport.Profile{KNPriceProfile} }

// wirePrice dựng pf/price trên pool đơn vị; officers (W11.72) là danh bạ cán bộ cho phân công xã báo giá (nil → không kiểm).
func wirePrice(p *pgxpool.Pool, clk clock.Clock, officers priceport.Officers) (*price.Module, error) {
	m, err := price.New(p, clk, PriceProfiles())
	if err != nil {
		return nil, err
	}
	m.WireItemCalendar() // W11.73: mặt hàng áp dụng từ ngày báo giá kế tiếp theo lịch W11.74
	if officers != nil {
		m.UseOfficers(officers)
	}
	return m, nil
}

// PriceOfficers cài price/port.Officers từ pf/accounts (W11.72): cán bộ có lần gán Cấp Xã (kn_xa) / Cấp Tỉnh (kn_tinh)
// còn hiệu lực ở phòng ban chủ dữ liệu giá của Profile (KN: Phòng TTTT), tên hiển thị từ danh bạ tài khoản.
type PriceOfficers struct {
	Assignments accport.Assignments
	Names       partyport.ActorNames
}

// Officers trả cán bộ cấp level (XA | TINH) của phòng ban prof.Department, sắp theo tên.
func (o PriceOfficers) Officers(ctx context.Context, prof priceport.Profile, level string) ([]priceport.Officer, error) {
	role := authz.KNXa
	if level == "TINH" {
		role = authz.KNTinh
	}
	as, err := o.Assignments.ActiveAssignments(ctx, role, prof.Department)
	if err != nil {
		return nil, err
	}
	by := map[uuid.UUID]*priceport.Officer{}
	var refs []uuid.UUID
	for _, a := range as {
		x, ok := by[a.UserRef]
		if !ok {
			x = &priceport.Officer{Ref: a.UserRef, Level: level}
			by[a.UserRef] = x
			refs = append(refs, a.UserRef)
		}
		x.Communes = append(x.Communes, a.Communes...)
	}
	names := map[uuid.UUID]string{}
	if o.Names != nil && len(refs) > 0 {
		if names, err = o.Names.DisplayNames(ctx, refs); err != nil {
			return nil, err
		}
	}
	out := make([]priceport.Officer, 0, len(refs))
	for _, r := range refs {
		x := *by[r]
		x.Name = names[r]
		if x.Name == "" {
			x.Name = r.String()
		}
		out = append(out, x)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

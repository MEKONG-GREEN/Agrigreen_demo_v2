package assembly

import (
	"context"
	"errors"

	"github.com/google/uuid"

	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	notifyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	priceport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

// ---------------------------------------------------------------------------
// Gói NOTIF (G1, D5): người nhận và email của thông báo Báo giá nông sản (pf/price → pf/notify).
// ---------------------------------------------------------------------------

// NotifyPriceDesk cài notify/port.PriceDesk từ pf/accounts: cán bộ Cấp Tỉnh (kn_tinh) còn hiệu lực ở phòng ban chủ dữ
// liệu giá của Profile, và người có cờ "Quản trị nghiệp vụ báo giá" (Profile.StewardDuty) trên lần gán Cấp Tỉnh.
type NotifyPriceDesk struct {
	Holders  accport.RoleHolders
	Profiles []priceport.Profile
}

var _ notifyport.PriceDesk = NotifyPriceDesk{}

func (d NotifyPriceDesk) profile(name string) (priceport.Profile, bool) {
	for _, p := range d.Profiles {
		if p.Name == name || (name == "" && len(d.Profiles) > 0) {
			return p, true
		}
	}
	return priceport.Profile{}, false
}

// Stewards thực hiện PriceDesk. Bên dùng không có cờ QTNV → rỗng.
func (d NotifyPriceDesk) Stewards(ctx context.Context, tid uuid.UUID, profile string) ([]uuid.UUID, error) {
	p, ok := d.profile(profile)
	if !ok || p.StewardDuty == "" || d.Holders == nil {
		return nil, nil
	}
	return d.Holders.DutyHolders(ctx, tid, authz.KNTinh, p.Department, p.StewardDuty)
}

// ProvinceOfficers thực hiện PriceDesk.
func (d NotifyPriceDesk) ProvinceOfficers(ctx context.Context, tid uuid.UUID, profile string) ([]uuid.UUID, error) {
	p, ok := d.profile(profile)
	if !ok || d.Holders == nil {
		return nil, nil
	}
	return d.Holders.RoleHolders(ctx, tid, authz.KNTinh, p.Department)
}

// NotifyEmails bọc accounts/port.MailDirectory thành notify/port.EmailDirectory (đổi lỗi "không có email").
type NotifyEmails struct{ Dir accport.MailDirectory }

// EmailOf thực hiện notify/port.EmailDirectory.
func (e NotifyEmails) EmailOf(ctx context.Context, tid, actor uuid.UUID) (string, error) {
	addr, err := e.Dir.EmailOf(ctx, tid, actor)
	if errors.Is(err, accport.ErrNoEmail) {
		return "", notifyport.ErrNoEmail
	}
	return addr, err
}

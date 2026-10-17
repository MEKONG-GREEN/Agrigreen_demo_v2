package price

import (
	"context"
	priceport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/port"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/google/uuid"
)

type notificationProfile struct{ name, department, duty string }

// NotificationDirectory resolves price recipients from the generic accounts role/duty ports.
type NotificationDirectory struct {
	holders  accport.RoleHolders
	profiles []notificationProfile
}

var _ priceport.PriceDesk = (*NotificationDirectory)(nil)

// NewNotificationDirectory freezes only the immutable fields used by this selector.
func NewNotificationDirectory(holders accport.RoleHolders, profiles []priceport.Profile) *NotificationDirectory {
	d := &NotificationDirectory{holders: holders, profiles: make([]notificationProfile, len(profiles))}
	for i, p := range profiles {
		d.profiles[i] = notificationProfile{p.Name, p.Department, p.StewardDuty}
	}
	return d
}
func (d *NotificationDirectory) profile(name string) (notificationProfile, bool) {
	for _, p := range d.profiles {
		if p.name == name || (name == "" && len(d.profiles) > 0) {
			return p, true
		}
	}
	return notificationProfile{}, false
}
func (d *NotificationDirectory) Stewards(ctx context.Context, tid uuid.UUID, profile string) ([]uuid.UUID, error) {
	p, ok := d.profile(profile)
	if !ok || p.duty == "" || d.holders == nil {
		return nil, nil
	}
	return d.holders.DutyHolders(ctx, tid, authz.KNTinh, p.department, p.duty)
}
func (d *NotificationDirectory) ProvinceOfficers(ctx context.Context, tid uuid.UUID, profile string) ([]uuid.UUID, error) {
	p, ok := d.profile(profile)
	if !ok || d.holders == nil {
		return nil, nil
	}
	return d.holders.RoleHolders(ctx, tid, authz.KNTinh, p.department)
}

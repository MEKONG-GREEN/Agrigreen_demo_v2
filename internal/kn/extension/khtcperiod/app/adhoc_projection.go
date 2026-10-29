package app

import (
	"context"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/store"
	ext "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	forms "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/jackc/pgx/v5"
)

func adhocReader(c caller) bool {
	return !c.k.ReadOnly() && (c.k.Role == ext.SessionRoleProvince || c.k.Role == ext.SessionRoleAdmin && c.p.MFA)
}
func (s *Service) activeRequests(ctx context.Context, c caller, p store.Period) (map[string]*store.AdhocRequest, error) {
	out := map[string]*store.AdhocRequest{}
	if p.Type != domain.TypeAdhoc {
		return out, nil
	}
	err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		for _, dept := range p.Departments {
			r, e := store.ActiveAdhocRequest(ctx, tx, p.ID, dept)
			if e != nil {
				return e
			}
			if r != nil {
				out[dept] = r
			}
		}
		return nil
	})
	return out, err
}
func projectAdhocRequest(row *FormRow, r *store.AdhocRequest, u forms.UnitProgress, term string) {
	if r == nil {
		return
	}
	due := r.DueAt
	row.RequestDueAt = &due
	row.RequestState = r.State
	row.ReviewNote = r.Instruction
	if r.NativeSubmissionRef != nil && u.LastSubmissionID != nil && *r.NativeSubmissionRef == *u.LastSubmissionID && u.LastSubmittedAt != nil {
		native := u.Late
		row.NativeLate = &native
		row.Late = u.LastSubmittedAt.After(r.DueAt)
	}
	if term == "" && (r.State == "intent" || r.State == "issued") {
		row.View.CountsAsSent = false
		// Delivery in flight has no new native reviewable response yet.
		row.View.CanConfirm = false
		row.View.CanRequestSupplement = false
	}
}

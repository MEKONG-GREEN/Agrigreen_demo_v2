package app

// Hồ sơ HTX (pf_coop.cooperative) và sổ hồ sơ KN của HTX (pf_coop.kn_profile_event) cho sổ tổ chức pf/party —
// partyport.CoopProfiles, gốc lắp ráp nối (W3.3). pf/party giữ các màn HTX hướng Nút (G1 /v1/cooperatives*,
// /v1/orgs, KN FN-XA-03.03 /v1/kn/cooperatives*, danh bạ đối tác) và điều phối hai bước: Nút ở pf_party
// (vai r_party), hồ sơ ở đây (vai r_cooperative) — không giao dịch nào chạy qua hai vai.

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/member/store"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

var _ partyport.CoopProfiles = (*Service)(nil)

func toCoopProfile(p store.Profile) partyport.CoopProfile {
	return partyport.CoopProfile{TaxCode: p.TaxCode, AreaHa: p.AreaHa, EstablishedOn: p.EstablishedOn, Lat: p.Lat, Lon: p.Lon,
		Address: p.Address, UpdatedAt: p.UpdatedAt}
}

// isTaxCodeConflict: vi phạm chỉ mục duy nhất mã số thuế của hồ sơ HTX.
func isTaxCodeConflict(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "cooperative_tax_code"
}

// Profiles thực hiện partyport.CoopProfiles.
func (s *Service) Profiles(ctx context.Context, refs []uuid.UUID) (map[uuid.UUID]partyport.CoopProfile, error) {
	out := map[uuid.UUID]partyport.CoopProfile{}
	if len(refs) == 0 {
		return out, nil
	}
	var rows map[uuid.UUID]store.Profile
	if err := tenant.FromCtxTx(ctx, s.DB, func(tx pgx.Tx) error {
		var e error
		rows, e = store.ProfilesByRefs(ctx, tx, refs)
		return e
	}); err != nil {
		return nil, err
	}
	for k, v := range rows {
		out[k] = toCoopProfile(v)
	}
	return out, nil
}

// CreateProfile thực hiện partyport.CoopProfiles: bước 2 của tạo HTX, lũy đẳng theo Nút (đã có hồ sơ → nil).
func (s *Service) CreateProfile(ctx context.Context, ref uuid.UUID, p partyport.CoopProfile) error {
	tid, err := tenant.FromContext(ctx)
	if err != nil {
		return err
	}
	row := store.Profile{OrgRef: ref, TaxCode: strings.TrimSpace(p.TaxCode), AreaHa: p.AreaHa, EstablishedOn: p.EstablishedOn,
		Lat: p.Lat, Lon: p.Lon, Address: p.Address}
	err = tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		_, e := store.InsertProfile(ctx, tx, tid, row)
		return e
	})
	if isTaxCodeConflict(err) {
		return partyport.ErrCoopTaxCodeTaken
	}
	return err
}

// TaxCodeOwner thực hiện partyport.CoopProfiles.
func (s *Service) TaxCodeOwner(ctx context.Context, taxCode string) (uuid.UUID, error) {
	var ref uuid.UUID
	err := tenant.FromCtxTx(ctx, s.DB, func(tx pgx.Tx) error {
		var e error
		ref, e = store.TaxCodeOwner(ctx, tx, taxCode)
		return e
	})
	return ref, err
}

// MatchProfiles thực hiện partyport.CoopProfiles.
func (s *Service) MatchProfiles(ctx context.Context, f partyport.CoopProfileFilter) ([]uuid.UUID, error) {
	var out []uuid.UUID
	err := tenant.FromCtxTx(ctx, s.DB, func(tx pgx.Tx) error {
		var e error
		out, e = store.MatchProfiles(ctx, tx, f.TaxPrefix, f.AreaMin, f.AreaMax)
		return e
	})
	return out, err
}

// ActiveHouseholds thực hiện partyport.CoopProfiles.
func (s *Service) ActiveHouseholds(ctx context.Context, coops []uuid.UUID) (map[uuid.UUID]int64, error) {
	if len(coops) == 0 {
		return map[uuid.UUID]int64{}, nil
	}
	return s.memberCounts(ctx, coops)
}

// UpdateProfile thực hiện partyport.CoopProfiles: sự kiện bù trên kn_profile_event + bảng chiếu hồ sơ trong MỘT
// giao dịch (RB-1, R-21). Đã có dòng sổ cùng IdemKey → nil (lũy đẳng).
func (s *Service) UpdateProfile(ctx context.Context, c partyport.CoopProfileChange) error {
	tid, err := tenant.FromContext(ctx)
	if err != nil {
		return err
	}
	err = tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		if prev, err := store.ProfileEventByIdem(ctx, tx, c.IdemKey); err != nil || prev != nil {
			return err
		}
		ev := store.ProfileEvent{ID: ids.New(), TenantID: tid, SubjectKind: store.ProfileCooperative, SubjectRef: c.Ref,
			CommuneCode: c.CommuneCode, Action: "updated", Before: c.Before, After: c.After, Reason: c.Reason, OnBehalfOf: c.OnBehalfOf,
			SupportSessionID: c.SupportSessionID, SupportReason: c.SupportReason, EventTime: c.EventTime, ActorRef: c.ActorRef, IdemKey: c.IdemKey}
		if err := store.InsertProfileEvent(ctx, tx, &ev); err != nil {
			return err
		}
		return store.UpdateProfile(ctx, tx, c.Ref, c.AreaHa, c.TaxCode, c.Address)
	})
	switch {
	case isTaxCodeConflict(err):
		return partyport.ErrCoopTaxCodeTaken
	case errors.Is(ledger.Classify(err), ledger.ErrDuplicate):
		return nil // hai yêu cầu cùng khóa chạy song song: bên thắng đã ghi
	}
	return err
}

func toProfileEvent(e store.ProfileEvent) partyport.CoopProfileEvent {
	return partyport.CoopProfileEvent{SubjectRef: e.SubjectRef, Action: e.Action, Before: e.Before, After: e.After, Reason: e.Reason,
		SupportReason: e.SupportReason, ActorRef: e.ActorRef, EventTime: e.EventTime, RecordedAt: e.RecordedAt, Seq: e.Seq}
}

// ProfileEventByIdem thực hiện partyport.CoopProfiles.
func (s *Service) ProfileEventByIdem(ctx context.Context, key string) (*partyport.CoopProfileEvent, error) {
	var ev *store.ProfileEvent
	if err := tenant.FromCtxTx(ctx, s.DB, func(tx pgx.Tx) error {
		var e error
		ev, e = store.ProfileEventByIdem(ctx, tx, key)
		return e
	}); err != nil || ev == nil {
		return nil, err
	}
	out := toProfileEvent(*ev)
	return &out, nil
}

// ProfileEvents thực hiện partyport.CoopProfiles (chỉ dòng sổ của HTX).
func (s *Service) ProfileEvents(ctx context.Context, ref uuid.UUID) ([]partyport.CoopProfileEvent, error) {
	var evs []store.ProfileEvent
	if err := tenant.FromCtxTx(ctx, s.DB, func(tx pgx.Tx) error {
		var e error
		evs, e = store.ProfileEvents(ctx, tx, store.ProfileCooperative, ref)
		return e
	}); err != nil {
		return nil, err
	}
	out := make([]partyport.CoopProfileEvent, 0, len(evs))
	for _, e := range evs {
		out = append(out, toProfileEvent(e))
	}
	return out, nil
}

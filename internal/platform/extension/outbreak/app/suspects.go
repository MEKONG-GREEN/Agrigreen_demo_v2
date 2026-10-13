package app

import (
	"context"
	"slices"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/outbreak/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/outbreak/store"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// SuspectView là một phiên bản tin báo nghi dịch.
type SuspectView struct {
	store.SuspectRow
	Actor Actor
}

func (s *Service) susViews(ctx context.Context, rs []store.SuspectRow) []SuspectView {
	refs := make([]uuid.UUID, 0, len(rs))
	for _, r := range rs {
		refs = append(refs, r.ActorRef)
	}
	act := s.actors(ctx, refs)
	out := make([]SuspectView, 0, len(rs))
	for _, r := range rs {
		out = append(out, SuspectView{SuspectRow: r, Actor: act(r.ActorRef)})
	}
	return out
}

// ListSuspects trả bản hiện hành các tin báo nghi dịch trong phạm vi (status "" = mọi trạng thái).
func (s *Service) ListSuspects(ctx context.Context, status, commune string) ([]SuspectView, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return nil, err
	}
	if status != "" && !slices.Contains(domain.SuspectStatuses, status) {
		return nil, problem.Validation.WithField("status", "reported | confirmed | dismissed")
	}
	f := store.SuspectFilter{Status: status}
	if commune != "" {
		if !c.sees(commune) {
			return nil, ErrOutOfScope.WithField("commune", "xã ngoài phạm vi")
		}
		f.Communes = []string{commune}
	}
	var rs []store.SuspectRow
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		rs, e = store.SuspectsCurrent(ctx, tx, f)
		return e
	}); err != nil {
		return nil, err
	}
	out := rs[:0]
	for _, r := range rs {
		if c.sees(r.CommuneCode) {
			out = append(out, r)
		}
	}
	return s.susViews(ctx, out), nil
}

// GetSuspect trả mọi phiên bản của tin báo, mới nhất trước.
func (s *Service) GetSuspect(ctx context.Context, ref uuid.UUID) ([]SuspectView, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return nil, err
	}
	var rs []store.SuspectRow
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		rs, e = store.SuspectHistory(ctx, tx, ref)
		return e
	}); err != nil {
		return nil, err
	}
	if len(rs) == 0 || !c.sees(rs[0].CommuneCode) {
		return nil, ErrSuspectNotFound
	}
	return s.susViews(ctx, rs), nil
}

// SuspectInput là tin báo nghi dịch mới.
type SuspectInput struct {
	Suspect domain.Suspect
	IdemKey string
}

func (s *Service) replaySuspect(ctx context.Context, c caller, key string) (*SuspectView, error) {
	var r *store.SuspectRow
	err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		r, e = store.SuspectByIdem(ctx, tx, key)
		return e
	})
	if err != nil || r == nil {
		return nil, err
	}
	v := s.susViews(ctx, []store.SuspectRow{*r})[0]
	return &v, nil
}

func (s *Service) withSuspectReplay(ctx context.Context, c caller, key string, write func() (store.SuspectRow, error)) (SuspectView, bool, error) {
	if key == "" {
		return SuspectView{}, false, problem.IdempotencyRequired
	}
	if v, err := s.replaySuspect(ctx, c, key); err != nil || v != nil {
		if err != nil {
			return SuspectView{}, false, err
		}
		return *v, true, nil
	}
	row, err := write()
	if dup(err) {
		if v, rerr := s.replaySuspect(ctx, c, key); rerr == nil && v != nil {
			return *v, true, nil
		}
	}
	if err != nil {
		return SuspectView{}, false, err
	}
	return s.susViews(ctx, []store.SuspectRow{row})[0], false, nil
}

// ReportSuspect ghi nhận tin báo nghi dịch (Cấp Xã nhận tin của nông hộ, UBND xã, hoặc tự phát hiện; Web hoặc Mobile).
func (s *Service) ReportSuspect(ctx context.Context, in SuspectInput) (SuspectView, bool, error) {
	c, err := s.reporter(ctx)
	if err != nil {
		return SuspectView{}, false, err
	}
	su := in.Suspect
	return s.withSuspectReplay(ctx, c, in.IdemKey, func() (store.SuspectRow, error) {
		if es := domain.NormalizeSuspect(&su, s.now()); len(es) > 0 {
			return store.SuspectRow{}, fieldsProblem(ErrSuspectInvalid, es)
		}
		on := day(su.ObservedAt)
		if su.DiseaseCode != "" {
			if _, err := s.catalogItem(ctx, ErrSuspectInvalid, "disease_code", mdport.CatalogCNTSDisease, su.DiseaseCode, on); err != nil {
				return store.SuspectRow{}, err
			}
		}
		if su.SpeciesCode != "" {
			if _, err := s.catalogItem(ctx, ErrSuspectInvalid, "species_code", mdport.CatalogCNTSSpecies, su.SpeciesCode, on); err != nil {
				return store.SuspectRow{}, err
			}
		}
		if _, err := s.checkCommune(ctx, ErrSuspectInvalid, c, su.CommuneCode); err != nil {
			return store.SuspectRow{}, err
		}
		row := store.SuspectRow{Ref: newID(), Suspect: su, Meta: c.meta(domain.SuspectReported, s.now(), in.IdemKey)}
		err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error { return store.InsertSuspect(ctx, tx, c.p.TenantID, &row) })
		return row, err
	})
}

func latestSuspect(ctx context.Context, tx pgx.Tx, c caller, ref uuid.UUID, base *int64) (store.SuspectRow, error) {
	if err := store.Lock(ctx, tx, "suspect:"+ref.String()); err != nil {
		return store.SuspectRow{}, err
	}
	hist, err := store.SuspectHistory(ctx, tx, ref)
	if err != nil {
		return store.SuspectRow{}, err
	}
	if len(hist) == 0 || !c.sees(hist[0].CommuneCode) {
		return store.SuspectRow{}, ErrSuspectNotFound
	}
	cur := hist[0]
	if cur.Event != domain.SuspectReported {
		return cur, ErrSuspectResolved
	}
	if base != nil && *base != cur.Seq {
		return cur, ErrSuspectConflict.WithDetail("Đã có người cập nhật lúc " +
			cur.RecordedAt.In(clock.Zone()).Format("15:04 02/01/2006") + " (phiên bản " + strconv.FormatInt(cur.Seq, 10) + ")")
	}
	return cur, nil
}

// ConfirmInput là xác nhận tin báo thành ổ dịch: nội dung BM-10 đầy đủ (vị trí trống → lấy vị trí của tin báo).
type ConfirmInput struct {
	BaseVersion *int64
	Outbreak    domain.Outbreak
	IdemKey     string
}

// ConfirmSuspect: cán bộ xã xác nhận tin báo và tạo báo cáo dịch bệnh (FN-11 "cán bộ xã xác nhận và tạo báo cáo") —
// một giao dịch: ổ dịch phiên bản 1 (suspect_ref) + tin báo 'confirmed' (outbreak_ref) + sự kiện ổ dịch mới.
func (s *Service) ConfirmSuspect(ctx context.Context, ref uuid.UUID, in ConfirmInput) (OutbreakView, bool, error) {
	c, err := s.reporter(ctx)
	if err != nil {
		return OutbreakView{}, false, err
	}
	if in.BaseVersion == nil {
		return OutbreakView{}, false, errBaseVersionMissing
	}
	o := in.Outbreak
	return s.withReplay(ctx, c, in.IdemKey, func() (store.OutbreakRow, error) {
		// Đọc tin báo trước để bổ sung vị trí, xã (kiểm lại trong giao dịch có khóa).
		var pre store.SuspectRow
		if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
			var e error
			pre, e = latestSuspect(ctx, tx, c, ref, in.BaseVersion)
			return e
		}); err != nil {
			return store.OutbreakRow{}, err
		}
		if o.CommuneCode == "" {
			o.CommuneCode = pre.CommuneCode
		}
		if o.Location == nil && pre.Location != nil {
			o.Location, o.GPSAccuracyM, o.LocationSource = pre.Location, pre.GPSAccuracyM, "gps"
		}
		if o.Source == "" {
			o.Source = pre.ReporterKind
		}
		if o.ReceivedAt.IsZero() {
			o.ReceivedAt = pre.ObservedAt
		}
		if o.AsOf.IsZero() && !o.ReceivedAt.IsZero() {
			o.AsOf = day(o.ReceivedAt)
		}
		if o.CommuneCode != pre.CommuneCode {
			return store.OutbreakRow{}, ErrOutbreakInvalid.WithField("commune_code", "khác xã của tin báo nghi dịch")
		}
		disease, err := s.prepare(ctx, c, &o)
		if err != nil {
			return store.OutbreakRow{}, err
		}
		unit, err := s.checkCommune(ctx, ErrOutbreakInvalid, c, o.CommuneCode)
		if err != nil {
			return store.OutbreakRow{}, err
		}
		obRef := newID()
		o.SuspectRef = &ref
		row := store.OutbreakRow{Ref: obRef, Outbreak: o, Meta: c.meta(domain.EventReported, s.now(), in.IdemKey)}
		err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
			cur, err := latestSuspect(ctx, tx, c, ref, in.BaseVersion)
			if err != nil {
				return err
			}
			if err := store.InsertOutbreak(ctx, tx, c.p.TenantID, &row); err != nil {
				return err
			}
			sr := store.SuspectRow{Ref: ref, Suspect: cur.Suspect, OutbreakRef: &obRef,
				Meta: c.meta(domain.SuspectConfirmed, s.now(), in.IdemKey)}
			sr.CapturedAt = nil
			if err := store.InsertSuspect(ctx, tx, c.p.TenantID, &sr); err != nil {
				return err
			}
			return emitReported(ctx, tx, c, row, unit, disease)
		})
		return row, err
	})
}

// DismissSuspect: cán bộ xã bác bỏ tin báo (không phải dịch), có lý do.
func (s *Service) DismissSuspect(ctx context.Context, ref uuid.UUID, in StatusInput) (SuspectView, bool, error) {
	c, err := s.reporter(ctx)
	if err != nil {
		return SuspectView{}, false, err
	}
	if in.BaseVersion == nil {
		return SuspectView{}, false, errBaseVersionMissing
	}
	reason, es := domain.Reason(in.Reason)
	if len(es) > 0 {
		return SuspectView{}, false, fieldsProblem(ErrSuspectInvalid, es)
	}
	return s.withSuspectReplay(ctx, c, in.IdemKey, func() (store.SuspectRow, error) {
		var row store.SuspectRow
		err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
			cur, err := latestSuspect(ctx, tx, c, ref, in.BaseVersion)
			if err != nil {
				return err
			}
			row = store.SuspectRow{Ref: ref, Suspect: cur.Suspect, Meta: c.meta(domain.SuspectDismissed, s.now(), in.IdemKey)}
			row.CapturedAt = nil
			row.StatusReason = &reason
			return store.InsertSuspect(ctx, tx, c.p.TenantID, &row)
		})
		return row, err
	})
}

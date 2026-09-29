package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// GPSWire là toạ độ gửi lên: số thập phân dạng chuỗi (R-10).
type GPSWire struct {
	Lat       string  `json:"lat" pii:"true"`
	Lon       string  `json:"lon" pii:"true"`
	AccuracyM *string `json:"accuracy_m,omitempty"`
}

// FileRefWire là tham chiếu tệp minh chứng gửi lên.
type FileRefWire struct {
	FileRef string `json:"file_ref"`
	SHA256  string `json:"sha256"`
}

// CapturePayload là thân minh chứng — dùng chung cho POST /v1/mrv/evidence và
// payload của entity "evidence" khi đồng bộ (sync-contract.md). Khi đồng bộ, id,
// event_time, supersedes_id, correction_reason, device_id lấy từ bản ghi đồng bộ.
type CapturePayload struct {
	ID               *string                    `json:"id,omitempty"`
	EvidenceType     string                     `json:"evidence_type"`
	CatalogVersion   *int                       `json:"catalog_version,omitempty"`
	AdminUnitCode    string                     `json:"admin_unit_code"`
	PlotRef          *string                    `json:"plot_ref,omitempty"`
	HouseholdRef     *string                    `json:"household_ref,omitempty"`
	SeasonCode       *string                    `json:"season_code,omitempty"`
	Values           map[string]json.RawMessage `json:"values"`
	FileRefs         []FileRefWire              `json:"file_refs"`
	GPS              *GPSWire                   `json:"gps,omitempty"`
	EventTime        string                     `json:"event_time,omitempty"`
	SupersedesID     *string                    `json:"supersedes_id,omitempty"`
	CorrectionReason *string                    `json:"correction_reason,omitempty"`
	DeviceID         *string                    `json:"device_id,omitempty"`
}

// CaptureInput là dữ liệu vào của Capture đã kiểm dạng.
type CaptureInput struct {
	ID               *uuid.UUID
	EvidenceType     string
	CatalogVersion   int // 0 = bản đã duyệt mới nhất lúc ghi (được ghim vào bản ghi)
	AdminUnitCode    string
	PlotRef          *uuid.UUID
	HouseholdRef     *uuid.UUID
	SeasonCode       *string
	Values           map[string]json.RawMessage
	FileRefs         []domain.FileRef
	GPS              *domain.GeoPoint
	EventTime        time.Time
	SupersedesID     *uuid.UUID
	CorrectionReason *string
	DeviceID         *string
	IdemKey          string
}

// Parse kiểm dạng payload. Lỗi trả theo trường; không có giá trị toạ độ trong lỗi (R-14).
func (w CapturePayload) Parse() (CaptureInput, []problem.FieldError) {
	var errs []problem.FieldError
	add := func(f, r string) { errs = append(errs, problem.FieldError{Field: f, Reason: r}) }
	in := CaptureInput{
		EvidenceType: strings.TrimSpace(w.EvidenceType), AdminUnitCode: strings.TrimSpace(w.AdminUnitCode),
		SeasonCode: w.SeasonCode, Values: w.Values, CorrectionReason: w.CorrectionReason, DeviceID: w.DeviceID,
	}
	if w.CatalogVersion != nil {
		if *w.CatalogVersion <= 0 {
			add("catalog_version", "phải là số nguyên dương")
		}
		in.CatalogVersion = *w.CatalogVersion
	}
	parseID := func(field string, s *string, v7 bool) *uuid.UUID {
		if s == nil {
			return nil
		}
		var id uuid.UUID
		var err error
		if v7 {
			id, err = ids.Parse(*s)
		} else {
			id, err = ids.ParseRef(*s)
		}
		if err != nil {
			reason := "phải là UUID"
			if v7 {
				reason = "phải là UUIDv7"
			}
			add(field, reason)
			return nil
		}
		return &id
	}
	in.ID = parseID("id", w.ID, true)
	in.PlotRef = parseID("plot_ref", w.PlotRef, false)
	in.HouseholdRef = parseID("household_ref", w.HouseholdRef, false)
	in.SupersedesID = parseID("supersedes_id", w.SupersedesID, false)
	if w.EventTime != "" {
		t, err := time.Parse(time.RFC3339Nano, w.EventTime)
		if err != nil {
			add("event_time", "phải theo RFC 3339 có múi giờ")
		}
		in.EventTime = t
	}
	for _, f := range w.FileRefs {
		id, err := ids.ParseRef(f.FileRef)
		if err != nil {
			add("file_refs", "file_ref phải là UUID")
			continue
		}
		in.FileRefs = append(in.FileRefs, domain.FileRef{FileRef: id, SHA256: strings.ToLower(strings.TrimSpace(f.SHA256))})
	}
	if w.GPS != nil {
		lat, err1 := decimal.NewFromString(w.GPS.Lat)
		lon, err2 := decimal.NewFromString(w.GPS.Lon)
		if err1 != nil || err2 != nil {
			add("gps", "lat, lon phải là số thập phân dạng chuỗi")
		} else {
			g := domain.GeoPoint{Lat: lat, Lon: lon}
			if w.GPS.AccuracyM != nil {
				a, err := decimal.NewFromString(*w.GPS.AccuracyM)
				if err != nil {
					add("gps", "accuracy_m phải là số thập phân dạng chuỗi")
				} else {
					a = a.Round(2)
					g.AccuracyM = &a
				}
			}
			in.GPS = &g
		}
	}
	if in.CorrectionReason != nil {
		r := strings.TrimSpace(*in.CorrectionReason)
		in.CorrectionReason = &r
	}
	return in, errs
}

// Capture ghi một minh chứng (hoặc bản ghi bù khi có SupersedesID) sau khi kiểm:
// vai, phạm vi xã (scope.Allows), đồng ý của nông hộ (mrv.evidence), và dữ liệu
// theo ĐÚNG phiên bản danh mục được ghim. Gọi lặp cùng IdemKey trả lại bản ghi
// cũ, replayed = true.
func (s *Service) Capture(ctx context.Context, scope authz.Scope, in CaptureInput) (e domain.Evidence, replayed bool, err error) {
	p, err := authz.Require(ctx, RoleCollector)
	if err != nil {
		return e, false, err
	}
	if in.IdemKey == "" {
		return e, false, problem.IdempotencyRequired
	}
	id := ids.New()
	if in.ID != nil {
		id = *in.ID
	}
	values := in.Values
	if values == nil {
		values = map[string]json.RawMessage{}
	}
	e = domain.Evidence{
		ID: id, TenantID: p.TenantID, RootID: id, ChainVersion: 1, EvidenceType: in.EvidenceType,
		CatalogVersion: in.CatalogVersion, AdminUnitCode: in.AdminUnitCode, PlotRef: in.PlotRef, HouseholdRef: in.HouseholdRef,
		SeasonCode: in.SeasonCode, Values: values, FileRefs: in.FileRefs, GPS: in.GPS, EventTime: in.EventTime.UTC(),
		SupersedesID: in.SupersedesID, CorrectionReason: in.CorrectionReason, ActorRef: p.ActorRef, DeviceID: in.DeviceID,
		IdemKey: in.IdemKey,
	}
	if err := validation(e.ValidateEnvelope(s.Clock.Now())); err != nil {
		return e, false, err
	}
	// Đường nhanh cho gọi lặp / đồng bộ lặp: trả bản đã ghi, không kiểm lại.
	if prev, err := s.evidenceByIdem(ctx, p.TenantID, in.IdemKey); err != nil || prev != nil {
		if prev != nil {
			return *prev, true, nil
		}
		return e, false, err
	}
	if !scope.Allows(e.AdminUnitCode) {
		return e, false, ErrOutOfScope
	}
	if e.HouseholdRef != nil {
		if s.Consent == nil {
			return e, false, ErrDependencyUnavailable
		}
		ok, err := s.Consent.HasConsent(ctx, *e.HouseholdRef, port.ConsentPurpose)
		if err != nil {
			return e, false, err
		}
		if !ok {
			return e, false, ErrConsentMissing
		}
	}

	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.EvidenceByIdemKey(ctx, tx, in.IdemKey); err != nil {
			return err
		} else if prev != nil {
			e, replayed = *prev, true
			return nil
		}
		if in.ID != nil {
			if prev, err := store.EvidenceByID(ctx, tx, id); err != nil {
				return err
			} else if prev != nil {
				return ErrEvidenceIDReused
			}
		}
		cat, err := store.Approved(ctx, tx, e.EvidenceType, e.CatalogVersion)
		if err != nil {
			return err
		}
		if cat == nil {
			return ErrCatalogUnavailable
		}
		e.CatalogVersion = cat.Version // ghim phiên bản
		if err := validation(cat.ValidateRecord(e.Subject())); err != nil {
			return err
		}
		if e.SupersedesID != nil {
			if err := s.chainCorrection(ctx, tx, scope, &e); err != nil {
				return err
			}
		}
		if err := store.InsertEvidence(ctx, tx, &e); err != nil {
			return ledger.Classify(err)
		}
		return store.UpsertCurrent(ctx, tx, e)
	})
	if errors.Is(err, ledger.ErrDuplicate) {
		// Hai request cùng khóa chạy song song: bên thua đọc lại bản ghi của bên thắng.
		if prev, rerr := s.evidenceByIdem(ctx, p.TenantID, in.IdemKey); rerr == nil && prev != nil {
			return *prev, true, nil
		}
	}
	return e, replayed, err
}

// chainCorrection nối bản ghi bù vào chuỗi: chỉ sửa được bản HIỆN HÀNH, cùng loại
// minh chứng, bản cũ phải trong phạm vi.
func (s *Service) chainCorrection(ctx context.Context, tx pgx.Tx, scope authz.Scope, e *domain.Evidence) error {
	prev, err := store.EvidenceByID(ctx, tx, *e.SupersedesID)
	if err != nil {
		return err
	}
	if prev == nil || !scope.Allows(prev.AdminUnitCode) {
		return ErrEvidenceNotFound.WithDetail("supersedes_id không tồn tại hoặc ngoài phạm vi")
	}
	if prev.EvidenceType != e.EvidenceType {
		return problem.Validation.WithField("evidence_type", "bản ghi bù phải cùng loại minh chứng với bản bị thay")
	}
	cur, err := store.CurrentForUpdate(ctx, tx, prev.RootID)
	if err != nil {
		return err
	}
	if cur == nil || cur.HeadID != prev.ID {
		return ErrEvidenceSuperseded
	}
	e.RootID, e.ChainVersion = prev.RootID, cur.ChainVersion+1
	return nil
}

func (s *Service) evidenceByIdem(ctx context.Context, tid uuid.UUID, key string) (*domain.Evidence, error) {
	var prev *domain.Evidence
	err := s.tx(ctx, tid, func(tx pgx.Tx) error {
		var e error
		prev, e = store.EvidenceByIdemKey(ctx, tx, key)
		return e
	})
	return prev, err
}

// CaptureHTTP là Capture với phạm vi tính từ người gọi (Application API).
func (s *Service) CaptureHTTP(ctx context.Context, in CaptureInput) (domain.Evidence, bool, error) {
	if _, err := authz.Require(ctx, RoleCollector); err != nil {
		return domain.Evidence{}, false, err
	}
	sc, err := s.scope(ctx)
	if err != nil {
		return domain.Evidence{}, false, err
	}
	return s.Capture(ctx, sc, in)
}

// EvidenceView là một phiên bản minh chứng kèm trạng thái chuỗi.
type EvidenceView struct {
	Evidence     domain.Evidence
	HeadID       uuid.UUID
	ReviewStatus domain.ReviewStatus // của bản hiện hành
	Chain        []domain.Evidence   // mọi phiên bản (sửa sai), cũ → mới
	Reviews      []domain.Review     // mọi quyết định duyệt của chuỗi
}

// Get đọc một phiên bản minh chứng trong phạm vi người gọi (ngoài phạm vi → 404).
func (s *Service) Get(ctx context.Context, id uuid.UUID) (EvidenceView, error) {
	p, err := authz.Require(ctx, evidenceReaders...)
	if err != nil {
		return EvidenceView{}, err
	}
	sc, err := s.scope(ctx)
	if err != nil {
		return EvidenceView{}, err
	}
	var v EvidenceView
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		e, err := store.EvidenceByID(ctx, tx, id)
		if err != nil {
			return err
		}
		if e == nil || !sc.Allows(e.AdminUnitCode) {
			return ErrEvidenceNotFound
		}
		v.Evidence = *e
		cur, err := store.CurrentForUpdate(ctx, tx, e.RootID)
		if err != nil {
			return err
		}
		if cur != nil {
			v.HeadID, v.ReviewStatus = cur.HeadID, cur.ReviewStatus
		}
		if v.Chain, err = store.Chain(ctx, tx, e.RootID); err != nil {
			return err
		}
		v.Reviews, err = store.ReviewsOfRoot(ctx, tx, e.RootID)
		return err
	})
	return v, err
}

// ListInput lọc danh sách minh chứng hiện hành.
type ListInput struct {
	EvidenceType  string
	AdminUnitCode string
	ReviewStatus  string
	After         uuid.UUID
	Limit         int
}

// List liệt kê bản hiện hành của các chuỗi trong phạm vi (hàng chờ duyệt: review_status=pending).
func (s *Service) List(ctx context.Context, in ListInput) (Page[store.Listed], error) {
	p, err := authz.Require(ctx, evidenceReaders...)
	if err != nil {
		return Page[store.Listed]{}, err
	}
	switch domain.ReviewStatus(in.ReviewStatus) {
	case "", domain.ReviewPending, domain.ReviewAccepted, domain.ReviewRejected:
	default:
		return Page[store.Listed]{}, problem.Validation.WithField("review_status", "phải là pending, accepted hoặc rejected")
	}
	if in.EvidenceType != "" && !domain.ValidEvidenceType(in.EvidenceType) {
		return Page[store.Listed]{}, problem.Validation.WithField("evidence_type", "không hợp lệ")
	}
	sc, err := s.scope(ctx)
	if err != nil {
		return Page[store.Listed]{}, err
	}
	limit := clampLimit(in.Limit)
	var items []store.Listed
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		items, e = store.ListCurrent(ctx, tx, store.ListFilter{
			EvidenceType: in.EvidenceType, AdminUnitCode: in.AdminUnitCode, ReviewStatus: in.ReviewStatus,
			All: sc.All, Units: sc.Units,
		}, in.After, limit+1)
		return e
	})
	if err != nil {
		return Page[store.Listed]{}, err
	}
	return page(items, limit, func(l store.Listed) uuid.UUID { return l.Evidence.RootID }), nil
}

// ReviewInput là dữ liệu vào của Review.
type ReviewInput struct {
	EvidenceID uuid.UUID
	Decision   domain.ReviewStatus
	Reason     string
	IdemKey    string
}

// Review ghi quyết định duyệt của TTKN tỉnh cho BẢN HIỆN HÀNH của một minh
// chứng (KN US-MRV-05). Người duyệt không được là người đã thu bất kỳ phiên bản
// nào của chuỗi. Mỗi phiên bản chỉ có một quyết định; sửa sai minh chứng tạo
// phiên bản mới chờ duyệt lại.
func (s *Service) Review(ctx context.Context, in ReviewInput) (r domain.Review, replayed bool, err error) {
	p, err := authz.Require(ctx, RoleReviewer)
	if err != nil {
		return r, false, err
	}
	if in.IdemKey == "" {
		return r, false, problem.IdempotencyRequired
	}
	if err := validation(domain.ValidateReview(in.Decision, in.Reason)); err != nil {
		return r, false, err
	}
	sc, err := s.scope(ctx)
	if err != nil {
		return r, false, err
	}
	r = domain.Review{
		ID: ids.New(), TenantID: p.TenantID, EvidenceID: in.EvidenceID, Decision: in.Decision,
		Reason: strings.TrimSpace(in.Reason), EventTime: s.Clock.Now(), ActorRef: p.ActorRef, IdemKey: in.IdemKey,
	}
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.ReviewByIdemKey(ctx, tx, in.IdemKey); err != nil {
			return err
		} else if prev != nil {
			r, replayed = *prev, true
			return nil
		}
		e, err := store.EvidenceByID(ctx, tx, in.EvidenceID)
		if err != nil {
			return err
		}
		if e == nil || !sc.Allows(e.AdminUnitCode) {
			return ErrEvidenceNotFound
		}
		cur, err := store.CurrentForUpdate(ctx, tx, e.RootID)
		if err != nil {
			return err
		}
		if cur == nil || cur.HeadID != e.ID {
			return ErrEvidenceSuperseded
		}
		if cur.ReviewID != nil {
			return ErrReviewExists
		}
		actors, err := store.ChainActors(ctx, tx, e.RootID)
		if err != nil {
			return err
		}
		if containsID(actors, p.ActorRef) {
			return ErrReviewerIsCapturer
		}
		r.RootID = e.RootID
		if err := store.InsertReview(ctx, tx, &r, e.AdminUnitCode); err != nil {
			return ledger.Classify(err)
		}
		if err := store.SetReview(ctx, tx, e.RootID, r.Decision, r.ID); err != nil {
			return err
		}
		// G3: chỉ minh chứng đã duyệt được tính vào ước tính (KN US-MRV-05) → tính lại
		// ước tính TẠM của các vụ cùng thửa + mã vụ trong cùng giao dịch.
		if e.PlotRef != nil && e.SeasonCode != nil {
			refs, err := store.SeasonRefsOf(ctx, tx, *e.PlotRef, *e.SeasonCode)
			if err != nil {
				return err
			}
			for _, ref := range refs {
				if err := s.recompute(ctx, tx, p.TenantID, ref, p.ActorRef); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if errors.Is(err, ledger.ErrDuplicate) {
		var prev *domain.Review
		if rerr := s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
			var e error
			prev, e = store.ReviewByIdemKey(ctx, tx, in.IdemKey)
			return e
		}); rerr == nil && prev != nil {
			return *prev, true, nil
		}
	}
	return r, replayed, err
}

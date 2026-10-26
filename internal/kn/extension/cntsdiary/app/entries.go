package app

// Lần ghi nhật ký (UR-XA-11, UR-XA-12) và rà soát, xác thực (UR-TINH-12, FN-13). Vòng đời Mục III.5: Lưu nháp → Đã gửi →
// Đã xác thực; Đã gửi → Yêu cầu bổ sung (lý do) → Đã gửi. Chưa xác thực: sửa trong n ngày (tham số có phiên bản, mặc định 7 — params.go) kể từ
// ngày ghi; đã xác thực không sửa — điều chỉnh tạo phiên bản 'corrected' có lý do, chờ xác thực lại (AC-04).
//
// Vị trí (NT-15, NC-16): tọa độ thiết bị gửi kèm LẦN TẠO, không đổi ở phiên bản sau. Tọa độ vào bảng thường
// cnts_diary_fix (QĐ-8, có hạn lưu); sổ chỉ giữ sai số, khoảng cách tới điểm của hộ (hoặc điểm trình diễn) và cờ.
// Ngoại tuyến (AC-03): máy khách sinh mã lần ghi (UUIDv7), gửi thời điểm thiết bị (captured_at) và vị trí lúc ghi; gửi
// lại cùng Idempotency-Key trả bản đã ghi.

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	alertapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsalert/app"
	alertdomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsalert/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsdiary/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsdiary/store"
	mstore "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmodel/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// EntryView là lần ghi (bản hiện hành) cho API.
type EntryView struct {
	store.Entry
	SiteName      string
	ProjectCode   string
	Breed         string
	OwnerLabel    string
	EditableUntil time.Time
	CanEdit       bool
	CanSubmit     bool
	CanReview     bool
	CanCorrect    bool
	// Chỉ chi tiết:
	Versions  []store.Entry
	Fix       *store.FixRow // tọa độ lúc ghi (người có quyền; nil nếu đã xóa theo hạn lưu)
	Target    *domain.Point // tọa độ hộ / điểm để so
	PhotoMeta []store.Photo // ảnh của lần ghi
	Previous  *store.Entry  // lần ghi trước của lứa (so sánh, biểu đồ)
	Weights   []WeightPoint // trọng lượng theo lần cân của lứa (biểu đồ tăng trọng)
	Names     map[uuid.UUID]string
	Replayed  bool
}

// WeightPoint là một lần cân (biểu đồ tăng trọng UR-TINH-12).
type WeightPoint struct {
	Date   time.Time
	Weight decimal.Decimal
}

// EntryInput là thân tạo/sửa/điều chỉnh lần ghi.
type EntryInput struct {
	Ref         *uuid.UUID // tạo: UUIDv7 máy khách sinh (tùy chọn — ngoại tuyến)
	CycleRef    uuid.UUID  // tạo
	BaseVersion *int64     // sửa, điều chỉnh
	Submit      bool       // tạo/sửa: gửi luôn
	CapturedAt  *time.Time // thời điểm thiết bị (mặc định: giờ máy chủ)
	EntryDate   *time.Time // ngày ghi (mặc định: ngày của captured_at)
	Fix         *domain.Fix
	Photos      []uuid.UUID
	Kind        string
	Metrics     map[string]string
	Tables      domain.Tables
	Reason      string // điều chỉnh
	IdemKey     string
}

func idemEntry(k string) string { return "dentry:" + k }

func (s *Service) entryView(c caller, hc *hctx, cy *store.Cycle, e store.Entry, editDays int) EntryView {
	v := EntryView{Entry: e, SiteName: hc.x.Name, ProjectCode: hc.p.Code, EditableUntil: domain.EditableUntil(e.EntryDate, editDays)}
	if cy != nil {
		v.Breed = cy.Breed
	}
	open := hc.open() && (cy == nil || cy.Event != domain.EventClosed)
	keeper := open && c.keeper(hc)
	switch e.Status {
	case domain.StatusDraft, domain.StatusReturned:
		v.CanEdit, v.CanSubmit = keeper, keeper
	case domain.StatusSubmitted:
		v.CanEdit = keeper && !s.today().After(v.EditableUntil)
		v.CanReview = c.reviewer(hc.p) && !authoredBy(e, c.me())
	case domain.StatusVerified:
		v.CanCorrect = keeper
	}
	return v
}

// authoredBy: người gọi là người ghi lần ghi (bản hiện hành).
func authoredBy(e store.Entry, me uuid.UUID) bool { return e.AuthorRef == me }

// visible: bản nháp chỉ người ghi được thấy.
func (c caller) visibleEntry(hc *hctx, e store.Entry) bool {
	return e.Status != domain.StatusDraft || c.keeper(hc) || c.admin()
}

// loadEntryCtx đọc lần ghi, khóa lứa, kiểm phạm vi.
func (s *Service) lockEntry(ctx context.Context, tx pgx.Tx, c caller, ref uuid.UUID) (*store.Entry, *store.Cycle, *hctx, error) {
	e, err := store.LatestEntry(ctx, tx, ref)
	if err != nil {
		return nil, nil, nil, err
	}
	if e == nil {
		return nil, nil, nil, ErrEntryNotFound
	}
	if err := store.Lock(ctx, tx, "hh:"+e.HouseholdRef.String()); err != nil {
		return nil, nil, nil, err
	}
	if e, err = store.LatestEntry(ctx, tx, ref); err != nil {
		return nil, nil, nil, err
	}
	hc, err := s.loadHousehold(ctx, tx, c, e.HouseholdRef)
	if err != nil {
		return nil, nil, nil, err
	}
	if hc == nil || !c.visibleEntry(hc, *e) {
		return nil, nil, nil, ErrEntryNotFound
	}
	cy, err := store.LatestCycle(ctx, tx, e.CycleRef)
	if err != nil {
		return nil, nil, nil, err
	}
	if cy == nil {
		return nil, nil, nil, ErrEntryNotFound
	}
	return e, cy, hc, nil
}

// normalizeInput kiểm phần nghiệp vụ, ngày ghi, thời điểm thiết bị.
func (s *Service) normalizeInput(in *EntryInput, cy store.Cycle) (domain.Entry, time.Time, error) {
	now := s.now()
	captured := now
	if in.CapturedAt != nil {
		captured = in.CapturedAt.UTC()
	}
	var es domain.Errors
	if captured.After(now.Add(domain.FutureSkew)) {
		es = append(es, domain.FieldErr{Field: "captured_at", Reason: "không ở tương lai"})
	}
	if captured.Before(now.AddDate(0, 0, -domain.MaxOfflineDays)) {
		es = append(es, domain.FieldErr{Field: "captured_at", Reason: "bản ngoại tuyến quá 30 ngày — liên hệ chủ nhiệm"})
	}
	capDay := domain.Day(captured, clock.Zone())
	day := capDay
	if in.EntryDate != nil {
		day = *in.EntryDate
	}
	switch {
	case day.After(capDay):
		es = append(es, domain.FieldErr{Field: "entry_date", Reason: "không sau ngày ghi trên thiết bị"})
	case day.Before(capDay.AddDate(0, 0, -domain.MaxBackfillDay)):
		es = append(es, domain.FieldErr{Field: "entry_date", Reason: "ghi bù tối đa 7 ngày"})
	case day.Before(cy.ReceivedDate):
		es = append(es, domain.FieldErr{Field: "entry_date", Reason: "trước ngày nhận giống của lứa"})
	}
	en := domain.Entry{Kind: in.Kind, EntryDate: day, Metrics: in.Metrics, Tables: in.Tables}
	es = append(es, en.Normalize(cy.Form)...)
	if len(in.Photos) > domain.MaxPhotos {
		es = append(es, domain.FieldErr{Field: "photo_refs", Reason: "tối đa 10 ảnh"})
	}
	if in.Fix != nil {
		es = append(es, in.Fix.Validate()...)
	}
	if len(es) > 0 {
		return en, captured, fieldsProblem(ErrEntryInvalid, es)
	}
	return en, captured, nil
}

// photosOf kiểm ảnh thuộc hộ; trả số ảnh cân mẫu.
func photosOf(ctx context.Context, tx pgx.Tx, household uuid.UUID, refs []uuid.UUID) (int, error) {
	ps, err := store.Photos(ctx, tx, refs)
	if err != nil {
		return 0, err
	}
	n := 0
	for i, r := range refs {
		p, ok := ps[r]
		if !ok || p.HouseholdRef != household {
			return 0, ErrEntryInvalid.WithField("photo_refs["+itoa(i)+"]", "ảnh chưa tải lên hoặc không thuộc hộ này")
		}
		if p.Kind == domain.PhotoWeighing {
			n++
		}
	}
	return n, nil
}

func itoa(i int) string { return decimal.NewFromInt(int64(i)).String() }

// evaluate dựng kết quả kiểm tra tự động của bản en (mã ref) so với các lần ghi khác của lứa.
func (s *Service) evaluate(ctx context.Context, tx pgx.Tx, cy store.Cycle, modelType string, ref uuid.UUID, en domain.Entry, captured time.Time,
	weighing int, loc domain.Location) ([]domain.Check, error) {
	others, err := store.ListEntries(ctx, tx, store.EntryFilter{Cycle: &cy.Ref})
	if err != nil {
		return nil, err
	}
	in := domain.EvalInput{Form: cy.Form, Entry: en, InitialCount: cy.InitialCount, WeighingPhotos: weighing, Location: loc,
		Thresholds: s.thresholds(ctx, modelType), MaxDistM: s.param(ctx, ParamDistance, "200"), MaxAccM: s.param(ctx, ParamAccuracy, "50")}
	var prev, prevW *store.Entry
	for i := range others {
		o := others[i]
		if o.Ref == ref || o.Status == domain.StatusDraft {
			continue
		}
		in.Treatments = append(in.Treatments, o.Tables.Treatments...)
		before := o.EntryDate.Before(en.EntryDate) || (o.EntryDate.Equal(en.EntryDate) && o.CapturedAt.Before(captured))
		if !before {
			continue
		}
		if prev == nil || later(o, *prev) {
			prev = &others[i]
		}
		if domain.Dec(o.Metrics, "weight_avg_g") != nil && (prevW == nil || later(o, *prevW)) {
			prevW = &others[i]
		}
	}
	if prev != nil {
		in.Prev = &domain.Previous{EntryDate: prev.EntryDate, Metrics: prev.Metrics}
	}
	if prevW != nil {
		in.PrevWeight = &domain.Previous{EntryDate: prevW.EntryDate, Metrics: prevW.Metrics}
	}
	return domain.Evaluate(in), nil
}

func later(a, b store.Entry) bool {
	return a.EntryDate.After(b.EntryDate) || (a.EntryDate.Equal(b.EntryDate) && a.CapturedAt.After(b.CapturedAt))
}

// target trả tọa độ của hộ (ưu tiên) hoặc điểm trình diễn (W11.47 cnts_model_point).
func target(ctx context.Context, tx pgx.Tx, hc *hctx) (*domain.Point, error) {
	hp, err := mstore.Points(ctx, tx, "household", []uuid.UUID{hc.h.Ref})
	if err != nil {
		return nil, err
	}
	if p, ok := hp[hc.h.Ref]; ok {
		return &domain.Point{Lat: p.Lat, Lng: p.Lng}, nil
	}
	sp, err := mstore.Points(ctx, tx, "site", []uuid.UUID{hc.x.Ref})
	if err != nil {
		return nil, err
	}
	if p, ok := sp[hc.x.Ref]; ok {
		return &domain.Point{Lat: p.Lat, Lng: p.Lng}, nil
	}
	return nil, nil
}

func blockedProblem(cs []domain.Check) error {
	b := domain.Blocking(cs)
	if len(b) == 0 {
		return nil
	}
	p := ErrEntryBlocked
	for _, x := range b {
		p = p.WithField(x.Field, x.Message)
	}
	return p
}

// dayTaken: BM-08/09 một lần ghi mỗi ngày mỗi lứa.
func dayTaken(ctx context.Context, tx pgx.Tx, cy store.Cycle, ref uuid.UUID, day time.Time) error {
	if cy.Form == domain.FormBM19 {
		return nil
	}
	es, err := store.ListEntries(ctx, tx, store.EntryFilter{Cycle: &cy.Ref})
	if err != nil {
		return err
	}
	for _, o := range es {
		if o.Ref != ref && o.EntryDate.Equal(day) {
			return ErrEntryDayTaken.WithField("entry_date", "đã có lần ghi ngày "+day.Format("02/01/2006")+" — sửa lần ghi đó")
		}
	}
	return nil
}

// alerts sinh / tự đóng cảnh báo dòng 6–9 cho lần ghi vừa gửi.
func (s *Service) alerts(ctx context.Context, tx pgx.Tx, c caller, hc *hctx, e store.Entry) error {
	if s.Alerts == nil {
		return nil
	}
	province := s.provinceOf(ctx, e.CommuneCode)
	for _, kind := range domain.AlertKinds {
		hits := domain.Hits(e.Checks, kind)
		var fs []alertdomain.Finding
		for _, h := range hits {
			f := alertdomain.Finding{Field: h.Field, Label: h.Code, Reason: h.Message}
			if v, err := decimal.NewFromString(h.Value); err == nil {
				f.Current = v
			}
			if v, err := decimal.NewFromString(h.Compare); err == nil {
				f.Compare = &v
			}
			fs = append(fs, f)
		}
		recips := []uuid.UUID{hc.p.ManagerRef}
		if (kind == domain.AlertWater || kind == domain.AlertMortality) && hc.x.OfficerRef != nil {
			recips = append(recips, *hc.x.OfficerRef)
		}
		if err := s.Alerts.LogVerdict(ctx, tx, c.p.TenantID, alertapp.LogAlert{Kind: kind, Hit: len(hits) > 0, HouseholdRef: e.HouseholdRef,
			EntryRef: e.Ref, CommuneCode: e.CommuneCode, ProvinceCode: province, Form: e.Form,
			Title: e.Form + " · " + hc.x.Name + " · " + e.EntryDate.Format("02/01/2006"), Findings: fs, Recipients: recips, At: s.now()}); err != nil {
			return err
		}
	}
	return nil
}

// CreateEntry ghi lần ghi mới (nháp, hoặc gửi luôn khi Submit).
func (s *Service) CreateEntry(ctx context.Context, in EntryInput) (EntryView, error) {
	c, err := s.writer(ctx, in.IdemKey)
	if err != nil {
		return EntryView{}, err
	}
	ref := ids.New()
	if in.Ref != nil {
		ref = *in.Ref
	}
	var out store.Entry
	replayed := false
	// Hạn lưu GPS: phiên bản có hiệu lực ở ngày thiết bị ghi, đọc NGOÀI giao dịch (#85). Chốt thời điểm ghi mặc định trước
	// giao dịch để ngày tra tham số trùng ngày tính purge_after.
	if in.CapturedAt == nil {
		now := s.now()
		in.CapturedAt = &now
	}
	retention := FixRetentionDays
	if in.Fix != nil {
		if retention, err = s.fixRetentionDays(ctx, *in.CapturedAt); err != nil {
			return EntryView{}, err
		}
	}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.EntryByIdem(ctx, tx, idemEntry(in.IdemKey)); err != nil || prev != nil {
			if prev != nil {
				out, replayed = *prev, true
			}
			return err
		}
		cy, err := store.LatestCycle(ctx, tx, in.CycleRef)
		if err != nil {
			return err
		}
		if cy == nil {
			return ErrCycleNotFound.WithField("cycle_ref", "không có lứa nuôi")
		}
		if err := store.Lock(ctx, tx, "hh:"+cy.HouseholdRef.String()); err != nil {
			return err
		}
		if cy, err = store.LatestCycle(ctx, tx, in.CycleRef); err != nil {
			return err
		}
		hc, err := s.loadHousehold(ctx, tx, c, cy.HouseholdRef)
		if err != nil {
			return err
		}
		if hc == nil {
			return ErrCycleNotFound.WithField("cycle_ref", "không có lứa nuôi")
		}
		if !c.keeper(hc) {
			return problem.Forbidden.WithDetail("chỉ cán bộ theo dõi điểm / người ghi nhật ký của hộ ghi nhật ký")
		}
		if cy.Event == domain.EventClosed || !hc.open() {
			return ErrCycleClosed
		}
		en, captured, err := s.normalizeInput(&in, *cy)
		if err != nil {
			return err
		}
		if old, err := store.LatestEntry(ctx, tx, ref); err != nil {
			return err
		} else if old != nil {
			return ErrEntryInvalid.WithField("id", "mã lần ghi đã dùng")
		}
		if err := dayTaken(ctx, tx, *cy, ref, en.EntryDate); err != nil {
			return err
		}
		weighing, err := photosOf(ctx, tx, hc.h.Ref, in.Photos)
		if err != nil {
			return err
		}
		tp, err := target(ctx, tx, hc)
		if err != nil {
			return err
		}
		var others []domain.Point
		if in.Fix != nil {
			if others, err = store.HouseholdFixes(ctx, tx, hc.h.Ref, ref); err != nil {
				return err
			}
		}
		loc := domain.EvaluateLocation(in.Fix, tp, s.param(ctx, ParamDistance, "200"), s.param(ctx, ParamAccuracy, "50"), others)
		checks, err := s.evaluate(ctx, tx, *cy, hc.x.ModelType, ref, en, captured, weighing, loc)
		if err != nil {
			return err
		}
		status := domain.StatusDraft
		if in.Submit {
			if err := blockedProblem(checks); err != nil {
				return err
			}
			status = domain.StatusSubmitted
		}
		out = store.Entry{Meta: c.meta(s.now(), idemEntry(in.IdemKey)), Ref: ref, Event: domain.EventCreated, Status: status,
			CycleRef: cy.Ref, HouseholdRef: hc.h.Ref, SiteRef: hc.x.Ref, ProjectRef: hc.p.Ref, CommuneCode: hc.h.CommuneCode,
			Form: cy.Form, CapturedAt: captured, Photos: in.Photos, Checks: checks, Location: loc, FixStored: in.Fix != nil,
			AuthorRef: c.me(), Entry: en}
		if err := store.InsertEntry(ctx, tx, c.p.TenantID, &out); err != nil {
			return err
		}
		if in.Fix != nil {
			purge := domain.Day(captured, clock.Zone()).AddDate(0, 0, retention)
			if err := store.InsertFix(ctx, tx, c.p.TenantID, ref, hc.h.Ref, *in.Fix, captured, purge, c.me()); err != nil {
				return err
			}
		}
		if status == domain.StatusSubmitted {
			return s.alerts(ctx, tx, c, hc, out)
		}
		return nil
	})
	if dup(err) {
		return s.CreateEntry(ctx, in)
	}
	if err != nil {
		return EntryView{}, err
	}
	v, err := s.GetEntry(ctx, out.Ref)
	v.Replayed = replayed
	return v, err
}

// change ghi phiên bản mới của lần ghi theo hành động act.
func (s *Service) change(ctx context.Context, ref uuid.UUID, act string, in EntryInput) (EntryView, error) {
	c, err := s.writer(ctx, in.IdemKey)
	if err != nil {
		return EntryView{}, err
	}
	if in.BaseVersion == nil {
		return EntryView{}, errBaseVersion
	}
	if (act == domain.ActReturn || act == domain.ActCorrect) && !domain.ValidReason(in.Reason) {
		return EntryView{}, problem.Validation.WithField("reason", "5–1000 ký tự")
	}
	editDays, err := s.editDays(ctx, s.now())
	if err != nil {
		return EntryView{}, err
	}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.EntryByIdem(ctx, tx, idemEntry(in.IdemKey)); err != nil || prev != nil {
			return err
		}
		e, cy, hc, err := s.lockEntry(ctx, tx, c, ref)
		if err != nil {
			return err
		}
		event, next, ok := domain.Transition(e.Status, act)
		if !ok {
			if e.Status == domain.StatusVerified && act == domain.ActUpdate {
				return ErrEntryLocked.WithDetail("lần ghi đã xác thực — dùng Điều chỉnh (có lý do)")
			}
			return ErrEntryState
		}
		review := act == domain.ActReturn || act == domain.ActVerify
		switch {
		case review && !c.reviewer(hc.p):
			return problem.Forbidden.WithDetail("chỉ chủ nhiệm dự án hoặc lãnh đạo phòng rà soát nhật ký")
		case !review && !c.keeper(hc):
			return problem.Forbidden.WithDetail("chỉ cán bộ theo dõi điểm / người ghi nhật ký của hộ sửa lần ghi")
		case !review && (cy.Event == domain.EventClosed || !hc.open()):
			return ErrCycleClosed
		}
		if act == domain.ActVerify {
			vs, err := store.EntryVersions(ctx, tx, ref)
			if err != nil {
				return err
			}
			for _, v := range vs {
				wrote := v.Event == domain.EventCreated || v.Event == domain.EventUpdated || v.Event == domain.EventSubmitted ||
					v.Event == domain.EventCorrected
				if v.AuthorRef == c.me() || (wrote && v.ActorRef == c.me()) {
					return ErrSelfVerify
				}
			}
		}
		if !versionOK(in.BaseVersion, e.Seq) {
			return ErrEntryConflict
		}
		if act == domain.ActUpdate && e.Status == domain.StatusSubmitted && s.today().After(domain.EditableUntil(e.EntryDate, editDays)) {
			return ErrEntryLocked.WithDetail("quá hạn sửa lần ghi đã gửi (" + itoa(editDays) + " ngày)")
		}
		n := *e
		n.Meta, n.Event, n.Status, n.StatusReason, n.ReviewerRef = c.meta(s.now(), idemEntry(in.IdemKey)), event, next, "", nil
		switch act {
		case domain.ActUpdate, domain.ActCorrect:
			in.CapturedAt = &e.CapturedAt // thời điểm, vị trí lúc ghi không đổi
			en, _, err := s.normalizeInput(&in, *cy)
			if err != nil {
				return err
			}
			if !en.EntryDate.Equal(e.EntryDate) {
				if err := dayTaken(ctx, tx, *cy, ref, en.EntryDate); err != nil {
					return err
				}
			}
			n.Entry, n.Photos = en, in.Photos
			if act == domain.ActCorrect {
				n.StatusReason = strings.TrimSpace(in.Reason)
			}
		case domain.ActReturn:
			n.StatusReason, n.ReviewerRef = strings.TrimSpace(in.Reason), &c.p.ActorRef
		case domain.ActVerify:
			n.ReviewerRef = &c.p.ActorRef
		}
		if act != domain.ActReturn && act != domain.ActVerify {
			weighing, err := photosOf(ctx, tx, hc.h.Ref, n.Photos)
			if err != nil {
				return err
			}
			if n.Checks, err = s.evaluate(ctx, tx, *cy, hc.x.ModelType, ref, n.Entry, n.CapturedAt, weighing, n.Location); err != nil {
				return err
			}
			if next == domain.StatusSubmitted {
				if err := blockedProblem(n.Checks); err != nil {
					return err
				}
			}
		}
		if err := store.InsertEntry(ctx, tx, c.p.TenantID, &n); err != nil {
			return err
		}
		if next == domain.StatusSubmitted && act != domain.ActVerify {
			return s.alerts(ctx, tx, c, hc, n)
		}
		return nil
	})
	if dup(err) {
		err = nil
	}
	if err != nil {
		return EntryView{}, err
	}
	return s.GetEntry(ctx, ref)
}

// UpdateEntry sửa lần ghi (nháp, yêu cầu bổ sung, đã gửi trong hạn sửa); Submit = gửi luôn (nháp, yêu cầu bổ sung).
func (s *Service) UpdateEntry(ctx context.Context, ref uuid.UUID, in EntryInput) (EntryView, error) {
	v, err := s.change(ctx, ref, domain.ActUpdate, in)
	if err != nil || !in.Submit || v.Status == domain.StatusSubmitted {
		return v, err
	}
	base := v.Seq
	return s.change(ctx, ref, domain.ActSubmit, EntryInput{BaseVersion: &base, IdemKey: in.IdemKey + ":submit", Kind: v.Kind,
		EntryDate: &v.EntryDate, Metrics: v.Metrics, Tables: v.Tables, Photos: v.Photos})
}

// SubmitEntry gửi lần ghi (nháp, yêu cầu bổ sung) — chạy quy tắc chặn.
func (s *Service) SubmitEntry(ctx context.Context, ref uuid.UUID, base *int64, idem string) (EntryView, error) {
	return s.change(ctx, ref, domain.ActSubmit, EntryInput{BaseVersion: base, IdemKey: idem, Photos: nil, Kind: "", Metrics: nil})
}

// ReturnEntry yêu cầu bổ sung (lý do bắt buộc).
func (s *Service) ReturnEntry(ctx context.Context, ref uuid.UUID, base *int64, reason, idem string) (EntryView, error) {
	return s.change(ctx, ref, domain.ActReturn, EntryInput{BaseVersion: base, Reason: reason, IdemKey: idem})
}

// VerifyEntry xác thực lần ghi (người xác thực khác người ghi).
func (s *Service) VerifyEntry(ctx context.Context, ref uuid.UUID, base *int64, idem string) (EntryView, error) {
	return s.change(ctx, ref, domain.ActVerify, EntryInput{BaseVersion: base, IdemKey: idem})
}

// CorrectEntry điều chỉnh lần ghi đã xác thực (bản mới có lý do, chờ xác thực lại).
func (s *Service) CorrectEntry(ctx context.Context, ref uuid.UUID, in EntryInput) (EntryView, error) {
	return s.change(ctx, ref, domain.ActCorrect, in)
}

// EntryQuery lọc danh sách lần ghi.
type EntryQuery struct {
	Cycle     *uuid.UUID
	Household *uuid.UUID
	Project   *uuid.UUID
	Status    string
	Review    bool // hàng chờ rà soát: đã gửi, thuộc dự án người gọi rà soát được
	Flagged   bool // chỉ lần ghi có cảnh báo (vị trí, chỉ số)
}

// EntryList là danh sách lần ghi.
type EntryList struct {
	Items []EntryView
	Names map[uuid.UUID]string
}

// ListEntries liệt kê lần ghi trong phạm vi.
func (s *Service) ListEntries(ctx context.Context, q EntryQuery) (EntryList, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return EntryList{}, err
	}
	f := store.EntryFilter{Cycle: q.Cycle, Household: q.Household, Project: q.Project}
	switch q.Status {
	case "":
	case domain.StatusDraft, domain.StatusSubmitted, domain.StatusReturned, domain.StatusVerified:
		f.Statuses = []string{q.Status}
	default:
		return EntryList{}, problem.Validation.WithField("status", "draft, submitted, returned hoặc verified")
	}
	if q.Review {
		f.Statuses = []string{domain.StatusSubmitted}
	}
	editDays, err := s.editDays(ctx, s.now())
	if err != nil {
		return EntryList{}, err
	}
	out := EntryList{Items: []EntryView{}}
	refs := []uuid.UUID{}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		es, err := store.ListEntries(ctx, tx, f)
		if err != nil {
			return err
		}
		hcs, cys := map[uuid.UUID]*hctx{}, map[uuid.UUID]*store.Cycle{}
		for _, e := range es {
			hc, ok := hcs[e.HouseholdRef]
			if !ok {
				if hc, err = s.loadHousehold(ctx, tx, c, e.HouseholdRef); err != nil {
					return err
				}
				hcs[e.HouseholdRef] = hc
			}
			if hc == nil || !c.visibleEntry(hc, e) {
				continue
			}
			if q.Review && !c.reviewer(hc.p) {
				continue
			}
			if q.Flagged && !slices.ContainsFunc(e.Checks, func(x domain.Check) bool { return x.Level == domain.LevelWarn }) {
				continue
			}
			cy, ok := cys[e.CycleRef]
			if !ok {
				if cy, err = store.LatestCycle(ctx, tx, e.CycleRef); err != nil {
					return err
				}
				cys[e.CycleRef] = cy
			}
			out.Items = append(out.Items, s.entryView(c, hc, cy, e, editDays))
			refs = append(refs, e.AuthorRef)
		}
		return nil
	})
	if err != nil {
		return EntryList{}, err
	}
	out.Names = s.names(ctx, refs)
	return out, nil
}

// GetEntry đọc chi tiết lần ghi: lịch sử, vị trí so với điểm, ảnh, lần ghi trước, biểu đồ tăng trọng.
func (s *Service) GetEntry(ctx context.Context, ref uuid.UUID) (EntryView, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return EntryView{}, err
	}
	editDays, err := s.editDays(ctx, s.now())
	if err != nil {
		return EntryView{}, err
	}
	var v EntryView
	var person uuid.UUID
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		vs, err := store.EntryVersions(ctx, tx, ref)
		if err != nil {
			return err
		}
		if len(vs) == 0 {
			return ErrEntryNotFound
		}
		cur := vs[len(vs)-1]
		hc, err := s.loadHousehold(ctx, tx, c, cur.HouseholdRef)
		if err != nil {
			return err
		}
		if hc == nil || !c.visibleEntry(hc, cur) {
			return ErrEntryNotFound
		}
		cy, err := store.LatestCycle(ctx, tx, cur.CycleRef)
		if err != nil {
			return err
		}
		v = s.entryView(c, hc, cy, cur, editDays)
		v.Versions, person = vs, hc.h.PersonRef
		if c.privileged(hc) {
			if v.Fix, err = store.FixOf(ctx, tx, ref); err != nil {
				return err
			}
			ps, err := store.Photos(ctx, tx, cur.Photos)
			if err != nil {
				return err
			}
			for _, r := range cur.Photos {
				if p, ok := ps[r]; ok {
					v.PhotoMeta = append(v.PhotoMeta, p)
				}
			}
		}
		if v.Target, err = target(ctx, tx, hc); err != nil {
			return err
		}
		es, err := store.ListEntries(ctx, tx, store.EntryFilter{Cycle: &cur.CycleRef})
		if err != nil {
			return err
		}
		for i := range es {
			o := es[i]
			if o.Status == domain.StatusDraft {
				continue
			}
			if w := domain.Dec(o.Metrics, "weight_avg_g"); w != nil {
				v.Weights = append(v.Weights, WeightPoint{Date: o.EntryDate, Weight: *w})
			}
			if o.Ref != ref && later(cur, o) && (v.Previous == nil || later(o, *v.Previous)) {
				v.Previous = &es[i]
			}
		}
		slices.SortFunc(v.Weights, func(a, b WeightPoint) int { return a.Date.Compare(b.Date) })
		return nil
	})
	if err != nil {
		return EntryView{}, err
	}
	v.OwnerLabel = s.ownerLabel(ctx, person)
	refs := []uuid.UUID{}
	for _, x := range v.Versions {
		refs = append(refs, x.ActorRef, x.AuthorRef)
	}
	v.Names = s.names(ctx, refs)
	return v, nil
}

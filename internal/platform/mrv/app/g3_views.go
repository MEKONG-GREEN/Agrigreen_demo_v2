package app

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Trạng thái một mục bằng chứng của thửa/vụ.
const (
	ItemMissing  = "missing"  // chưa có
	ItemPresent  = "present"  // có, chưa duyệt / chưa đủ điều kiện
	ItemApproved = "approved" // có bản đã duyệt ("Đạt chuẩn" / HTX duyệt)
	ItemRejected = "rejected" // chỉ có bản "Yêu cầu kiểm tra lại"
)

// CompletenessItem là tình trạng một mục bằng chứng.
type CompletenessItem struct {
	Key            string // loại minh chứng, htx_farm_log hoặc collection_evidence
	Source         string // kn_evidence | htx_farm_log | collection_evidence
	CatalogVersion *int   // phiên bản danh mục đã duyệt mới nhất (nếu có)
	Status         string
	Count          int
	ApprovedCount  int
}

// Anomaly là cờ bất thường nhật ký (KN FR-08).
type Anomaly struct {
	NoRecord      bool
	LastRecordAt  *time.Time
	DaysSince     int
	ThresholdDays int
}

// Completeness là tình trạng bằng chứng của một thửa/vụ.
type Completeness struct {
	Season           store.SeasonRow
	Items            []CompletenessItem
	CountsToEstimate bool // có minh chứng cán bộ "Đạt chuẩn" (KN US-MRV-05)
	Anomaly          Anomaly
	Estimate         *store.EstimateRow
}

// seasonInScope đọc vụ hiện hành và kiểm phạm vi (ngoài phạm vi → 404, không lộ sự tồn tại).
func seasonInScope(ctx context.Context, tx pgx.Tx, sc authz.Scope, seasonRef uuid.UUID) (store.SeasonRow, error) {
	season, err := store.SeasonAt(ctx, tx, seasonRef, nil)
	if err != nil {
		return store.SeasonRow{}, err
	}
	if season == nil || !sc.Allows(season.CommuneCode) {
		return store.SeasonRow{}, ErrSeasonNotFound
	}
	return *season, nil
}

func (s *Service) completeness(ctx context.Context, tx pgx.Tx, season store.SeasonRow, days int) (Completeness, error) {
	out := Completeness{Season: season}
	cats, err := store.ApprovedLatest(ctx, tx)
	if err != nil {
		return out, err
	}
	officer, err := store.OfficerRecords(ctx, tx, season.PlotRef, season.SeasonCode)
	if err != nil {
		return out, err
	}
	logs, err := store.FarmLogs(ctx, tx, store.FarmLogFilter{SeasonRef: &season.SeasonRef, All: true})
	if err != nil {
		return out, err
	}
	cols, err := store.Collections(ctx, tx, store.CollectionFilter{SeasonRef: &season.SeasonRef, All: true})
	if err != nil {
		return out, err
	}
	var last time.Time
	var logPin *int
	for _, c := range cats {
		v := c.Version.Version
		if c.Version.EvidenceType == port.EvidenceHTXFarmLog {
			logPin = &v
			continue
		}
		it := CompletenessItem{Key: c.Version.EvidenceType, Source: "kn_evidence", CatalogVersion: &v, Status: ItemMissing}
		rejected := 0
		for _, o := range officer {
			if o.EvidenceType != it.Key {
				continue
			}
			it.Count++
			switch o.ReviewStatus {
			case domain.ReviewAccepted:
				it.ApprovedCount++
				out.CountsToEstimate = true
			case domain.ReviewRejected:
				rejected++
			}
		}
		switch {
		case it.ApprovedCount > 0:
			it.Status = ItemApproved
		case it.Count > rejected:
			it.Status = ItemPresent
		case rejected > 0:
			it.Status = ItemRejected
		}
		out.Items = append(out.Items, it)
	}
	for _, o := range officer {
		if o.EventTime.After(last) {
			last = o.EventTime
		}
	}
	logItem := CompletenessItem{Key: port.EvidenceHTXFarmLog, Source: "htx_farm_log", CatalogVersion: logPin, Status: ItemMissing}
	for _, l := range logs {
		logItem.Count++
		// Chỉ tính là "đã duyệt" khi HTX duyệt VÀ liên kết đã ghim danh mục đã duyệt.
		if l.Decision != nil && *l.Decision == "approved" && l.CatalogVersion != nil {
			logItem.ApprovedCount++
		}
		if l.LogEventTime.After(last) {
			last = l.LogEventTime
		}
	}
	switch {
	case logItem.ApprovedCount > 0:
		logItem.Status = ItemApproved
	case logItem.Count > 0:
		logItem.Status = ItemPresent
	}
	colItem := CompletenessItem{Key: "collection_evidence", Source: "collection_evidence", Status: ItemMissing, Count: len(cols)}
	if len(cols) > 0 {
		colItem.Status = ItemPresent
		if out.CountsToEstimate {
			colItem.Status, colItem.ApprovedCount = ItemApproved, len(cols)
		}
	}
	out.Items = append(out.Items, logItem, colItem)
	ref := last
	if ref.IsZero() {
		ref = season.EventTime // chưa có bản ghi nào: tính từ lúc mở vụ
	} else {
		l := last
		out.Anomaly.LastRecordAt = &l
	}
	out.Anomaly.ThresholdDays = days
	out.Anomaly.NoRecord, out.Anomaly.DaysSince = domain.NoRecordAnomaly(season.Status, ref, s.Clock.Now(), days)
	out.Estimate, err = store.LatestEstimate(ctx, tx, season.SeasonRef)
	return out, err
}

// SeasonCompleteness trả tình trạng bằng chứng của một thửa/vụ.
func (s *Service) SeasonCompleteness(ctx context.Context, seasonRef uuid.UUID) (Completeness, error) {
	p, err := authz.Require(ctx, evidenceReaders...)
	if err != nil {
		return Completeness{}, err
	}
	sc, err := s.scope(ctx)
	if err != nil {
		return Completeness{}, err
	}
	days, err := s.intParam(ctx, port.ParamAnomalyNoRecordDays, s.Clock.Now())
	if err != nil {
		return Completeness{}, err
	}
	var out Completeness
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		season, err := seasonInScope(ctx, tx, sc, seasonRef)
		if err != nil {
			return err
		}
		out, err = s.completeness(ctx, tx, season, days)
		return err
	})
	return out, err
}

// CompletenessFilter lọc danh sách tình trạng bằng chứng.
type CompletenessFilter struct {
	AdminUnitCode string
	SeasonCode    string
	PlotRef       *uuid.UUID
	AnomalyOnly   bool
	After         uuid.UUID
	Limit         int
}

// CompletenessList phân trang tình trạng bằng chứng theo vụ trong phạm vi.
func (s *Service) CompletenessList(ctx context.Context, f CompletenessFilter) (Page[Completeness], error) {
	p, err := authz.Require(ctx, evidenceReaders...)
	if err != nil {
		return Page[Completeness]{}, err
	}
	sc, err := s.scope(ctx)
	if err != nil {
		return Page[Completeness]{}, err
	}
	days, err := s.intParam(ctx, port.ParamAnomalyNoRecordDays, s.Clock.Now())
	if err != nil {
		return Page[Completeness]{}, err
	}
	limit := clampLimit(f.Limit)
	var items []Completeness
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		after := f.After
		for len(items) <= limit {
			seasons, err := store.Seasons(ctx, tx, store.SeasonFilter{All: sc.All, Units: sc.Units, Unit: f.AdminUnitCode,
				SeasonCode: f.SeasonCode, PlotRef: f.PlotRef}, after, limit+1)
			if err != nil {
				return err
			}
			for _, se := range seasons {
				c, err := s.completeness(ctx, tx, se, days)
				if err != nil {
					return err
				}
				if !f.AnomalyOnly || c.Anomaly.NoRecord {
					items = append(items, c)
				}
				after = se.SeasonRef
			}
			if len(seasons) <= limit {
				break
			}
		}
		return nil
	})
	if err != nil {
		return Page[Completeness]{}, err
	}
	return page(items, limit, func(c Completeness) uuid.UUID { return c.Season.SeasonRef }), nil
}

// ---------------------------------------------------------------------------
// Đối chiếu nhật ký HTX ↔ minh chứng cán bộ (KN US-MRV-02)

// ComparisonLine là một nhật ký HTX hiện hành với gợi ý và kết quả đã xác nhận.
type ComparisonLine struct {
	Log        store.FarmLogCurrent
	Suggested  string                // match | mismatch | unverified
	Officer    *domain.OfficerRecord // minh chứng cán bộ gợi ý ghép
	Recorded   []store.ComparisonRow // kết quả cán bộ đã xác nhận (hiện hành)
	FinalState string                // kết quả xác nhận mới nhất, hoặc gợi ý khi chưa xác nhận
	Confirmed  bool
}

// ComparisonView là bảng đối chiếu song song của một thửa/vụ.
type ComparisonView struct {
	Season     store.SeasonRow
	WindowDays int
	Lines      []ComparisonLine
	Officer    []domain.OfficerRecord
}

// SeasonComparison dựng bảng đối chiếu: mỗi nhật ký HTX hiện hành ghép với minh
// chứng cán bộ cùng thửa/vụ trong cửa sổ ± mrv.compare_window_days.
func (s *Service) SeasonComparison(ctx context.Context, seasonRef uuid.UUID) (ComparisonView, error) {
	p, err := authz.Require(ctx, evidenceReaders...)
	if err != nil {
		return ComparisonView{}, err
	}
	sc, err := s.scope(ctx)
	if err != nil {
		return ComparisonView{}, err
	}
	days, err := s.intParam(ctx, port.ParamCompareWindowDays, s.Clock.Now())
	if err != nil {
		return ComparisonView{}, err
	}
	v := ComparisonView{WindowDays: days}
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		season, err := seasonInScope(ctx, tx, sc, seasonRef)
		if err != nil {
			return err
		}
		v.Season = season
		if v.Officer, err = store.OfficerRecords(ctx, tx, season.PlotRef, season.SeasonCode); err != nil {
			return err
		}
		logs, err := store.FarmLogs(ctx, tx, store.FarmLogFilter{SeasonRef: &seasonRef, All: true})
		if err != nil {
			return err
		}
		recs, err := store.Comparisons(ctx, tx, store.ComparisonFilter{SeasonRef: &seasonRef, All: true})
		if err != nil {
			return err
		}
		window := time.Duration(days) * 24 * time.Hour
		for _, l := range logs {
			line := ComparisonLine{Log: l}
			line.Suggested, line.Officer = domain.SuggestMatch(domain.LogItem{LogRef: l.LogRef, LogID: l.LogID,
				ActivityCode: l.ActivityCode, EventTime: l.LogEventTime}, v.Officer, window)
			line.FinalState = line.Suggested
			var latest *store.ComparisonRow
			for i := range recs {
				if recs[i].LogRef == l.LogRef {
					line.Recorded = append(line.Recorded, recs[i])
					if latest == nil || recs[i].RecordedAt.After(latest.RecordedAt) {
						latest = &recs[i]
					}
				}
			}
			if latest != nil {
				line.FinalState, line.Confirmed = latest.Result, true
			}
			v.Lines = append(v.Lines, line)
		}
		return nil
	})
	return v, err
}

// CompareInput là dữ liệu vào của RecordComparison.
type CompareInput struct {
	SeasonRef        uuid.UUID
	LogRef           uuid.UUID
	EvidenceID       uuid.UUID
	Result           string
	Note             *string
	FileRefs         []domain.FileRef
	SupersedesID     *uuid.UUID
	CorrectionReason *string
	IdemKey          string
}

var hexSHA = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (in CompareInput) validate() []problem.FieldError {
	var errs []problem.FieldError
	add := func(f, r string) { errs = append(errs, problem.FieldError{Field: f, Reason: r}) }
	if in.Result != domain.CompareMatch && in.Result != domain.CompareMismatch {
		add("result", "phải là match hoặc mismatch")
	}
	if in.Note != nil && utf8.RuneCountInString(*in.Note) > 1000 {
		add("note", "tối đa 1000 ký tự")
	}
	if in.Result == domain.CompareMismatch && (in.Note == nil || utf8.RuneCountInString(*in.Note) < 5) {
		add("note", "Khác biệt phải kèm ghi chú (≥ 5 ký tự)")
	}
	if len(in.FileRefs) > domain.MaxFileRefs {
		add("file_refs", "tối đa 20 tệp")
	}
	for _, f := range in.FileRefs {
		if f.FileRef == uuid.Nil || !hexSHA.MatchString(f.SHA256) {
			add("file_refs", "file_ref phải là UUID, sha256 64 ký tự hex thường")
		}
	}
	if (in.SupersedesID == nil) != (in.CorrectionReason == nil) {
		add("correction_reason", "bản ghi bù cần cả supersedes_id và correction_reason")
	}
	if in.CorrectionReason != nil {
		if n := utf8.RuneCountInString(*in.CorrectionReason); n < domain.MinCorrectionLen || n > 1000 {
			add("correction_reason", "10–1000 ký tự")
		}
	}
	return errs
}

// RecordComparison ghi kết quả đối chiếu "Khớp"/"Khác biệt" của cán bộ KN, gắn với
// bản ghi MRV tương ứng (evidence_id = bản hiện hành của minh chứng cán bộ).
// Sửa kết quả = bản ghi bù có lý do. Gọi lặp cùng IdemKey trả lại bản ghi cũ.
func (s *Service) RecordComparison(ctx context.Context, in CompareInput) (store.ComparisonRow, bool, error) {
	p, err := authz.Require(ctx, RoleCollector)
	if err != nil {
		return store.ComparisonRow{}, false, err
	}
	if in.IdemKey == "" {
		return store.ComparisonRow{}, false, problem.IdempotencyRequired
	}
	if in.Note != nil {
		n := strings.TrimSpace(*in.Note)
		in.Note = &n
	}
	if in.CorrectionReason != nil {
		r := strings.TrimSpace(*in.CorrectionReason)
		in.CorrectionReason = &r
	}
	if err := validation(in.validate()); err != nil {
		return store.ComparisonRow{}, false, err
	}
	sc, err := s.scope(ctx)
	if err != nil {
		return store.ComparisonRow{}, false, err
	}
	var out store.ComparisonRow
	replayed := false
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.ComparisonByIdem(ctx, tx, in.IdemKey); err != nil {
			return err
		} else if prev != nil {
			out, replayed = *prev, true
			return nil
		}
		season, err := seasonInScope(ctx, tx, sc, in.SeasonRef)
		if err != nil {
			return err
		}
		if err := store.LockComparisons(ctx, tx, p.TenantID, in.SeasonRef); err != nil {
			return err
		}
		e, err := store.EvidenceByID(ctx, tx, in.EvidenceID)
		if err != nil {
			return err
		}
		if e == nil || !sc.Allows(e.AdminUnitCode) {
			return ErrEvidenceNotFound
		}
		if e.PlotRef == nil || *e.PlotRef != season.PlotRef || e.SeasonCode == nil || *e.SeasonCode != season.SeasonCode {
			return problem.Validation.WithField("evidence_id", "minh chứng phải cùng thửa và vụ với nhật ký")
		}
		cur, err := store.CurrentForUpdate(ctx, tx, e.RootID)
		if err != nil {
			return err
		}
		if cur == nil || cur.HeadID != e.ID {
			return ErrEvidenceSuperseded
		}
		logs, err := store.FarmLogs(ctx, tx, store.FarmLogFilter{SeasonRef: &in.SeasonRef, All: true})
		if err != nil {
			return err
		}
		var log *store.FarmLogCurrent
		for i := range logs {
			if logs[i].LogRef == in.LogRef {
				log = &logs[i]
			}
		}
		if log == nil {
			return ErrFarmLogNotFound
		}
		existing, err := store.Comparisons(ctx, tx, store.ComparisonFilter{SeasonRef: &in.SeasonRef, All: true})
		if err != nil {
			return err
		}
		var current *store.ComparisonRow
		for i := range existing {
			if existing[i].LogRef == in.LogRef && existing[i].EvidenceRootID == e.RootID {
				current = &existing[i]
			}
		}
		if in.SupersedesID != nil {
			prev, err := store.ComparisonByID(ctx, tx, *in.SupersedesID)
			if err != nil {
				return err
			}
			if prev == nil || prev.SeasonRef != in.SeasonRef || prev.LogRef != in.LogRef || prev.EvidenceRootID != e.RootID {
				return ErrComparisonNotFound.WithDetail("supersedes_id không thuộc cặp nhật ký ↔ minh chứng này")
			}
			if current == nil || current.ID != prev.ID {
				return ErrComparisonSuperseded
			}
		} else if current != nil {
			return ErrComparisonExists
		}
		out = store.ComparisonRow{
			ID: ids.New(), EvidenceID: e.ID, EvidenceRootID: e.RootID, LogRef: log.LogRef, LogID: log.LogID, SeasonRef: in.SeasonRef,
			PlotRef: season.PlotRef, AdminUnitCode: e.AdminUnitCode, Result: in.Result, Note: in.Note, FileRefs: in.FileRefs,
			EventTime: s.Clock.Now(), SupersedesID: in.SupersedesID, CorrectionReason: in.CorrectionReason, ActorRef: p.ActorRef,
			IdemKey: in.IdemKey,
		}
		return ledger.Classify(store.InsertComparison(ctx, tx, p.TenantID, &out))
	})
	if errors.Is(err, ledger.ErrDuplicate) {
		var prev *store.ComparisonRow
		if rerr := s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
			var e error
			prev, e = store.ComparisonByIdem(ctx, tx, in.IdemKey)
			return e
		}); rerr == nil && prev != nil {
			return *prev, true, nil
		}
	}
	return out, replayed, err
}

// ---------------------------------------------------------------------------
// Ước tính (đọc) và tính lại

// EstimateFilter lọc danh sách ước tính mới nhất theo vụ.
type EstimateFilter struct {
	SeasonRef     *uuid.UUID
	AdminUnitCode string
	After         uuid.UUID
	Limit         int
}

// Estimates đọc ước tính mới nhất của các vụ trong phạm vi. Mọi ước tính G3 là
// TẠM ("chờ khách ký phương pháp") trừ khi official = true.
func (s *Service) Estimates(ctx context.Context, f EstimateFilter) (Page[store.EstimateRow], error) {
	p, err := authz.Require(ctx, evidenceReaders...)
	if err != nil {
		return Page[store.EstimateRow]{}, err
	}
	sc, err := s.scope(ctx)
	if err != nil {
		return Page[store.EstimateRow]{}, err
	}
	limit := clampLimit(f.Limit)
	var rows []store.EstimateRow
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		rows, e = store.Estimates(ctx, tx, store.EstimateFilter{SeasonRef: f.SeasonRef, All: sc.All, Units: sc.Units,
			Unit: f.AdminUnitCode, After: f.After, Limit: limit + 1})
		return e
	})
	if err != nil {
		return Page[store.EstimateRow]{}, err
	}
	return page(rows, limit, func(r store.EstimateRow) uuid.UUID { return r.SeasonRef }), nil
}

// RecomputeEstimate tính lại ước tính của một vụ (vd sau khi tham số hoặc phương
// pháp được duyệt). Không đổi đầu vào → trả dòng hiện có, không ghi thêm.
func (s *Service) RecomputeEstimate(ctx context.Context, seasonRef uuid.UUID) (*store.EstimateRow, error) {
	p, err := authz.Require(ctx, RoleReviewer)
	if err != nil {
		return nil, err
	}
	sc, err := s.scope(ctx)
	if err != nil {
		return nil, err
	}
	var out *store.EstimateRow
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if _, err := seasonInScope(ctx, tx, sc, seasonRef); err != nil {
			return err
		}
		var e error
		out, e = s.recomputeRow(ctx, tx, p.TenantID, seasonRef, p.ActorRef)
		return e
	})
	return out, err
}

// ---------------------------------------------------------------------------
// Phương pháp ước tính (Admin API, R-56)

// MethodInput là đề xuất phương pháp.
type MethodInput struct {
	RuleVersion string            `json:"rule_version"`
	Params      []domain.ParamPin `json:"params"`
	SourceNote  string            `json:"source_note"`
	IdemKey     string            `json:"-"`
}

var ruleVersionRe = regexp.MustCompile(`^[a-z][a-z0-9_.]{2,59}$`)

// ProposeMethod đề xuất một phiên bản phương pháp (vai param_setter), trạng thái
// submitted — chờ người KHÁC duyệt.
func (s *Service) ProposeMethod(ctx context.Context, in MethodInput) (store.MethodRow, bool, error) {
	p, err := authz.Require(ctx, RoleCatalogSetter)
	if err != nil {
		return store.MethodRow{}, false, err
	}
	if in.IdemKey == "" {
		return store.MethodRow{}, false, problem.IdempotencyRequired
	}
	var errs []problem.FieldError
	if !ruleVersionRe.MatchString(in.RuleVersion) {
		errs = append(errs, problem.FieldError{Field: "rule_version", Reason: "dạng [a-z][a-z0-9_.]{2,59}"})
	}
	if n := utf8.RuneCountInString(strings.TrimSpace(in.SourceNote)); n < 5 || n > 1000 {
		errs = append(errs, problem.FieldError{Field: "source_note", Reason: "5–1000 ký tự (văn bản phương pháp đã ký)"})
	}
	if len(in.Params) == 0 || len(in.Params) > 20 {
		errs = append(errs, problem.FieldError{Field: "params", Reason: "1–20 tham số được ghim"})
	}
	seen := map[string]bool{}
	for _, pin := range in.Params {
		if pin.Key == "" || pin.Version <= 0 || seen[pin.Key] {
			errs = append(errs, problem.FieldError{Field: "params", Reason: "mỗi tham số cần key không trùng và version > 0"})
		}
		seen[pin.Key] = true
	}
	if err := validation(errs); err != nil {
		return store.MethodRow{}, false, err
	}
	m := store.MethodRow{ID: ids.New(), RuleVersion: in.RuleVersion, Params: in.Params, SourceNote: strings.TrimSpace(in.SourceNote),
		SetterRef: p.ActorRef, IdemKey: in.IdemKey}
	replayed := false
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.MethodByIdem(ctx, tx, in.IdemKey); err != nil {
			return err
		} else if prev != nil {
			m, replayed = *prev, true
			return nil
		}
		return store.InsertMethod(ctx, tx, p.TenantID, &m)
	})
	return m, replayed, err
}

// DecideMethod duyệt/từ chối phương pháp (vai param_approver ≠ người đề xuất).
// Duyệt xong KHÔNG tự đổi ước tính cũ: ước tính chỉ thành chính thức ở lần tính
// lại kế tiếp khi tham số đã dùng đúng phiên bản được ghim.
func (s *Service) DecideMethod(ctx context.Context, id uuid.UUID, approve bool, note *string) (store.MethodRow, error) {
	p, err := authz.Require(ctx, RoleCatalogApprover)
	if err != nil {
		return store.MethodRow{}, err
	}
	if note != nil {
		n := strings.TrimSpace(*note)
		note = &n
		if utf8.RuneCountInString(n) > 500 {
			return store.MethodRow{}, problem.Validation.WithField("note", "tối đa 500 ký tự")
		}
	}
	if !approve && (note == nil || utf8.RuneCountInString(*note) < 5) {
		return store.MethodRow{}, problem.Validation.WithField("note", "từ chối phải nêu lý do (≥ 5 ký tự)")
	}
	var m store.MethodRow
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		cur, err := store.MethodByID(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if cur == nil {
			return ErrMethodNotFound
		}
		m = *cur
		if err := authz.EnsureDistinctApprover(m.SetterRef, p); err != nil {
			return err
		}
		if m.Status != "submitted" {
			return ErrMethodTransition
		}
		st := "rejected"
		if approve {
			st = "approved"
		}
		now := s.Clock.Now()
		if err := store.DecideMethod(ctx, tx, id, st, p.ActorRef, now, note); err != nil {
			return err
		}
		approver := p.ActorRef
		m.Status, m.ApproverRef, m.DecidedAt, m.DecisionNote = st, &approver, &now, note
		return nil
	})
	return m, err
}

// Methods liệt kê phiên bản phương pháp (Admin API).
func (s *Service) Methods(ctx context.Context, after uuid.UUID, limit int) (Page[store.MethodRow], error) {
	p, err := authz.Require(ctx, catalogAdminRoles...)
	if err != nil {
		return Page[store.MethodRow]{}, err
	}
	limit = clampLimit(limit)
	var rows []store.MethodRow
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		rows, e = store.Methods(ctx, tx, after, limit+1)
		return e
	})
	if err != nil {
		return Page[store.MethodRow]{}, err
	}
	return page(rows, limit, func(m store.MethodRow) uuid.UUID { return m.ID }), nil
}

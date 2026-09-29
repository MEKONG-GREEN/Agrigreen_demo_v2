package app

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// LogContent là nội dung sửa được của nhật ký.
type LogContent struct {
	ActivityCode string
	SOPStepSeq   *int
	MachineCodes []string
	Inputs       []domain.Input
	Note         string
	FileRefs     []uuid.UUID
}

// LogInput ghi một nhật ký canh tác (HTX US-LOG-01, US-LOG-02 ghi hộ).
type LogInput struct {
	ID        uuid.UUID // UUIDv7 máy sinh hoặc Nil
	SeasonRef uuid.UUID // một trong SeasonRef / PlotRef (vụ đang mở của thửa)
	PlotRef   uuid.UUID
	LogContent
	EventTime time.Time // tự đính khi lưu (AC-1), khóa sau khi lưu (AC-3)
	GPS       domain.GPS
	DeviceID  *string
	Channel   string
	IdemKey   string
}

// LogResult là kết quả ghi nhật ký.
type LogResult struct {
	Log      store.FarmLog
	State    store.LogState
	Replayed bool
}

func (s *Service) normalizeContent(ctx context.Context, c *LogContent) error {
	c.ActivityCode = strings.TrimSpace(c.ActivityCode)
	if !domain.KnownActivity(c.ActivityCode) {
		return problem.Validation.WithField("activity_code", "phải là lam_dat, gieo_sa, tuoi_nuoc, bon_phan, phun_thuoc, kiem_tra hoặc thu_hoach")
	}
	if c.SOPStepSeq != nil && *c.SOPStepSeq < 1 {
		return problem.Validation.WithField("sop_step_seq", "số thứ tự bước ≥ 1")
	}
	var err error
	if c.MachineCodes, err = s.checkMachines(ctx, c.MachineCodes); err != nil {
		return err
	}
	if c.Inputs, err = domain.NormalizeInputs(c.Inputs); err != nil {
		return fieldProblem(err)
	}
	if c.Note, err = domain.NormalizeNote("note", c.Note); err != nil {
		return fieldProblem(err)
	}
	if len(c.FileRefs) > domain.MaxFiles {
		return problem.Validation.WithField("file_refs", "tối đa 20 tệp")
	}
	uniq := make([]uuid.UUID, 0, len(c.FileRefs))
	for _, f := range c.FileRefs {
		if f == uuid.Nil {
			return problem.Validation.WithField("file_refs", "mã tệp rỗng")
		}
		if !slices.Contains(uniq, f) {
			uniq = append(uniq, f)
		}
	}
	c.FileRefs = uniq
	return nil
}

// checkStep kiểm bước SOP của vụ (bước phải có trong phiên bản đã ghim).
func checkStep(ctx context.Context, tx pgx.Tx, season store.Season, seq *int) error {
	if seq == nil {
		return nil
	}
	v, err := store.VersionByID(ctx, tx, season.SOPVersionID, false)
	if err != nil {
		return err
	}
	if v == nil {
		return ErrSOPNotFound
	}
	if _, ok := domain.StepBySeq(v.Steps, *seq); !ok {
		return problem.Validation.WithField("sop_step_seq", "không có bước này trong quy trình SOP đã ghim của vụ")
	}
	return nil
}

// logSeason tìm vụ cho nhật ký: theo season_ref, hoặc vụ đang mở của thửa.
func (s *Service) logSeason(ctx context.Context, tid uuid.UUID, seasonRef, plotRef uuid.UUID) (*store.Season, error) {
	var cur *store.Season
	err := s.inTx(ctx, tid, func(tx pgx.Tx) error {
		var e error
		if seasonRef != uuid.Nil {
			cur, e = store.SeasonByRef(ctx, tx, seasonRef, false)
		} else if plotRef != uuid.Nil {
			cur, e = store.OpenSeasonOfPlot(ctx, tx, plotRef, false)
		}
		return e
	})
	return cur, err
}

// seasonAcceptsLog: vụ đang mở; bản ghi ngoại tuyến tới muộn được nhận nếu xảy
// ra trước lúc vụ hoàn thành (HTX US-LOG-02 AC-2, US-LOG-04).
func seasonAcceptsLog(season store.Season, at time.Time, channel string) bool {
	switch season.Status {
	case port.SeasonOpen:
		return true
	case port.SeasonCompleted:
		return channel == ChannelSync && season.CompletedAt != nil && !at.After(*season.CompletedAt)
	}
	return false
}

// RecordLog ghi nhật ký canh tác. Lô chưa mở vụ → cropcycle.season_not_open.
func (s *Service) RecordLog(ctx context.Context, in LogInput) (LogResult, error) {
	a, err := s.access(ctx, writeRoles...)
	if err != nil {
		return LogResult{}, err
	}
	return s.recordLog(ctx, a, in)
}

func (s *Service) recordLog(ctx context.Context, a access, in LogInput) (LogResult, error) {
	if in.IdemKey == "" {
		return LogResult{}, problem.IdempotencyRequired
	}
	var err error
	if in.ID, err = newID("id", in.ID); err != nil {
		return LogResult{}, err
	}
	if in.Channel == "" {
		in.Channel = ChannelApp
	}
	if in.SeasonRef == uuid.Nil && in.PlotRef == uuid.Nil {
		return LogResult{}, problem.Validation.WithField("plot_ref", "cần season_ref hoặc plot_ref")
	}
	if err := s.normalizeContent(ctx, &in.LogContent); err != nil {
		return LogResult{}, err
	}
	if err := in.GPS.Normalize(); err != nil {
		return LogResult{}, fieldProblem(err)
	}
	if in.EventTime, err = s.eventTime(in.EventTime); err != nil {
		return LogResult{}, err
	}
	season, err := s.logSeason(ctx, a.p.TenantID, in.SeasonRef, in.PlotRef)
	if err != nil {
		return LogResult{}, err
	}
	if season == nil {
		if in.SeasonRef != uuid.Nil {
			return LogResult{}, ErrSeasonNotFound
		}
		return LogResult{}, ErrSeasonNotOpen
	}
	// Ghi: báo "không thuộc phạm vi quản lý" (HTX US-SEASON-02 AC-2) thay vì ẩn.
	if err := a.canWrite(season.CooperativeRef, season.HouseholdRef); err != nil {
		return LogResult{}, err
	}
	if !seasonAcceptsLog(*season, in.EventTime, in.Channel) {
		return LogResult{}, ErrSeasonNotOpen
	}
	loc, dist := s.locate(ctx, season.PlotRef, in.GPS, in.EventTime)
	return s.writeLog(ctx, a.p.TenantID, in.IdemKey, func(tx pgx.Tx) (LogResult, error) {
		if prev, err := store.FarmLogByID(ctx, tx, in.ID); err != nil {
			return LogResult{}, err
		} else if prev != nil {
			return LogResult{}, ErrIDReused
		}
		if err := checkStep(ctx, tx, *season, in.SOPStepSeq); err != nil {
			return LogResult{}, err
		}
		l := newLog(a, *season, in.ID, in.LogContent, in.GPS, loc, dist, in.EventTime, in.Channel, in.DeviceID, in.IdemKey)
		return insertLog(ctx, tx, a.p.TenantID, l, nil)
	})
}

func newLog(a access, season store.Season, id uuid.UUID, c LogContent, g domain.GPS, loc string, dist *decimal.Decimal,
	at time.Time, channel string, device *string, key string) store.FarmLog {
	l := store.FarmLog{
		ID: id, TenantID: a.p.TenantID, LogRef: id, SeasonRef: season.SeasonRef, PlotRef: season.PlotRef, HouseholdRef: season.HouseholdRef,
		CooperativeRef: season.CooperativeRef, CommuneCode: season.CommuneCode, ActivityCode: c.ActivityCode, SOPStepSeq: c.SOPStepSeq,
		GPS: g, LocationStatus: loc, DistanceM: dist, MachineCodes: c.MachineCodes, Inputs: c.Inputs, Note: c.Note, FileRefs: c.FileRefs,
		BecameCurrent: true, OnBehalf: a.onBehalf(), Channel: channel, EventTime: at, ActorRef: a.p.ActorRef, DeviceID: device, IdemKey: key,
	}
	return l
}

// insertLog ghi phiên bản + trạng thái + chiếu sản lượng + outbox (R-21). prev ≠ nil:
// phiên bản bị thay (đánh dấu superseded nếu phiên bản mới thành hiện hành).
func insertLog(ctx context.Context, tx pgx.Tx, tid uuid.UUID, l store.FarmLog, prev *store.LogState) (LogResult, error) {
	if err := store.InsertFarmLog(ctx, tx, &l); err != nil {
		return LogResult{}, err
	}
	if prev != nil && l.BecameCurrent {
		prev.Status, prev.SupersededBy = port.LogSuperseded, l.ID
		if err := store.SaveLogState(ctx, tx, tid, *prev); err != nil {
			return LogResult{}, err
		}
	}
	st := store.StateFromLog(l)
	if err := store.SaveLogState(ctx, tx, tid, st); err != nil {
		return LogResult{}, err
	}
	if err := store.RefreshYield(ctx, tx, l.SeasonRef); err != nil {
		return LogResult{}, err
	}
	if err := enqueue(ctx, tx, tid, port.EventFarmLogRecorded, l.EventTime, port.FarmLogRecordedV1{
		LogID: l.ID, LogRef: l.LogRef, Version: l.Version, SeasonRef: l.SeasonRef, PlotRef: l.PlotRef, HouseholdRef: uuidPtr(l.HouseholdRef),
		CooperativeRef: l.CooperativeRef, CommuneCode: l.CommuneCode, ActivityCode: l.ActivityCode, SOPStepSeq: l.SOPStepSeq,
		LocationStatus: l.LocationStatus, GPSSource: l.GPS.Source, BatchRef: uuidPtr(l.BatchRef), BecameCurrent: l.BecameCurrent,
		SupersedesID: l.SupersedesID, OnBehalf: l.OnBehalf, AuthorRef: l.ActorRef, EventTime: l.EventTime.Format(time.RFC3339),
	}); err != nil {
		return LogResult{}, err
	}
	cur, err := store.LogStateByID(ctx, tx, l.ID, false)
	if err != nil {
		return LogResult{}, err
	}
	return LogResult{Log: l, State: *cur}, nil
}

func (s *Service) replayLog(ctx context.Context, tx pgx.Tx, key string) (*LogResult, error) {
	l, err := store.FarmLogByIdem(ctx, tx, key)
	if err != nil || l == nil {
		return nil, err
	}
	st, err := store.LogStateByID(ctx, tx, l.ID, false)
	if err != nil || st == nil {
		return nil, err
	}
	return &LogResult{Log: *l, State: *st, Replayed: true}, nil
}

func (s *Service) writeLog(ctx context.Context, tid uuid.UUID, key string, fn func(pgx.Tx) (LogResult, error)) (LogResult, error) {
	var res LogResult
	err := s.inTx(ctx, tid, func(tx pgx.Tx) error {
		if prev, err := s.replayLog(ctx, tx, key); err != nil {
			return err
		} else if prev != nil {
			res = *prev
			return nil
		}
		var err error
		res, err = fn(tx)
		return err
	})
	err = ledger.Classify(err)
	if errors.Is(err, ledger.ErrDuplicate) {
		var prev *LogResult
		if rerr := s.inTx(ctx, tid, func(tx pgx.Tx) error {
			var e error
			prev, e = s.replayLog(ctx, tx, key)
			return e
		}); rerr == nil && prev != nil {
			return *prev, nil
		}
	}
	return res, err
}

// ---------------------------------------------------------------------------
// Sửa nhật ký (bản ghi bù) — HTX US-LOG-01 AC-3, US-LOG-02 AC-1 (ghi hộ, X1)
// ---------------------------------------------------------------------------

// CorrectLogInput sửa nhật ký bằng phiên bản mới thay phiên bản hiện hành.
// EventTime/GPS khác nil phải trùng phiên bản bị thay (khóa sau khi lưu).
type CorrectLogInput struct {
	ID           uuid.UUID
	SupersedesID uuid.UUID
	Reason       string
	LogContent
	EventTime *time.Time
	GPS       *domain.GPS
	DeviceID  *string
	Channel   string
	IdemKey   string
	// offline: bản ghi bù ngoại tuyến dựa trên phiên bản không còn hiện hành được
	// GIỮ (became_current = false) thay vì từ chối (mục 15).
	offline bool
}

// CorrectLog ghi bản ghi bù cho nhật ký. Phiên bản bị thay phải là hiện hành,
// nếu không → cropcycle.concurrent_edit. Giữ nguyên event_time và GPS.
func (s *Service) CorrectLog(ctx context.Context, in CorrectLogInput) (LogResult, error) {
	a, err := s.access(ctx, writeRoles...)
	if err != nil {
		return LogResult{}, err
	}
	return s.correctLog(ctx, a, in)
}

func sameGPS(a, b domain.GPS) bool {
	if a.Source != b.Source || a.HasPoint() != b.HasPoint() {
		return false
	}
	if a.HasPoint() && (!a.Lon.Equal(*b.Lon) || !a.Lat.Equal(*b.Lat)) {
		return false
	}
	return true
}

func (s *Service) correctLog(ctx context.Context, a access, in CorrectLogInput) (LogResult, error) {
	if in.IdemKey == "" {
		return LogResult{}, problem.IdempotencyRequired
	}
	if !domain.ValidReason(in.Reason) {
		return LogResult{}, problem.Validation.WithField("correction_reason", "bắt buộc, ít nhất 10 ký tự")
	}
	in.Reason = strings.TrimSpace(in.Reason)
	var err error
	if in.ID, err = newID("id", in.ID); err != nil {
		return LogResult{}, err
	}
	if in.Channel == "" {
		in.Channel = ChannelApp
	}
	if err := s.normalizeContent(ctx, &in.LogContent); err != nil {
		return LogResult{}, err
	}
	if in.GPS != nil {
		if err := in.GPS.Normalize(); err != nil {
			return LogResult{}, fieldProblem(err)
		}
	}
	var conflict bool
	res, err := s.writeLog(ctx, a.p.TenantID, in.IdemKey, func(tx pgx.Tx) (LogResult, error) {
		old, err := store.FarmLogByID(ctx, tx, in.SupersedesID)
		if err != nil {
			return LogResult{}, err
		}
		if old == nil || !a.sees(old.CommuneCode, old.CooperativeRef, old.HouseholdRef) {
			return LogResult{}, ErrLogNotFound
		}
		if err := a.canWrite(old.CooperativeRef, old.HouseholdRef); err != nil {
			return LogResult{}, err
		}
		if prev, err := store.FarmLogByID(ctx, tx, in.ID); err != nil {
			return LogResult{}, err
		} else if prev != nil {
			return LogResult{}, ErrIDReused
		}
		// Khóa toạ độ và thời gian sau khi lưu (HTX US-LOG-01 AC-3).
		if in.EventTime != nil && !in.EventTime.UTC().Equal(old.EventTime.UTC()) {
			return LogResult{}, ErrLockedFields
		}
		if in.GPS != nil && !sameGPS(*in.GPS, old.GPS) {
			return LogResult{}, ErrLockedFields
		}
		st, err := store.LogStateByID(ctx, tx, old.ID, true)
		if err != nil {
			return LogResult{}, err
		}
		season, err := store.SeasonByRef(ctx, tx, old.SeasonRef, false)
		if err != nil {
			return LogResult{}, err
		}
		if season.Status == port.SeasonCancelled {
			return LogResult{}, ErrSeasonState.WithDetail("vụ đã hủy")
		}
		if err := checkStep(ctx, tx, *season, in.SOPStepSeq); err != nil {
			return LogResult{}, err
		}
		stale := st.Status == port.LogSuperseded
		if stale && !in.offline {
			return LogResult{}, ErrConcurrentEdit.With("current_log_id", st.SupersededBy.String())
		}
		sup, reason := old.ID, in.Reason
		l := store.FarmLog{
			ID: in.ID, TenantID: a.p.TenantID, LogRef: old.LogRef, SeasonRef: old.SeasonRef, PlotRef: old.PlotRef, HouseholdRef: old.HouseholdRef,
			CooperativeRef: old.CooperativeRef, CommuneCode: old.CommuneCode, ActivityCode: in.ActivityCode, SOPStepSeq: in.SOPStepSeq,
			GPS: old.GPS, LocationStatus: old.LocationStatus, DistanceM: old.DistanceM, MachineCodes: in.MachineCodes, Inputs: in.Inputs,
			Note: in.Note, FileRefs: in.FileRefs, BatchRef: old.BatchRef, BecameCurrent: !stale, OnBehalf: a.onBehalf(), Channel: in.Channel,
			EventTime: old.EventTime, SupersedesID: &sup, CorrectionReason: &reason, ActorRef: a.p.ActorRef, DeviceID: in.DeviceID, IdemKey: in.IdemKey,
		}
		conflict = stale
		return insertLog(ctx, tx, a.p.TenantID, l, st)
	})
	if err == nil && conflict {
		res.State.Status = port.LogSuperseded
	}
	return res, err
}

// ---------------------------------------------------------------------------
// Ghi hàng loạt (HTX US-LOG-03): một đầu lô + một dòng cho mỗi thửa
// ---------------------------------------------------------------------------

// BatchInput ghi cùng hoạt động cho nhiều thửa trong cùng ngày.
type BatchInput struct {
	ID       uuid.UUID
	PlotRefs []uuid.UUID
	LogContent
	EventTime time.Time
	GPS       domain.GPS
	DeviceID  *string
	Channel   string
	IdemKey   string
}

// BatchResult là kết quả lượt ghi hàng loạt.
type BatchResult struct {
	Batch    store.Batch
	Lines    []store.FarmLog
	Replayed bool
}

// Lý do bỏ thửa khỏi lượt ghi hàng loạt (HTX US-LOG-03 AC-2).
const SkipNotOpen = "Lô chưa mở vụ"

// RecordBatch ghi nhật ký hàng loạt: thửa chưa mở vụ hoặc ngoài phạm vi bị loại kèm lý do.
func (s *Service) RecordBatch(ctx context.Context, in BatchInput) (BatchResult, error) {
	a, err := s.access(ctx, writeRoles...)
	if err != nil {
		return BatchResult{}, err
	}
	return s.recordBatch(ctx, a, in)
}

func (s *Service) recordBatch(ctx context.Context, a access, in BatchInput) (BatchResult, error) {
	if in.IdemKey == "" {
		return BatchResult{}, problem.IdempotencyRequired
	}
	if a.kind == accKN {
		return BatchResult{}, ErrReadOnly
	}
	var err error
	if in.ID, err = newID("id", in.ID); err != nil {
		return BatchResult{}, err
	}
	if in.Channel == "" {
		in.Channel = ChannelWeb
	}
	if len(in.PlotRefs) == 0 || len(in.PlotRefs) > MaxBulk {
		return BatchResult{}, problem.Validation.WithField("plot_refs", "1–200 lô ruộng")
	}
	if err := s.normalizeContent(ctx, &in.LogContent); err != nil {
		return BatchResult{}, err
	}
	if in.GPS.Source == "" && !in.GPS.HasPoint() {
		in.GPS.Source = domain.GPSManualPick // ghi hộ từ danh sách lô
	}
	if err := in.GPS.Normalize(); err != nil {
		return BatchResult{}, fieldProblem(err)
	}
	if in.EventTime, err = s.eventTime(in.EventTime); err != nil {
		return BatchResult{}, err
	}
	// Tra trước ngoài giao dịch (gọi pf/geo): vụ đang mở + nhãn vị trí từng thửa.
	var (
		lines   []store.FarmLog
		skipped []store.Skipped
		seen    = map[uuid.UUID]bool{}
		coop    uuid.UUID
		coops   = map[uuid.UUID]bool{}
	)
	var prevBatch *store.Batch
	if err := s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		prevBatch, e = store.BatchByIdem(ctx, tx, in.IdemKey)
		return e
	}); err != nil {
		return BatchResult{}, err
	}
	if prevBatch != nil {
		return s.batchResult(ctx, a.p.TenantID, *prevBatch, true)
	}
	for _, plot := range in.PlotRefs {
		if seen[plot] {
			continue
		}
		seen[plot] = true
		season, err := s.logSeason(ctx, a.p.TenantID, uuid.Nil, plot)
		if err != nil {
			return BatchResult{}, err
		}
		switch {
		case season == nil:
			skipped = append(skipped, store.Skipped{PlotRef: plot, Code: ErrSeasonNotOpen.Code, Reason: SkipNotOpen})
			continue
		case a.canWrite(season.CooperativeRef, season.HouseholdRef) != nil:
			skipped = append(skipped, store.Skipped{PlotRef: plot, Code: ErrOutOfScope.Code, Reason: SkipOutOfScope})
			continue
		}
		loc, dist := s.locate(ctx, plot, in.GPS, in.EventTime)
		id := ids.New()
		l := newLog(a, *season, id, in.LogContent, in.GPS, loc, dist, in.EventTime, in.Channel, in.DeviceID, in.IdemKey+":"+plot.String())
		l.BatchRef = in.ID
		lines = append(lines, l)
		coops[season.CooperativeRef] = true
		coop = season.CooperativeRef
	}
	if len(lines) == 0 {
		return BatchResult{}, ErrBatchEmpty.With("skipped", skippedDTO(skipped))
	}
	if len(coops) > 1 {
		coop = uuid.Nil
	}
	b := store.Batch{
		ID: in.ID, TenantID: a.p.TenantID, CooperativeRef: coop, ActivityCode: in.ActivityCode, SOPStepSeq: in.SOPStepSeq,
		Requested: len(seen), Accepted: len(lines), Skipped: skipped, MachineCodes: in.MachineCodes, Inputs: in.Inputs, Note: in.Note,
		OnBehalf: a.onBehalf(), Channel: in.Channel, EventTime: in.EventTime, ActorRef: a.p.ActorRef, DeviceID: in.DeviceID, IdemKey: in.IdemKey,
	}
	var res BatchResult
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		if err := store.InsertBatch(ctx, tx, &b); err != nil {
			return err
		}
		res.Batch = b
		for _, l := range lines {
			season, err := store.SeasonByRef(ctx, tx, l.SeasonRef, false)
			if err != nil {
				return err
			}
			if err := checkStep(ctx, tx, *season, l.SOPStepSeq); err != nil {
				return err
			}
			r, err := insertLog(ctx, tx, a.p.TenantID, l, nil)
			if err != nil {
				return err
			}
			res.Lines = append(res.Lines, r.Log)
		}
		return nil
	})
	err = ledger.Classify(err)
	if errors.Is(err, ledger.ErrDuplicate) {
		var prev *store.Batch
		if rerr := s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
			var e error
			prev, e = store.BatchByIdem(ctx, tx, in.IdemKey)
			return e
		}); rerr == nil && prev != nil {
			return s.batchResult(ctx, a.p.TenantID, *prev, true)
		}
	}
	return res, err
}

func (s *Service) batchResult(ctx context.Context, tid uuid.UUID, b store.Batch, replayed bool) (BatchResult, error) {
	res := BatchResult{Batch: b, Replayed: replayed}
	err := s.inTx(ctx, tid, func(tx pgx.Tx) error {
		var e error
		res.Lines, e = store.BatchLines(ctx, tx, b.ID)
		return e
	})
	return res, err
}

func skippedDTO(sk []store.Skipped) []map[string]any {
	out := make([]map[string]any, 0, len(sk))
	for _, x := range sk {
		out = append(out, map[string]any{"plot_ref": x.PlotRef.String(), "code": x.Code, "reason": x.Reason})
	}
	return out
}

// ---------------------------------------------------------------------------
// Duyệt (HTX US-LOG-05)
// ---------------------------------------------------------------------------

// ReviewInput là quyết định duyệt một phiên bản nhật ký.
type ReviewInput struct {
	LogID    uuid.UUID
	Decision string // approved | needs_supplement
	Note     string
	IdemKey  string
}

// ReviewResult là kết quả duyệt.
type ReviewResult struct {
	Review   store.Review
	State    store.LogState
	Replayed bool
}

// ReviewLog duyệt nhật ký chờ duyệt: quản lý HTX của đúng HTX; người duyệt ≠ người ghi.
// Chỉ nhật ký đã duyệt tính vào số liệu (AC-2); yêu cầu bổ sung cần ghi chú (AC-3).
func (s *Service) ReviewLog(ctx context.Context, in ReviewInput) (ReviewResult, error) {
	a, err := s.access(ctx, managerRoles...)
	if err != nil {
		return ReviewResult{}, err
	}
	if in.IdemKey == "" {
		return ReviewResult{}, problem.IdempotencyRequired
	}
	if in.Decision != port.LogApproved && in.Decision != port.LogNeedsSupplement {
		return ReviewResult{}, problem.Validation.WithField("decision", "phải là approved hoặc needs_supplement")
	}
	note, err := domain.NormalizeNote("note", in.Note)
	if err != nil {
		return ReviewResult{}, fieldProblem(err)
	}
	if in.Decision == port.LogNeedsSupplement && len([]rune(note)) < 3 {
		return ReviewResult{}, problem.Validation.WithField("note", "Yêu cầu bổ sung cần ghi rõ cần bổ sung gì.")
	}
	now := s.now()
	replay := func(tx pgx.Tx) (*ReviewResult, error) {
		r, err := store.ReviewByIdem(ctx, tx, in.IdemKey)
		if err != nil || r == nil {
			return nil, err
		}
		st, err := store.LogStateByID(ctx, tx, r.LogVersionID, false)
		if err != nil || st == nil {
			return nil, err
		}
		return &ReviewResult{Review: *r, State: *st, Replayed: true}, nil
	}
	var res ReviewResult
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		if prev, err := replay(tx); err != nil {
			return err
		} else if prev != nil {
			res = *prev
			return nil
		}
		st, err := store.LogStateByID(ctx, tx, in.LogID, true)
		if err != nil {
			return err
		}
		if st == nil || !a.sees(st.CommuneCode, st.CooperativeRef, st.HouseholdRef) {
			return ErrLogNotFound
		}
		if err := a.canWrite(st.CooperativeRef, st.HouseholdRef); err != nil {
			return err
		}
		if st.AuthorRef == a.p.ActorRef {
			return ErrSelfReview
		}
		if st.Status != port.LogPending {
			return ErrLogState.With("status", st.Status)
		}
		r := store.Review{ID: ids.New(), TenantID: a.p.TenantID, LogRef: st.LogRef, LogVersionID: st.ID, SeasonRef: st.SeasonRef,
			CooperativeRef: st.CooperativeRef, Decision: in.Decision, Note: note, AuthorRef: st.AuthorRef, EventTime: now,
			ActorRef: a.p.ActorRef, IdemKey: in.IdemKey}
		if err := store.InsertReview(ctx, tx, &r); err != nil {
			return err
		}
		applyReview(st, r)
		if err := store.SaveLogState(ctx, tx, a.p.TenantID, *st); err != nil {
			return err
		}
		if err := store.RefreshYield(ctx, tx, st.SeasonRef); err != nil {
			return err
		}
		if err := enqueue(ctx, tx, a.p.TenantID, port.EventFarmLogReviewed, now, port.FarmLogReviewedV1{
			ReviewID: r.ID, LogID: st.ID, LogRef: st.LogRef, SeasonRef: st.SeasonRef, CooperativeRef: st.CooperativeRef,
			Decision: r.Decision, Note: strPtr(r.Note), AuthorRef: r.AuthorRef, ReviewerRef: r.ActorRef, EventTime: now.Format(time.RFC3339),
		}); err != nil {
			return err
		}
		cur, err := store.LogStateByID(ctx, tx, st.ID, false)
		if err != nil {
			return err
		}
		res = ReviewResult{Review: r, State: *cur}
		return nil
	})
	err = ledger.Classify(err)
	if errors.Is(err, ledger.ErrDuplicate) {
		var prev *ReviewResult
		if rerr := s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
			var e error
			prev, e = replay(tx)
			return e
		}); rerr == nil && prev != nil {
			return *prev, nil
		}
	}
	return res, err
}

// applyReview áp một lần duyệt lên trạng thái phiên bản (dùng chung cho dựng lại).
func applyReview(st *store.LogState, r store.Review) {
	if st.Status != port.LogSuperseded {
		st.Status = r.Decision
	}
	at := r.EventTime
	st.LastReviewID, st.ReviewNote, st.ReviewedBy, st.ReviewedAt = r.ID, r.Note, r.ActorRef, &at
}

// ---------------------------------------------------------------------------
// Đọc
// ---------------------------------------------------------------------------

// LogDetail là phiên bản nhật ký kèm trạng thái và lịch sử phiên bản.
type LogDetail struct {
	Log      store.FarmLog
	State    store.LogState
	Versions []store.FarmLog
}

// GetLog đọc một phiên bản nhật ký trong phạm vi.
func (s *Service) GetLog(ctx context.Context, id uuid.UUID) (LogDetail, error) {
	a, err := s.access(ctx, readRoles...)
	if err != nil {
		return LogDetail{}, err
	}
	var d LogDetail
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		l, err := store.FarmLogByID(ctx, tx, id)
		if err != nil {
			return err
		}
		if l == nil || !a.sees(l.CommuneCode, l.CooperativeRef, l.HouseholdRef) {
			return ErrLogNotFound
		}
		st, err := store.LogStateByID(ctx, tx, id, false)
		if err != nil {
			return err
		}
		d.Log, d.State = *l, *st
		d.Versions, err = store.FarmLogVersions(ctx, tx, l.LogRef)
		return err
	})
	return d, err
}

// ListLogsInput lọc danh sách nhật ký (phiên bản hiện hành).
type ListLogsInput struct {
	CooperativeRef uuid.UUID
	SeasonRef      uuid.UUID
	Status         string
	After          uuid.UUID
	Limit          int
}

// ListLogs liệt kê nhật ký trong phạm vi (vd "Chờ duyệt" của HTX — US-LOG-05).
func (s *Service) ListLogs(ctx context.Context, in ListLogsInput) (Page[store.LogState], error) {
	a, err := s.access(ctx, readRoles...)
	if err != nil {
		return Page[store.LogState]{}, err
	}
	limit := clampLimit(in.Limit)
	var items []store.LogState
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		items, e = store.ListLogStates(ctx, tx, store.LogFilter{Scope: a.filter(), CooperativeRef: in.CooperativeRef, SeasonRef: in.SeasonRef,
			Status: in.Status, After: in.After, Limit: limit + 1})
		return e
	})
	if err != nil {
		return Page[store.LogState]{}, err
	}
	return pageOf(items, limit, func(x store.LogState) uuid.UUID { return x.ID }), nil
}

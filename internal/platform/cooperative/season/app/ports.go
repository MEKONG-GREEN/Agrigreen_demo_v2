package app

// Cổng của vụ mùa cho mặt tiền pf/cooperative (cooperativeport.Seasons, Diary; phân giải chủ thể season, harvest,
// diary_entry cho Resolve). W3.3 gộp bộ chuyển cmd/internal/coopwire (W3.2) vào đây: đọc thẳng store pf_coop
// trong giao dịch của module (tenant lấy từ context), không qua port cũ của pf/cropcycle.

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	port "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/season/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port/cropcyclesrc"
)

// SeasonsPort hiện thực cooperativeport.Seasons trên store vụ mùa.
type SeasonsPort struct{ Svc *Service }

var (
	_ port.Seasons = SeasonsPort{}
	_ port.Diary   = DiaryPort{}
)

// ActivityByCooperative thực hiện port.Seasons.
func (p SeasonsPort) ActivityByCooperative(ctx context.Context, coops []uuid.UUID) (map[uuid.UUID]port.CoopActivity, error) {
	return p.Svc.ActivityByCooperative(ctx, coops)
}

// Areas thực hiện port.Seasons.
func (p SeasonsPort) Areas(ctx context.Context, coop uuid.UUID, seasonCode string, year int) ([]port.CoopSeasonArea, error) {
	return p.Svc.SeasonAreas(ctx, coop, seasonCode, year)
}

// Supply thực hiện port.Seasons.
func (p SeasonsPort) Supply(ctx context.Context, year int) ([]port.CoopSeasonSupply, error) {
	return p.Svc.SeasonSupply(ctx, year)
}

// Production thực hiện port.Seasons (cùng truy vấn với ReportData của pf/reports).
func (p SeasonsPort) Production(ctx context.Context, q port.ProductionQuery) (port.ProductionData, error) {
	d, err := ReportData(p).Production(ctx, cropcyclesrc.ProductionQuery(q))
	if err != nil {
		return port.ProductionData{}, err
	}
	out := port.ProductionData{Totals: port.ProductionFigures(d.Totals), Rows: make([]port.ProductionRow, len(d.Rows))}
	for i, r := range d.Rows {
		out.Rows[i] = port.ProductionRow{CooperativeRef: r.CooperativeRef, SeasonCode: r.SeasonCode, SeasonYear: r.SeasonYear,
			VarietyCode: r.VarietyCode, ProductionFigures: port.ProductionFigures(r.ProductionFigures)}
	}
	return out, nil
}

// HarvestDates thực hiện port.Seasons: mọi vụ của các thửa (ngày gieo, ngày thu hoạch dự kiến / mở thu hoạch, trạng
// thái) — thay đọc chéo của van thu gom.
func (p SeasonsPort) HarvestDates(ctx context.Context, plotRefs []uuid.UUID) ([]port.SeasonHarvestDate, error) {
	var rows []store.Season
	if err := p.Svc.fromCtx(ctx, func(tx pgx.Tx) error {
		var e error
		rows, e = store.SeasonsOfPlots(ctx, tx, plotRefs)
		return e
	}); err != nil {
		return nil, err
	}
	out := make([]port.SeasonHarvestDate, 0, len(rows))
	for _, s := range rows {
		out = append(out, port.SeasonHarvestDate{SeasonRef: s.SeasonRef, PlotRef: s.PlotRef, CooperativeRef: s.CooperativeRef,
			CommuneCode: s.CommuneCode, SeasonCode: s.SeasonCode, SeasonYear: s.SeasonYear, SowingDate: s.SowingDate,
			ExpectedHarvestDate: s.ExpectedHarvestDate, HarvestOpenDate: s.HarvestOpenDate, Status: s.Status, CompletedAt: s.CompletedAt})
	}
	return out, nil
}

// Codes thực hiện port.Seasons.
func (p SeasonsPort) Codes(ctx context.Context, refs []uuid.UUID) (map[uuid.UUID]port.SeasonKey, error) {
	var m map[uuid.UUID]store.SeasonKey
	if err := p.Svc.fromCtx(ctx, func(tx pgx.Tx) error {
		var e error
		m, e = store.SeasonCodes(ctx, tx, refs)
		return e
	}); err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]port.SeasonKey, len(m))
	for k, v := range m {
		out[k] = port.SeasonKey(v)
	}
	return out, nil
}

// Progress thực hiện port.Seasons.
func (p SeasonsPort) Progress(ctx context.Context, asOf time.Time) ([]port.SeasonProgress, error) {
	var rows []store.ProgressRow
	if err := p.Svc.fromCtx(ctx, func(tx pgx.Tx) error {
		var e error
		rows, e = store.SeasonProgress(ctx, tx, asOf)
		return e
	}); err != nil {
		return nil, err
	}
	out := make([]port.SeasonProgress, len(rows))
	for i, r := range rows {
		out[i] = port.SeasonProgress(r)
	}
	return out, nil
}

// DiaryPort hiện thực cooperativeport.Diary trên bảng chiếu farm_log_current (bằng chứng MRV, ADR-0011).
type DiaryPort struct{ Svc *Service }

func diaryEntry(s store.LogState) port.DiaryEntry {
	return port.DiaryEntry{ID: s.ID, LogRef: s.LogRef, Version: s.Version, SeasonRef: s.SeasonRef, PlotRef: s.PlotRef,
		HouseholdRef: s.HouseholdRef, CooperativeRef: s.CooperativeRef, CommuneCode: s.CommuneCode, ActivityCode: s.ActivityCode,
		SOPStepSeq: s.SOPStepSeq, EventTime: s.EventTime, LocationStatus: s.LocationStatus, DistanceM: s.DistanceM,
		AuthorRef: s.AuthorRef, OnBehalf: s.OnBehalf, BatchRef: s.BatchRef, Status: s.Status, RecordedAt: s.RecordedAt,
		OwnerTenant: s.TenantID}
}

// Get thực hiện port.Diary: phiên bản hiện hành của nhật ký ref (log_ref); không có → port.ErrDiaryEntryNotFound.
func (d DiaryPort) Get(ctx context.Context, ref uuid.UUID) (port.DiaryEntry, error) {
	var st *store.LogState
	if err := d.Svc.fromCtx(ctx, func(tx pgx.Tx) error {
		var e error
		st, e = store.CurrentLogOfRef(ctx, tx, ref)
		return e
	}); err != nil {
		return port.DiaryEntry{}, err
	}
	if st == nil {
		return port.DiaryEntry{}, port.ErrDiaryEntryNotFound
	}
	return diaryEntry(*st), nil
}

// BySeason thực hiện port.Diary: phiên bản hiện hành của vụ, id > after, tối đa limit (mặt tiền đã chuẩn hóa).
func (d DiaryPort) BySeason(ctx context.Context, season, after uuid.UUID, limit int) ([]port.DiaryEntry, error) {
	var rows []store.LogState
	if err := d.Svc.fromCtx(ctx, func(tx pgx.Tx) error {
		var e error
		rows, e = store.LiveLogsOfSeasonPage(ctx, tx, season, after, limit)
		return e
	}); err != nil {
		return nil, err
	}
	out := make([]port.DiaryEntry, 0, len(rows))
	for _, r := range rows {
		out = append(out, diaryEntry(r))
	}
	return out, nil
}

// ResolveSeason trả Nút (HTX), xã, nông hộ, trạng thái của vụ ref (Resolve, R-DB-07).
func (s *Service) ResolveSeason(ctx context.Context, ref uuid.UUID) (port.Subject, error) {
	var se *store.Season
	if err := s.fromCtx(ctx, func(tx pgx.Tx) error {
		var e error
		se, e = store.SeasonByRef(ctx, tx, ref, false)
		return e
	}); err != nil {
		return port.Subject{}, err
	}
	if se == nil {
		return port.Subject{}, port.ErrSubjectNotFound
	}
	return port.Subject{Type: port.SubjectSeason, ID: ref, NodeRef: se.CooperativeRef, CommuneCode: se.CommuneCode,
		HouseholdRef: se.HouseholdRef, Status: se.Status}, nil
}

// ResolveHarvest trả Nút (HTX), xã, nông hộ của một khai báo sản lượng (id dòng sổ harvest_declaration).
func (s *Service) ResolveHarvest(ctx context.Context, id uuid.UUID) (port.Subject, error) {
	var h *store.Harvest
	if err := s.fromCtx(ctx, func(tx pgx.Tx) error {
		var e error
		h, e = store.HarvestByID(ctx, tx, id)
		return e
	}); err != nil {
		return port.Subject{}, err
	}
	if h == nil {
		return port.Subject{}, port.ErrSubjectNotFound
	}
	status := "declared"
	if h.SupersedesID != nil {
		status = "correction"
	}
	return port.Subject{Type: port.SubjectHarvest, ID: id, NodeRef: h.CooperativeRef, CommuneCode: h.CommuneCode,
		HouseholdRef: h.HouseholdRef, Status: status}, nil
}

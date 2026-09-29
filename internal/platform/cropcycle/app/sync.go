package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/store"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// SyncSource là chiều xuống của các entity sop_version, season, farm_log
// (docs/dev/sync-contract.md).
type SyncSource struct{ Svc *Service }

// SyncSink là chiều lên của các entity season_open, farm_log, farm_log_batch, harvest.
type SyncSink struct{ Svc *Service }

var (
	_ syncport.Source = SyncSource{}
	_ syncport.Sink   = SyncSink{}
)

// Entities thực hiện syncport.Source.
func (SyncSource) Entities() []string {
	return []string{port.EntitySOPVersion, port.EntitySeason, port.EntityFarmLog}
}

// syncAccess tính quyền đọc cho đồng bộ: cán bộ KN dùng phạm vi pf/sync truyền vào.
// Người gọi không có vai đọc của module → không nhận entity (ok = false).
func (s *Service) syncAccess(ctx context.Context, scope authz.Scope) (access, bool, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return access{}, false, problem.Unauthenticated
	}
	hasAny := false
	for _, r := range readRoles {
		if p.Has(r) {
			hasAny = true
		}
	}
	if !hasAny {
		return access{}, false, nil
	}
	var a access
	switch {
	case p.Has(authz.RolePlatformAdmin):
		a = access{p: p, kind: accAll, sc: authz.Scope{All: true}}
	case p.Has(port.RoleCoopManager):
		a = access{p: p, kind: accCoop}
	case p.Has(port.RoleFarmer):
		var err error
		if a, err = s.accessFor(ctx, p, []string{port.RoleFarmer}); err != nil {
			return access{}, false, err
		}
	default:
		a = access{p: p, kind: accKN, sc: scope}
	}
	return a, true, nil
}

// SOPVersionSyncData là dữ liệu một phiên bản SOP đã phát hành gửi xuống máy.
type SOPVersionSyncData struct {
	ID                string        `json:"id"`
	TemplateRef       string        `json:"template_ref"`
	Code              string        `json:"code"`
	Name              string        `json:"name"`
	Crop              string        `json:"crop"`
	Scope             string        `json:"scope"`
	CooperativeRef    *string       `json:"cooperative_ref"`
	TemplateStatus    string        `json:"template_status"`
	VersionNo         int           `json:"version_no"`
	Steps             []domain.Step `json:"steps"`
	HarvestDayOffset  *int          `json:"harvest_day_offset"`
	HarvestWindowDays *int          `json:"harvest_window_days"`
	PublishedAt       *string       `json:"published_at"`
}

// SeasonSyncData là dữ liệu một vụ gửi xuống máy (không có dữ liệu cá nhân).
type SeasonSyncData struct {
	SeasonRef           string  `json:"season_ref"`
	PlotRef             string  `json:"plot_ref"`
	PlotCode            string  `json:"plot_code"`
	HouseholdRef        *string `json:"household_ref"`
	CooperativeRef      string  `json:"cooperative_ref"`
	CommuneCode         string  `json:"commune_code"`
	SeasonCode          string  `json:"season_code"`
	SeasonYear          int     `json:"season_year"`
	VarietyCode         string  `json:"variety_code"`
	SOPVersionID        string  `json:"sop_version_id"`
	SowingDate          string  `json:"sowing_date"`
	ExpectedHarvestDate string  `json:"expected_harvest_date"`
	HarvestOpenDate     string  `json:"harvest_open_date"`
	AreaHa              string  `json:"area_ha"`
	Status              string  `json:"status"`
	CurrentEventID      string  `json:"current_event_id"`
	HarvestID           *string `json:"harvest_id"`
	UpdatedAt           string  `json:"updated_at"`
}

// FarmLogSyncData là dữ liệu một phiên bản nhật ký kèm trạng thái duyệt. Không
// gửi toạ độ (chỉ nhãn vị trí) để máy không mang toạ độ gắn với hộ khác.
type FarmLogSyncData struct {
	ID             string         `json:"id"`
	LogRef         string         `json:"log_ref"`
	Version        int64          `json:"version"`
	SeasonRef      string         `json:"season_ref"`
	PlotRef        string         `json:"plot_ref"`
	ActivityCode   string         `json:"activity_code"`
	SOPStepSeq     *int           `json:"sop_step_seq"`
	EventTime      string         `json:"event_time"`
	GPSSource      string         `json:"gps_source"`
	LocationStatus string         `json:"location_status"`
	MachineCodes   []string       `json:"machine_codes"`
	Inputs         []domain.Input `json:"inputs"`
	Note           string         `json:"note"`
	FileRefs       []string       `json:"file_refs"`
	BatchRef       *string        `json:"batch_ref"`
	OnBehalf       bool           `json:"on_behalf"`
	Status         string         `json:"status"`
	ReviewNote     *string        `json:"review_note"`
	ReviewedAt     *string        `json:"reviewed_at"`
	UpdatedAt      string         `json:"updated_at"`
}

func refStr(u uuid.UUID) *string {
	if u == uuid.Nil {
		return nil
	}
	s := u.String()
	return &s
}

func timeStr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339Nano)
	return &s
}

// Changes thực hiện syncport.Source.
func (src SyncSource) Changes(ctx context.Context, scope authz.Scope, entity string, after int64, limit int) ([]syncport.Change, int64, error) {
	s := src.Svc
	a, ok, err := s.syncAccess(ctx, scope)
	if err != nil || !ok {
		return []syncport.Change{}, after, err
	}
	f := a.filter()
	if f.Mode == store.FilterNone || (f.Mode == store.FilterUnits && len(f.Units) == 0) {
		return []syncport.Change{}, after, nil
	}
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	var out []syncport.Change
	last := after
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		switch entity {
		case port.EntitySOPVersion:
			all, coop := a.sopVisibility()
			rows, err := store.PublishedVersionsAfter(ctx, tx, all, []uuid.UUID{coop}, after, limit)
			if err != nil {
				return err
			}
			for _, r := range rows {
				last = r.SyncSeq
				var pub *string
				if r.PublishedAt != nil {
					pub = timeStr(r.PublishedAt)
				}
				data, err := json.Marshal(SOPVersionSyncData{ID: r.ID.String(), TemplateRef: r.TemplateRef.String(), Code: r.Template.Code,
					Name: r.Template.Name, Crop: r.Template.Crop, Scope: r.Template.Scope, CooperativeRef: refStr(r.Template.CooperativeRef),
					TemplateStatus: r.Template.Status, VersionNo: r.VersionNo, Steps: r.Steps, HarvestDayOffset: r.HarvestDayOffset,
					HarvestWindowDays: r.HarvestWindowDays, PublishedAt: pub})
				if err != nil {
					return err
				}
				out = append(out, syncport.Change{Entity: entity, ID: r.ID, Version: r.SyncSeq, Op: syncport.OpUpsert, Data: data})
			}
		case port.EntitySeason:
			rows, err := store.SeasonsChangedAfter(ctx, tx, f, after, limit)
			if err != nil {
				return err
			}
			for _, r := range rows {
				last = r.SyncSeq
				ch := syncport.Change{Entity: entity, ID: r.SeasonRef, Version: r.SyncSeq}
				if r.Status == port.SeasonCancelled {
					ch.Op = syncport.OpTombstone
					out = append(out, ch)
					continue
				}
				data, err := json.Marshal(SeasonSyncData{SeasonRef: r.SeasonRef.String(), PlotRef: r.PlotRef.String(), PlotCode: r.PlotCode,
					HouseholdRef: refStr(r.HouseholdRef), CooperativeRef: r.CooperativeRef.String(), CommuneCode: r.CommuneCode,
					SeasonCode: r.SeasonCode, SeasonYear: r.SeasonYear, VarietyCode: r.VarietyCode, SOPVersionID: r.SOPVersionID.String(),
					SowingDate: domain.FormatDay(r.SowingDate), ExpectedHarvestDate: domain.FormatDay(r.ExpectedHarvestDate),
					HarvestOpenDate: domain.FormatDay(r.HarvestOpenDate), AreaHa: r.AreaHa.StringFixed(4), Status: r.Status,
					CurrentEventID: r.CurrentEventID.String(), HarvestID: refStr(r.HarvestID), UpdatedAt: r.UpdatedAt.UTC().Format(time.RFC3339Nano)})
				if err != nil {
					return err
				}
				ch.Op, ch.Data = syncport.OpUpsert, data
				out = append(out, ch)
			}
		case port.EntityFarmLog:
			rows, err := store.LogStatesChangedAfter(ctx, tx, f, after, limit)
			if err != nil {
				return err
			}
			idList := make([]uuid.UUID, 0, len(rows))
			for _, r := range rows {
				idList = append(idList, r.ID)
			}
			logs, err := store.FarmLogsByIDs(ctx, tx, idList)
			if err != nil {
				return err
			}
			for _, r := range rows {
				last = r.SyncSeq
				ch := syncport.Change{Entity: entity, ID: r.ID, Version: r.SyncSeq}
				if r.Status == port.LogSuperseded {
					ch.Op = syncport.OpTombstone // máy thay bằng phiên bản mới cùng log_ref
					out = append(out, ch)
					continue
				}
				l := logs[r.ID]
				files := make([]string, 0, len(l.FileRefs))
				for _, f := range l.FileRefs {
					files = append(files, f.String())
				}
				data, err := json.Marshal(FarmLogSyncData{ID: r.ID.String(), LogRef: r.LogRef.String(), Version: r.Version,
					SeasonRef: r.SeasonRef.String(), PlotRef: r.PlotRef.String(), ActivityCode: r.ActivityCode, SOPStepSeq: r.SOPStepSeq,
					EventTime: r.EventTime.UTC().Format(time.RFC3339), GPSSource: r.GPSSource, LocationStatus: r.LocationStatus,
					MachineCodes: nonNilStrs(l.MachineCodes), Inputs: nonNilInputs(l.Inputs), Note: l.Note, FileRefs: files,
					BatchRef: refStr(r.BatchRef), OnBehalf: r.OnBehalf, Status: r.Status, ReviewNote: strPtr(r.ReviewNote),
					ReviewedAt: timeStr(r.ReviewedAt), UpdatedAt: r.UpdatedAt.UTC().Format(time.RFC3339Nano)})
				if err != nil {
					return err
				}
				ch.Op, ch.Data = syncport.OpUpsert, data
				out = append(out, ch)
			}
		default:
			return fmt.Errorf("cropcycle: entity %q không thuộc module", entity)
		}
		return nil
	})
	if err != nil {
		return nil, after, err
	}
	if out == nil {
		out = []syncport.Change{}
	}
	return out, last, nil
}

func nonNilStrs(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func nonNilInputs(s []domain.Input) []domain.Input {
	if s == nil {
		return []domain.Input{}
	}
	return s
}

// ---------------------------------------------------------------------------
// Chiều lên
// ---------------------------------------------------------------------------

// Entities thực hiện syncport.Sink.
func (SyncSink) Entities() []string {
	return []string{port.EntitySeasonOpen, port.EntityFarmLog, port.EntityLogBatch, port.EntityHarvest}
}

// GPSPayload là vị trí gửi lên (số thập phân dạng chuỗi).
type GPSPayload struct {
	Lon       *string `json:"lon"`
	Lat       *string `json:"lat"`
	AccuracyM *string `json:"accuracy_m"`
	Source    string  `json:"source"`
}

// Parse đọc GPS; nil → không có vị trí (denied).
func (g *GPSPayload) Parse() (domain.GPS, error) {
	if g == nil {
		return domain.GPS{}, nil
	}
	out := domain.GPS{Source: strings.TrimSpace(g.Source)}
	for _, p := range []struct {
		name string
		s    *string
		d    **decimal.Decimal
	}{{"gps.lon", g.Lon, &out.Lon}, {"gps.lat", g.Lat, &out.Lat}, {"gps.accuracy_m", g.AccuracyM, &out.AccuracyM}} {
		if p.s == nil {
			continue
		}
		d, err := decimal.NewFromString(strings.TrimSpace(*p.s))
		if err != nil {
			return out, problem.Validation.WithField(p.name, "phải là số thập phân dạng chuỗi")
		}
		*p.d = &d
	}
	return out, nil
}

// SeasonOpenPayload là payload của entity season_open.
//
//	op create: id bản ghi = id sự kiện mở vụ; season_ref (UUIDv7 máy sinh) mặc định = id.
//	op correct: supersedes_id = sự kiện hiện hành của vụ; season_ref bắt buộc.
type SeasonOpenPayload struct {
	SeasonRef    string      `json:"season_ref"`
	PlotRef      string      `json:"plot_ref"`
	SeasonCode   string      `json:"season_code"`
	SeasonYear   int         `json:"season_year"`
	VarietyCode  *string     `json:"variety_code"`
	SowingDate   *string     `json:"sowing_date"`
	SOPVersionID *string     `json:"sop_version_id"`
	CopyPrevious bool        `json:"copy_previous"`
	Cancel       bool        `json:"cancel"`
	GPS          *GPSPayload `json:"gps"`
}

// LogPayload là payload của entity farm_log (create: nhật ký mới; correct: bản ghi bù,
// event_time và GPS lấy từ phiên bản bị thay).
type LogPayload struct {
	SeasonRef    string         `json:"season_ref"`
	PlotRef      string         `json:"plot_ref"`
	ActivityCode string         `json:"activity_code"`
	SOPStepSeq   *int           `json:"sop_step_seq"`
	MachineCodes []string       `json:"machine_codes"`
	Inputs       []domain.Input `json:"inputs"`
	Note         string         `json:"note"`
	FileRefs     []string       `json:"file_refs"`
	GPS          *GPSPayload    `json:"gps"`
}

// BatchPayload là payload của entity farm_log_batch (chỉ create).
type BatchPayload struct {
	LogPayload
	PlotRefs []string `json:"plot_refs"`
}

// HarvestPayload là payload của entity harvest.
type HarvestPayload struct {
	SeasonRef      string      `json:"season_ref"`
	PlotRef        string      `json:"plot_ref"`
	HarvestDate    *string     `json:"harvest_date"`
	MassKg         string      `json:"mass_kg"`
	MoisturePct    *string     `json:"moisture_pct"`
	ConfirmAnomaly bool        `json:"confirm_anomaly"`
	GPS            *GPSPayload `json:"gps"`
}

func rejected(id uuid.UUID, err error) syncport.Result {
	var pr *problem.Problem
	if !errors.As(err, &pr) {
		pr = problem.Internal // lỗi hệ thống: pf/sync coi internal_error là thử lại được
	}
	errs := pr.Errors
	if len(errs) == 0 {
		reason := pr.Detail
		if reason == "" {
			reason = pr.Title
		}
		errs = []problem.FieldError{{Field: "payload", Reason: reason}}
	}
	return syncport.Result{ID: id, Status: syncport.StatusRejected, Code: pr.Code, Errors: errs}
}

func optRef(field, s string) (uuid.UUID, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return uuid.Nil, nil
	}
	u, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, problem.Validation.WithField("payload."+field, "phải là UUID")
	}
	return u, nil
}

func optDay(field string, s *string) (*time.Time, error) {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil, nil
	}
	d, err := domain.ParseDay(*s)
	if err != nil {
		return nil, problem.Validation.WithField("payload."+field, "YYYY-MM-DD")
	}
	return &d, nil
}

func fileRefs(list []string) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, 0, len(list))
	for _, s := range list {
		u, err := uuid.Parse(strings.TrimSpace(s))
		if err != nil {
			return nil, problem.Validation.WithField("payload.file_refs", "phải là UUID")
		}
		out = append(out, u)
	}
	return out, nil
}

func (p LogPayload) content() (LogContent, error) {
	files, err := fileRefs(p.FileRefs)
	if err != nil {
		return LogContent{}, err
	}
	return LogContent{ActivityCode: p.ActivityCode, SOPStepSeq: p.SOPStepSeq, MachineCodes: p.MachineCodes, Inputs: p.Inputs,
		Note: p.Note, FileRefs: files}, nil
}

// Accept thực hiện syncport.Sink: idempotent theo IdemKey (sổ chống trùng).
func (snk SyncSink) Accept(ctx context.Context, scope authz.Scope, rec syncport.Record) syncport.Result {
	s := snk.Svc
	a, err := s.access(ctx, writeRoles...)
	if err != nil {
		return rejected(rec.ID, err)
	}
	if rec.IdemKey == "" {
		return rejected(rec.ID, problem.Validation.WithField("idem_key", "bắt buộc"))
	}
	if rec.ID.Version() != 7 {
		return rejected(rec.ID, problem.Validation.WithField("id", "phải là UUIDv7"))
	}
	if rec.EventTime.IsZero() {
		return rejected(rec.ID, problem.Validation.WithField("event_time", "bắt buộc (R-13)"))
	}
	var device *string
	if rec.DeviceID != uuid.Nil {
		d := rec.DeviceID.String()
		device = &d
	}
	var replayed, conflict bool
	switch rec.Entity {
	case port.EntitySeasonOpen:
		replayed, err = snk.season(ctx, a, rec, device)
	case port.EntityFarmLog:
		replayed, conflict, err = snk.log(ctx, a, rec, device)
	case port.EntityLogBatch:
		replayed, err = snk.batch(ctx, a, rec, device)
	case port.EntityHarvest:
		replayed, err = snk.harvest(ctx, a, rec, device)
	default:
		err = problem.Validation.WithField("entity", "không thuộc module pf/cropcycle")
	}
	switch {
	case err != nil:
		return rejected(rec.ID, err)
	case replayed:
		return syncport.Result{ID: rec.ID, Status: syncport.StatusDuplicate}
	case conflict:
		// Giữ cả hai (mục 15): pf/sync mở xung đột có người phụ trách + SLA.
		return syncport.Result{ID: rec.ID, Status: syncport.StatusConflict}
	}
	return syncport.Result{ID: rec.ID, Status: syncport.StatusAccepted}
}

func (snk SyncSink) season(ctx context.Context, a access, rec syncport.Record, device *string) (bool, error) {
	var pl SeasonOpenPayload
	if err := json.Unmarshal(rec.Payload, &pl); err != nil {
		return false, problem.Validation.WithField("payload", "phải là JSON hợp lệ")
	}
	g, err := pl.GPS.Parse()
	if err != nil {
		return false, err
	}
	seasonRef, err := optRef("season_ref", pl.SeasonRef)
	if err != nil {
		return false, err
	}
	sow, err := optDay("sowing_date", pl.SowingDate)
	if err != nil {
		return false, err
	}
	var sop uuid.UUID
	if pl.SOPVersionID != nil {
		if sop, err = optRef("sop_version_id", *pl.SOPVersionID); err != nil {
			return false, err
		}
	}
	switch rec.Op {
	case syncport.OpCreate:
		plot, err := optRef("plot_ref", pl.PlotRef)
		if err != nil || plot == uuid.Nil {
			return false, problem.Validation.WithField("payload.plot_ref", "bắt buộc")
		}
		if seasonRef == uuid.Nil {
			seasonRef = rec.ID
		}
		in := OpenInput{SeasonRef: seasonRef, EventID: rec.ID, PlotRef: plot, SeasonCode: pl.SeasonCode, SeasonYear: pl.SeasonYear,
			SOPVersionID: sop, CopyPrevious: pl.CopyPrevious, GPS: g, EventTime: rec.EventTime, DeviceID: device, Channel: ChannelSync, IdemKey: rec.IdemKey}
		if pl.VarietyCode != nil {
			in.VarietyCode = *pl.VarietyCode
		}
		if sow != nil {
			in.SowingDate = *sow
		}
		r, err := snk.Svc.openSeason(ctx, a, in)
		return r.Replayed, err
	case syncport.OpCorrect:
		if rec.SupersedesID == nil || rec.CorrectionReason == nil || seasonRef == uuid.Nil {
			return false, problem.Validation.WithField("supersedes_id", "bản ghi bù cần supersedes_id, correction_reason và payload.season_ref")
		}
		in := CorrectSeasonInput{EventID: rec.ID, SeasonRef: seasonRef, SupersedesID: *rec.SupersedesID, Reason: *rec.CorrectionReason,
			VarietyCode: pl.VarietyCode, SowingDate: sow, Cancel: pl.Cancel, EventTime: rec.EventTime, DeviceID: device, Channel: ChannelSync, IdemKey: rec.IdemKey}
		if pl.SOPVersionID != nil {
			in.SOPVersionID = &sop
		}
		r, err := snk.Svc.correctSeason(ctx, a, in)
		return r.Replayed, err
	}
	return false, problem.Validation.WithField("op", "chỉ nhận create hoặc correct (ghi thêm, không sửa/xóa)")
}

func (snk SyncSink) log(ctx context.Context, a access, rec syncport.Record, device *string) (replayed, conflict bool, err error) {
	var pl LogPayload
	if err := json.Unmarshal(rec.Payload, &pl); err != nil {
		return false, false, problem.Validation.WithField("payload", "phải là JSON hợp lệ")
	}
	content, err := pl.content()
	if err != nil {
		return false, false, err
	}
	switch rec.Op {
	case syncport.OpCreate:
		g, err := pl.GPS.Parse()
		if err != nil {
			return false, false, err
		}
		season, err := optRef("season_ref", pl.SeasonRef)
		if err != nil {
			return false, false, err
		}
		plot, err := optRef("plot_ref", pl.PlotRef)
		if err != nil {
			return false, false, err
		}
		r, err := snk.Svc.recordLog(ctx, a, LogInput{ID: rec.ID, SeasonRef: season, PlotRef: plot, LogContent: content, EventTime: rec.EventTime,
			GPS: g, DeviceID: device, Channel: ChannelSync, IdemKey: rec.IdemKey})
		return r.Replayed, false, err
	case syncport.OpCorrect:
		if rec.SupersedesID == nil || rec.CorrectionReason == nil {
			return false, false, problem.Validation.WithField("supersedes_id", "bản ghi bù cần supersedes_id và correction_reason")
		}
		r, err := snk.Svc.correctLog(ctx, a, CorrectLogInput{ID: rec.ID, SupersedesID: *rec.SupersedesID, Reason: *rec.CorrectionReason,
			LogContent: content, DeviceID: device, Channel: ChannelSync, IdemKey: rec.IdemKey, offline: true})
		if err != nil {
			return false, false, err
		}
		return r.Replayed, !r.Log.BecameCurrent, nil
	}
	return false, false, problem.Validation.WithField("op", "chỉ nhận create hoặc correct (ghi thêm, không sửa/xóa)")
}

func (snk SyncSink) batch(ctx context.Context, a access, rec syncport.Record, device *string) (bool, error) {
	if rec.Op != syncport.OpCreate {
		return false, problem.Validation.WithField("op", "farm_log_batch chỉ nhận create; sửa từng dòng bằng entity farm_log")
	}
	var pl BatchPayload
	if err := json.Unmarshal(rec.Payload, &pl); err != nil {
		return false, problem.Validation.WithField("payload", "phải là JSON hợp lệ")
	}
	content, err := pl.content()
	if err != nil {
		return false, err
	}
	g, err := pl.GPS.Parse()
	if err != nil {
		return false, err
	}
	plots := make([]uuid.UUID, 0, len(pl.PlotRefs))
	for _, p := range pl.PlotRefs {
		u, err := optRef("plot_refs", p)
		if err != nil || u == uuid.Nil {
			return false, problem.Validation.WithField("payload.plot_refs", "phải là UUID")
		}
		plots = append(plots, u)
	}
	r, err := snk.Svc.recordBatch(ctx, a, BatchInput{ID: rec.ID, PlotRefs: plots, LogContent: content, EventTime: rec.EventTime, GPS: g,
		DeviceID: device, Channel: ChannelSync, IdemKey: rec.IdemKey})
	return r.Replayed, err
}

func (snk SyncSink) harvest(ctx context.Context, a access, rec syncport.Record, device *string) (bool, error) {
	var pl HarvestPayload
	if err := json.Unmarshal(rec.Payload, &pl); err != nil {
		return false, problem.Validation.WithField("payload", "phải là JSON hợp lệ")
	}
	m, err := mass.ParseKg(strings.TrimSpace(pl.MassKg))
	if err != nil {
		return false, problem.Validation.WithField("payload.mass_kg", "khối lượng kg dạng chuỗi decimal, tối đa 3 chữ số lẻ")
	}
	hd, err := optDay("harvest_date", pl.HarvestDate)
	if err != nil {
		return false, err
	}
	var moist *decimal.Decimal
	if pl.MoisturePct != nil {
		d, err := decimal.NewFromString(strings.TrimSpace(*pl.MoisturePct))
		if err != nil {
			return false, problem.Validation.WithField("payload.moisture_pct", "số thập phân dạng chuỗi")
		}
		moist = &d
	}
	switch rec.Op {
	case syncport.OpCreate:
		g, err := pl.GPS.Parse()
		if err != nil {
			return false, err
		}
		season, err := optRef("season_ref", pl.SeasonRef)
		if err != nil {
			return false, err
		}
		plot, err := optRef("plot_ref", pl.PlotRef)
		if err != nil {
			return false, err
		}
		in := HarvestInput{ID: rec.ID, SeasonRef: season, PlotRef: plot, MassKg: m, MoisturePct: moist, ConfirmAnomaly: pl.ConfirmAnomaly,
			GPS: g, EventTime: rec.EventTime, DeviceID: device, Source: SourceSync, IdemKey: rec.IdemKey}
		if hd != nil {
			in.HarvestDate = *hd
		}
		r, err := snk.Svc.declareHarvest(ctx, a, in)
		return r.Replayed, err
	case syncport.OpCorrect:
		if rec.SupersedesID == nil || rec.CorrectionReason == nil {
			return false, problem.Validation.WithField("supersedes_id", "bản ghi bù cần supersedes_id và correction_reason")
		}
		r, err := snk.Svc.correctHarvest(ctx, a, CorrectHarvestInput{ID: rec.ID, SupersedesID: *rec.SupersedesID, Reason: *rec.CorrectionReason,
			HarvestDate: hd, MassKg: m, MoisturePct: moist, ConfirmAnomaly: pl.ConfirmAnomaly, EventTime: rec.EventTime, DeviceID: device,
			Source: SourceSync, IdemKey: rec.IdemKey})
		return r.Replayed, err
	}
	return false, problem.Validation.WithField("op", "chỉ nhận create hoặc correct (ghi thêm, không sửa/xóa)")
}

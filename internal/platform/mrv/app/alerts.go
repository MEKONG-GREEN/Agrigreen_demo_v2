package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Cảnh báo MRV (G3): nhãn "Cảnh báo khẩn" (KN US-MRV-04), cờ AWD thiếu hạ tầng (KN FR-08), leo
// thang hồ sơ quá hạn (KN US-MRV-06). Mọi cảnh báo: một dòng sổ pf_mrv.alert + pf.mrv.alert.raised
// trong CÙNG giao dịch với sự việc gây ra nó; tham số đã dùng (có phiên bản) ghi kèm.

// textParamDefaults: tham số CHUỖI (danh sách) — mặc định TẠM, chờ khách.
var textParamDefaults = map[string]struct{ value, unit string }{
	port.ParamUrgentCategories: {"ray_nau,dao_on,dao_on_co_bong,sau_cuon_la,vang_lun_xoan_la,lu_lut,han_han,xam_nhap_man,bao_loc", ""},
	port.ParamAWDActivityCodes: {"tuoi_nuoc", ""},
}

func init() {
	paramDefaults[port.ParamUrgentMinSeverity] = struct{ value, unit string }{"4", "cấp"}
	paramDefaults[port.ParamUrgentMinAreaHa] = struct{ value, unit string }{"5", "ha"}
	paramDefaults[port.ParamReviewSLAHours] = struct{ value, unit string }{"72", "giờ"}
	paramDefaults[port.ParamReviewEscalateHours] = struct{ value, unit string }{"120", "giờ"}
}

// Thông tin người/hệ thống gây cảnh báo khi không phải người (consumer, job).
var alertSystemActor = store.SystemActor

// ---------------------------------------------------------------------------
// KN US-MRV-04: nhãn "Cảnh báo khẩn"

// pestRule là tham số của mrv.pest_urgent.v1 đọc TRƯỚC giao dịch ghi minh chứng.
type pestRule struct {
	categories  []string
	minSeverity int
	minArea     decimal.Decimal
	used        []domain.ParamUse
}

func (s *Service) pestRule(ctx context.Context, at time.Time) (*pestRule, error) {
	cats, err := s.param(ctx, port.ParamUrgentCategories, at)
	if err != nil {
		return nil, err
	}
	sev, err := s.param(ctx, port.ParamUrgentMinSeverity, at)
	if err != nil {
		return nil, err
	}
	area, err := s.param(ctx, port.ParamUrgentMinAreaHa, at)
	if err != nil {
		return nil, err
	}
	r := &pestRule{categories: domain.SplitList(cats.Value), used: []domain.ParamUse{cats, sev, area}}
	sd, err := decimal.NewFromString(sev.Value)
	if err != nil {
		return nil, fmt.Errorf("mrv: tham số %s không phải số: %w", port.ParamUrgentMinSeverity, err)
	}
	r.minSeverity = int(sd.IntPart())
	if r.minArea, err = decimal.NewFromString(area.Value); err != nil {
		return nil, fmt.Errorf("mrv: tham số %s không phải số: %w", port.ParamUrgentMinAreaHa, err)
	}
	return r, nil
}

// flagPest chạy trong giao dịch ghi minh chứng: phiên bản vượt ngưỡng → nhãn khẩn (idempotent theo
// phiên bản). Bản ghi bù là phiên bản mới, được đánh giá lại (nhãn cũ vẫn gắn với phiên bản cũ).
func (s *Service) flagPest(ctx context.Context, tx pgx.Tx, e domain.Evidence, r *pestRule) error {
	if r == nil || e.EvidenceType != port.EvidencePestDisaster {
		return nil
	}
	urgent, detail := domain.PestUrgent(e.Values, r.categories, r.minSeverity, r.minArea)
	if !urgent {
		return nil
	}
	b, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	return s.raise(ctx, tx, e.TenantID, &store.AlertRow{
		Kind: port.AlertPestUrgent, Severity: "urgent", AdminUnitCode: e.AdminUnitCode, SubjectType: port.AlertSubjectEvidence,
		SubjectID: e.ID, SubjectRootID: e.RootID, PlotRef: e.PlotRef, Detail: b, Params: r.used, EventTime: e.EventTime,
		ActorRef: alertSystemActor, IdemKey: "pest:" + e.ID.String(), RuleVersion: port.RulePestUrgent,
	})
}

// ---------------------------------------------------------------------------
// KN FR-08: nhật ký HTX ghi AWD nhưng vùng chưa đủ hạ tầng

type awdDetail struct {
	ActivityCode string `json:"activity_code"`
	LogEventTime string `json:"log_event_time"`
	InfraType    string `json:"infrastructure_evidence_type"`
	Activities   string `json:"awd_activity_codes"`
}

// flagAWDInfra chạy trong giao dịch của consumer liên kết nhật ký (sau khi ghi farm_log_link).
func (s *Service) flagAWDInfra(ctx context.Context, tx pgx.Tx, tid uuid.UUID, p farmLogRecordedV1, logTime time.Time) error {
	if !p.BecameCurrent || p.PlotRef == uuid.Nil || !domain.ValidUnitCode(p.CommuneCode) {
		return nil
	}
	codes, err := s.param(ctx, port.ParamAWDActivityCodes, logTime)
	if err != nil {
		return err
	}
	acts := domain.SplitList(codes.Value)
	if !domain.AWDNeedsInfrastructure(p.ActivityCode, p.BecameCurrent, acts, false) {
		return nil // không phải AWD: khỏi tra hạ tầng
	}
	has, err := store.HasInfrastructure(ctx, tx, port.EvidenceIrrigationInfra, p.PlotRef, p.CommuneCode)
	if err != nil || !domain.AWDNeedsInfrastructure(p.ActivityCode, p.BecameCurrent, acts, has) {
		return err
	}
	b, err := json.Marshal(awdDetail{ActivityCode: p.ActivityCode, LogEventTime: domain.FormatTime(logTime),
		InfraType: port.EvidenceIrrigationInfra, Activities: codes.Value})
	if err != nil {
		return err
	}
	plot, season := p.PlotRef, p.SeasonRef
	return s.raise(ctx, tx, tid, &store.AlertRow{
		Kind: port.AlertAWDNoInfrastructure, Severity: "normal", AdminUnitCode: p.CommuneCode, SubjectType: port.AlertSubjectFarmLog,
		SubjectID: p.LogID, SubjectRootID: p.LogRef, PlotRef: &plot, SeasonRef: &season, Detail: b,
		Params: []domain.ParamUse{codes}, EventTime: logTime, ActorRef: alertSystemActor, IdemKey: "awd:" + p.LogID.String(),
		RuleVersion: port.RuleAWDInfra,
	})
}

// ---------------------------------------------------------------------------
// KN US-MRV-06: leo thang hồ sơ quá hạn

// escalationBatch giới hạn số hồ sơ xử lý trong một giao dịch.
const escalationBatch = 200

type overdueDetail struct {
	PendingSince  string `json:"pending_since"`
	AgeHours      int    `json:"age_hours"`
	SLAHours      int    `json:"sla_hours"`
	EscalateHours int    `json:"escalate_hours"`
	EvidenceType  string `json:"evidence_type"`
}

// EscalateOverdue leo thang mọi hồ sơ chờ duyệt quá hạn của tenant tại now: quá SLA → mức 1
// (người duyệt), quá hạn leo thang → mức 2 (quản trị TTKN tỉnh). Idempotent theo (phiên bản hồ
// sơ, mức): khóa 'overdue:<head_id>:<mức>' — chạy lại/ chạy chồng không ghi hai lần. Sửa hồ sơ
// (bản ghi bù) tạo phiên bản mới chờ duyệt với hạn mới.
func (s *Service) EscalateOverdue(ctx context.Context, tid uuid.UUID, now time.Time) (int, error) {
	if s.DB == nil {
		return 0, ErrDependencyUnavailable
	}
	now = now.UTC()
	sla, err := s.param(ctx, port.ParamReviewSLAHours, now)
	if err != nil {
		return 0, err
	}
	esc, err := s.param(ctx, port.ParamReviewEscalateHours, now)
	if err != nil {
		return 0, err
	}
	slaH, escH := atoiParam(sla.Value), atoiParam(esc.Value)
	if slaH <= 0 {
		return 0, nil
	}
	if escH < slaH {
		escH = slaH
	}
	total := 0
	for {
		n, err := s.escalateBatch(ctx, tid, now, slaH, escH, []domain.ParamUse{sla, esc})
		total += n
		if err != nil || n == 0 || ctx.Err() != nil {
			return total, err
		}
	}
}

func atoiParam(v string) int {
	d, err := decimal.NewFromString(v)
	if err != nil {
		return 0
	}
	return int(d.IntPart())
}

func (s *Service) escalateBatch(ctx context.Context, tid uuid.UUID, now time.Time, slaH, escH int, used []domain.ParamUse) (int, error) {
	n := 0
	err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		heads, err := store.OverduePending(ctx, tx, now.Add(-time.Duration(slaH)*time.Hour), now.Add(-time.Duration(escH)*time.Hour), escalationBatch)
		if err != nil {
			return err
		}
		for _, h := range heads {
			level := domain.OverdueLevel(h.Since, now, slaH, escH)
			b, err := json.Marshal(overdueDetail{PendingSince: domain.FormatTime(h.Since), AgeHours: int(now.Sub(h.Since).Hours()),
				SLAHours: slaH, EscalateHours: escH, EvidenceType: h.EvidenceType})
			if err != nil {
				return err
			}
			for l := h.Raised + 1; l <= level; l++ {
				key := fmt.Sprintf("overdue:%s:%d", h.HeadID, l)
				if dup, err := store.IdemExists(ctx, tx, "alert", key); err != nil {
					return err
				} else if dup {
					continue
				}
				sev := "normal"
				if l == port.OverdueLevelProvinceAdmin {
					sev = "urgent"
				}
				if err := s.raise(ctx, tx, tid, &store.AlertRow{
					Kind: port.AlertReviewOverdue, Level: l, Severity: sev, AdminUnitCode: h.AdminUnitCode,
					SubjectType: port.AlertSubjectEvidence, SubjectID: h.HeadID, SubjectRootID: h.RootID, PlotRef: h.PlotRef,
					Detail: b, Params: used, EventTime: now, ActorRef: alertSystemActor, IdemKey: key, RuleVersion: port.RuleReviewOverdue,
				}); err != nil {
					return err
				}
				n++
			}
		}
		return nil
	})
	if errors.Is(ledger.Classify(err), ledger.ErrDuplicate) {
		return 0, nil // bản chạy khác vừa leo thang cùng hồ sơ: coi như xong
	}
	return n, err
}

// TenantLister trả các tenant có hồ sơ chờ duyệt.
type TenantLister func(ctx context.Context) ([]uuid.UUID, error)

// PendingTenants dùng pool vai ag_ops_jobs (BYPASSRLS, chỉ SELECT hai cột tenant_id,
// review_status của evidence_current — pf 000045) để tìm tenant cần chạy; leo thang chạy bằng vai
// mrv_app theo từng tenant.
func PendingTenants(pool *pgxpool.Pool) TenantLister {
	return func(ctx context.Context) ([]uuid.UUID, error) {
		rows, err := pool.Query(ctx, `SELECT DISTINCT tenant_id FROM pf_mrv.evidence_current WHERE review_status = 'pending'`)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	}
}

// RunEscalation là thân job port.JobReviewEscalator: mọi tenant có hồ sơ chờ duyệt.
func (s *Service) RunEscalation(ctx context.Context, tenants TenantLister) (int, error) {
	now := s.Clock.Now()
	tids, err := tenants(ctx)
	if err != nil {
		return 0, err
	}
	total := 0
	var errs []error
	for _, tid := range tids {
		n, err := s.EscalateOverdue(ctx, tid, now)
		total += n
		if err != nil {
			errs = append(errs, fmt.Errorf("tenant %s: %w", tid, err))
		}
	}
	return total, errors.Join(errs...)
}

// ---------------------------------------------------------------------------
// Ghi cảnh báo + sự kiện

func (s *Service) raise(ctx context.Context, tx pgx.Tx, tid uuid.UUID, a *store.AlertRow) error {
	if dup, err := store.IdemExists(ctx, tx, "alert", a.IdemKey); err != nil || dup {
		return err
	}
	a.ID = ids.New()
	if err := store.InsertAlert(ctx, tx, tid, a); err != nil {
		return err
	}
	return outbox.Enqueue(ctx, tx, outbox.Event{
		ID: ids.New(), TenantID: tid, Type: port.EventAlertRaised, SchemaVersion: 1, OccurredAt: a.EventTime,
		TraceID: telemetry.TraceID(ctx), Producer: Producer, Payload: alertEvent(*a),
	})
}

func alertEvent(a store.AlertRow) port.AlertRaisedV1 {
	return port.AlertRaisedV1{
		AlertID: a.ID.String(), Kind: a.Kind, Level: a.Level, Severity: a.Severity, AdminUnitCode: a.AdminUnitCode,
		SubjectType: a.SubjectType, SubjectID: a.SubjectID.String(), SubjectRootID: a.SubjectRootID.String(),
		PlotRef: uuidStr(a.PlotRef), SeasonRef: uuidStr(a.SeasonRef), RuleVersion: a.RuleVersion, EventTime: domain.FormatTime(a.EventTime),
	}
}

func uuidStr(u *uuid.UUID) *string {
	if u == nil {
		return nil
	}
	s := u.String()
	return &s
}

// ---------------------------------------------------------------------------
// Đọc

// AlertFilter lọc danh sách cảnh báo.
type AlertFilter struct {
	Kind          string
	AdminUnitCode string
	After         uuid.UUID
	Limit         int
}

var alertKinds = map[string]bool{port.AlertPestUrgent: true, port.AlertAWDNoInfrastructure: true, port.AlertReviewOverdue: true}

// Alerts liệt kê cảnh báo trong phạm vi người gọi (vai đọc minh chứng).
func (s *Service) Alerts(ctx context.Context, f AlertFilter) (Page[store.AlertRow], error) {
	p, err := authz.Require(ctx, evidenceReaders...)
	if err != nil {
		return Page[store.AlertRow]{}, err
	}
	if f.Kind != "" && !alertKinds[f.Kind] {
		return Page[store.AlertRow]{}, problem.Validation.WithField("kind", "phải là pest_urgent, awd_no_infrastructure hoặc review_overdue")
	}
	sc, err := s.scope(ctx)
	if err != nil {
		return Page[store.AlertRow]{}, err
	}
	limit := clampLimit(f.Limit)
	var rows []store.AlertRow
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		rows, e = store.Alerts(ctx, tx, store.AlertFilter{Kind: f.Kind, Unit: f.AdminUnitCode, All: sc.All, Units: sc.Units,
			After: f.After, Limit: limit + 1})
		return e
	})
	if err != nil {
		return Page[store.AlertRow]{}, err
	}
	return page(rows, limit, func(r store.AlertRow) uuid.UUID { return r.ID }), nil
}

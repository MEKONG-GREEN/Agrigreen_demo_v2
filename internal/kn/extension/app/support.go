package app

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/store"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mã lỗi yêu cầu hỗ trợ (docs/errors.md).
var (
	ErrNoHousehold      = problem.New(http.StatusForbidden, "extension.farmer_household_required", "Tài khoản chưa liên kết nông hộ nào")
	ErrHouseholdChoice  = problem.New(http.StatusUnprocessableEntity, "extension.household_invalid", "Nông hộ không thuộc tài khoản của bạn")
	ErrSupportNotFound  = problem.New(http.StatusNotFound, "extension.support_request_not_found", "Không tìm thấy yêu cầu hỗ trợ")
	errNoFarmerAccounts = errors.New("extension: chưa nối pf/accounts (FarmerHouseholds)")
)

// SupportInput là một yêu cầu hỗ trợ nông dân gửi (API hoặc bản ghi đồng bộ support_request).
type SupportInput struct {
	ID           *uuid.UUID // UUIDv7 sinh trên máy (ngoại tuyến, HTX US-SUPPORT-01 AC-2); nil → server sinh
	HouseholdRef uuid.UUID  // Nil → nông hộ duy nhất của tài khoản
	PlotRef      uuid.UUID
	Category     string
	Urgency      string
	Description  string
	PhotoRefs    []uuid.UUID
	Location     *domain.Point
	GPSAccuracyM *decimal.Decimal
	EventTime    *time.Time // lúc bấm Gửi trên máy; nil = lúc nhận
	DeviceID     *string
	IdemKey      string
}

// SupportResult là kết quả gửi yêu cầu.
type SupportResult struct {
	Request  domain.SupportRequest
	Task     domain.Task
	Replayed bool
}

// latency đo độ trễ nhận yêu cầu → nhiệm vụ đã ghi (KN US-TASK-01 ≤ 1 phút).
var (
	latencyOnce sync.Once
	latencyHist metric.Float64Histogram
)

func recordLatency(ctx context.Context, d time.Duration, rule string) {
	if !telemetry.Enabled() {
		return
	}
	latencyOnce.Do(func() {
		latencyHist, _ = telemetry.Meter().Float64Histogram(port.MetricSupportTaskLatency, metric.WithUnit("s"),
			metric.WithDescription("Độ trễ từ lúc nhận yêu cầu hỗ trợ tới lúc nhiệm vụ đã ghi (KN US-TASK-01)"),
			metric.WithExplicitBucketBoundaries(0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30, 60, 120))
	})
	if latencyHist != nil {
		latencyHist.Record(ctx, d.Seconds(), metric.WithAttributes(attribute.String("assignment_rule", rule)))
	}
}

// farmerLinks trả nông hộ liên kết với nông dân gọi.
func (s *Service) farmerLinks(ctx context.Context) (authz.Principal, []accport.HouseholdLink, error) {
	p, err := authz.Require(ctx, accport.RoleFarmer)
	if err != nil {
		return p, nil, err
	}
	if s.Farmers == nil {
		return p, nil, ErrDependency.WithCause(errNoFarmerAccounts)
	}
	links, err := s.Farmers.HouseholdsForSubject(tenant.WithID(ctx, p.TenantID), p.ActorRef)
	if err != nil {
		return p, nil, err
	}
	if len(links) == 0 {
		return p, nil, ErrNoHousehold
	}
	return p, links, nil
}

// SubmitSupport nhận yêu cầu hỗ trợ của nông dân (HTX US-SUPPORT-01) và trong CÙNG
// giao dịch tự sinh nhiệm vụ cho cán bộ khuyến nông (KN US-TASK-01): giao cán bộ xã
// đang trực → cán bộ tỉnh đang trực → cán bộ tỉnh → để trống báo quản trị tỉnh; phát
// pf.extension.support_request.submitted và pf.extension.task.created. Mốc SLA 1 phút
// là received_at (giờ máy chủ), đo bằng metric ag.extension.support_task.latency.
func (s *Service) SubmitSupport(ctx context.Context, in SupportInput) (SupportResult, error) {
	received := s.now()
	p, links, err := s.farmerLinks(ctx)
	if err != nil {
		return SupportResult{}, err
	}
	if in.IdemKey == "" {
		return SupportResult{}, problem.IdempotencyRequired
	}
	link := links[0]
	if in.HouseholdRef != uuid.Nil {
		i := slices.IndexFunc(links, func(l accport.HouseholdLink) bool { return l.HouseholdRef == in.HouseholdRef })
		if i < 0 {
			return SupportResult{}, ErrHouseholdChoice.WithField("household_ref", "không thuộc tài khoản")
		}
		link = links[i]
	} else if len(links) > 1 {
		return SupportResult{}, ErrHouseholdChoice.WithField("household_ref", "tài khoản có nhiều nông hộ — chọn một")
	}
	r := domain.SupportRequest{
		HouseholdRef: link.HouseholdRef, CooperativeRef: link.CooperativeRef, CommuneCode: link.CommuneCode, PlotRef: in.PlotRef,
		Category: in.Category, Urgency: in.Urgency, Description: in.Description, PhotoRefs: in.PhotoRefs, Location: in.Location,
		GPSAccuracyM: in.GPSAccuracyM, EventTime: received, ReceivedAt: received, SubmittedBy: p.ActorRef, DeviceID: in.DeviceID,
		IdemKey: in.IdemKey,
	}
	if in.EventTime != nil && !in.EventTime.IsZero() && !in.EventTime.After(received) {
		r.EventTime = in.EventTime.UTC()
	}
	if err := r.Validate(); err != nil {
		return SupportResult{}, fieldProblem(err)
	}
	province, level, err := s.provinceOf(ctx, r.CommuneCode, "commune_code")
	if err != nil {
		return SupportResult{}, err
	}
	if level != "commune" {
		return SupportResult{}, ErrUnitInvalid.WithField("commune_code", "nông hộ phải gắn với một xã")
	}
	r.ProvinceCode = province
	sla, err := s.paramDecimal(ctx, port.ParamTaskSLAHours, received, domain.DefaultSLAHours)
	if err != nil {
		return SupportResult{}, err
	}
	if !domain.ValidSLA(sla) {
		sla = domain.DefaultSLAHours
	}
	escOrg := s.provinceOrgOf(ctx, link.CooperativeRef)
	r.ID = ids.New()
	if in.ID != nil {
		r.ID = *in.ID
	}
	var res SupportResult
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.SupportByIdemKey(ctx, tx, in.IdemKey); err != nil {
			return err
		} else if prev != nil {
			t, err := store.TaskByID(ctx, tx, prev.TaskID, false)
			if err != nil || t == nil {
				return errors.Join(errors.New("extension: không đọc lại được nhiệm vụ của yêu cầu"), err)
			}
			res = SupportResult{Request: *prev, Task: *t, Replayed: true}
			return nil
		}
		if prev, err := store.SupportByID(ctx, tx, r.ID); err != nil {
			return err
		} else if prev != nil {
			return ErrIDReuse
		}
		// Người nhận: danh bạ của xã và tỉnh, trạng thái trực tại lúc NHẬN.
		staff, err := store.StaffInUnits(ctx, tx, false, []string{r.CommuneCode, province})
		if err != nil {
			return err
		}
		actors := make([]uuid.UUID, 0, len(staff))
		for _, st := range staff {
			actors = append(actors, st.ActorRef)
		}
		manual, err := store.LatestDutyStatus(ctx, tx, actors, received)
		if err != nil {
			return err
		}
		shifts, err := store.ShiftsCovering(ctx, tx, actors, received)
		if err != nil {
			return err
		}
		load, err := store.OpenTaskLoad(ctx, tx, actors)
		if err != nil {
			return err
		}
		cands := make([]domain.DutyCandidate, 0, len(staff))
		for _, st := range staff {
			c := domain.DutyCandidate{Staff: st, Shifts: shifts[st.ActorRef]}
			if m, ok := manual[st.ActorRef]; ok {
				c.Manual = &m
			}
			cands = append(cands, c)
		}
		assignee, rule := domain.PickAssignee(received, r.CommuneCode, province, cands, load,
			port.AssignCommuneOnDuty, port.AssignProvinceOnDuty, port.AssignProvinceStaff, port.AssignAdminQueue)

		created := s.now()
		t := domain.Task{
			ID: ids.New(), CommuneCode: r.CommuneCode, ProvinceCode: province, CooperativeRef: r.CooperativeRef,
			HouseholdRef: r.HouseholdRef, PlotRef: r.PlotRef, Category: r.TaskCategory(), Description: r.Description,
			Priority: r.TaskPriority(), Source: domain.SourceHTXApp, Status: domain.StatusNew, AssigneeRef: assignee,
			CreatedBy: p.ActorRef, CreatedAt: created, SLAHours: sla, DueAt: domain.DueAt(created, sla),
			EscalatedUnit: province, EscalationOrgRef: escOrg,
		}
		if assignee != uuid.Nil {
			t.Status = domain.StatusAssigned
		}
		snap := snapshot(t)
		snap["support_request_id"], snap["assignment_rule"] = r.ID.String(), rule
		ev := domain.Event{
			ID: ids.New(), TenantID: p.TenantID, TaskID: t.ID, Action: domain.ActionCreated, StatusAfter: t.Status,
			AssigneeRef: t.AssigneeRef, DueAt: t.DueAt, EscalatedUnit: province, EscalationOrgRef: escOrg, Snapshot: snap,
			EventTime: received, ActorRef: p.ActorRef, DeviceID: in.DeviceID, IdemKey: "support:" + in.IdemKey,
		}
		if err := store.InsertEvent(ctx, tx, &ev); err != nil {
			return err
		}
		t.LastSeq = ev.Seq
		if err := store.InsertTask(ctx, tx, p.TenantID, t, ev); err != nil {
			return err
		}
		r.TaskID, r.TaskCreatedAt, r.AssigneeRef, r.AssignmentRule = t.ID, created, assignee, rule
		if err := store.InsertSupportRequest(ctx, tx, p.TenantID, r); err != nil {
			return err
		}
		if err := enqueueSupport(ctx, tx, p.TenantID, r, t, ev); err != nil {
			return err
		}
		cur, err := store.TaskByID(ctx, tx, t.ID, false)
		if err != nil || cur == nil {
			return errors.Join(errors.New("extension: không đọc lại được nhiệm vụ vừa tạo"), err)
		}
		r.TaskStatus, r.TaskSeq = cur.Status, cur.SyncSeq
		res = SupportResult{Request: r, Task: *cur}
		return nil
	})
	if err = ledger.Classify(err); errors.Is(err, ledger.ErrDuplicate) {
		// Hai lần gửi cùng khóa chạy song song: bên thua đọc lại bản đã ghi.
		var prev *domain.SupportRequest
		var t *domain.Task
		if rerr := tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
			var e error
			if prev, e = store.SupportByIdemKey(ctx, tx, in.IdemKey); e != nil || prev == nil {
				return e
			}
			t, e = store.TaskByID(ctx, tx, prev.TaskID, false)
			return e
		}); rerr == nil && prev != nil && t != nil {
			return SupportResult{Request: *prev, Task: *t, Replayed: true}, nil
		}
	}
	if err != nil {
		return SupportResult{}, err
	}
	if !res.Replayed {
		recordLatency(ctx, s.now().Sub(received), res.Request.AssignmentRule)
	}
	return res, nil
}

func enqueueSupport(ctx context.Context, tx pgx.Tx, tid uuid.UUID, r domain.SupportRequest, t domain.Task, ev domain.Event) error {
	trace := telemetry.TraceID(ctx)
	if err := outbox.Enqueue(ctx, tx, outbox.Event{
		ID: ids.New(), TenantID: tid, Type: port.EventSupportRequestSubmitted, SchemaVersion: 1, OccurredAt: r.ReceivedAt,
		TraceID: trace, Producer: port.EventProducerName,
		Payload: port.SupportRequestSubmittedV1{
			SupportRequestID: r.ID, TaskID: t.ID, HouseholdRef: r.HouseholdRef, CooperativeRef: r.CooperativeRef,
			CommuneCode: r.CommuneCode, ProvinceCode: r.ProvinceCode, Category: r.Category, Urgency: r.Urgency,
			PhotoCount: len(r.PhotoRefs), GPSMissing: r.GPSMissing(), EventTime: r.EventTime.UTC().Format(time.RFC3339),
			ReceivedAt: r.ReceivedAt.UTC().Format(time.RFC3339Nano),
		},
	}); err != nil {
		return err
	}
	pl := port.TaskCreatedV1{
		TaskID: t.ID, Source: t.Source, CommuneCode: t.CommuneCode, ProvinceCode: t.ProvinceCode, Category: t.Category,
		Priority: t.Priority, Status: string(t.Status), AssignmentRule: r.AssignmentRule,
		DueAt: t.DueAt.UTC().Format(time.RFC3339), CreatedAt: t.CreatedAt.UTC().Format(time.RFC3339Nano), Seq: ev.Seq,
	}
	if t.CooperativeRef != uuid.Nil {
		c := t.CooperativeRef
		pl.CooperativeRef = &c
	}
	if t.AssigneeRef != uuid.Nil {
		a := t.AssigneeRef
		pl.AssigneeRef = &a
	}
	sr := r.ID
	pl.SupportRequestID = &sr
	return outbox.Enqueue(ctx, tx, outbox.Event{
		ID: ids.New(), TenantID: tid, Type: port.EventTaskCreated, SchemaVersion: 1, OccurredAt: t.CreatedAt,
		TraceID: trace, Producer: port.EventProducerName, Payload: pl,
	})
}

// SupportPage là một trang yêu cầu hỗ trợ của nông dân.
type SupportPage struct {
	Items []domain.SupportRequest
	Next  *uuid.UUID
}

// ListMySupport liệt kê yêu cầu hỗ trợ của (các) nông hộ của nông dân, kèm trạng thái nhiệm vụ.
func (s *Service) ListMySupport(ctx context.Context, before uuid.UUID, limit int) (SupportPage, error) {
	p, links, err := s.farmerLinks(ctx)
	if err != nil {
		return SupportPage{}, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	hh := make([]uuid.UUID, 0, len(links))
	for _, l := range links {
		hh = append(hh, l.HouseholdRef)
	}
	var items []domain.SupportRequest
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		items, e = store.SupportForHouseholds(ctx, tx, hh, before, limit+1)
		return e
	})
	if err != nil {
		return SupportPage{}, err
	}
	pg := SupportPage{Items: items}
	if len(items) > limit {
		pg.Items = items[:limit]
		last := pg.Items[limit-1].ID
		pg.Next = &last
	}
	return pg, nil
}

package app

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/store"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// SyncSource cung cấp chiều xuống "task" và "directive" (sync-contract).
type SyncSource struct{ Svc *Service }

// SyncSink nhận chiều lên "task_update" và "field_report".
type SyncSink struct{ Svc *Service }

var (
	_ syncport.Source = SyncSource{}
	_ syncport.Sink   = SyncSink{}
)

// Entities thực hiện syncport.Source.
func (SyncSource) Entities() []string {
	return []string{port.EntityTask, port.EntityDirective, port.EntitySupportRequest}
}

// taskData là dữ liệu nhiệm vụ gửi xuống máy — không có dữ liệu cá nhân (mục 10.4).
type taskData struct {
	TaskID         string  `json:"task_id"`
	CommuneCode    string  `json:"commune_code"`
	ProvinceCode   string  `json:"province_code"`
	CooperativeRef *string `json:"cooperative_ref"`
	HouseholdRef   *string `json:"household_ref"`
	PlotRef        *string `json:"plot_ref"`
	Category       string  `json:"category"`
	Description    string  `json:"description"`
	Priority       string  `json:"priority"`
	Status         string  `json:"status"`
	AssigneeRef    *string `json:"assignee_ref"`
	DueAt          string  `json:"due_at"`
	EscalatedAt    *string `json:"escalated_at"`
	LastNote       string  `json:"last_note"`
	Version        int64   `json:"version"`
}

type directiveData struct {
	DirectiveID   string   `json:"directive_id"`
	Title         string   `json:"title"`
	Body          string   `json:"body"`
	Severity      string   `json:"severity"`
	AudienceUnits []string `json:"audience_units"`
	IssuedAt      string   `json:"issued_at"`
	ExpiresAt     *string  `json:"expires_at"`
	AckedAt       *string  `json:"acked_at"`
	IsTarget      bool     `json:"is_target"`
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
	s := t.UTC().Format(time.RFC3339)
	return &s
}

// Changes thực hiện syncport.Source: nhiệm vụ trong phạm vi hoặc được giao cho
// người gọi; chỉ đạo người gọi là người nhận (cấp giám sát: cả chỉ đạo trong phạm vi).
func (src SyncSource) Changes(ctx context.Context, sc authz.Scope, entity string, after int64, limit int) ([]syncport.Change, int64, error) {
	if entity == port.EntitySupportRequest {
		return src.supportChanges(ctx, after, limit)
	}
	p, err := authz.Require(ctx, taskReaders...)
	if err != nil {
		return nil, after, err
	}
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	var out []syncport.Change
	last := after
	err = tenant.WithTenantTx(ctx, src.Svc.DB, p.TenantID, func(tx pgx.Tx) error {
		switch entity {
		case port.EntityTask:
			tasks, err := store.TasksChangedSince(ctx, tx, sc.All, sc.Units, p.ActorRef, after, limit)
			if err != nil {
				return err
			}
			for _, t := range tasks {
				if onlyCoopManager(p) && t.CooperativeRef != p.OrgRef {
					if t.SyncSeq > last {
						last = t.SyncSeq
					}
					continue
				}
				b, err := json.Marshal(taskData{
					TaskID: t.ID.String(), CommuneCode: t.CommuneCode, ProvinceCode: t.ProvinceCode,
					CooperativeRef: refStr(t.CooperativeRef), HouseholdRef: refStr(t.HouseholdRef), PlotRef: refStr(t.PlotRef),
					Category: t.Category, Description: t.Description, Priority: t.Priority, Status: string(t.Status),
					AssigneeRef: refStr(t.AssigneeRef), DueAt: t.DueAt.UTC().Format(time.RFC3339),
					EscalatedAt: timeStr(t.EscalatedAt), LastNote: t.LastNote, Version: t.LastSeq,
				})
				if err != nil {
					return err
				}
				out = append(out, syncport.Change{Entity: port.EntityTask, ID: t.ID, Version: t.LastSeq, Op: syncport.OpUpsert, Data: b})
				last = t.SyncSeq
			}
		case port.EntityDirective:
			ds, err := store.ListDirectives(ctx, tx, store.DirectiveFilter{
				Actor: p.ActorRef, All: sc.All, Units: sc.Units, OnlyMine: !isSupervisor(p), After: after, Limit: limit,
			})
			if err != nil {
				return err
			}
			for _, d := range ds {
				b, err := json.Marshal(directiveData{
					DirectiveID: d.ID.String(), Title: d.Title, Body: d.Body, Severity: d.Severity,
					AudienceUnits: d.AudienceUnits, IssuedAt: d.IssuedAt.UTC().Format(time.RFC3339),
					ExpiresAt: timeStr(d.ExpiresAt), AckedAt: timeStr(d.AckedByMe), IsTarget: d.IsTarget,
				})
				if err != nil {
					return err
				}
				out = append(out, syncport.Change{Entity: port.EntityDirective, ID: d.ID, Version: 1, Op: syncport.OpUpsert, Data: b})
				last = d.SyncSeq
			}
		default:
			return problem.Validation.WithField("entity", "module không cung cấp entity này")
		}
		return nil
	})
	return out, last, err
}

// FarmerEntities thực hiện syncport.FarmerSource: nông dân chỉ nhận yêu cầu hỗ trợ của nông
// hộ mình (HTX US-SUPPORT-01); task/directive đóng với nông dân.
func (SyncSource) FarmerEntities() []string { return []string{port.EntitySupportRequest} }

// FarmerChanges thực hiện syncport.FarmerSource (nông hộ tra lại qua pf/accounts, như REST).
func (src SyncSource) FarmerChanges(ctx context.Context, _ syncport.FarmerScope, entity string, after int64, limit int) ([]syncport.Change, int64, error) {
	if entity != port.EntitySupportRequest {
		return []syncport.Change{}, after, nil
	}
	return src.supportChanges(ctx, after, limit)
}

var _ syncport.FarmerSource = SyncSource{}

// Entities thực hiện syncport.Sink.
func (SyncSink) Entities() []string {
	return []string{port.EntityTaskUpdate, port.EntityFieldReport, port.EntitySupportRequest}
}

// Accept thực hiện syncport.Sink: idempotent theo IdemKey; một bản ghi lỗi trả
// rejected, không làm hỏng cả lô.
func (snk SyncSink) Accept(ctx context.Context, sc authz.Scope, rec syncport.Record) syncport.Result {
	res := syncport.Result{ID: rec.ID}
	p, ok := authz.FromContext(ctx)
	if !ok {
		return reject(res, problem.Unauthenticated)
	}
	if rec.IdemKey == "" {
		return reject(res, problem.IdempotencyRequired)
	}
	ctx = withScope(ctx, sc)
	var device *string
	if rec.DeviceID != uuid.Nil {
		d := rec.DeviceID.String()
		device = &d
	}
	var evTime *time.Time
	if !rec.EventTime.IsZero() {
		t := rec.EventTime
		evTime = &t
	}
	switch rec.Entity {
	case port.EntitySupportRequest:
		return snk.acceptSupport(ctx, rec, res, device, evTime)
	case port.EntityTaskUpdate:
		if dup, err := snk.seen(ctx, p.TenantID, rec.IdemKey, false); err != nil {
			return reject(res, err)
		} else if dup {
			res.Status = syncport.StatusDuplicate
			return res
		}
		if rec.Op != syncport.OpCreate {
			return reject(res, problem.Validation.WithField("op", "task_update chỉ nhận op = create (mỗi cập nhật là một sự việc mới)"))
		}
		var body TaskUpdatePayload
		if err := json.Unmarshal(rec.Payload, &body); err != nil {
			return reject(res, problem.Validation.WithField("payload", "JSON không hợp lệ"))
		}
		in, err := body.ToInput()
		if err != nil {
			return reject(res, err)
		}
		id := rec.ID
		in.EventID, in.IdemKey, in.DeviceID, in.EventTime = &id, rec.IdemKey, device, evTime
		out, err := snk.Svc.UpdateTask(ctx, in)
		if err != nil {
			return reject(res, err)
		}
		res.Status = syncport.StatusAccepted
		if out.Replayed {
			res.Status = syncport.StatusDuplicate
		}
		return res
	case port.EntityFieldReport:
		if dup, err := snk.seen(ctx, p.TenantID, rec.IdemKey, true); err != nil {
			return reject(res, err)
		} else if dup {
			res.Status = syncport.StatusDuplicate
			return res
		}
		var body ReportPayload
		if err := json.Unmarshal(rec.Payload, &body); err != nil {
			return reject(res, problem.Validation.WithField("payload", "JSON không hợp lệ"))
		}
		r, err := body.ToReport()
		if err != nil {
			return reject(res, err)
		}
		id := rec.ID
		in := ReportInput{ID: &id, Report: r, EventTime: evTime, DeviceID: device, IdemKey: rec.IdemKey}
		switch rec.Op {
		case syncport.OpCreate:
			if rec.SupersedesID != nil {
				return reject(res, problem.Validation.WithField("supersedes_id", "chỉ dùng với op = correct"))
			}
		case syncport.OpCorrect:
			if rec.SupersedesID == nil {
				return reject(res, problem.Validation.WithField("supersedes_id", "bắt buộc với op = correct"))
			}
			if !domain.ValidReason(rec.CorrectionReason) {
				return reject(res, ErrReasonRequired.WithField("correction_reason", "bắt buộc, ≥ 10 ký tự"))
			}
			in.SupersedesID, in.CorrectionReason = rec.SupersedesID, rec.CorrectionReason
		default:
			return reject(res, problem.Validation.WithField("op", "create hoặc correct"))
		}
		out, err := snk.Svc.SubmitReport(ctx, in)
		if err != nil {
			return reject(res, err)
		}
		res.Status = syncport.StatusAccepted
		if out.Replayed {
			res.Status = syncport.StatusDuplicate
		}
		return res
	}
	return reject(res, problem.Validation.WithField("entity", "module không nhận entity này"))
}

// seen cho biết idem_key đã được ghi (bản ghi máy gửi lặp → duplicate).
func (snk SyncSink) seen(ctx context.Context, tid uuid.UUID, key string, report bool) (bool, error) {
	var dup bool
	err := tenant.WithTenantTx(ctx, snk.Svc.DB, tid, func(tx pgx.Tx) error {
		if report {
			r, err := store.ReportByIdemKey(ctx, tx, key)
			dup = r != nil
			return err
		}
		e, err := store.EventByIdemKey(ctx, tx, key)
		dup = e != nil
		return err
	})
	return dup, err
}

func reject(res syncport.Result, err error) syncport.Result {
	pr := problem.From(err)
	if pr.Status >= 500 && !errors.Is(err, context.Canceled) {
		// Lỗi hệ thống: không lộ chi tiết; máy giữ bản ghi và gửi lại sau.
		pr = problem.Internal
	}
	res.Status, res.Code, res.Errors = syncport.StatusRejected, pr.Code, pr.Errors
	return res
}

package app

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/store"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// ScheduleData là dữ liệu lịch gom (API và chiều xuống htx_collection_schedule).
type ScheduleData struct {
	ScheduleRef    string `json:"schedule_ref"`
	CooperativeRef string `json:"cooperative_ref"`
	CommuneCode    string `json:"commune_code"`
	WindowFrom     string `json:"window_from"`
	WindowTo       string `json:"window_to"`
	Status         string `json:"status"`
	Revision       int    `json:"revision"`
	PublishedAt    string `json:"published_at"`
	UpdatedAt      string `json:"updated_at"`
}

// ResponseData là phản hồi hiện hành / một phản hồi trong lịch sử.
type ResponseData struct {
	ID           string  `json:"id"`
	Revision     int     `json:"revision"`
	Outcome      string  `json:"outcome"`
	HTXRolls     int     `json:"htx_rolls"`
	Reason       *string `json:"reason"`
	RespondedAt  string  `json:"responded_at"`
	SupersedesID *string `json:"supersedes_id,omitempty"`
}

// StatementData là dữ liệu bảng kê (API và chiều xuống htx_collection_statement).
type StatementData struct {
	StatementRef   string        `json:"statement_ref"`
	CooperativeRef string        `json:"cooperative_ref"`
	PlotRef        string        `json:"plot_ref"`
	PlotCode       *string       `json:"plot_code"`
	SeasonRef      *string       `json:"season_ref"`
	CollectedOn    string        `json:"collected_on"`
	Rolls          int           `json:"rolls"`
	Revision       int           `json:"revision"`
	Pending        bool          `json:"pending"`
	Response       *ResponseData `json:"response"`
	PublishedAt    string        `json:"published_at"`
	UpdatedAt      string        `json:"updated_at"`
}

// ScheduleOut dựng dữ liệu hiển thị của lịch gom.
func ScheduleOut(v store.ScheduleView) ScheduleData {
	return ScheduleData{ScheduleRef: v.ScheduleRef.String(), CooperativeRef: v.CooperativeRef.String(), CommuneCode: v.CommuneCode,
		WindowFrom: domain.FormatDay(v.WindowFrom), WindowTo: domain.FormatDay(v.WindowTo), Status: v.Status, Revision: v.Revision,
		PublishedAt: v.PublishedAt.UTC().Format(time.RFC3339), UpdatedAt: v.UpdatedAt.UTC().Format(time.RFC3339Nano)}
}

// StatementOut dựng dữ liệu hiển thị của bảng kê.
func StatementOut(v store.StatementView, plotCode string) StatementData {
	d := StatementData{StatementRef: v.StatementRef.String(), CooperativeRef: v.CooperativeRef.String(), PlotRef: v.PlotRef.String(),
		SeasonRef: refPtrStr(v.SeasonRef), CollectedOn: domain.FormatDay(v.CollectedOn), Rolls: v.Rolls, Revision: v.Revision,
		Pending: v.Pending(), PublishedAt: v.PublishedAt.UTC().Format(time.RFC3339), UpdatedAt: v.UpdatedAt.UTC().Format(time.RFC3339Nano)}
	if plotCode != "" {
		d.PlotCode = &plotCode
	}
	if v.ResponseID != nil && v.ResponseOutcome != nil && v.ResponseRevision != nil {
		r := ResponseData{ID: v.ResponseID.String(), Revision: *v.ResponseRevision, Outcome: *v.ResponseOutcome, Reason: v.ResponseReason}
		if v.ResponseHTXRolls != nil {
			r.HTXRolls = *v.ResponseHTXRolls
		}
		if v.RespondedAt != nil {
			r.RespondedAt = v.RespondedAt.UTC().Format(time.RFC3339)
		}
		d.Response = &r
	}
	return d
}

// ResponseOut dựng một phản hồi của lịch sử.
func ResponseOut(r store.Response) ResponseData {
	return ResponseData{ID: r.ID.String(), Revision: r.StatementRevision, Outcome: r.Outcome, HTXRolls: r.HTXRolls, Reason: r.Reason,
		RespondedAt: r.EventTime.UTC().Format(time.RFC3339), SupersedesID: refPtrStr(r.SupersedesID)}
}

func refPtrStr(u *uuid.UUID) *string {
	if u == nil {
		return nil
	}
	s := u.String()
	return &s
}

// collectionChanges: chiều xuống lịch gom / bảng kê — chỉ quản lý HTX (HTX của mình) và
// quản trị; vai khác không nhận entity (không lỗi).
func (src SyncSource) collectionChanges(ctx context.Context, a access, entity string, after int64, limit int) ([]syncport.Change, int64, error) {
	all := a.kind == accAll
	if !all && (a.kind != accCoop || a.p.OrgRef == uuid.Nil) {
		return []syncport.Change{}, after, nil
	}
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	out := []syncport.Change{}
	last := after
	err := src.Svc.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		if entity == port.EntityCollectionSchedule {
			rows, err := store.SchedulesChangedAfter(ctx, tx, a.p.OrgRef, all, after, limit)
			if err != nil {
				return err
			}
			for _, r := range rows {
				b, err := json.Marshal(ScheduleOut(r))
				if err != nil {
					return err
				}
				last = r.SyncSeq
				out = append(out, syncport.Change{Entity: entity, ID: r.ScheduleRef, Version: r.SyncSeq, Op: syncport.OpUpsert, Data: b})
			}
			return nil
		}
		rows, err := store.StatementsChangedAfter(ctx, tx, a.p.OrgRef, all, after, limit)
		if err != nil {
			return err
		}
		for _, r := range rows {
			b, err := json.Marshal(StatementOut(r, ""))
			if err != nil {
				return err
			}
			last = r.SyncSeq
			out = append(out, syncport.Change{Entity: entity, ID: r.StatementRef, Version: r.SyncSeq, Op: syncport.OpUpsert, Data: b})
		}
		return nil
	})
	if err != nil {
		return nil, after, err
	}
	return out, last, nil
}

// CollectionResponsePayload là payload của entity htx_collection_response (op create) và
// thân POST /v1/cropcycle/htx-collection/statements/{ref}/responses (statement_ref lấy từ đường dẫn).
type CollectionResponsePayload struct {
	StatementRef string `json:"statement_ref"`
	Revision     int    `json:"revision"`
	Outcome      string `json:"outcome"`
	HTXRolls     *int   `json:"htx_rolls"`
	Reason       string `json:"reason"`
}

// collectionResponse nhận phản hồi gửi ngoại tuyến từ App HTX.
func (snk SyncSink) collectionResponse(ctx context.Context, rec syncport.Record, device *string) (bool, error) {
	if rec.Op != syncport.OpCreate {
		return false, problem.Validation.WithField("op", "htx_collection_response chỉ nhận op = create")
	}
	var pl CollectionResponsePayload
	if err := json.Unmarshal(rec.Payload, &pl); err != nil {
		return false, problem.Validation.WithField("payload", "phải là JSON hợp lệ")
	}
	ref, err := uuid.Parse(strings.TrimSpace(pl.StatementRef))
	if err != nil {
		return false, problem.Validation.WithField("statement_ref", "phải là UUID")
	}
	id, at := rec.ID, rec.EventTime
	res, err := snk.Svc.RespondToStatement(ctx, RespondInput{StatementRef: ref, Revision: pl.Revision, Outcome: pl.Outcome,
		HTXRolls: pl.HTXRolls, Reason: pl.Reason, ID: &id, EventTime: &at, DeviceID: device, IdemKey: rec.IdemKey})
	if err != nil {
		return false, err
	}
	return res.Replayed, nil
}

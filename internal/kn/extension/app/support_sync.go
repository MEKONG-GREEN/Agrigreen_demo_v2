package app

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/store"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// SupportPayload là payload của bản ghi đồng bộ support_request (chiều lên, op create)
// và thân POST /v1/support-requests (docs/dev/sync-contract.md). Toạ độ và độ chính
// xác nhận chuỗi decimal hoặc số JSON.
type SupportPayload struct {
	HouseholdRef *string          `json:"household_ref"`
	PlotRef      *string          `json:"plot_ref"`
	Category     string           `json:"category"`
	Urgency      string           `json:"urgency"`
	Description  string           `json:"description"`
	PhotoRefs    []string         `json:"photo_refs"`
	Location     *PointPayload    `json:"location"`
	GPSAccuracyM *decimal.Decimal `json:"gps_accuracy_m"`
}

// PointPayload là toạ độ WGS84.
type PointPayload struct {
	Lat decimal.Decimal `json:"lat"`
	Lon decimal.Decimal `json:"lon"`
}

func optUUID(s *string, field string) (uuid.UUID, error) {
	if s == nil || *s == "" {
		return uuid.Nil, nil
	}
	u, err := uuid.Parse(*s)
	if err != nil || u == uuid.Nil {
		return uuid.Nil, problem.Validation.WithField(field, "phải là UUID")
	}
	return u, nil
}

// ToInput chuyển payload thành SupportInput (chưa có ID, khóa idem, thời điểm).
func (b SupportPayload) ToInput() (SupportInput, error) {
	in := SupportInput{Category: b.Category, Urgency: b.Urgency, Description: b.Description, GPSAccuracyM: b.GPSAccuracyM}
	var err error
	if in.HouseholdRef, err = optUUID(b.HouseholdRef, "household_ref"); err != nil {
		return in, err
	}
	if in.PlotRef, err = optUUID(b.PlotRef, "plot_ref"); err != nil {
		return in, err
	}
	if len(b.PhotoRefs) > domain.MaxSupportPhotos {
		return in, problem.Validation.WithField("photo_refs", domain.ErrSupportPhotos.Error())
	}
	for _, s := range b.PhotoRefs {
		u, err := uuid.Parse(s)
		if err != nil {
			return in, problem.Validation.WithField("photo_refs", "mỗi phần tử phải là mã tệp (UUID)")
		}
		in.PhotoRefs = append(in.PhotoRefs, u)
	}
	if b.Location != nil {
		in.Location = &domain.Point{Lat: b.Location.Lat, Lon: b.Location.Lon}
	}
	return in, nil
}

// supportData là dữ liệu yêu cầu gửi xuống máy của nông dân (chỉ hộ của mình).
type supportData struct {
	ID            string   `json:"id"`
	HouseholdRef  string   `json:"household_ref"`
	PlotRef       *string  `json:"plot_ref"`
	Category      string   `json:"category"`
	Urgency       string   `json:"urgency"`
	Description   string   `json:"description"`
	PhotoRefs     []string `json:"photo_refs"`
	GPSMissing    bool     `json:"gps_missing"`
	Labels        []string `json:"labels"`
	EventTime     string   `json:"event_time"`
	ReceivedAt    string   `json:"received_at"`
	TaskID        string   `json:"task_id"`
	TaskStatus    string   `json:"task_status"`
	Assigned      bool     `json:"assigned"`
	LastNote      string   `json:"last_note"`
	TaskCreatedAt string   `json:"task_created_at"`
}

// SupportData dựng dữ liệu hiển thị của một yêu cầu (dùng chung cho API và pull).
func SupportData(r domain.SupportRequest) json.RawMessage {
	photos := make([]string, 0, len(r.PhotoRefs))
	for _, p := range r.PhotoRefs {
		photos = append(photos, p.String())
	}
	labels := []string{}
	if r.GPSMissing() {
		labels = append(labels, domain.LabelGPSMissing)
	}
	b, _ := json.Marshal(supportData{
		ID: r.ID.String(), HouseholdRef: r.HouseholdRef.String(), PlotRef: refStr(r.PlotRef), Category: r.Category, Urgency: r.Urgency,
		Description: r.Description, PhotoRefs: photos, GPSMissing: r.GPSMissing(), Labels: labels,
		EventTime: r.EventTime.UTC().Format(time.RFC3339), ReceivedAt: r.ReceivedAt.UTC().Format(time.RFC3339),
		TaskID: r.TaskID.String(), TaskStatus: string(r.TaskStatus), Assigned: r.AssigneeRef != uuid.Nil,
		LastNote: r.LastNote, TaskCreatedAt: r.TaskCreatedAt.UTC().Format(time.RFC3339),
	})
	return b
}

// supportChanges: chiều xuống support_request — yêu cầu của các hộ liên kết với nông
// dân gọi; cursor = sync_seq của nhiệm vụ (đổi khi cán bộ tiếp nhận, xử lý, đóng).
// Người gọi không phải nông dân → không có thay đổi (không lỗi).
func (src SyncSource) supportChanges(ctx context.Context, after int64, limit int) ([]syncport.Change, int64, error) {
	p, ok := authz.FromContext(ctx)
	if !ok || !p.Has(accport.RoleFarmer) || src.Svc.Farmers == nil {
		return []syncport.Change{}, after, nil
	}
	links, err := src.Svc.Farmers.HouseholdsForSubject(tenant.WithID(ctx, p.TenantID), p.ActorRef)
	if err != nil || len(links) == 0 {
		return []syncport.Change{}, after, err
	}
	hh := make([]uuid.UUID, 0, len(links))
	for _, l := range links {
		hh = append(hh, l.HouseholdRef)
	}
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	var rows []domain.SupportRequest
	err = tenant.WithTenantTx(ctx, src.Svc.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		rows, e = store.SupportChangesForHouseholds(ctx, tx, hh, after, limit)
		return e
	})
	if err != nil {
		return nil, after, err
	}
	out := make([]syncport.Change, 0, len(rows))
	last := after
	for _, r := range rows {
		out = append(out, syncport.Change{Entity: port.EntitySupportRequest, ID: r.ID, Version: r.TaskSeq, Op: syncport.OpUpsert, Data: SupportData(r)})
		last = r.TaskSeq
	}
	return out, last, nil
}

// acceptSupport nhận bản ghi support_request gửi ngoại tuyến (HTX US-SUPPORT-01 AC-2).
func (snk SyncSink) acceptSupport(ctx context.Context, rec syncport.Record, res syncport.Result, device *string, evTime *time.Time) syncport.Result {
	if rec.Op != syncport.OpCreate {
		return reject(res, problem.Validation.WithField("op", "support_request chỉ nhận op = create"))
	}
	var body SupportPayload
	if err := json.Unmarshal(rec.Payload, &body); err != nil {
		return reject(res, problem.Validation.WithField("payload", "JSON không hợp lệ"))
	}
	in, err := body.ToInput()
	if err != nil {
		return reject(res, err)
	}
	id := rec.ID
	in.ID, in.IdemKey, in.DeviceID, in.EventTime = &id, rec.IdemKey, device, evTime
	out, err := snk.Svc.SubmitSupport(ctx, in)
	if err != nil {
		return reject(res, err)
	}
	res.Status = syncport.StatusAccepted
	if out.Replayed {
		res.Status = syncport.StatusDuplicate
	}
	return res
}

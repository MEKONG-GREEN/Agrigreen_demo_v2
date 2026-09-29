package app

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/store"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// SyncSource cung cấp chiều xuống "notification": hộp thư của người gọi trong
// SyncWindow, để máy ngoại tuyến có hộp thư (docs/dev/sync-contract.md).
// version = số đồng bộ lớn nhất của thông báo hoặc biên nhận đã đọc; đánh dấu đã
// đọc ở web làm thông báo đi xuống máy lần nữa với read_at.
type SyncSource struct{ Svc *Service }

var _ syncport.FarmerSource = SyncSource{}

// FarmerEntities thực hiện syncport.FarmerSource.
func (SyncSource) FarmerEntities() []string { return []string{port.EntityNotification} }

// FarmerChanges thực hiện syncport.FarmerSource: nông dân CHỈ nhận thông báo cá nhân gửi
// tới chính mình (kết quả duyệt nhật ký, cảnh báo khẩn theo thửa — HTX US-LOG-05,
// US-NEWS-03); thông báo chủ đề theo vai/đơn vị/toàn quốc là của cán bộ.
func (src SyncSource) FarmerChanges(ctx context.Context, _ syncport.FarmerScope, entity string, after int64, limit int) ([]syncport.Change, int64, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, after, err
	}
	return src.changes(ctx, p, domain.Viewer{ActorRef: p.ActorRef, Roles: p.Roles}, entity, after, limit)
}

// Entities thực hiện syncport.Source.
func (SyncSource) Entities() []string { return []string{port.EntityNotification} }

// NotificationData là data của một thay đổi "notification".
type NotificationData struct {
	ID         string  `json:"id"`
	Kind       string  `json:"kind"`
	Severity   string  `json:"severity"`
	Title      string  `json:"title"`
	Body       string  `json:"body"`
	DeepLink   string  `json:"deep_link"`
	SubjectRef *string `json:"subject_ref"`
	CreatedAt  string  `json:"created_at"`
	ReadAt     *string `json:"read_at"`
}

// ToData chuyển thông báo sang dạng API/đồng bộ.
func ToData(n domain.Notification) NotificationData {
	d := NotificationData{ID: n.ID.String(), Kind: n.Kind, Severity: n.Severity, Title: n.Title, Body: n.Body,
		DeepLink: n.DeepLink, CreatedAt: n.CreatedAt.UTC().Format(time.RFC3339)}
	if n.SubjectRef != nil {
		s := n.SubjectRef.String()
		d.SubjectRef = &s
	}
	if n.ReadAt != nil {
		s := n.ReadAt.UTC().Format(time.RFC3339)
		d.ReadAt = &s
	}
	return d
}

// Changes thực hiện syncport.Source.
func (src SyncSource) Changes(ctx context.Context, scope authz.Scope, entity string, after int64, limit int) ([]syncport.Change, int64, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, after, err
	}
	return src.changes(ctx, p, domain.Viewer{ActorRef: p.ActorRef, OrgRef: p.OrgRef, Roles: p.Roles, Scope: scope}, entity, after, limit)
}

func (src SyncSource) changes(ctx context.Context, p authz.Principal, v domain.Viewer, entity string, after int64, limit int) ([]syncport.Change, int64, error) {
	s := src.Svc
	var items []domain.Notification
	err := tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		items, e = store.SyncChanges(ctx, tx, v, s.now().Add(-SyncWindow), after, limit)
		return e
	})
	if err != nil {
		return nil, after, err
	}
	out := make([]syncport.Change, 0, len(items))
	last := after
	for _, n := range items {
		raw, err := json.Marshal(ToData(n))
		if err != nil {
			return nil, after, err
		}
		out = append(out, syncport.Change{Entity: entity, ID: n.ID, Version: n.SyncSeq, Op: syncport.OpUpsert, Data: raw})
		if n.SyncSeq > last {
			last = n.SyncSeq
		}
	}
	return out, last, nil
}

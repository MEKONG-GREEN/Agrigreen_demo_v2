package app

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/content/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/content/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/content/store"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Sync thực hiện sync/port.Source cho entity "document": tài liệu đã xuất bản
// trong phạm vi để đọc ngoại tuyến (KN US-LIB-05, HTX US-NEWS-02). Gỡ →
// tombstone; máy vẫn giữ bản đã tải và gắn nhãn "đã gỡ" (US-NEWS-02 AC-2).
//
// Cursor là sync_seq (sequence pf_content.document_sync_seq) — tăng mỗi khi bản
// hiện hành đổi. Phạm vi áp dụng của tài liệu cố định, nên tài liệu không tự
// ra khỏi phạm vi; khi phạm vi của NGƯỜI GỌI đổi, pf/sync yêu cầu làm mới toàn bộ.
type Sync struct{ Svc *Service }

var _ syncport.Source = Sync{}

// Entities thực hiện syncport.Source.
func (Sync) Entities() []string { return []string{port.SyncEntityDocument, port.SyncEntityLibrary} }

// SyncAttachment là siêu dữ liệu tệp trong payload đồng bộ; máy tải tệp qua
// GET /v1/documents/{ref} (URL có hạn), không kèm URL ở đây.
type SyncAttachment struct {
	ID          string `json:"id"`
	FileName    string `json:"file_name"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
	Role        string `json:"role"`
}

// SyncDocument là payload gọn của một tài liệu trên máy.
type SyncDocument struct {
	Ref           string           `json:"ref"`
	Kind          string           `json:"kind"`
	AudienceLevel string           `json:"audience_level"`
	AudienceUnits []string         `json:"audience_units"`
	ProvinceCode  *string          `json:"province_code"`
	ParentRef     *string          `json:"parent_ref"`
	Category      *string          `json:"category"`
	Tags          []string         `json:"tags"`
	VersionNo     int              `json:"version_no"`
	Title         string           `json:"title"`
	Summary       string           `json:"summary"`
	Body          string           `json:"body"`
	BodyFormat    string           `json:"body_format"`
	SearchText    string           `json:"search_text"` // đã chuẩn hóa không dấu cho tìm kiếm ngoại tuyến
	PublishedAt   string           `json:"published_at"`
	Attachments   []SyncAttachment `json:"attachments"`
}

// Changes thực hiện syncport.Source.
func (s Sync) Changes(ctx context.Context, scope authz.Scope, entity string, after int64, limit int) ([]syncport.Change, int64, error) {
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	if entity == port.SyncEntityLibrary {
		return s.libraryChanges(ctx, scope, after, limit)
	}
	if entity != port.SyncEntityDocument {
		return nil, after, fmt.Errorf("content: entity %q không thuộc module", entity)
	}
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	vis, err := s.Svc.visibility(ctx, scope)
	if err != nil {
		return nil, after, err
	}
	var rows []store.SyncRow
	err = tenant.FromCtxTx(ctx, s.Svc.DB, func(tx pgx.Tx) error {
		var e error
		rows, e = store.SyncChanges(ctx, tx, vis, after, limit)
		return e
	})
	if err != nil {
		return nil, after, err
	}
	last := after
	out := make([]syncport.Change, 0, len(rows))
	for _, r := range rows {
		seq := *r.Doc.SyncSeq
		last = seq
		ch := syncport.Change{Entity: port.SyncEntityDocument, ID: r.Doc.Ref, Version: seq, Op: syncport.OpTombstone}
		if r.Doc.Status == domain.StatusPublished && r.Version != nil {
			data, err := json.Marshal(syncPayload(r.Doc, *r.Version))
			if err != nil {
				return nil, after, err
			}
			ch.Op, ch.Data = syncport.OpUpsert, data
		}
		out = append(out, ch)
	}
	return out, last, nil
}

func syncPayload(d domain.Document, v domain.Version) SyncDocument {
	p := SyncDocument{
		Ref: d.Ref.String(), Kind: string(d.Kind), AudienceLevel: string(d.Audience.Level), AudienceUnits: d.Audience.Units,
		ProvinceCode: strOrNil(d.Audience.ProvinceCode), Category: strOrNil(v.Content.Category), Tags: v.Content.Tags,
		VersionNo: v.VersionNo, Title: v.Content.Title, Summary: v.Content.Summary, Body: v.Content.Body,
		BodyFormat: string(v.Content.BodyFormat), SearchText: domain.SearchText(v.Content),
		Attachments: make([]SyncAttachment, 0, len(v.Content.Attachments)),
	}
	if d.ParentRef != nil {
		s := d.ParentRef.String()
		p.ParentRef = &s
	}
	if d.PublishedAt != nil {
		p.PublishedAt = d.PublishedAt.UTC().Format(time.RFC3339Nano)
	}
	for _, a := range v.Content.Attachments {
		p.Attachments = append(p.Attachments, SyncAttachment{ID: a.ID.String(), FileName: a.FileName, ContentType: a.ContentType, Size: a.Size, SHA256: a.SHA256, Role: a.Role})
	}
	return p
}

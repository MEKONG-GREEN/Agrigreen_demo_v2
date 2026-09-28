package app

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/store"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func tsPtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := ts(*t)
	return &s
}

// FieldDoc là định nghĩa một trường trong CatalogDoc.
type FieldDoc struct {
	Key        string   `json:"key"`
	LabelVi    string   `json:"label_vi"`
	DataType   string   `json:"data_type"`
	Unit       *string  `json:"unit"`
	Required   bool     `json:"required"`
	Min        *string  `json:"min"`
	Max        *string  `json:"max"`
	MaxScale   *int     `json:"max_scale"`
	EnumValues []string `json:"enum_values"`
	HelpVi     *string  `json:"help_vi"`
	Position   int      `json:"position"`
	Reserved   bool     `json:"reserved"` // giá trị lấy từ cột riêng (plot_ref, season_code, gps, file_refs)
}

// CatalogDoc là dạng JSON của một phiên bản danh mục trường: thân của
// GET /v1/mrv/catalog, Admin API và payload entity đồng bộ mrv_catalog.
type CatalogDoc struct {
	ID           string     `json:"id"`
	EvidenceType string     `json:"evidence_type"`
	Version      int        `json:"version"`
	Status       string     `json:"status"`
	Provisional  bool       `json:"provisional"`
	SourceNote   string     `json:"source_note"`
	Fields       []FieldDoc `json:"fields"`
	SetterRef    string     `json:"setter_ref"`
	ApproverRef  *string    `json:"approver_ref"`
	SubmittedAt  *string    `json:"submitted_at"`
	DecidedAt    *string    `json:"decided_at"`
	DecisionNote *string    `json:"decision_note"`
	CreatedAt    string     `json:"created_at"`
}

// CatalogDocOf dựng CatalogDoc từ một phiên bản danh mục.
func CatalogDocOf(v domain.CatalogVersion) CatalogDoc {
	d := CatalogDoc{
		ID: v.ID.String(), EvidenceType: v.EvidenceType, Version: v.Version, Status: string(v.Status), Provisional: v.Provisional,
		SourceNote: v.SourceNote, Fields: make([]FieldDoc, 0, len(v.Fields)), SetterRef: v.SetterRef.String(),
		SubmittedAt: tsPtr(v.SubmittedAt), DecidedAt: tsPtr(v.DecidedAt), DecisionNote: v.DecisionNote, CreatedAt: ts(v.CreatedAt),
	}
	if v.ApproverRef != nil {
		s := v.ApproverRef.String()
		d.ApproverRef = &s
	}
	for _, f := range v.Fields {
		fd := FieldDoc{Key: f.Key, LabelVi: f.LabelVi, DataType: string(f.Type), Required: f.Required, MaxScale: f.MaxScale,
			EnumValues: f.EnumValues, Position: f.Position, Reserved: domain.IsReserved(f.Key)}
		if fd.EnumValues == nil {
			fd.EnumValues = []string{}
		}
		if f.Unit != "" {
			u := f.Unit
			fd.Unit = &u
		}
		if f.HelpVi != "" {
			h := f.HelpVi
			fd.HelpVi = &h
		}
		if f.Min != nil {
			s := f.Min.String()
			fd.Min = &s
		}
		if f.Max != nil {
			s := f.Max.String()
			fd.Max = &s
		}
		d.Fields = append(d.Fields, fd)
	}
	return d
}

// catalogReaders đọc được danh mục đã duyệt: người thu/duyệt/xem/xuất minh chứng
// và người quản trị danh mục.
func catalogReaders() []string { return append(slices.Clone(evidenceReaders), catalogAdminRoles...) }

// EvidenceType là một loại minh chứng đang dùng được: có ít nhất một phiên bản
// danh mục đã duyệt.
type EvidenceType struct {
	EvidenceType string
	CatalogID    uuid.UUID
	Version      int
	Provisional  bool
	SourceNote   string
	FieldCount   int
	DecidedAt    *time.Time
}

// EvidenceTypes liệt kê loại minh chứng có danh mục đã duyệt, kèm phiên bản đã
// duyệt mới nhất (GET /v1/mrv/evidence-types), theo loại.
func (s *Service) EvidenceTypes(ctx context.Context) ([]EvidenceType, error) {
	p, err := authz.Require(ctx, catalogReaders()...)
	if err != nil {
		return nil, err
	}
	var rows []store.ApprovedSummary
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		rows, e = store.ApprovedLatest(ctx, tx)
		return e
	})
	if err != nil {
		return nil, err
	}
	out := make([]EvidenceType, 0, len(rows))
	for _, r := range rows {
		v := r.Version
		out = append(out, EvidenceType{
			EvidenceType: v.EvidenceType, CatalogID: v.ID, Version: v.Version, Provisional: v.Provisional,
			SourceNote: v.SourceNote, FieldCount: r.FieldCount, DecidedAt: v.DecidedAt,
		})
	}
	return out, nil
}

// CatalogSource thực hiện sync/port.Source cho entity "mrv_catalog": bản danh
// mục ĐÃ DUYỆT mới nhất của mỗi loại minh chứng, để app dựng và kiểm biểu mẫu
// khi ngoại tuyến.
//
//   - id = port.CatalogSyncID(loại) — cố định theo loại; version = số phiên bản
//     danh mục; data = CatalogDoc (như GET /v1/mrv/catalog).
//   - Cursor = thời điểm duyệt (micro giây Unix). Bản đã duyệt bất biến, nên
//     không cần cột sync_seq; phiên bản mới được duyệt → thay đổi mới cùng id.
//   - Không có tombstone (danh mục đã duyệt không bị thu hồi ở G1).
//   - Người gọi không có vai đọc danh mục → không nhận entity này; entity khác
//     vẫn đồng bộ.
type CatalogSource struct{ Svc *Service }

var _ syncport.Source = CatalogSource{}

// Entities thực hiện syncport.Source.
func (CatalogSource) Entities() []string { return []string{port.SyncEntityCatalog} }

// Changes thực hiện syncport.Source. Các loại có cùng thời điểm duyệt luôn nằm
// trọn trong một trang (có thể vượt limit vài dòng) để con trỏ không bỏ sót.
func (src CatalogSource) Changes(ctx context.Context, _ authz.Scope, entity string, after int64, limit int) ([]syncport.Change, int64, error) {
	if entity != port.SyncEntityCatalog {
		return nil, after, fmt.Errorf("mrv: entity %q không thuộc module", entity)
	}
	p, ok := authz.FromContext(ctx)
	if !ok || !slices.ContainsFunc(catalogReaders(), p.Has) {
		return nil, after, nil
	}
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	var rows []store.ApprovedSyncRow
	err := src.Svc.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		rows, e = store.ApprovedSince(ctx, tx, after)
		return e
	})
	if err != nil {
		return nil, after, err
	}
	last := after
	out := make([]syncport.Change, 0, min(len(rows), limit))
	for i, r := range rows {
		if i >= limit && r.Cursor != last {
			break
		}
		data, err := json.Marshal(CatalogDocOf(r.Version))
		if err != nil {
			return nil, after, err
		}
		out = append(out, syncport.Change{
			Entity: port.SyncEntityCatalog, ID: port.CatalogSyncID(r.Version.EvidenceType),
			Version: int64(r.Version.Version), Op: syncport.OpUpsert, Data: data,
		})
		last = r.Cursor
	}
	return out, last, nil
}

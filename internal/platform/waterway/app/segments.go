package app

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/waterway/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/waterway/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/waterway/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// SegmentFields là nội dung một bản sửa đoạn (GIS US-WATER-02/03). Chiều dài
// KHÔNG nhận từ client — CSDL tính từ hình học.
type SegmentFields struct {
	Name        string
	Class       string
	Status      string // mặc định draft
	Points      []domain.Point
	WidthM      *decimal.Decimal
	DepthM      *decimal.Decimal
	MaxLoadTons *decimal.Decimal
	DataSource  string // mặc định digitised
	Note        *string
	EventTime   *time.Time
}

// Revision là yêu cầu sửa: thay toàn bộ nội dung, ghi bản ghi bù có lý do (RB-1).
type Revision struct {
	ExpectedRevision int
	Reason           string
}

func (s *Service) segmentFrom(p authz.Principal, f SegmentFields) domain.Segment {
	seg := domain.Segment{
		Name: f.Name, Class: f.Class, Status: f.Status, Points: f.Points, WidthM: f.WidthM, DepthM: f.DepthM,
		MaxLoadTons: f.MaxLoadTons, DataSource: f.DataSource, Note: f.Note, ActorRef: p.ActorRef, EventTime: s.now(),
	}
	if seg.Status == "" {
		seg.Status = domain.StatusDraft
	}
	if seg.DataSource == "" {
		seg.DataSource = domain.SourceDigitised
	}
	if f.EventTime != nil {
		seg.EventTime = f.EventTime.UTC()
	}
	return seg
}

func checkEventTime(t time.Time, now time.Time) []problem.FieldError {
	if t.After(now.Add(5 * time.Minute)) {
		return []problem.FieldError{{Field: "event_time", Reason: "không được ở tương lai"}}
	}
	return nil
}

// CreateSegment vẽ mới một tuyến sông/kênh (kể cả nhánh rẽ) — GIS US-WATER-02, BR-39.
func (s *Service) CreateSegment(ctx context.Context, f SegmentFields) (domain.Segment, error) {
	p, err := authz.Require(ctx, port.RoleEditor)
	if err != nil {
		return domain.Segment{}, err
	}
	seg := s.segmentFrom(p, f)
	if err := validation(append(domain.ValidateSegment(seg), checkEventTime(seg.EventTime, s.now())...)); err != nil {
		return domain.Segment{}, err
	}
	seg.ID, seg.SegmentRef, seg.Revision = ids.New(), ids.New(), 1
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error { return store.InsertSegment(ctx, tx, p.TenantID, &seg) })
	return seg, geometryProblem(err)
}

// ReviseSegment sửa tuyến đã có (kéo/thêm/xóa điểm, đổi thuộc tính, xác nhận,
// ngừng dùng) bằng một bản sửa mới trỏ về bản trước — GIS US-WATER-03, BR-40.
// Bản sửa phải dựa trên bản hiện hành (expected_revision), không rẽ nhánh.
func (s *Service) ReviseSegment(ctx context.Context, ref uuid.UUID, f SegmentFields, r Revision) (domain.Segment, error) {
	p, err := authz.Require(ctx, port.RoleEditor)
	if err != nil {
		return domain.Segment{}, err
	}
	seg := s.segmentFrom(p, f)
	errs := append(domain.ValidateSegment(seg), checkEventTime(seg.EventTime, s.now())...)
	if !domain.ValidReason(r.Reason) {
		errs = append(errs, problem.FieldError{Field: "reason", Reason: "bắt buộc, 5–500 ký tự"})
	}
	if r.ExpectedRevision <= 0 {
		errs = append(errs, problem.FieldError{Field: "expected_revision", Reason: "bắt buộc (số sửa hiện hành)"})
	}
	if err := validation(errs); err != nil {
		return domain.Segment{}, err
	}
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if err := store.LockRef(ctx, tx, ref); err != nil {
			return err
		}
		head, err := store.SegmentHead(ctx, tx, ref)
		if err != nil {
			return err
		}
		if head == nil {
			return ErrSegmentNotFound
		}
		if head.Revision != r.ExpectedRevision {
			return ErrRevisionConflict.WithDetail("bản hiện hành là số sửa " + itoa(head.Revision))
		}
		reason := r.Reason
		seg.ID, seg.SegmentRef, seg.Revision = ids.New(), ref, head.Revision+1
		seg.SupersedesID, seg.CorrectionReason = &head.ID, &reason
		return store.InsertSegment(ctx, tx, p.TenantID, &seg)
	})
	if errors.Is(err, ledger.ErrDuplicate) {
		return domain.Segment{}, ErrRevisionConflict
	}
	return seg, geometryProblem(err)
}

// SegmentDetail là bản hiện hành cùng lịch sử sửa.
type SegmentDetail struct {
	Head    domain.Segment
	History []domain.Segment
}

// Segment đọc một đoạn (Admin API, kể cả nháp).
func (s *Service) Segment(ctx context.Context, ref uuid.UUID) (SegmentDetail, error) {
	p, err := authz.Require(ctx, adminReaders...)
	if err != nil {
		return SegmentDetail{}, err
	}
	var out SegmentDetail
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		h, err := store.SegmentHistory(ctx, tx, ref)
		if err != nil {
			return err
		}
		if len(h) == 0 {
			return ErrSegmentNotFound
		}
		out = SegmentDetail{Head: h[len(h)-1], History: h}
		return nil
	})
	return out, err
}

// Page là một trang kết quả phân trang cursor (R-53).
type Page[T any] struct {
	Items []T
	Next  *string
}

// Segments liệt kê bản hiện hành của các đoạn (Admin API, kể cả nháp).
func (s *Service) Segments(ctx context.Context, status string, after uuid.UUID, limit int) (Page[domain.Segment], error) {
	p, err := authz.Require(ctx, adminReaders...)
	if err != nil {
		return Page[domain.Segment]{}, err
	}
	switch status {
	case "", domain.StatusDraft, domain.StatusConfirmed, domain.StatusRetired:
	default:
		return Page[domain.Segment]{}, problem.Validation.WithField("status", "phải là draft, confirmed hoặc retired")
	}
	limit = ClampLimit(limit)
	var items []domain.Segment
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		items, e = store.ListSegmentHeads(ctx, tx, store.SegmentFilter{Status: status, After: after, Limit: limit + 1})
		return e
	})
	return pageUUID(items, limit, func(x domain.Segment) uuid.UUID { return x.SegmentRef }), err
}

func pageUUID[T any](items []T, limit int, id func(T) uuid.UUID) Page[T] {
	var pg Page[T]
	if len(items) > limit {
		items = items[:limit]
		c := EncodeUUIDCursor(id(items[limit-1]))
		pg.Next = &c
	}
	pg.Items = items
	return pg
}

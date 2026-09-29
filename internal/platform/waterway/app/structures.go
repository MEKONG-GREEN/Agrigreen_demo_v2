package app

import (
	"context"
	"errors"
	"fmt"
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

// StructureFields là nội dung một bản sửa công trình vượt sông.
type StructureFields struct {
	Kind             string
	Name             string
	Status           string // mặc định active
	SegmentRef       uuid.UUID
	At               domain.Point
	ClearanceHeightM *decimal.Decimal
	ClearanceWidthM  *decimal.Decimal
	DepthM           *decimal.Decimal
	SurveyDate       *time.Time
	DataSource       string // mặc định survey
	Note             *string
	EventTime        *time.Time
}

func (s *Service) structureFrom(p authz.Principal, f StructureFields) domain.Structure {
	st := domain.Structure{
		Kind: f.Kind, Name: f.Name, Status: f.Status, SegmentRef: f.SegmentRef, At: f.At, ClearanceHeightM: f.ClearanceHeightM,
		ClearanceWidthM: f.ClearanceWidthM, DepthM: f.DepthM, SurveyDate: f.SurveyDate, DataSource: f.DataSource, Note: f.Note,
		ActorRef: p.ActorRef, EventTime: s.now(),
	}
	if st.Status == "" {
		st.Status = domain.StructureActive
	}
	if st.DataSource == "" {
		st.DataSource = domain.SourceSurvey
	}
	if f.EventTime != nil {
		st.EventTime = f.EventTime.UTC()
	}
	return st
}

// checkOnSegment: công trình phải nằm trên tuyến (≤ waterway.structure_max_offset_m) —
// điểm cách tuyến hàng km là dữ liệu sai chỗ, không ràng buộc được tải trọng.
func (s *Service) checkOnSegment(ctx context.Context, tx pgx.Tx, st domain.Structure) error {
	seg, err := store.SegmentHead(ctx, tx, st.SegmentRef)
	if err != nil {
		return err
	}
	if seg == nil {
		return problem.Validation.WithField("segment_ref", "không có đoạn với mã này")
	}
	if seg.Status == domain.StatusRetired && st.Status == domain.StructureActive {
		return ErrSegmentRetired
	}
	maxOff, err := s.decParam(ctx, port.ParamStructureMaxOffset, domain.DefaultStructureMaxOffsetM, s.now())
	if err != nil {
		return err
	}
	dist, err := store.DistanceToSegmentM(ctx, tx, st.SegmentRef, st.At)
	if err != nil {
		return err
	}
	if dist > maxOff.InexactFloat64() {
		return ErrStructureOffSegment.WithDetail(fmt.Sprintf("Vị trí công trình cách tuyến %s tới %.0f m (tối đa %s m) — kiểm tra lại toạ độ.",
			seg.Name, dist, maxOff.String()))
	}
	return nil
}

// CreateStructure ghi công trình vượt sông gắn với một đoạn.
func (s *Service) CreateStructure(ctx context.Context, f StructureFields) (domain.Structure, error) {
	p, err := authz.Require(ctx, port.RoleEditor)
	if err != nil {
		return domain.Structure{}, err
	}
	st := s.structureFrom(p, f)
	if err := validation(append(domain.ValidateStructure(st), checkEventTime(st.EventTime, s.now())...)); err != nil {
		return domain.Structure{}, err
	}
	st.ID, st.StructureRef, st.Revision = ids.New(), ids.New(), 1
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if err := s.checkOnSegment(ctx, tx, st); err != nil {
			return err
		}
		return store.InsertStructure(ctx, tx, p.TenantID, &st)
	})
	return st, err
}

// ReviseStructure sửa công trình (bản ghi bù có lý do; ngừng dùng = status retired).
func (s *Service) ReviseStructure(ctx context.Context, ref uuid.UUID, f StructureFields, r Revision) (domain.Structure, error) {
	p, err := authz.Require(ctx, port.RoleEditor)
	if err != nil {
		return domain.Structure{}, err
	}
	st := s.structureFrom(p, f)
	errs := append(domain.ValidateStructure(st), checkEventTime(st.EventTime, s.now())...)
	if !domain.ValidReason(r.Reason) {
		errs = append(errs, problem.FieldError{Field: "reason", Reason: "bắt buộc, 5–500 ký tự"})
	}
	if r.ExpectedRevision <= 0 {
		errs = append(errs, problem.FieldError{Field: "expected_revision", Reason: "bắt buộc (số sửa hiện hành)"})
	}
	if err := validation(errs); err != nil {
		return domain.Structure{}, err
	}
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if err := store.LockRef(ctx, tx, ref); err != nil {
			return err
		}
		head, err := store.StructureHead(ctx, tx, ref)
		if err != nil {
			return err
		}
		if head == nil {
			return ErrStructureNotFound
		}
		if head.Revision != r.ExpectedRevision {
			return ErrRevisionConflict.WithDetail("bản hiện hành là số sửa " + itoa(head.Revision))
		}
		if err := s.checkOnSegment(ctx, tx, st); err != nil {
			return err
		}
		reason := r.Reason
		st.ID, st.StructureRef, st.Revision = ids.New(), ref, head.Revision+1
		st.SupersedesID, st.CorrectionReason = &head.ID, &reason
		return store.InsertStructure(ctx, tx, p.TenantID, &st)
	})
	if errors.Is(err, ledger.ErrDuplicate) {
		return domain.Structure{}, ErrRevisionConflict
	}
	return st, err
}

// Structures liệt kê bản hiện hành của công trình (lọc theo đoạn).
func (s *Service) Structures(ctx context.Context, segmentRef *uuid.UUID, after uuid.UUID, limit int) (Page[domain.Structure], error) {
	p, err := authz.Require(ctx, adminReaders...)
	if err != nil {
		return Page[domain.Structure]{}, err
	}
	limit = ClampLimit(limit)
	var items []domain.Structure
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		items, e = store.ListStructureHeads(ctx, tx, store.StructureFilter{SegmentRef: segmentRef, After: after, Limit: limit + 1})
		return e
	})
	return pageUUID(items, limit, func(x domain.Structure) uuid.UUID { return x.StructureRef }), err
}

package app

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/khtc/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/khtc/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

func newID() uuid.UUID { return ids.New() }

// ProgramView là bản hiện hành (hoặc một phiên bản) của CT/DA.
type ProgramView struct {
	store.ProgramRow
	Actor Actor
}

func (s *Service) progViews(ctx context.Context, rs []store.ProgramRow) []ProgramView {
	refs := make([]uuid.UUID, 0, len(rs))
	for _, r := range rs {
		refs = append(refs, r.ActorRef)
	}
	act := s.actors(ctx, refs)
	out := make([]ProgramView, 0, len(rs))
	for _, r := range rs {
		out = append(out, ProgramView{ProgramRow: r, Actor: act(r.ActorRef)})
	}
	return out
}

// ListPrograms trả danh sách CT/DA chủ (bản hiện hành) theo mã.
func (s *Service) ListPrograms(ctx context.Context, kind string, includeRetired bool) ([]ProgramView, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return nil, err
	}
	var rs []store.ProgramRow
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		rs, e = store.ProgramsCurrent(ctx, tx)
		return e
	}); err != nil {
		return nil, err
	}
	return s.progViews(ctx, filterPrograms(rs, kind, includeRetired)), nil
}

func filterPrograms(rs []store.ProgramRow, kind string, includeRetired bool) []store.ProgramRow {
	out := []store.ProgramRow{}
	for _, r := range rs {
		if (kind == "" || r.Kind == kind) && (includeRetired || r.Status == domain.StatusActive) {
			out = append(out, r)
		}
	}
	return out
}

// ProgramInput là nội dung thêm/sửa CT/DA.
type ProgramInput struct {
	Ref     uuid.UUID // Nil khi thêm
	Program domain.Program
	IdemKey string
}

func (s *Service) writeProgram(ctx context.Context, in ProgramInput) (ProgramView, bool, error) {
	c, err := s.planWriter(ctx)
	if err != nil {
		return ProgramView{}, false, err
	}
	if in.IdemKey == "" {
		return ProgramView{}, false, problem.IdempotencyRequired
	}
	p := in.Program
	if es := domain.NormalizeProgram(&p); len(es) > 0 {
		return ProgramView{}, false, fieldsProblem(problem.Validation, es)
	}
	replay := func() (*ProgramView, error) {
		var r *store.ProgramRow
		err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
			var e error
			r, e = store.ProgramByIdem(ctx, tx, in.IdemKey)
			return e
		})
		if err != nil || r == nil {
			return nil, err
		}
		v := s.progViews(ctx, []store.ProgramRow{*r})[0]
		return &v, nil
	}
	if v, err := replay(); err != nil || v != nil {
		if err != nil {
			return ProgramView{}, false, err
		}
		return *v, true, nil
	}
	ref := in.Ref
	create := ref == uuid.Nil
	if create {
		ref = newID()
	}
	row := store.ProgramRow{Ref: ref, Program: p}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if err := store.Lock(ctx, tx, "program-code"); err != nil {
			return err
		}
		if !create {
			h, err := store.ProgramHistory(ctx, tx, ref)
			if err != nil {
				return err
			}
			if len(h) == 0 {
				return ErrProgramNotFound
			}
		}
		taken, err := store.ProgramCodeTaken(ctx, tx, p.Code, ref)
		if err != nil {
			return err
		}
		if taken {
			return ErrProgramExists.WithField("code", "mã đã thuộc CT/DA khác")
		}
		if p.FieldCode != "" {
			cur, err := store.CatalogCurrent(ctx, tx, domain.KindField)
			if err != nil {
				return err
			}
			if !activeCodes(cur)[domain.KindField][p.FieldCode] {
				return problem.Validation.WithField("field_code", "lĩnh vực không có hoặc đã ngừng trong Danh mục KHTC")
			}
		}
		row.Meta = c.meta(newID(), s.now(), in.IdemKey)
		return store.InsertProgram(ctx, tx, c.p.TenantID, &row)
	})
	if dup(err) {
		if v, rerr := replay(); rerr == nil && v != nil {
			return *v, true, nil
		}
	}
	if err != nil {
		return ProgramView{}, false, err
	}
	return s.progViews(ctx, []store.ProgramRow{row})[0], false, nil
}

// CreateProgram thêm CT/DA vào danh sách chủ (FN-TI-02.02 B7 "Thêm chương trình").
func (s *Service) CreateProgram(ctx context.Context, in ProgramInput) (ProgramView, bool, error) {
	in.Ref = uuid.Nil
	return s.writeProgram(ctx, in)
}

// ReviseProgram ghi phiên bản mới của CT/DA (sửa, ngừng = status retired). CT/DA đã có số liệu chỉ ngừng, không xóa —
// sổ không có thao tác xóa.
func (s *Service) ReviseProgram(ctx context.Context, in ProgramInput) (ProgramView, bool, error) {
	if in.Ref == uuid.Nil {
		return ProgramView{}, false, ErrProgramNotFound
	}
	return s.writeProgram(ctx, in)
}

// ProgramHistory trả mọi phiên bản của CT/DA, mới nhất trước.
func (s *Service) ProgramHistory(ctx context.Context, ref uuid.UUID) ([]ProgramView, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return nil, err
	}
	var rs []store.ProgramRow
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		rs, e = store.ProgramHistory(ctx, tx, ref)
		return e
	}); err != nil {
		return nil, err
	}
	if len(rs) == 0 {
		return nil, ErrProgramNotFound
	}
	return s.progViews(ctx, rs), nil
}

// ---------------------------------------------------------------------------
// Cổng cho module khác (D20) — không qua ngữ cảnh phiên KN; tenant lấy từ ctx.
// ---------------------------------------------------------------------------

// Ports hiện thực port.KHTCPrograms và port.KHTCPlans.
type Ports struct{ Svc *Service }

var (
	_ port.KHTCPrograms = Ports{}
	_ port.KHTCPlans    = Ports{}
)

func toPort(r store.ProgramRow) port.KHTCProgram {
	return port.KHTCProgram{Ref: r.Ref, Code: r.Code, Name: r.Name, Kind: r.Kind, FieldCode: r.FieldCode,
		OwnerDepartment: r.OwnerDepartment, StartYear: r.StartYear, EndYear: r.EndYear, Active: r.Status == domain.StatusActive,
		Version: int(r.Seq)}
}

func (p Ports) tx(ctx context.Context, fn func(pgx.Tx) error) error {
	tid, err := tenant.FromContext(ctx)
	if err != nil {
		return err
	}
	return p.Svc.tx(ctx, tid, fn)
}

// Program trả bản hiện hành của CT/DA.
func (p Ports) Program(ctx context.Context, ref uuid.UUID) (port.KHTCProgram, error) {
	var rs []store.ProgramRow
	if err := p.tx(ctx, func(tx pgx.Tx) error {
		var e error
		rs, e = store.ProgramHistory(ctx, tx, ref)
		return e
	}); err != nil {
		return port.KHTCProgram{}, err
	}
	if len(rs) == 0 {
		return port.KHTCProgram{}, port.ErrKHTCProgramNotFound
	}
	return toPort(rs[0]), nil
}

// Programs trả danh sách CT/DA theo bộ lọc.
func (p Ports) Programs(ctx context.Context, f port.KHTCProgramFilter) ([]port.KHTCProgram, error) {
	var rs []store.ProgramRow
	if err := p.tx(ctx, func(tx pgx.Tx) error {
		var e error
		rs, e = store.ProgramsCurrent(ctx, tx)
		return e
	}); err != nil {
		return nil, err
	}
	out := []port.KHTCProgram{}
	for _, r := range filterPrograms(rs, f.Kind, f.IncludeRetired) {
		out = append(out, toPort(r))
	}
	return out, nil
}

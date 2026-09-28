package app

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Vai nghiệp vụ — khai ở port để lớp ngoài không phải import app.
const (
	RoleCatalogSetter   = port.RoleCatalogSetter
	RoleCatalogApprover = port.RoleCatalogApprover
	RoleCollector       = port.RoleCollector
	RoleReviewer        = port.RoleReviewer
	RoleViewer          = port.RoleViewer
	RoleExporter        = port.RoleExporter
)

// FieldInput là định nghĩa trường gửi lên (Admin API và tệp mẫu).
type FieldInput struct {
	Key        string   `json:"key"`
	LabelVi    string   `json:"label_vi"`
	DataType   string   `json:"data_type"`
	Unit       string   `json:"unit,omitempty"`
	Required   bool     `json:"required"`
	Min        *string  `json:"min,omitempty"` // decimal dạng chuỗi
	Max        *string  `json:"max,omitempty"`
	MaxScale   *int     `json:"max_scale,omitempty"`
	EnumValues []string `json:"enum_values,omitempty"`
	HelpVi     string   `json:"help_vi,omitempty"`
}

// ProposeInput là dữ liệu vào của Propose.
type ProposeInput struct {
	EvidenceType string       `json:"evidence_type"`
	Provisional  bool         `json:"provisional"`
	SourceNote   string       `json:"source_note"`
	Fields       []FieldInput `json:"fields"`
	IdemKey      string       `json:"-"`
}

func (in ProposeInput) fields() ([]domain.FieldDef, []problem.FieldError) {
	var errs []problem.FieldError
	out := make([]domain.FieldDef, 0, len(in.Fields))
	for i, f := range in.Fields {
		d := domain.FieldDef{
			Key: strings.TrimSpace(f.Key), LabelVi: strings.TrimSpace(f.LabelVi), Type: domain.DataType(f.DataType),
			Unit: strings.TrimSpace(f.Unit), Required: f.Required, MaxScale: f.MaxScale, EnumValues: f.EnumValues,
			HelpVi: strings.TrimSpace(f.HelpVi),
		}
		for name, src := range map[string]*string{"min": f.Min, "max": f.Max} {
			if src == nil {
				continue
			}
			v, err := decimal.NewFromString(*src)
			if err != nil {
				errs = append(errs, problem.FieldError{Field: fmt.Sprintf("fields[%d].%s", i, name), Reason: "phải là số thập phân dạng chuỗi"})
				continue
			}
			if name == "min" {
				d.Min = &v
			} else {
				d.Max = &v
			}
		}
		out = append(out, d)
	}
	return out, errs
}

// Propose tạo phiên bản danh mục nháp (vai param_setter). Số phiên bản tăng
// dần theo loại minh chứng. Gọi lặp cùng IdemKey trả lại phiên bản cũ.
func (s *Service) Propose(ctx context.Context, in ProposeInput) (domain.CatalogVersion, bool, error) {
	p, err := authz.Require(ctx, RoleCatalogSetter)
	if err != nil {
		return domain.CatalogVersion{}, false, err
	}
	if in.IdemKey == "" {
		return domain.CatalogVersion{}, false, problem.IdempotencyRequired
	}
	fields, ferrs := in.fields()
	v := domain.CatalogVersion{
		ID: ids.New(), TenantID: p.TenantID, EvidenceType: strings.TrimSpace(in.EvidenceType), Status: domain.CatalogDraft,
		Provisional: in.Provisional, SourceNote: strings.TrimSpace(in.SourceNote), Fields: fields, SetterRef: p.ActorRef,
		IdemKey: in.IdemKey,
	}
	if err := validation(append(ferrs, v.ValidateDefinition()...)); err != nil {
		return v, false, err
	}
	replayed := false
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.VersionByIdemKey(ctx, tx, in.IdemKey); err != nil {
			return err
		} else if prev != nil {
			v, replayed = *prev, true
			return nil
		}
		if err := store.LockEvidenceType(ctx, tx, p.TenantID, v.EvidenceType); err != nil {
			return err
		}
		n, err := store.NextVersion(ctx, tx, v.EvidenceType)
		if err != nil {
			return err
		}
		v.Version = n
		return store.InsertVersion(ctx, tx, &v, s.Clock.Now())
	})
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		var prev *domain.CatalogVersion
		if rerr := s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
			var e error
			prev, e = store.VersionByIdemKey(ctx, tx, in.IdemKey)
			return e
		}); rerr == nil && prev != nil {
			return *prev, true, nil
		}
	}
	return v, replayed, err
}

func lockedVersion(ctx context.Context, tx pgx.Tx, id uuid.UUID) (domain.CatalogVersion, error) {
	v, err := store.VersionByID(ctx, tx, id, true)
	if err != nil {
		return domain.CatalogVersion{}, err
	}
	if v == nil {
		return domain.CatalogVersion{}, ErrCatalogNotFound
	}
	return *v, nil
}

// Submit gửi duyệt một phiên bản nháp — chỉ người đặt.
func (s *Service) Submit(ctx context.Context, id uuid.UUID) (domain.CatalogVersion, error) {
	p, err := authz.Require(ctx, RoleCatalogSetter)
	if err != nil {
		return domain.CatalogVersion{}, err
	}
	now := s.Clock.Now()
	var v domain.CatalogVersion
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		if v, e = lockedVersion(ctx, tx, id); e != nil {
			return e
		}
		switch e := v.CanSubmit(p.ActorRef); {
		case errors.Is(e, domain.ErrNotSetter):
			return ErrCatalogNotSetter
		case e != nil:
			return ErrCatalogTransition.WithDetail(e.Error())
		}
		if e := store.MarkSubmitted(ctx, tx, v.ID, now); e != nil {
			return e
		}
		v.Status, v.SubmittedAt = domain.CatalogSubmitted, &now
		return nil
	})
	return v, err
}

// Decide duyệt (approve = true) hoặc từ chối một phiên bản đã gửi duyệt (vai
// param_approver; R-56: không được là người đặt — authz.EnsureDistinctApprover).
func (s *Service) Decide(ctx context.Context, id uuid.UUID, approve bool, note *string) (domain.CatalogVersion, error) {
	p, err := authz.Require(ctx, RoleCatalogApprover)
	if err != nil {
		return domain.CatalogVersion{}, err
	}
	if note != nil {
		n := strings.TrimSpace(*note)
		note = &n
		if len([]rune(n)) > 500 {
			return domain.CatalogVersion{}, problem.Validation.WithField("note", "tối đa 500 ký tự")
		}
	}
	if !approve && (note == nil || len([]rune(*note)) < 5) {
		return domain.CatalogVersion{}, problem.Validation.WithField("note", "từ chối phải nêu lý do (≥ 5 ký tự)")
	}
	now := s.Clock.Now()
	var v domain.CatalogVersion
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		if v, e = lockedVersion(ctx, tx, id); e != nil {
			return e
		}
		if e := authz.EnsureDistinctApprover(v.SetterRef, p); e != nil {
			return e
		}
		if e := v.CanDecide(); e != nil {
			return ErrCatalogTransition.WithDetail(e.Error())
		}
		st := domain.CatalogRejected
		if approve {
			st = domain.CatalogApproved
		}
		if e := store.MarkDecided(ctx, tx, v.ID, st, p.ActorRef, now, note); e != nil {
			return e
		}
		approver := p.ActorRef
		v.Status, v.ApproverRef, v.DecidedAt, v.DecisionNote = st, &approver, &now, note
		return nil
	})
	return v, err
}

var catalogAdminRoles = []string{RoleCatalogSetter, RoleCatalogApprover}

// Version đọc một phiên bản danh mục (Admin API).
func (s *Service) Version(ctx context.Context, id uuid.UUID) (domain.CatalogVersion, error) {
	p, err := authz.Require(ctx, catalogAdminRoles...)
	if err != nil {
		return domain.CatalogVersion{}, err
	}
	var v *domain.CatalogVersion
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		v, e = store.VersionByID(ctx, tx, id, false)
		return e
	})
	if err != nil {
		return domain.CatalogVersion{}, err
	}
	if v == nil {
		return domain.CatalogVersion{}, ErrCatalogNotFound
	}
	return *v, nil
}

// Versions liệt kê phiên bản danh mục mọi trạng thái (Admin API).
func (s *Service) Versions(ctx context.Context, evidenceType, status string, after uuid.UUID, limit int) (Page[domain.CatalogVersion], error) {
	p, err := authz.Require(ctx, catalogAdminRoles...)
	if err != nil {
		return Page[domain.CatalogVersion]{}, err
	}
	if evidenceType != "" && !domain.ValidEvidenceType(evidenceType) {
		return Page[domain.CatalogVersion]{}, problem.Validation.WithField("evidence_type", "không hợp lệ")
	}
	switch domain.CatalogStatus(status) {
	case "", domain.CatalogDraft, domain.CatalogSubmitted, domain.CatalogApproved, domain.CatalogRejected:
	default:
		return Page[domain.CatalogVersion]{}, problem.Validation.WithField("status", "phải là draft, submitted, approved hoặc rejected")
	}
	limit = clampLimit(limit)
	var vs []domain.CatalogVersion
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		vs, e = store.ListVersions(ctx, tx, evidenceType, status, after, limit+1)
		return e
	})
	if err != nil {
		return Page[domain.CatalogVersion]{}, err
	}
	return page(vs, limit, func(v domain.CatalogVersion) uuid.UUID { return v.ID }), nil
}

// ApprovedCatalog trả phiên bản đã duyệt (version = 0: mới nhất) cho app hiện
// trường dựng biểu mẫu (Application API).
func (s *Service) ApprovedCatalog(ctx context.Context, evidenceType string, version int) (domain.CatalogVersion, error) {
	p, err := authz.Require(ctx, catalogReaders()...)
	if err != nil {
		return domain.CatalogVersion{}, err
	}
	if !domain.ValidEvidenceType(evidenceType) {
		return domain.CatalogVersion{}, problem.Validation.WithField("evidence_type", "bắt buộc, không hợp lệ")
	}
	var v *domain.CatalogVersion
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		v, e = store.Approved(ctx, tx, evidenceType, version)
		return e
	})
	if err != nil {
		return domain.CatalogVersion{}, err
	}
	if v == nil {
		return domain.CatalogVersion{}, ErrCatalogNotFound
	}
	return *v, nil
}

//go:embed seed_g1.json
var seedG1 []byte

// SeedResult là kết quả nạp danh mục mẫu cho một loại minh chứng.
type SeedResult struct {
	EvidenceType string
	Created      bool
	Version      domain.CatalogVersion
}

// SeedG1 nạp danh mục mẫu G1 (AWD, kiểm tra thực địa) dạng NHÁP, provisional —
// chờ chốt C-1. Loại minh chứng đã có phiên bản thì bỏ qua. Bản nháp vẫn phải
// qua gửi duyệt + phê duyệt bởi người khác (R-56) mới dùng được.
func (s *Service) SeedG1(ctx context.Context, idemKey string) ([]SeedResult, error) {
	p, err := authz.Require(ctx, RoleCatalogSetter)
	if err != nil {
		return nil, err
	}
	if idemKey == "" {
		return nil, problem.IdempotencyRequired
	}
	var seeds []ProposeInput
	if err := json.Unmarshal(seedG1, &seeds); err != nil {
		return nil, fmt.Errorf("mrv: tệp danh mục mẫu hỏng: %w", err)
	}
	var out []SeedResult
	for _, in := range seeds {
		var exists bool
		if err := s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
			var e error
			exists, e = store.HasVersion(ctx, tx, in.EvidenceType)
			return e
		}); err != nil {
			return nil, err
		}
		if exists {
			out = append(out, SeedResult{EvidenceType: in.EvidenceType})
			continue
		}
		in.IdemKey = "seed:" + idemKey + ":" + in.EvidenceType
		v, replayed, err := s.Propose(ctx, in)
		if err != nil {
			return nil, err
		}
		out = append(out, SeedResult{EvidenceType: in.EvidenceType, Created: !replayed, Version: v})
	}
	return out, nil
}

// Package app owns KHTC authoring and publication. It never rewrites report history.
package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtctemplate/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtctemplate/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	party "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var (
	ErrNotFound    = problem.New(http.StatusNotFound, "extension.khtc_template_not_found", "Không tìm thấy phiên bản mẫu báo cáo")
	ErrInvalid     = problem.New(http.StatusUnprocessableEntity, "extension.khtc_template_invalid", "Mục lục mẫu không hợp lệ")
	ErrVersion     = problem.New(http.StatusConflict, "extension.khtc_template_version_conflict", "Mẫu đã có phiên bản mới hơn, tải lại trước khi lưu")
	ErrUnpublished = problem.New(http.StatusConflict, "extension.khtc_template_unpublished", "Chưa có mẫu đã phát hành áp dụng cho kỳ này")
	ErrNarrative   = problem.New(http.StatusConflict, "extension.khtc_template_text_in_use", "Vùng lời văn đã có nội dung trên bản soạn; cần xác nhận thay đổi cho phiên bản mới")
	ErrDependency  = problem.New(http.StatusServiceUnavailable, "extension.dependency_unavailable", "Dịch vụ phụ thuộc chưa sẵn sàng")
)

type Service struct {
	DB              tenant.Beginner
	Clock           clock.Clock
	Contexts        port.KHTCTemplateContextReader
	Names           func() party.ActorNames
	LegacyTemplates map[string]port.KHTCTemplateSnapshot
}
type caller struct {
	p authz.Principal
	k port.SessionContext
}

func (s *Service) reader(ctx context.Context, admin bool) (caller, error) {
	p, k, err := port.RequireSession(ctx)
	if err != nil {
		return caller{}, err
	}
	allowed := k.Role == port.SessionRoleAdmin || !admin && k.Department == party.DeptKeHoachTaiChinh && (k.Role == port.SessionRoleProvince || k.Role == port.SessionRoleNational)
	if !allowed {
		return caller{}, problem.Forbidden
	}
	if s.DB == nil || s.Clock == nil {
		return caller{}, ErrDependency
	}
	return caller{p, k}, nil
}
func (s *Service) writer(ctx context.Context) (caller, error) {
	c, err := s.reader(ctx, true)
	if err != nil {
		return c, err
	}
	if c.k.ReadOnly() || c.k.Support != nil {
		return c, problem.Forbidden.WithDetail("Phiên chỉ xem hoặc hỗ trợ không cấu hình mẫu")
	}
	return c, nil
}
func (s *Service) tx(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	return tenant.WithTenantTx(ctx, s.DB, tid, fn)
}
func inputError(err error) error {
	var e domain.InputError
	if errors.As(err, &e) {
		return ErrInvalid.WithField(e.Field, e.Message)
	}
	return err
}
func hash(v any) string {
	raw, _ := json.Marshal(v)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func idemKey(c caller, operation, key, idem string) (string, error) {
	if idem == "" {
		return "", problem.IdempotencyRequired
	}
	if len(idem) > 128 || strings.TrimSpace(idem) != idem || strings.ContainsAny(idem, "\x00\r\n") {
		return "", problem.BadRequest.WithField("idempotency_key", "Khóa thao tác không hợp lệ")
	}
	return "khtc:template:" + operation + ":" + c.p.ActorRef.String() + ":" + key + ":" + idem, nil
}

type Detail struct {
	Key          string              `json:"key"`
	VersionID    *uuid.UUID          `json:"version_id"`
	Version      int64               `json:"version"`
	Definition   domain.Definition   `json:"definition"`
	Hash         string              `json:"hash"`
	History      []store.Version     `json:"history"`
	Publications []store.Publication `json:"publications"`
	Editable     bool                `json:"editable"`
	ProtectedIDs []string            `json:"protected_ids"`
	SeededIDs    []string            `json:"seeded_ids"`
}

func (s *Service) Detail(ctx context.Context, key string) (Detail, error) {
	c, err := s.reader(ctx, true)
	if err != nil {
		return Detail{}, err
	}
	if !domain.Fixed(key) {
		return Detail{}, ErrNotFound
	}
	out := Detail{Key: key, Definition: domain.Seed(key), History: []store.Version{}, Publications: []store.Publication{}, Editable: !c.k.ReadOnly() && c.k.Support == nil, ProtectedIDs: []string{}, SeededIDs: []string{}}
	for _, n := range out.Definition.Nodes {
		out.SeededIDs = append(out.SeededIDs, n.ID)
		if domain.Protected(key, n.ID) {
			out.ProtectedIDs = append(out.ProtectedIDs, n.ID)
		}
	}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		latest, e := store.Latest(ctx, tx, key)
		if e != nil {
			return e
		}
		if latest != nil {
			out.Definition = latest.Definition
			out.Version = latest.Version
			out.VersionID = &latest.ID
			out.Hash = latest.Hash
		}
		if out.History, e = store.History(ctx, tx, key, 100); e != nil {
			return e
		}
		out.Publications, e = store.Publications(ctx, tx, key, 100)
		return e
	})
	if out.Hash == "" {
		_, out.Hash, _ = out.Definition.BytesHash()
	}
	if err == nil {
		refs := []uuid.UUID{}
		for _, v := range out.History {
			refs = append(refs, v.Actor)
		}
		for _, p := range out.Publications {
			refs = append(refs, p.Actor)
		}
		names := s.displayNames(ctx, refs)
		for i := range out.History {
			out.History[i].ActorDisplay = names[out.History[i].Actor]
		}
		for i := range out.Publications {
			out.Publications[i].ActorDisplay = names[out.Publications[i].Actor]
		}
	}
	return out, err
}

// Display names are a presentation read after the catalog transaction has ended.
func (s *Service) displayNames(ctx context.Context, refs []uuid.UUID) map[uuid.UUID]string {
	if s.Names == nil {
		return nil
	}
	reader := s.Names()
	if reader == nil {
		return nil
	}
	names, err := reader.DisplayNames(ctx, refs)
	if err != nil {
		return nil
	}
	return names
}

func (s *Service) Catalog(ctx context.Context) ([]Detail, error) {
	if _, err := s.reader(ctx, true); err != nil {
		return nil, err
	}
	out := make([]Detail, 0, len(domain.Keys))
	for _, key := range domain.Keys {
		d, err := s.Detail(ctx, key)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// Resolve selects by the report period START, not cutoff/publication time. It has
// no lazy write and no fallback to a static template if the new snapshot is absent.
func (s *Service) Resolve(ctx context.Context, key string, start time.Time) (port.KHTCTemplateSnapshot, error) {
	c, err := s.reader(ctx, false)
	if err != nil {
		return port.KHTCTemplateSnapshot{}, err
	}
	if !domain.Fixed(key) {
		return port.KHTCTemplateSnapshot{}, ErrNotFound
	}
	if start.IsZero() || start.Year() < 2000 || start.Year() > 2100 {
		return port.KHTCTemplateSnapshot{}, ErrInvalid.WithField("period_start", "Ngày bắt đầu kỳ không hợp lệ")
	}
	var out port.KHTCTemplateSnapshot
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		p, e := store.Applicable(ctx, tx, key, start)
		if e != nil {
			return e
		}
		if p == nil {
			return ErrUnpublished
		}
		v, e := store.ByID(ctx, tx, p.VersionID)
		if e != nil {
			return e
		}
		if v == nil || v.Key != key || v.Hash != p.Hash || v.Version != p.Version {
			return ErrNotFound
		}
		out, e = snapshot(*v, p.AppliesFrom)
		return e
	})
	return out, err
}

func (s *Service) ByID(ctx context.Context, id uuid.UUID) (port.KHTCTemplateSnapshot, error) {
	c, err := s.reader(ctx, false)
	if err != nil {
		return port.KHTCTemplateSnapshot{}, err
	}
	var out port.KHTCTemplateSnapshot
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		v, e := store.ByID(ctx, tx, id)
		if e != nil {
			return e
		}
		if v == nil {
			return ErrNotFound
		}
		p, e := store.FirstPublication(ctx, tx, id)
		if e != nil {
			return e
		}
		if p == nil {
			return ErrUnpublished
		}
		out, e = snapshot(*v, p.AppliesFrom)
		return e
	})
	return out, err
}
func snapshot(v store.Version, from time.Time) (port.KHTCTemplateSnapshot, error) {
	if v.Rule != domain.RuleVersion {
		return port.KHTCTemplateSnapshot{}, ErrInvalid.WithDetail("Phiên bản quy tắc dựng mẫu chưa được hỗ trợ")
	}
	raw, actual, err := v.Definition.BytesHash()
	if err != nil {
		return port.KHTCTemplateSnapshot{}, err
	}
	if actual != v.Hash {
		return port.KHTCTemplateSnapshot{}, ErrInvalid.WithDetail("Băm định nghĩa mẫu không khớp")
	}
	return port.KHTCTemplateSnapshot{Mode: "published", Rule: v.Rule, ID: v.ID, Key: v.Key, Version: v.Version, Hash: v.Hash, Definition: raw, ApplicableFrom: from}, nil
}

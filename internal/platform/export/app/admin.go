package app

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/export/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/export/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/doctpl"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

func (s *Service) admin(ctx context.Context) (authz.Principal, error) {
	p, err := principal(ctx)
	if err != nil {
		return p, err
	}
	if s.Access == nil || !s.Access.CanAdmin(ctx) {
		return p, problem.Forbidden
	}
	if s.DB == nil {
		return p, port.ErrUnavailable
	}
	return p, nil
}

// VersionInfo là một phiên bản mẫu: đăng ký trong mã (Registered) và/hoặc đã ghi sổ trong tenant (RecordedAt).
type VersionInfo struct {
	Version    int
	Hash       string
	Registered bool
	RecordedAt *time.Time
	Drift      bool // đã ghi sổ với băm khác mẫu đang đăng ký cùng phiên bản
}

// TemplateInfo là một khóa mẫu cho màn quản trị.
type TemplateInfo struct {
	Key      string
	Title    string
	Owner    string
	Latest   int
	Vars     []doctpl.Var
	Lists    []doctpl.List
	Sections []string // tiêu đề mục cấp 1 của phiên bản mới nhất
	Versions []VersionInfo
}

// Templates trả mọi khóa mẫu (đăng ký + đã ghi sổ trong tenant), theo khóa.
func (s *Service) Templates(ctx context.Context) ([]TemplateInfo, error) {
	p, err := s.admin(ctx)
	if err != nil {
		return nil, err
	}
	var recorded []store.TemplateVersion
	if err := s.tx(ctx, p.TenantID, func(tx pgx.Tx) error { var e error; recorded, e = store.Templates(ctx, tx); return e }); err != nil {
		return nil, err
	}
	byKey := map[string]*TemplateInfo{}
	info := func(key string) *TemplateInfo {
		if byKey[key] == nil {
			byKey[key] = &TemplateInfo{Key: key}
		}
		return byKey[key]
	}
	for key, versions := range s.templates {
		ti := info(key)
		latest := versions[len(versions)-1]
		ti.Title, ti.Owner, ti.Latest, ti.Vars, ti.Lists = latest.Title, latest.Owner, latest.Version, latest.Vars, latest.Lists
		for _, b := range latest.Body {
			if b.Kind == doctpl.KindSection {
				ti.Sections = append(ti.Sections, b.Text)
			}
		}
		for _, t := range versions {
			ti.Versions = append(ti.Versions, VersionInfo{Version: t.Version, Hash: t.Hash(), Registered: true})
		}
	}
	for _, r := range recorded {
		ti := info(r.Key)
		if ti.Title == "" {
			ti.Title, ti.Owner = r.Title, r.Owner
		}
		at := r.At
		i := slices.IndexFunc(ti.Versions, func(v VersionInfo) bool { return v.Version == r.Version })
		if i < 0 {
			ti.Versions = append(ti.Versions, VersionInfo{Version: r.Version, Hash: r.Hash, RecordedAt: &at})
			continue
		}
		ti.Versions[i].RecordedAt = &at
		ti.Versions[i].Drift = ti.Versions[i].Hash != r.Hash
	}
	out := make([]TemplateInfo, 0, len(byKey))
	for _, ti := range byKey {
		slices.SortFunc(ti.Versions, func(a, b VersionInfo) int { return b.Version - a.Version })
		out = append(out, *ti)
	}
	slices.SortFunc(out, func(a, b TemplateInfo) int { return strings.Compare(a.Key, b.Key) })
	return out, nil
}

// AdminRuns trả lịch sử lần xuất của tenant (không byte), lọc theo mẫu/subject.
func (s *Service) AdminRuns(ctx context.Context, template, subject string, limit int) ([]port.Run, error) {
	p, err := s.admin(ctx)
	if err != nil {
		return nil, err
	}
	return s.runs(ctx, p, store.RunFilter{Template: template, Subject: subject, Limit: limit})
}

// AdminRun trả một lần xuất kèm byte (tải lại từ màn quản trị).
func (s *Service) AdminRun(ctx context.Context, id uuid.UUID) (port.Run, error) {
	p, err := s.admin(ctx)
	if err != nil {
		return port.Run{}, err
	}
	var r *store.Run
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error { var e error; r, e = store.RunByID(ctx, tx, "", id); return e })
	if err != nil {
		return port.Run{}, err
	}
	return verified(r)
}

package assembly

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	coopport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/evidence"
	evport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/evidence/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// ---------------------------------------------------------------------------
// cap/evidence (W6.5): pf/evidence sở hữu pf_files; pf/sync giữ route /v1/files và gọi port.Files. Pool của đơn vị
// (app_platform → r_evidence, db.ForModule). Module chủ của chủ thể hiện thực evport.Resolver qua
// bộ chuyển ở đây — pf/evidence (capability) không import module domain (R-MOD-05).
// ---------------------------------------------------------------------------

// wireEvidence dựng pf/evidence và nối Resolver của các module chủ.
// Pool là vai đăng nhập của đơn vị (app_platform) — module chạy SET LOCAL ROLE r_evidence.
func wireEvidence(p *pgxpool.Pool, clk clock.Clock, d evidence.Deps, coop coopport.Resolve) *evidence.Module {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	ev := evidence.New(p, clk, d)
	ev.UseResolver(coopport.SubjectModule, CoopResolver{R: coop})
	return ev
}

// CoopResolver chuyển cooperative/port.Resolve thành evidence/port.Resolver (subject_module = pf/cooperative).
type CoopResolver struct{ R coopport.Resolve }

var _ evport.Resolver = CoopResolver{}

// Resolve thực hiện evidence/port.Resolver.
func (c CoopResolver) Resolve(ctx context.Context, subjectType string, id uuid.UUID) (evport.Resolved, error) {
	s, err := c.R.Resolve(ctx, coopport.SubjectType(subjectType), id)
	switch {
	case errors.Is(err, coopport.ErrSubjectNotFound):
		return evport.Resolved{}, errors.Join(evport.ErrSubjectNotFound, err)
	case errors.Is(err, coopport.ErrSubjectTypeUnsupported):
		return evport.Resolved{}, errors.Join(evport.ErrSubjectTypeUnsupported, err)
	case errors.Is(err, coopport.ErrUnavailable):
		return evport.Resolved{}, errors.Join(evport.ErrResolverUnavailable, err)
	case err != nil:
		return evport.Resolved{}, err
	}
	return evport.Resolved{CommuneCode: s.CommuneCode, NodeRef: s.NodeRef, ViaShareGrant: s.ViaShareGrant}, nil
}

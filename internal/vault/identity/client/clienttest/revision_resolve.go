package clienttest

import (
	"context"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/domain"
	"github.com/google/uuid"
)

var _ client.RevisionResolver = (*Fake)(nil)

func (f *Fake) ResolveRevisions(ctx context.Context, purpose client.Purpose, refs []uuid.UUID) ([]client.RevisionNames, error) {
	tid, actor, err := f.start(ctx, domain.OpResolve, purpose, true)
	if err != nil {
		return nil, err
	}
	if purpose != client.PurposeFieldSupport || len(refs) > client.MaxRevisionRefs {
		return nil, problem.Validation
	}
	if p, ok := authz.FromContext(ctx); ok && !p.Has(client.RoleRegistrar) {
		return nil, problem.Forbidden
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []client.RevisionNames{}
	seen := map[uuid.UUID]bool{}
	for _, ref := range refs {
		r, ok := f.revisions[ref]
		if !ok || r.tenant != tid {
			continue
		}
		if _, err := f.get(tid, r.subject); err != nil {
			continue
		}
		if !seen[r.subject] {
			f.record(domain.OpResolve, purpose, r.subject, actor)
			seen[r.subject] = true
		}
		out = append(out, client.RevisionNames{Ref: ref, SubjectRef: r.subject, PrevName: r.prevName, NextName: r.nextName})
	}
	return out, nil
}

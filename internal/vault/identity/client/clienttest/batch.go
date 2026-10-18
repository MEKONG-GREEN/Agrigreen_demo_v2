package clienttest

import (
	"context"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/domain"
)

var _ client.ProfileBatch = (*Fake)(nil)

// ReadBatch thực hiện client.ProfileBatch (W11.28) như kho thật: mục đích theo thao tác thật, đúng mục đích của ngăn,
// tối đa client.MaxBatchRefs, ref lạ bỏ qua, ghi nhật ký resolve/section_read cho từng chủ thể. Fail nhận op "batch_read".
func (f *Fake) ReadBatch(ctx context.Context, req client.BatchRequest) ([]client.BatchItem, error) {
	tid, actor, err := f.start(ctx, "batch_read", req.Purpose, true)
	if err != nil {
		return nil, err
	}
	if (!req.Identity && req.Section == "") || len(req.Refs) > client.MaxBatchRefs {
		return nil, problem.Validation
	}
	if req.Identity && !domain.AllowedPurpose(domain.OpResolve, string(req.Purpose)) {
		return nil, problem.Validation
	}
	if req.Section != "" {
		if err := sectionCheck(domain.OpSectionRead, req.Purpose, req.Section); err != nil {
			return nil, err
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]client.BatchItem, 0, len(req.Refs))
	for _, ref := range req.Refs {
		s, ok := f.subjects[ref]
		if !ok || s.tenant != tid {
			continue
		}
		it := client.BatchItem{Ref: ref, Fields: client.Fields{}, Erased: s.erased}
		if req.Identity {
			f.record(domain.OpResolve, req.Purpose, ref, actor)
		}
		if req.Section != "" {
			f.record(domain.OpSectionRead, req.Purpose, ref, actor)
		}
		if !s.erased {
			if req.Identity {
				it.FullName, it.Phone, it.Address = s.id.FullName, s.id.Phone, s.id.Address
			}
			for k, v := range s.sections[req.Section] {
				it.Fields[k] = v
			}
		}
		out = append(out, it)
	}
	return out, nil
}

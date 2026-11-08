package app

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/store"
)

var _ client.RevisionResolver = (*Service)(nil)

// ResolveRevisions (#276, Q9) mở đúng bản mã họ tên trước/sau, không đọc tên hiện hành.
// Giữ kiểm Resolve; không thay đổi quyền Display/Revisions, không giải mã SĐT.
func (s *Service) ResolveRevisions(ctx context.Context, purpose client.Purpose, refs []uuid.UUID) ([]client.RevisionNames, error) {
	p, err := s.begin(ctx, domain.OpResolve, purpose, client.RoleRegistrar)
	if err != nil {
		return nil, err
	}
	if purpose != client.PurposeFieldSupport {
		return nil, problem.Validation.WithField("purpose", "lịch sử họ tên chỉ dùng extension.field_support")
	}
	if len(refs) > client.MaxRevisionRefs {
		return nil, problem.Validation.WithField("refs", "tối đa 200 bản sửa mỗi lần")
	}
	for range refs {
		if err := s.sensitive(ctx, p, domain.OpResolve, purpose, true, true); err != nil {
			return nil, err
		}
	}
	var rows []store.Revision
	keys := map[uuid.UUID]*store.Key{}
	// Giao dịch chỉ lấy bản mã và khóa bọc; RLS theo tenant của token đã xác thực.
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		if rows, e = store.Revisions(ctx, tx, refs); e != nil {
			return e
		}
		for _, r := range rows {
			if _, ok := keys[r.SubjectRef]; ok {
				continue
			}
			k, e := store.GetKey(ctx, tx, r.SubjectRef)
			if e != nil {
				return e
			}
			keys[r.SubjectRef] = k
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := make([]client.RevisionNames, 0, len(rows))
	for _, r := range rows {
		k := keys[r.SubjectRef]
		if k == nil {
			continue
		}
		names, e := s.openRevisionNames(ctx, p.TenantID, k, r)
		if e != nil {
			return nil, e
		}
		out = append(out, names)
	}
	// Bắt buộc audit thành công; không trả dù một tên khi giao dịch audit hỏng.
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		seen := map[uuid.UUID]bool{}
		for _, r := range out {
			if seen[r.SubjectRef] {
				continue
			}
			seen[r.SubjectRef] = true
			if e := s.logAccess(ctx, tx, p, &r.SubjectRef, true, domain.OpResolve, purpose, domain.OutcomeOK); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// KMS và giải mã luôn chạy ngoài giao dịch DB (pool cỡ 1 vẫn dùng lại được).
func (s *Service) openRevisionNames(ctx context.Context, tid uuid.UUID, k *store.Key, r store.Revision) (client.RevisionNames, error) {
	dk, err := s.KMS.Unwrap(ctx, k.KEKID, k.WrappedDataKey, domain.AAD(useDataKey, tid, r.SubjectRef))
	if err != nil {
		return client.RevisionNames{}, err
	}
	defer clear(dk)
	prev, err := openField(dk, domain.AAD(useName, tid, r.SubjectRef), r.PrevNameCT)
	if err != nil {
		return client.RevisionNames{}, err
	}
	next, err := openField(dk, domain.AAD(useName, tid, r.SubjectRef), r.NextNameCT)
	if err != nil {
		return client.RevisionNames{}, err
	}
	return client.RevisionNames{Ref: r.Ref, SubjectRef: r.SubjectRef, PrevName: prev, NextName: next}, nil
}

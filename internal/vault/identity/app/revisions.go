package app

// CNTS-V4-HOUSEHOLD-LIST (#251, vault 20261106164139, ADR-0056 mục 4): nhãn che bớt của bản sửa họ tên/SĐT
// (client.Revisions) — lịch sử "giá trị cũ → mới" HCN-09 của danh sách hộ chăn nuôi CNTS.

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

var _ client.Revisions = (*Service)(nil)

// Revisions trả nhãn che bớt họ tên (domain.Label) và SĐT che một phần (domain.MaskPhone) trước/sau của các bản sửa —
// KHÔNG BAO GIỜ trả họ tên/SĐT đầy đủ. Mã lạ, của tenant khác (RLS) hoặc của chủ thể đã xóa định danh (bản sửa xóa cùng
// chủ thể — RB-3) bị bỏ qua. Vai như Display; mỗi chủ thể đọc được ghi một dòng sổ op "revision_read".
func (s *Service) Revisions(ctx context.Context, purpose client.Purpose, refs []uuid.UUID) ([]client.Revision, error) {
	p, err := s.begin(ctx, domain.OpRevisionRead, purpose, client.RoleRegistrar, client.RoleDisplay, client.RoleEvidenceViewer)
	if err != nil {
		return nil, err
	}
	if len(refs) > domain.MaxRevisionRefs {
		return nil, problem.Validation.WithField("refs", "tối đa 200 bản sửa mỗi lần")
	}
	if len(refs) == 0 {
		return []client.Revision{}, nil
	}
	tid := p.TenantID
	// P0-POOL-SHARED (#126): giao dịch 1 chỉ đọc bản mã + khóa đã bọc; KMS + giải mã ngoài giao dịch; giao dịch 2 ghi sổ.
	var rows []store.Revision
	keys := map[uuid.UUID]*store.Key{}
	err = tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
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
	out := make([]client.Revision, 0, len(rows))
	read := []uuid.UUID{}
	for _, r := range rows {
		k := keys[r.SubjectRef]
		if k == nil {
			continue // chủ thể đã xóa định danh giữa hai bước — bỏ qua
		}
		x, err := s.openRevision(ctx, tid, k, r)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
		read = append(read, r.SubjectRef)
	}
	err = tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		seen := map[uuid.UUID]bool{}
		for _, ref := range read {
			if seen[ref] {
				continue
			}
			seen[ref] = true
			if err := s.logAccess(ctx, tx, p, &ref, true, domain.OpRevisionRead, purpose, domain.OutcomeOK); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// openRevision mở khóa dữ liệu của chủ thể và giải mã bản sửa thành nhãn che bớt — gọi NGOÀI giao dịch.
func (s *Service) openRevision(ctx context.Context, tid uuid.UUID, k *store.Key, r store.Revision) (client.Revision, error) {
	dk, err := s.KMS.Unwrap(ctx, k.KEKID, k.WrappedDataKey, domain.AAD(useDataKey, tid, r.SubjectRef))
	if err != nil {
		return client.Revision{}, err
	}
	defer clear(dk)
	var prevName, prevPhone, nextName, nextPhone string
	for _, f := range []struct {
		dst *string
		use string
		ct  []byte
	}{
		{&prevName, useName, r.PrevNameCT}, {&prevPhone, usePhone, r.PrevPhoneCT},
		{&nextName, useName, r.NextNameCT}, {&nextPhone, usePhone, r.NextPhoneCT},
	} {
		if *f.dst, err = openField(dk, domain.AAD(f.use, tid, r.SubjectRef), f.ct); err != nil {
			return client.Revision{}, err
		}
	}
	return client.Revision{Ref: r.Ref, SubjectRef: r.SubjectRef, PrevLabel: domain.Label(prevName), NextLabel: domain.Label(nextName),
		PrevPhone: domain.MaskPhone(prevPhone), NextPhone: domain.MaskPhone(nextPhone)}, nil
}

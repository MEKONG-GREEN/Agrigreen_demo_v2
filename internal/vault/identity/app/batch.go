package app

// W11.28: đọc hồ sơ nhân sự theo lô (client.ProfileBatch) cho báo cáo Sở NS01–NS06 và dashboard nhân sự (pf/hrm).

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/store"
)

var _ client.ProfileBatch = (*Service)(nil)

// opBatchQuota là khóa hạn mức của đọc theo lô (chỉ trong bộ nhớ — không phải giá trị op của sổ truy cập).
const opBatchQuota = "batch_read"

// ReadBatch đọc định danh (không CCCD) và/hoặc MỘT ngăn của tối đa client.MaxBatchRefs chủ thể trong một giao dịch.
// Kiểm như thao tác đơn: mục đích thuộc danh mục của thao tác thật (resolve khi đọc định danh, section_read khi đọc ngăn)
// và đúng mục đích của ngăn; vai vault_registrar; MFA khi đọc định danh hoặc ngăn nhạy cảm. Một lần gọi = một lượt hạn
// mức. Mỗi chủ thể đọc được ghi sổ từng thao tác đã làm; ref lạ bị bỏ qua (RLS: không lộ chủ thể tenant khác).
func (s *Service) ReadBatch(ctx context.Context, req client.BatchRequest) ([]client.BatchItem, error) {
	if !req.Identity && req.Section == "" {
		return nil, problem.Validation.WithField("section", "cần identity hoặc section")
	}
	if len(req.Refs) > client.MaxBatchRefs {
		return nil, problem.Validation.WithField("refs", "tối đa 200 chủ thể mỗi lần")
	}
	op := domain.OpSectionRead
	if req.Section == "" {
		op = domain.OpResolve
	}
	p, err := s.begin(ctx, op, req.Purpose, client.RoleRegistrar)
	if err != nil {
		return nil, err
	}
	if req.Identity {
		if _, err := s.begin(ctx, domain.OpResolve, req.Purpose, client.RoleRegistrar); err != nil {
			return nil, err
		}
	}
	if req.Section != "" {
		want, ok := domain.SectionPurpose(req.Section)
		if !ok || want != string(req.Purpose) {
			return nil, client.ProblemSectionInvalid.WithField("section", "ngăn không có hoặc không thuộc mục đích "+string(req.Purpose))
		}
	}
	mfa := req.Identity || req.Section == domain.SectionHRMSensitive
	if err := s.sensitive(ctx, p, opBatchQuota, req.Purpose, mfa, true); err != nil {
		return nil, err
	}
	tid := p.TenantID
	// P0-POOL-SHARED (#126): giao dịch 1 chỉ đọc bản mã; KMS + giải mã ngoài giao dịch; giao dịch 2 ghi sổ mọi chủ thể.
	type pending struct {
		ref      uuid.UUID
		erased   bool
		identity sealedIdentity
		section  *store.Section
	}
	var rows []pending
	seen := map[uuid.UUID]bool{}
	err = tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		for _, ref := range req.Refs {
			if seen[ref] {
				continue
			}
			seen[ref] = true
			subj, err := store.GetSubject(ctx, tx, ref, false)
			if err != nil {
				return err
			}
			if subj == nil {
				continue
			}
			r := pending{ref: ref, erased: subj.ErasedAt != nil}
			if !r.erased && req.Identity {
				if r.identity, err = fetchIdentity(ctx, tx, ref); err != nil {
					return err
				}
			}
			if !r.erased && req.Section != "" {
				if r.section, err = store.GetSection(ctx, tx, ref, req.Section); err != nil {
					return err
				}
			}
			rows = append(rows, r)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := make([]client.BatchItem, 0, len(rows))
	for _, r := range rows {
		it := client.BatchItem{Ref: r.ref, Fields: client.Fields{}, Erased: r.erased}
		if !r.erased && req.Identity {
			pl, err := s.openIdentity(ctx, tid, r.ref, r.identity)
			if err != nil {
				return nil, err
			}
			it.FullName, it.Phone, it.Address = pl.FullName, pl.Phone, pl.Address
			clearPlain(&pl)
		}
		if !r.erased && req.Section != "" {
			fields, err := s.openSectionRow(ctx, tid, r.ref, req.Section, r.section)
			if err != nil {
				return nil, err
			}
			it.Fields = fields
		}
		out = append(out, it)
	}
	err = tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		for _, r := range rows {
			outcome := domain.OutcomeOK
			if r.erased {
				outcome = domain.OutcomeErased
			}
			if err := s.logBatch(ctx, tx, p, r.ref, req, outcome); err != nil {
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

// logBatch ghi sổ cho một chủ thể: resolve (đọc định danh) và/hoặc section_read (đọc ngăn).
func (s *Service) logBatch(ctx context.Context, tx pgx.Tx, p authz.Principal, ref uuid.UUID, req client.BatchRequest, outcome string) error {
	if req.Identity {
		if err := s.logAccess(ctx, tx, p, &ref, true, domain.OpResolve, req.Purpose, outcome); err != nil {
			return err
		}
	}
	if req.Section != "" {
		return s.logAccess(ctx, tx, p, &ref, true, domain.OpSectionRead, req.Purpose, outcome)
	}
	return nil
}

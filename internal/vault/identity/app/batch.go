package app

// W11.28: đọc hồ sơ nhân sự theo lô (client.ProfileBatch) cho báo cáo Sở NS01–NS06 và dashboard nhân sự (pf/hrm).

import (
	"context"
	"encoding/json"

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
	out := make([]client.BatchItem, 0, len(req.Refs))
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
			it := client.BatchItem{Ref: ref, Fields: client.Fields{}}
			if subj.ErasedAt != nil {
				it.Erased = true
				out = append(out, it)
				if err := s.logBatch(ctx, tx, p, ref, req, domain.OutcomeErased); err != nil {
					return err
				}
				continue
			}
			if req.Identity {
				pl, err := s.decrypt(ctx, tx, tid, ref)
				if err != nil {
					return err
				}
				it.FullName, it.Phone, it.Address = pl.FullName, pl.Phone, pl.Address
			}
			if req.Section != "" {
				if it.Fields, err = s.batchSection(ctx, tx, tid, ref, req.Section); err != nil {
					return err
				}
			}
			out = append(out, it)
			if err := s.logBatch(ctx, tx, p, ref, req, domain.OutcomeOK); err != nil {
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

// batchSection giải mã một ngăn trong giao dịch (chưa ghi → map rỗng).
func (s *Service) batchSection(ctx context.Context, tx pgx.Tx, tid, ref uuid.UUID, section string) (client.Fields, error) {
	out := client.Fields{}
	row, err := store.GetSection(ctx, tx, ref, section)
	if err != nil || row == nil {
		return out, err
	}
	key, err := s.KMS.Unwrap(ctx, row.KEKID, row.WrappedKey, domain.AAD(useSectionKey(section), tid, ref))
	if err != nil {
		return nil, err
	}
	defer clear(key)
	plainText, err := openField(key, domain.AAD(useSectionData(section), tid, ref), row.PayloadCT)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(plainText), &out); err != nil {
		return nil, err
	}
	return out, nil
}

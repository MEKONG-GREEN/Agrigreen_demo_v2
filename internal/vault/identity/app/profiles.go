package app

// W11.22 (vault 000004): phần mở rộng hồ sơ (client.Profiles) — sửa định danh và ngăn hồ sơ có khóa riêng.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/store"
)

var _ client.Profiles = (*Service)(nil)

// Mục dùng trong AAD của ngăn.
func useSectionKey(section string) string  { return "section-key/" + section }
func useSectionData(section string) string { return "section/" + section }

// errAmendRace: request song song vừa ghi cùng chỉ mục mù SĐT.
var errAmendRace = errors.New("vault: tranh chấp chỉ mục mù khi sửa")

// Amend thay họ tên, SĐT, địa chỉ (CCCD giữ nguyên). SĐT thuộc chủ thể khác → ProblemDuplicate (ghi sổ duplicate).
func (s *Service) Amend(ctx context.Context, purpose client.Purpose, ref uuid.UUID, in client.Identity) error {
	p, err := s.begin(ctx, domain.OpAmend, purpose, client.RoleRegistrar)
	if err != nil {
		return err
	}
	name := domain.NormalizeName(in.FullName)
	v, bad := problem.Validation, false
	if n := utf8.RuneCountInString(name); n < 1 || n > 200 {
		v, bad = v.WithField("full_name", "bắt buộc, tối đa 200 ký tự"), true
	}
	if utf8.RuneCountInString(in.Address) > 500 {
		v, bad = v.WithField("address", "tối đa 500 ký tự"), true
	}
	var phone string
	var blind *store.Blind
	if in.Phone != "" {
		if phone, err = domain.NormalizePhone(in.Phone); err != nil {
			v, bad = v.WithField("phone", err.Error()), true
		} else {
			blind = &store.Blind{Kind: domain.BlindPhone, Value: domain.BlindIndex(s.BlindKey, p.TenantID, domain.BlindPhone, phone)}
		}
	}
	if bad {
		return v
	}
	tid := p.TenantID
	var result error
	err = tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		st, err := s.subjectState(ctx, tx, p, ref, domain.OpAmend, purpose, true)
		if err != nil || st.fail != nil {
			result = st.fail
			return err
		}
		if blind != nil {
			other, found, err := store.FindBlind(ctx, tx, []store.Blind{*blind})
			if err != nil {
				return err
			}
			if found && other != ref {
				result = client.ProblemDuplicate
				return s.logAccess(ctx, tx, p, &ref, true, domain.OpAmend, purpose, domain.OutcomeDuplicate)
			}
		}
		k, err := store.GetKey(ctx, tx, ref)
		if err != nil {
			return err
		}
		old, err := store.GetIdentity(ctx, tx, ref)
		if err != nil {
			return err
		}
		if k == nil || old == nil {
			return errors.New("vault: chủ thể chưa xóa nhưng thiếu khóa hoặc định danh")
		}
		dk, err := s.KMS.Unwrap(ctx, k.KEKID, k.WrappedDataKey, domain.AAD(useDataKey, tid, ref))
		if err != nil {
			return err
		}
		defer clear(dk)
		row := store.Identity{NationalIDCT: old.NationalIDCT}
		for _, f := range []struct {
			dst   *[]byte
			use   string
			value string
		}{{&row.FullNameCT, useName, name}, {&row.PhoneCT, usePhone, phone}, {&row.AddressCT, useAddress, in.Address}} {
			if *f.dst, err = sealField(s.rand(), dk, domain.AAD(f.use, tid, ref), f.value); err != nil {
				return err
			}
		}
		if err := store.UpdateIdentity(ctx, tx, ref, row); err != nil {
			return err
		}
		if err := store.DeleteBlind(ctx, tx, ref, domain.BlindPhone); err != nil {
			return err
		}
		if blind != nil {
			ok, err := store.InsertBlind(ctx, tx, tid, ref, *blind)
			if err != nil {
				return err
			}
			if !ok {
				return errAmendRace
			}
		}
		return s.logAccess(ctx, tx, p, &ref, true, domain.OpAmend, purpose, domain.OutcomeOK)
	})
	if errors.Is(err, errAmendRace) {
		return client.ProblemDuplicate
	}
	if err != nil {
		return err
	}
	return result
}

// sectionBegin kiểm mục đích của thao tác op VÀ đúng mục đích của ngăn (ngăn gắn cứng một mục đích), rồi vai.
func (s *Service) sectionBegin(ctx context.Context, op string, purpose client.Purpose, section string) (authz.Principal, error) {
	p, err := s.begin(ctx, op, purpose, client.RoleRegistrar)
	if err != nil {
		return authz.Principal{}, err
	}
	want, ok := domain.SectionPurpose(section)
	if !ok || want != string(purpose) {
		return authz.Principal{}, client.ProblemSectionInvalid.WithField("section", "ngăn không có hoặc không thuộc mục đích "+string(purpose))
	}
	return p, nil
}

// PutSection ghi (thay toàn bộ) ngăn: khóa dữ liệu MỚI của ngăn mỗi lần ghi, bọc bằng KEK (AAD theo ngăn).
func (s *Service) PutSection(ctx context.Context, purpose client.Purpose, ref uuid.UUID, section string, fields client.Fields) error {
	p, err := s.sectionBegin(ctx, domain.OpSectionWrite, purpose, section)
	if err != nil {
		return err
	}
	if err := domain.ValidSectionFields(section, fields); err != nil {
		var fe domain.FieldError
		if errors.As(err, &fe) {
			return client.ProblemSectionInvalid.WithField(fe.Field, "trường không thuộc ngăn hoặc quá dài")
		}
		return client.ProblemSectionInvalid
	}
	if fields == nil {
		fields = client.Fields{}
	}
	raw, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	defer clear(raw)
	tid := p.TenantID
	key := make([]byte, 32)
	if _, err := io.ReadFull(s.rand(), key); err != nil {
		return err
	}
	defer clear(key)
	wrapped, err := s.KMS.Wrap(ctx, key, domain.AAD(useSectionKey(section), tid, ref))
	if err != nil {
		return err
	}
	ct, err := sealField(s.rand(), key, domain.AAD(useSectionData(section), tid, ref), string(raw))
	if err != nil {
		return err
	}
	var result error
	err = tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		st, err := s.subjectState(ctx, tx, p, ref, domain.OpSectionWrite, purpose, true)
		if err != nil || st.fail != nil {
			result = st.fail
			return err
		}
		if err := store.PutSection(ctx, tx, tid, ref, section, store.Section{PayloadCT: ct, WrappedKey: wrapped, KEKID: s.KMS.KeyID()},
			p.ActorRef); err != nil {
			return err
		}
		return s.logAccess(ctx, tx, p, &ref, true, domain.OpSectionWrite, purpose, domain.OutcomeOK)
	})
	if err != nil {
		return err
	}
	return result
}

// GetSection đọc ngăn; ngăn nhạy cảm cần MFA (RequireMFASensitive). Mọi lần đọc đều ghi sổ (kể cả ngăn chưa ghi) và
// chịu hạn mức theo người yêu cầu như Resolve.
func (s *Service) GetSection(ctx context.Context, purpose client.Purpose, ref uuid.UUID, section string) (client.Fields, error) {
	p, err := s.sectionBegin(ctx, domain.OpSectionRead, purpose, section)
	if err != nil {
		return nil, err
	}
	if err := s.sensitive(ctx, p, domain.OpSectionRead, purpose, section == domain.SectionHRMSensitive, true); err != nil {
		return nil, err
	}
	tid := p.TenantID
	out := client.Fields{}
	var result error
	err = tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		st, err := s.subjectState(ctx, tx, p, ref, domain.OpSectionRead, purpose, false)
		if err != nil || st.fail != nil {
			result = st.fail
			return err
		}
		row, err := store.GetSection(ctx, tx, ref, section)
		if err != nil {
			return err
		}
		if row != nil {
			key, err := s.KMS.Unwrap(ctx, row.KEKID, row.WrappedKey, domain.AAD(useSectionKey(section), tid, ref))
			if err != nil {
				return err
			}
			defer clear(key)
			plain, err := openField(key, domain.AAD(useSectionData(section), tid, ref), row.PayloadCT)
			if err != nil {
				return err
			}
			if err := json.Unmarshal([]byte(plain), &out); err != nil {
				return err
			}
		}
		return s.logAccess(ctx, tx, p, &ref, true, domain.OpSectionRead, purpose, domain.OutcomeOK)
	})
	if err != nil {
		return nil, err
	}
	if result != nil {
		return nil, result
	}
	return out, nil
}

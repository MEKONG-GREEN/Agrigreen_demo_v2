package app

// W11.23 (vault 20261013165158): thẻ danh bạ (client.Directory) cho pf/contact — FN-13 Contact.

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/store"
)

var _ client.Directory = (*Service)(nil)

// Cards đọc thẻ danh bạ: CHỈ trường được hỏi. Họ tên, SĐT từ định danh; năm sinh, email từ ngăn contact.card (liên hệ
// ngoài tỉnh) hoặc — khi ngăn đó không có — PHÉP CHIẾU của ngăn hrm.profile: chỉ năm của ngày sinh và email (FN-13 BR-04,
// AC-02: sửa ở hồ sơ FN-07 thì Contact thấy ngay). Không bao giờ trả CCCD, địa chỉ, giới tính, ngày sinh đầy đủ, ghi chú
// hay ngăn nhạy cảm. Mỗi chủ thể đọc được ghi một dòng sổ op "card". Không áp hạn mức Resolve (danh bạ đọc theo trang,
// tối đa MaxCardRefs/lần) và không đòi MFA (chỉ dữ liệu liên hệ).
func (s *Service) Cards(ctx context.Context, purpose client.Purpose, refs []uuid.UUID, fields []string) ([]client.Card, error) {
	p, err := s.begin(ctx, domain.OpCard, purpose, client.RoleRegistrar, client.RoleDisplay, client.RoleDirectory)
	if err != nil {
		return nil, err
	}
	if !domain.ValidCardFields(fields) {
		return nil, problem.Validation.WithField("fields", "1–4 trường trong full_name, phone, birth_year, email; không lặp")
	}
	if len(refs) > domain.MaxCardRefs {
		return nil, problem.Validation.WithField("refs", "tối đa 200 chủ thể mỗi lần")
	}
	want := map[string]bool{}
	for _, f := range fields {
		want[f] = true
	}
	tid := p.TenantID
	out := make([]client.Card, 0, len(refs))
	seen := map[uuid.UUID]bool{}
	err = tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		for _, ref := range refs {
			if seen[ref] {
				continue
			}
			seen[ref] = true
			subj, err := store.GetSubject(ctx, tx, ref, false)
			if err != nil {
				return err
			}
			if subj == nil {
				continue // ref lạ: bỏ qua, không lộ chủ thể của tenant khác (RLS) — không ghi sổ
			}
			c := client.Card{Ref: ref}
			if subj.ErasedAt != nil {
				c.Erased = true
				out = append(out, c)
				if err := s.logAccess(ctx, tx, p, &ref, true, domain.OpCard, purpose, domain.OutcomeErased); err != nil {
					return err
				}
				continue
			}
			if want[domain.CardFullName] || want[domain.CardPhone] {
				pl, err := s.decrypt(ctx, tx, tid, ref)
				if err != nil {
					return err
				}
				if want[domain.CardFullName] {
					c.FullName = pl.FullName
				}
				if want[domain.CardPhone] {
					c.Phone = pl.Phone
				}
				clearPlain(&pl)
			}
			if want[domain.CardBirthYear] || want[domain.CardEmail] {
				card, err := s.openSection(ctx, tx, tid, ref, domain.SectionContactCard)
				if err != nil {
					return err
				}
				prof := map[string]string{}
				if card["birth_year"] == "" || card["email"] == "" {
					if prof, err = s.openSection(ctx, tx, tid, ref, domain.SectionHRMProfile); err != nil {
						return err
					}
				}
				if want[domain.CardBirthYear] {
					if c.BirthYear = domain.BirthYear(card["birth_year"]); c.BirthYear == "" {
						c.BirthYear = domain.BirthYear(prof["birth_date"])
					}
				}
				if want[domain.CardEmail] {
					if c.Email = card["email"]; c.Email == "" {
						c.Email = prof["email"]
					}
				}
				clear(card)
				clear(prof)
			}
			out = append(out, c)
			if err := s.logAccess(ctx, tx, p, &ref, true, domain.OpCard, purpose, domain.OutcomeOK); err != nil {
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

// openSection giải mã một ngăn trong giao dịch (chưa ghi → map rỗng). Dùng nội bộ cho phép chiếu của thẻ danh bạ — KHÔNG
// trả nguyên ngăn ra ngoài.
func (s *Service) openSection(ctx context.Context, tx pgx.Tx, tid, ref uuid.UUID, section string) (map[string]string, error) {
	out := map[string]string{}
	row, err := store.GetSection(ctx, tx, ref, section)
	if err != nil || row == nil {
		return out, err
	}
	key, err := s.KMS.Unwrap(ctx, row.KEKID, row.WrappedKey, domain.AAD(useSectionKey(section), tid, ref))
	if err != nil {
		return nil, err
	}
	defer clear(key)
	plain, err := openField(key, domain.AAD(useSectionData(section), tid, ref), row.PayloadCT)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(plain), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// clearPlain xóa tham chiếu tới định danh đã giải mã (chuỗi Go bất biến — chỉ bỏ tham chiếu sớm).
func clearPlain(p *plain) { *p = plain{} }

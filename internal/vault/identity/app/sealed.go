package app

// P0-POOL-SHARED (#126): KMS (Wrap/Unwrap — dịch vụ ngoài khi dùng KMS mạng) KHÔNG chạy khi đang giữ giao dịch DB.
// Đường đọc: giao dịch 1 chỉ ĐỌC (trạng thái chủ thể + bản mã + khóa đã bọc) rồi commit; mở khóa/giải mã ngoài
// giao dịch; giao dịch 2 ngắn ghi sổ truy cập. Giải mã lỗi → không ghi sổ OK (như trước: giao dịch hủy). Trả dữ liệu
// chỉ SAU khi dòng sổ đã commit.

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/store"
)

// sealedIdentity là định danh còn mã hóa đọc trong giao dịch — giải mã bằng openIdentity NGOÀI giao dịch.
type sealedIdentity struct {
	key *store.Key
	row *store.Identity
}

// fetchIdentity đọc khóa đã bọc và bản mã định danh (chủ thể chưa xóa).
func fetchIdentity(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (sealedIdentity, error) {
	k, err := store.GetKey(ctx, tx, ref)
	if err != nil {
		return sealedIdentity{}, err
	}
	row, err := store.GetIdentity(ctx, tx, ref)
	if err != nil {
		return sealedIdentity{}, err
	}
	if k == nil || row == nil {
		return sealedIdentity{}, errors.New("vault: chủ thể chưa xóa nhưng thiếu khóa hoặc định danh")
	}
	return sealedIdentity{key: k, row: row}, nil
}

// openIdentity mở khóa dữ liệu (KMS) và giải mã định danh — gọi NGOÀI giao dịch.
func (s *Service) openIdentity(ctx context.Context, tid, ref uuid.UUID, si sealedIdentity) (plain, error) {
	var out plain
	dk, err := s.KMS.Unwrap(ctx, si.key.KEKID, si.key.WrappedDataKey, domain.AAD(useDataKey, tid, ref))
	if err != nil {
		return out, err
	}
	defer clear(dk)
	for _, f := range []struct {
		dst *string
		use string
		ct  []byte
	}{
		{&out.FullName, useName, si.row.FullNameCT}, {&out.Phone, usePhone, si.row.PhoneCT},
		{&out.NationalID, useNational, si.row.NationalIDCT}, {&out.Address, useAddress, si.row.AddressCT},
	} {
		if *f.dst, err = openField(dk, domain.AAD(f.use, tid, ref), f.ct); err != nil {
			return plain{}, err
		}
	}
	return out, nil
}

// openSectionRow giải mã một ngăn đã đọc trong giao dịch (row nil = chưa ghi → map rỗng) — gọi NGOÀI giao dịch.
func (s *Service) openSectionRow(ctx context.Context, tid, ref uuid.UUID, section string, row *store.Section) (map[string]string, error) {
	out := map[string]string{}
	if row == nil {
		return out, nil
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

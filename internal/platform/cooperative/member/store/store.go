// Package store: truy cập DB của nông hộ, xã viên, hồ sơ HTX và sổ hồ sơ KN (schema pf_coop, W3.3 — chuyển từ
// pf_party). Mọi hàm nhận pgx.Tx đã ở ngữ cảnh tenant (tenant.WithTenantTx) trên pool của pf/cooperative (vai
// r_cooperative, db.ForModule). KHÔNG đọc pf_party: thuộc tính Nút (tên, mã, đơn vị, trạng thái) đi qua
// partyport.Orgs/OrgDirectory ở app/.
package store

import (
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func decPtr(d *decimal.Decimal) *string {
	if d == nil {
		return nil
	}
	s := d.String()
	return &s
}

func parseDec(s *string) (*decimal.Decimal, error) {
	if s == nil {
		return nil, nil
	}
	d, err := decimal.NewFromString(*s)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func uuidStrings(refs []uuid.UUID) []string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		out = append(out, r.String())
	}
	return out
}

// noRows đổi pgx.ErrNoRows thành (nil, nil).
func noRows[T any](v T, err error) (*T, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// escapeLike vô hiệu ký tự đại diện của LIKE trong từ khóa người dùng.
func escapeLike(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '%' || r == '_' || r == '\\' {
			b.WriteRune('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

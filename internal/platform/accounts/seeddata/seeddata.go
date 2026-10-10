// Package seeddata nhúng dữ liệu nền KN của pf/accounts (go:embed), TẠM — chờ khách xác nhận:
//
//   - internal_units_an_giang.json: đơn vị nội bộ mặc định của TTKN An Giang (Ban Giám đốc + 5 phòng gắn mã
//     phòng ban P2 — docs/kn/phong-ban-seed.md) — W11.07, Q-068.
//
// Nạp theo yêu cầu của Admin (POST /v1/accounts/internal-units/seed); chạy lặp an toàn.
package seeddata

import _ "embed"

// InternalUnits là đơn vị nội bộ mặc định (định dạng agrigreen.internal-units/v1).
//
//go:embed internal_units_an_giang.json
var InternalUnits []byte

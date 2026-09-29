package app

import (
	"encoding/base64"
	"errors"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

func itoa(n int) string { return strconv.Itoa(n) }

// geometryProblem quy lỗi ràng buộc hình học của CSDL (ST_IsValid, GeoJSON hỏng) về 422.
// Các ràng buộc khác đã được kiểm ở domain trước khi ghi.
func geometryProblem(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && (pg.Code == "23514" || pg.Code == "XX000" || pg.Code == "22023") {
		return problem.Validation.WithField("geometry", "Hình học tuyến không hợp lệ.")
	}
	return err
}

// EncodeUUIDCursor mã hóa cursor từ uuid.
func EncodeUUIDCursor(id uuid.UUID) string { return base64.RawURLEncoding.EncodeToString(id[:]) }

// DecodeUUIDCursor đọc cursor uuid; rỗng → uuid.Nil.
func DecodeUUIDCursor(s string) (uuid.UUID, error) {
	var id uuid.UUID
	if s == "" {
		return id, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || len(b) != 16 {
		return id, errors.New("cursor không hợp lệ")
	}
	copy(id[:], b)
	return id, nil
}

// EncodeIntCursor mã hóa cursor từ số.
func EncodeIntCursor(n int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(n, 10)))
}

// DecodeIntCursor đọc cursor số; rỗng → 0.
func DecodeIntCursor(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return 0, errors.New("cursor không hợp lệ")
	}
	n, err := strconv.ParseInt(string(b), 10, 64)
	if err != nil || n < 0 {
		return 0, errors.New("cursor không hợp lệ")
	}
	return n, nil
}

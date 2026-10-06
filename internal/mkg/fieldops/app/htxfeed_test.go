package app

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// M4: lỗi vĩnh viễn (gác, con trỏ tiến) và lỗi tạm thời (dừng lượt, thử lại) khi ghi phản hồi HTX.
func TestClassifyFeedErr(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		code      string
		permanent bool
	}{
		{"NUL trong text", &pgconn.PgError{Code: "22021"}, "sqlstate.22021", true},
		{"vi phạm CHECK", fmt.Errorf("ghi: %w", &pgconn.PgError{Code: "23514"}), "sqlstate.23514", true},
		{"trùng khóa (ghi song song)", &pgconn.PgError{Code: "23505"}, "sqlstate.23505", false},
		{"khóa chết", &pgconn.PgError{Code: "40P01"}, "sqlstate.40P01", false},
		{"thiếu quyền (cấu hình)", &pgconn.PgError{Code: "42501"}, "sqlstate.42501", false},
		{"mất kết nối", &pgconn.PgError{Code: "08006"}, "sqlstate.08006", false},
		{"nghiệp vụ 422", ErrStatementStale, ErrStatementStale.Code, true},
		{"nghiệp vụ 503", ErrDirectory, ErrDirectory.Code, false},
		{"lỗi trường", domain.FieldError{Field: "reason", Msg: "10–500 ký tự"}, "validation.reason", true},
		{"hết hạn", context.DeadlineExceeded, "", false},
		{"lỗi lạ", errors.New("io: kết nối bị đóng"), "", false},
		{"problem bọc", fmt.Errorf("x: %w", problem.Validation), problem.Validation.Code, true},
	}
	for _, c := range cases {
		code, perm := classifyFeedErr(c.err)
		if code != c.code || perm != c.permanent {
			t.Errorf("%s: (%q, %v), muốn (%q, %v)", c.name, code, perm, c.code, c.permanent)
		}
	}
}

func TestSanitizeText(t *testing.T) {
	in := "lý do\x00 có NUL \xff và dài"
	got := sanitizeText(in, 12)
	if got != "lý do có NUL" {
		t.Fatalf("%q", got)
	}
	if got := sanitizeText("abc\x00", 100); got != "abc" {
		t.Fatalf("%q", got)
	}
}

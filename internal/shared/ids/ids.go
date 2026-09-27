// Package ids sinh định danh nghiệp vụ UUIDv7 (R-11).
//
// Định danh sinh ở nơi tạo, kể cả trên thiết bị hiện trường khi ngoại tuyến.
// Không dùng số tự tăng làm định danh nghiệp vụ.
package ids

import (
	"fmt"

	"github.com/google/uuid"
)

// New sinh một UUIDv7 (có thứ tự theo thời gian).
func New() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		// Chỉ xảy ra khi nguồn ngẫu nhiên của hệ điều hành hỏng.
		panic(fmt.Sprintf("ids: không sinh được UUIDv7: %v", err))
	}
	return id
}

// Parse đọc UUID và bắt buộc là phiên bản 7 — định danh do client gửi lên
// (thiết bị ngoại tuyến) phải đúng quy ước.
func Parse(s string) (uuid.UUID, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, fmt.Errorf("ids: %q không phải UUID: %w", s, err)
	}
	if id.Version() != 7 {
		return uuid.Nil, fmt.Errorf("ids: %q là UUID phiên bản %d, cần phiên bản 7", s, id.Version())
	}
	return id, nil
}

// ParseRef đọc mã tham chiếu xuyên module (R-06): UUID bất kỳ phiên bản, khác Nil.
func ParseRef(s string) (uuid.UUID, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, fmt.Errorf("ids: %q không phải UUID: %w", s, err)
	}
	if id == uuid.Nil {
		return uuid.Nil, fmt.Errorf("ids: mã tham chiếu rỗng")
	}
	return id, nil
}

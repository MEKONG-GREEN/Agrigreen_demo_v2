package app

// W11.23 (Q-072 — đề xuất của dev: Contact thay danh bạ trực pf_extension.staff): tên hiển thị của cán bộ theo tài khoản
// (actor_ref) đọc từ Contact — hồ sơ nhân sự (tài khoản gắn) + kho định danh — thay cột display_name dạng rõ của
// pf_extension.staff. Cùng chữ ký với party/port.ActorNames (gốc lắp ráp nối, không import chéo module).

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/contact/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/contact/port"
)

// DisplayNames trả họ tên của các tài khoản refs có hồ sơ nhân sự đã gắn tài khoản. Tài khoản không có hồ sơ: không có
// trong kết quả (bên gọi dùng nguồn dự phòng). Cần Principal trong ctx (kho kiểm vai trên token người dùng); nguồn cán bộ
// tắt (AG_HRM_RECORDS) → map rỗng.
func (s *Service) DisplayNames(ctx context.Context, refs []uuid.UUID) (map[uuid.UUID]string, error) {
	out := map[uuid.UUID]string{}
	if len(refs) == 0 || s.Staff == nil {
		return out, nil
	}
	if _, err := s.who(ctx); err != nil {
		return out, err
	}
	st, err := s.Staff.Staff(ctx)
	if err != nil {
		if errors.Is(err, port.ErrStaffDisabled) {
			return out, nil
		}
		return out, err
	}
	want := map[uuid.UUID]bool{}
	for _, r := range refs {
		want[r] = true
	}
	byAcc := map[uuid.UUID]uuid.UUID{}
	var subjects []uuid.UUID
	for _, e := range st {
		if e.AccountRef != uuid.Nil && want[e.AccountRef] {
			byAcc[e.AccountRef] = e.SubjectRef
			subjects = append(subjects, e.SubjectRef)
		}
	}
	cards, err := s.cards(ctx, subjects, domain.SetBasic)
	if err != nil {
		return out, err
	}
	for acc, subj := range byAcc {
		if c, ok := cards[subj]; ok && !c.Erased && c.FullName != "" {
			out[acc] = c.FullName
		}
	}
	return out, nil
}

package app

import (
	"context"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntslivestock/store"
	knport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
)

// Species là một vật nuôi của danh mục FN-06 (tên, thứ tự hiển thị).
type Species struct {
	Code, Name string
	Order      int
}

// Tally là số hộ, số con của hộ Đang dùng tại lúc đọc — nguồn bảng "theo loại chăn nuôi" của báo cáo định kỳ (FN-16 BR-07,
// ADR-0056 mục 9).
type Tally struct {
	Types   []store.TypeCount
	Heads   []store.HeadCount
	Species map[string]Species // mã vật nuôi → tên, thứ tự (mã không còn trong danh mục: vắng)
}

// ActiveTally đọc Tally của tenant tid trong các xã communes (nil = mọi xã). Không kiểm quyền — chỉ cho bên gọi trong
// pf/extension đã kiểm quyền của mình (báo cáo phòng BM-11). Tên vật nuôi tra NGOÀI giao dịch; lỗi tra cứu không chặn đọc.
func (s *Service) ActiveTally(ctx context.Context, tid uuid.UUID, communes []string) (Tally, error) {
	var t Tally
	err := s.tx(ctx, tid, func(tx pgx.Tx) error {
		var e error
		t.Types, t.Heads, e = store.ActiveTally(ctx, tx, communes)
		return e
	})
	if err != nil {
		return Tally{}, err
	}
	t.Species = map[string]Species{}
	if s.Catalogs != nil {
		if items, err := s.Catalogs.ListAt(ctx, knport.CatalogCNTSSpecies, s.today()); err == nil {
			for i, it := range items {
				o, err := strconv.Atoi(it.Attrs["order"])
				if err != nil {
					o = 1000 + i
				}
				t.Species[it.Code] = Species{Code: it.Code, Name: it.Name, Order: o}
			}
		}
	}
	return t, nil
}

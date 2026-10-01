package domain

// Ngưỡng gom nhóm k cho sản phẩm dữ liệu collection_volume v1 (G5, g5-conventions §6, §1.2;
// C-3 / Q-MKG-11 — k tạm 10, chờ khách).
//
// Quy tắc "publicdata.kanon.v1":
//
//  1. Ô = (tỉnh × xã × vụ × tháng). Số nông hộ phân biệt của ô = số household_ref khác nhau của các
//     thửa đóng góp (thửa chưa gán nông hộ không tính vào k nhưng vẫn góp số cuộn/diện tích).
//  2. Ẩn sơ cấp: ô có < k nông hộ phân biệt bị bỏ.
//  3. Ẩn bổ sung (complementary): một ô bị ẩn duy nhất trong một nhóm có thể suy ra bằng phép trừ
//     nếu tổng của nhóm được biết (báo cáo khác, sản phẩm khác, hay chính số liệu công bố của bên cấp).
//     Hai chiều tổng được coi là có thể biết:
//       - hàng:  (tỉnh × vụ × tháng) — mọi xã của một tỉnh trong tháng;
//       - cột:   (xã × vụ) — mọi tháng của một xã trong vụ.
//     Nhóm nào có ĐÚNG MỘT ô bị ẩn và ít nhất một ô công bố → ẩn thêm ô công bố "nhỏ nhất" của nhóm
//     (ít nông hộ nhất, rồi ít cuộn nhất, rồi theo khóa). Lặp tới điểm bất động (ẩn ở chiều này có
//     thể tạo nhóm một-ô-ẩn ở chiều kia). Tất định: duyệt nhóm theo khóa đã sắp.
//  4. Ẩn tính trên TOÀN BỘ dữ liệu của bên cấp, không theo bộ lọc của lời gọi — đổi bộ lọc (vụ, tỉnh,
//     khoảng tháng) không làm lộ ô bị ẩn bằng cách so hai lời gọi. Bộ lọc chỉ chọn ô sau khi ẩn.
//
// Ô bị ẩn không bao giờ xuất hiện (kể cả trong tệp xuất); chỉ đếm vào Suppressed.

import "sort"

// KAnonRule là phiên bản quy tắc ẩn ô.
const KAnonRule = "publicdata.kanon.v1"

// VolumeCell là một ô tổng hợp trước khi ẩn. Households chỉ dùng để đếm — không bao giờ trả ra.
type VolumeCell struct {
	ProvinceCode string
	CommuneCode  string
	SeasonCode   string
	PeriodMonth  string
	Households   int64
	Rolls        int64
	Suppressed   bool
}

// Key là khóa sắp xếp / con trỏ của ô.
func (c VolumeCell) Key() string {
	return c.ProvinceCode + "|" + c.CommuneCode + "|" + c.SeasonCode + "|" + c.PeriodMonth
}

func smaller(a, b VolumeCell) bool {
	if a.Households != b.Households {
		return a.Households < b.Households
	}
	if a.Rolls != b.Rolls {
		return a.Rolls < b.Rolls
	}
	return a.Key() < b.Key()
}

// Suppress áp quy tắc KAnonRule tại chỗ cho cells (đã đủ toàn bộ dữ liệu của bên cấp) với ngưỡng k.
// Trả số ô bị ẩn.
func Suppress(cells []VolumeCell, k int64) int {
	for i := range cells {
		cells[i].Suppressed = cells[i].Households < k
	}
	groupings := []func(VolumeCell) string{
		func(c VolumeCell) string { return "r|" + c.ProvinceCode + "|" + c.SeasonCode + "|" + c.PeriodMonth },
		func(c VolumeCell) string { return "c|" + c.CommuneCode + "|" + c.SeasonCode },
	}
	for changed := true; changed; {
		changed = false
		for _, key := range groupings {
			groups := map[string][]int{}
			for i, c := range cells {
				g := key(c)
				groups[g] = append(groups[g], i)
			}
			names := make([]string, 0, len(groups))
			for g := range groups {
				names = append(names, g)
			}
			sort.Strings(names)
			for _, g := range names {
				hidden := 0
				var pick *VolumeCell
				for _, i := range groups[g] {
					c := &cells[i]
					if c.Suppressed {
						hidden++
						continue
					}
					if pick == nil || smaller(*c, *pick) {
						pick = c
					}
				}
				if hidden == 1 && pick != nil {
					pick.Suppressed = true
					changed = true
				}
			}
		}
	}
	n := 0
	for _, c := range cells {
		if c.Suppressed {
			n++
		}
	}
	return n
}

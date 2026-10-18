package main

// lockGrandfathered: tệp migration đã áp trước quy tắc khóa (CheckLocking) — vi phạm (ALTER TABLE
// bảng đã có không SET LOCAL lock_timeout; ADD CONSTRAINT CHECK/FOREIGN KEY không NOT VALID) được
// miễn theo số vì tệp đã áp không được sửa (R-24). Danh sách ĐÓNG: không thêm số mới — migration
// mới phải theo mẫu NOT VALID + VALIDATE CONSTRAINT và SET LOCAL lock_timeout.
//
// Ghi chú rủi ro khi có dữ liệu thật (đã áp ở môi trường dev/UAT, bảng còn nhỏ):
//   - pf 000018, 000046, 000079, 000080; mkg 000009, 000020: ADD CONSTRAINT CHECK trên bảng sổ
//     phân vùng (quét mọi phân vùng dưới ACCESS EXCLUSIVE);
//   - pf 000028, 000029; mkg 000006; shared 000004: CHECK trên bảng thường;
//   - các tệp còn lại: chỉ thiếu lock_timeout (ADD COLUMN, DROP CONSTRAINT, …).
//
// pf 000085/000086 đổi CHECK trong khối DO (EXECUTE động) — CheckLocking không thấy được.
var lockGrandfathered = map[string]map[int64]bool{
	"shared": set(2, 4, 5, 6),
	"pf":     set(18, 19, 28, 29, 32, 46, 77, 79, 80, 89),
	"mkg":    set(6, 9, 10, 20),
}

func set(ns ...int64) map[int64]bool {
	m := make(map[int64]bool, len(ns))
	for _, n := range ns {
		m[n] = true
	}
	return m
}

package assembly

import (
	"fmt"
	"strings"
)

// EnvConsumers chọn consumer của bản worker-internal (RB-5: tách đơn vị triển khai bằng cấu hình, không sửa mã —
// g4-conventions §1): danh sách tên hàng đợi phân tách dấu phẩy, hoặc "all" (mặc định, rỗng = all).
const EnvConsumers = "AG_WORKER_INTERNAL_CONSUMERS"

// Select trả consumer theo danh sách tên hàng đợi (thứ tự của Consumers). Tên lạ / lặp / danh sách rỗng → lỗi
// (worker dừng khởi động thay vì chạy thiếu consumer âm thầm).
func Select(list string) ([]Consumer, error) {
	list = strings.TrimSpace(list)
	if list == "" || list == "all" {
		return append([]Consumer(nil), Consumers...), nil
	}
	want := map[string]bool{}
	for _, n := range strings.Split(list, ",") {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		if want[n] {
			return nil, fmt.Errorf("%s: consumer %q lặp", EnvConsumers, n)
		}
		want[n] = true
	}
	var out []Consumer
	for _, c := range Consumers {
		if want[c.Name] {
			out = append(out, c)
			delete(want, c.Name)
		}
	}
	for n := range want {
		return nil, fmt.Errorf("%s: không có consumer %q", EnvConsumers, n)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s không chọn consumer nào", EnvConsumers)
	}
	return out, nil
}

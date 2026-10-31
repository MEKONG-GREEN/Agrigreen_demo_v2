//go:build race

package domain

// raceEnabled: bản build -race. sync.Pool dưới race detector cố ý bỏ ngẫu nhiên phần tử khi Put, nên máy so khớp của
// regexp (lấy từ pool) bị cấp phát lại ngẫu nhiên — số byte cấp phát không còn là thước đo ổn định.
const raceEnabled = true

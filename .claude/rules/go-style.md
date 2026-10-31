---
paths:
  - "internal/**/*.go"
  - "cmd/**/*.go"
  - "tools/**/*.go"
---
# Go: style, lỗi, log

Nguồn: `.golangci.yml`, `Makefile`, AGENTS.md §8, mã hiện có. Khi lệch, cấu hình lint thắng.

- `gofmt` (CI kiểm `gofmt -l` rỗng), `go vet`, `golangci-lint` (standard + `gosec`, `errorlint`, `bodyclose`, `depguard`, `forbidigo`).
- **Log:** `log/slog` có cấu trúc, logger truyền vào qua `Deps`/tham số. Cấm `fmt.Print*`, `print`, `println` trong mã sản phẩm (`forbidigo`, R-91). Không log PII, token, mật khẩu, khóa.
- **Lỗi:** bọc bằng `fmt.Errorf("…: %w", err)`, so sánh bằng `errors.Is` / `errors.As` (`errorlint`). Lỗi ra HTTP đi qua `problem`; lỗi thô không lộ ra client.
- **HTTP client/response:** luôn đóng `Body` (`bodyclose`).
- **Chú thích tiếng Việt**, giải thích vì sao và dẫn mã quy tắc/ADR/gói (vd `// R-12: …`, `// ADR-0028`, `// P0-WEB-IDEMKEY (#129)`). Chú thích gói (`// Package <tên> …`) mô tả vai trò module.
- Tên định danh tiếng Anh theo mã hiện có; thuật ngữ nghiệp vụ giữ đúng tên đã dùng trong module (vd `Org`, `Plot`, `Season`, `KN`, `HTX`, `KHTC`, `CNTS`).
- Không panic ở đường chạy bình thường; dependency thiếu → trả lỗi rõ (vd 503 `<module>.dependency_unavailable`) hoặc dừng khởi động.

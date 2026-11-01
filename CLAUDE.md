@AGENTS.md

## Claude Code — quy ước viết mã

<!-- Phần riêng cho Claude Code. AGENTS.md vẫn là điểm vào duy nhất; các tệp .claude/rules/ chỉ TÓM TẮT quy ước đã có trong repo theo loại tệp, kèm dẫn nguồn. Khi lệch, tài liệu nguồn thắng — sửa rule cho khớp. -->

- Quy ước chi tiết theo loại tệp nằm trong `.claude/rules/`; tệp có `paths:` tự nạp khi bạn đọc tệp khớp (Go, migration, handler/OpenAPI, test, web, Flutter). Tóm tắt có thể thiếu chi tiết: gặp điểm chưa chắc thì đọc tài liệu nguồn được dẫn ở đầu mỗi tệp rule.
- Trước mọi lệnh `go`: `export GOTOOLCHAIN=$(awk '/^toolchain/{print $2}' go.mod)` (PowerShell: AGENTS.md §8), rồi `go version`.
- Bắt chước module mẫu `internal/platform/party` khi viết module/lớp mới.
- Nhiều quy tắc đã có máy chặn (`archlint`, `migratecheck`, `golangci-lint`, `modgen -check`, `contractcheck`, `docscheck`, `knboundary`). Chạy cổng ở AGENTS.md §8 và sửa theo thông báo; không né cổng bằng cách thêm ngoại lệ.

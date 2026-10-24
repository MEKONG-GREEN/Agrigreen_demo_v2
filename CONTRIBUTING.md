# Đóng góp mã

Bản rút gọn cho người. Quy tắc đầy đủ (cả cho AI agent): [AGENTS.md](AGENTS.md).

> **Giai đoạn chuyển — tới khi [#142](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/142) vào `dev`:** `scripts/workflow/ticket.ps1` chưa có trong repo; dùng bản `ticket.ps1` hiện hành do người điều phối cung cấp (cùng lệnh `claim`/`ready`/`merge`…). Bản đó gộp vào `dev` dưới khóa nhưng **chưa** qua PR, **chưa** chờ CI và **không** tự kiểm bình luận review — người chạy `merge` tự xác nhận đã có `Review OK` đúng SHA. Chi tiết: [giai đoạn chuyển](docs/process/two-tool-workflow.md#giai-đoạn-chuyển-trước-142). Mục 4 bên dưới là quy trình sau #142.

## 1. Nhận việc

1. Chọn issue có nhãn `st:ready` (`gh issue list -R MEKONG-GREEN/Agrigreen_demo_v2 --label st:ready`) đúng làn của mình (`lane:kn` / `lane:shared`, xem [AGENTS.md §1](AGENTS.md#làn-lane--ai-được-ghi-ở-đâu)).
2. Nhận bằng `ticket.ps1 claim -Pkg <PKG> -Agent <…>` (sau #142: `.\scripts\workflow\ticket.ps1`; trước đó bản của người điều phối) hoặc nhờ người điều phối — không nhận gói đang `st:claimed`.
3. Việc mới → tạo issue theo mẫu [work-package](.github/ISSUE_TEMPLATE/work-package.md), tiêu đề `[<PKG>] …`.

## 2. Nhánh và commit

- Nhánh mới từ `origin/dev` cho mỗi ticket, trong worktree riêng. Không làm trên `main`, không xếp chồng nhánh.
- Commit tiếng Việt, tiền tố mã gói: `<PKG>: <việc đã làm>`.
- Migration: `go run ./tools/migratecheck -new <cụm>_<module>_<tên>`; tệp sinh thì chạy lại lệnh sinh ([AGENTS.md §6](AGENTS.md#6-tệp-sinh-ra-không-merge-tay)).

## 3. Kiểm trước khi bàn giao

Chạy cổng ở [AGENTS.md §8](AGENTS.md#8-kiểm-thử-và-cổng). Test DB phải in `--- PASS` (SKIP = trượt). Rà [9 lỗi bắt buộc tránh](AGENTS.md#9-lỗi-bắt-buộc-tránh).

Tài liệu gói: `docs/kn/gia-dinh/<PKG>.md` (nếu có giả định), `docs/roadmap.d/kn-2026-10/<PKG>.md`, mã lỗi mới ở `docs/errors/<cụm>-<module>.md` — xem [package-checklist](docs/dev/package-checklist.md).

## 4. Pull Request và review

1. `ticket.ps1 ready -Pkg <PKG> -Note "<commit; lệnh kiểm; kết quả>"` → `st:review`.
2. Bên khác chủ gói review theo [AGENTS.md §10](AGENTS.md#10-review-chéo) và bình luận `Review OK — <Claude|Codex> (review chéo tại <sha>)` ở đầu dòng (sau #142: `ticket.ps1 review … -Ok`).
3. Sau #142: `ticket.ps1 merge -Agent <bên chạy gộp>` mở PR vào `dev`, chờ CI (fail-closed), gộp ([AGENTS.md §5](AGENTS.md#5-luồng-gộp-qua-pull-request-phương-án-a)). Trước #142: `merge` của bản hiện hành gộp thẳng dưới khóa (xem khung trên). Không push tay vào `dev`/`main`.

## 5. Định nghĩa "xong"

- [ ] Đạt tiêu chí nghiệm thu trong issue.
- [ ] Cổng tĩnh xanh; test DB của gói và gói tiêu thụ trực tiếp `--- PASS`.
- [ ] Không vi phạm mục 9 của AGENTS.md; không bí mật, không đường dẫn máy.
- [ ] Tài liệu gói + roadmap; `go run ./tools/docscheck` xanh.
- [ ] Có "Review OK" của bên khác chủ gói đúng SHA đầu PR (trước #142: đúng SHA đầu nhánh); CI xanh (sau #142 script kiểm); gộp vào `dev`; issue `st:done`.

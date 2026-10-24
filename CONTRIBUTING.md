# Đóng góp mã

Bản rút gọn cho người. Quy tắc đầy đủ (cả cho AI agent): [AGENTS.md](AGENTS.md).

## 1. Nhận việc

1. Chọn issue có nhãn `st:ready` (`gh issue list -R MEKONG-GREEN/Agrigreen_demo_v2 --label st:ready`) đúng làn của mình (`lane:kn` / `lane:shared`, xem [AGENTS.md §1](AGENTS.md#làn-lane--ai-được-ghi-ở-đâu)).
2. Nhận bằng `.\scripts\workflow\ticket.ps1 claim -Pkg <PKG> -Agent <…>` (hoặc nhờ người điều phối) — không nhận gói đang `st:claimed`.
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
2. Bên còn lại review theo [AGENTS.md §10](AGENTS.md#10-review-chéo) và bình luận `Review OK — <tên> (review chéo tại <sha>)`.
3. `ticket.ps1 merge` mở PR vào `dev`, chờ CI, gộp ([AGENTS.md §5](AGENTS.md#5-luồng-gộp-qua-pull-request-phương-án-a)). Không push thẳng `dev`/`main`.

## 5. Định nghĩa "xong"

- [ ] Đạt tiêu chí nghiệm thu trong issue.
- [ ] Cổng tĩnh xanh; test DB của gói và gói tiêu thụ trực tiếp `--- PASS`.
- [ ] Không vi phạm mục 9 của AGENTS.md; không bí mật, không đường dẫn máy.
- [ ] Tài liệu gói + roadmap; `go run ./tools/docscheck` xanh.
- [ ] Có "Review OK" đúng SHA đầu PR; CI bắt buộc xanh; gộp vào `dev`; issue `st:done`.

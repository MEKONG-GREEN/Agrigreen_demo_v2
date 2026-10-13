# Dòng roadmap theo gói

Các mục của [../roadmap.md](../roadmap.md) có nhiều gói chạy song song ghi dòng của từng gói vào tệp riêng, để các gói không cùng sửa `roadmap.md` (WH-B, 13/10/2026):

- Thư mục theo mục: `kn-2026-10/` = mục "BRD KN 10/2026".
- Tệp: `<thư mục mục>/<mã gói>.md`, vd `kn-2026-10/W11.42.md`. Nội dung: một hoặc vài dòng danh sách `- [ ] <mã gói>: …` / `- [x] <mã gói> (<ngày>, <migration>): …`, như dòng roadmap cũ. Liên kết tương đối tính từ thư mục của tệp (`../../kn/…`, `../../adr/…`).
- Gói xong thì sửa dòng `- [ ]` của mình thành `- [x]` trong tệp của mình.

`go run ./tools/docscheck` (CI) kiểm: mỗi dòng không trống là một mục danh sách bắt đầu bằng mã gói của tên tệp; liên kết tương đối trỏ tới tệp có thật. Đọc gộp: `cat docs/roadmap.d/kn-2026-10/*.md`. Không có danh sách tệp ở đây.

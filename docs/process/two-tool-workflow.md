# Quy trình hai công cụ: Claude Code + Codex (và bên bảo trì ngoài)

Áp dụng từ 20/10/2026; cập nhật 24/10/2026 theo phương án (a) gộp qua Pull Request ([#142](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/142)). Tóm tắt nằm ở [AGENTS.md §3–§5, §10](../../AGENTS.md); tệp này ghi lý do và chi tiết vận hành.

Bối cảnh: hai công cụ AI (Claude Code, Codex) cùng làm trên một repo, có thể cùng máy và chung `.git` (mỗi gói một worktree), chung một tài khoản GitHub. Bên bảo trì ngoài và dev mới theo cùng quy trình.

## Giai đoạn chuyển (trước #142)

Áp dụng **tới khi đủ ba điều kiện**: [#142](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/142) đã vào `dev`, CI trên `dev` xanh, và người dùng đã xác nhận bật bảo vệ nhánh. #142 gộp xong **chưa** đủ để chuyển. Mục 3–4 dưới đây mô tả quy trình sau khi chuyển; trước đó:

- Trước khi #142 gộp, `scripts/workflow/` chưa có trong repo (`.\scripts\workflow\ticket.ps1` sẽ báo không tìm thấy tệp); sau khi gộp mà chưa chuyển thì vẫn chưa dùng bản trong repo. Dùng bản `ticket.ps1` hiện hành do **người điều phối** cung cấp, nằm ngoài repo; hỏi người điều phối vị trí trên máy của mình.
- Bản hiện hành có các lệnh `list`, `claim`, `ready`, `release`, `merge`, `lock-status` (cùng tham số như mục 3, kể cả `-Agent`; trừ: chưa có `review`).
- `merge` của bản hiện hành: giữ khóa `lock/integration`, rebase lên `origin/dev`, chạy cổng cục bộ và test `-Tests`, rồi **chính script** gộp vào `dev`. Nó **chưa** mở PR, **chưa** chờ CI và **không** kiểm bình luận review/SHA. Vì vậy người chạy `merge` phải tự xác nhận trên issue đã có `Review OK — <bên khác chủ gói> (review chéo tại <sha>)` đúng SHA đầu nhánh sẽ gộp, và CI trên `dev` sau khi gộp phải được theo dõi.
- Không push tay vào `dev`/`main` trong mọi giai đoạn.
- `dev` chưa bật bảo vệ nhánh; chỉ bật khi #142 đã vào `dev`, CI trên `dev` xanh và người dùng xác nhận — cũng là lúc chuyển sang luồng PR.

## 1. Ticket = GitHub Issue

- Mỗi gói một issue, tiêu đề bắt đầu `[<PKG>]`, tạo theo [mẫu](../../.github/ISSUE_TEMPLATE/work-package.md). Nhãn: `kn`, `g1`, `phong:*`, `size:*`, `lane:kn|shared`, `agent:claude|codex`, trạng thái `st:ready → st:claimed → st:review → st:merging → st:done` (+ `st:blocked`).
- Issue là **nguồn sự thật duy nhất** về ai làm gì. Nhật ký riêng của từng công cụ (nếu có) không thay issue.
- Ticket con (vd cần sửa ngoài làn) tạo bằng `gh issue create` theo cùng mẫu tiêu đề và nhãn, ghi rõ cần gì và vì sao.

## 2. Làn

| Làn | Chủ hiện tại | Được ghi |
|---|---|---|
| `lane:shared` | Codex | `internal/shared/authz`, `internal/platform/{accounts,party,forms,masterdata,notify,reports}`, `cmd/*/assembly` |
| `lane:kn` | Claude Code | `internal/kn/*`, module mới của KN, màn KN trong `apps/web-ops`, `docs/kn/*` |

- Cần sửa ngoài làn → mở **ticket con** cho làn kia; không tự sửa.
- Ngoại lệ nhỏ (một dòng đăng ký) hoặc tệp không thuộc làn nào (gốc repo, `.github/`, `scripts/workflow/`): ghi "NGOẠI LỆ LÀN" trong issue, bên còn lại review.
- Bên bảo trì ngoài nhận làn theo từng issue (nhãn `lane:` của issue).

## 3. Lệnh ticket

Script: `scripts/workflow/ticket.ps1` (PowerShell, chạy từ gốc repo) — **có từ khi #142 vào `dev`**; trước đó xem [giai đoạn chuyển](#giai-đoạn-chuyển-trước-142).

```powershell
.\scripts\workflow\ticket.ps1 list    -Lane shared
.\scripts\workflow\ticket.ps1 claim   -Pkg W11.26 -Agent codex     # exit 3 nếu bên kia đã nhận
.\scripts\workflow\ticket.ps1 ready   -Pkg W11.26 -Note "commit abc; go test -v …: --- PASS"
.\scripts\workflow\ticket.ps1 review  -Pkg W11.26 -Agent claude -Sha <sha> -Ok      # bên khác chủ gói
.\scripts\workflow\ticket.ps1 release -Pkg W11.26 -Note "lý do"
.\scripts\workflow\ticket.ps1 merge   -Pkg W11.26 -Agent codex -Worktree w1126 -Tests "./internal/kn/hrm/...,./tests/kn77"
.\scripts\workflow\ticket.ps1 lock-status
```

- **Nhận nguyên tử.** `claim` push một commit mới lên `refs/heads/claim/<PKG>` với lease rỗng (chỉ tạo, không ghi đè) → hai bên không thể cùng nhận. Bài học 21/10/2026: push cùng một SHA báo "Everything up-to-date" khiến hai agent cùng tưởng mình giữ khóa; vì vậy mỗi lần nhận tạo commit riêng (người giữ + thời điểm).
- Sau khi nhận: tạo worktree từ `origin/dev` (xem [AGENTS.md §4](../../AGENTS.md#4-nhánh-và-commit)).
- **Gói treo:** nhánh không có commit mới quá 12 giờ → bên kia bình luận trên issue, rồi được `release` + `claim` lại.

## 4. Gộp: khóa tích hợp + Pull Request

`ticket.ps1 merge` là đường gộp **duy nhất** (trước khi chuyển luồng PR là bản hiện hành, xem [giai đoạn chuyển](#giai-đoạn-chuyển-trước-142); các bước 3, 5–7 dưới đây chỉ có sau khi chuyển luồng PR):

1. Tạo nguyên tử `refs/heads/lock/integration` (cùng cơ chế với claim). Đang có người giữ → exit 3, thử lại sau. Khóa luôn được nhả (kể cả khi lỗi), và chỉ nhả khi còn trỏ đúng commit của mình.
2. Rebase lên `origin/dev`. Xung đột tài liệu → giữ cả hai phía; tệp sinh → lấy một phía rồi sinh lại; xung đột mã khác → dừng, chủ gói tự giải rồi chạy lại.
3. Kiểm bình luận `Review OK — <công cụ> (review chéo tại <sha>)` của **bên khác chủ gói** (nhãn `agent:`) khớp SHA đầu PR (hoặc SHA trước rebase khi nội dung tệp nguồn của gói không đổi).
4. Cổng tĩnh (đúng toolchain Go của CI) + test DB thật của `-Tests` + cổng migration (`migratecheck -base origin/dev`).
5. Push nhánh PR, mở PR vào `dev`.
6. Chờ CI fail-closed (mọi check phải `pass`; không có check/check bị bỏ qua/lỗi đọc → dừng), đọc lại cổng review, rồi `gh pr merge --merge --match-head-commit <sha>`.
7. Thành công → `st:done`, đóng issue, xóa `claim/<PKG>`, ghi SHA dev mới vào issue. Thất bại → `st:review` + đuôi log.

Chi tiết vận hành (sau khi chuyển luồng PR): `scripts/workflow/README.md`.

Vì sao PR thay vì `git push <nhánh>:dev` (trước 24/10): để bật **bảo vệ nhánh** `dev`/`main` (bắt buộc PR, CI xanh, cấm push thẳng/force-push, áp cả admin). Vì hai công cụ chung một tài khoản GitHub, GitHub không thể bắt "approval của người khác"; cổng review do script kiểm bằng bình luận theo SHA.

Không chạy script rebase/gộp cấp thấp trực tiếp (bị từ chối khi không có khóa tích hợp).

## 5. Review chéo

- Từ 23/10/2026 **mọi gói**, kể cả gói gọn trong một làn, phải được bên còn lại đọc diff và bình luận "Review OK" trước khi gộp. Checklist: [AGENTS.md §10](../../AGENTS.md#10-review-chéo).
- Từ 24/10/2026: bên review có thể **gộp luôn** gói của bên kia sau khi chính mình đã ghi "Review OK". Gặp xung đột mã thật thì không tự viết lại — trả về chủ gói.
- Nhánh xếp chồng (B dựa trên A chưa gộp): gộp A trước; với B dùng `git rebase --onto origin/dev <SHA gốc A> <B>`, chạy lại cổng, review lại tại SHA mới.

## 6. Tài nguyên chung

- PostgreSQL dùng chung trên một máy: mỗi công cụ chỉ một lượt `go test` nặng cùng lúc trên cụm chung; cần nhiều hơn → `scripts/pg-ephemeral.ps1` (cụm riêng, cổng riêng, tự xóa).
- Không dừng/khởi động lại cụm dùng chung; không xóa database mình không tạo; không đụng worktree của gói khác hay checkout chính đang có thay đổi chưa commit của người khác.
- Giới hạn số agent chạy song song do người điều phối đặt ngoài repo.

## 7. Đọc trước khi làm gói

[AGENTS.md](../../AGENTS.md), [package-rules.md](package-rules.md), issue của gói (tiêu chí nghiệm thu, làn), ADR/đặc tả được issue dẫn tới.

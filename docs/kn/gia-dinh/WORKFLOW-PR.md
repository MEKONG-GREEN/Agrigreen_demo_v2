## WORKFLOW-PR — ticket.ps1 gộp qua Pull Request thay vì push thẳng dev (#142)

Phương án (a) người dùng chọn 24/10/2026. Chi tiết vận hành: `scripts/workflow/README.md`.

| Câu hỏi / mục | Lựa chọn đã dùng | Ở đâu | Đổi thế nào |
|---|---|---|---|
| Bước cuối của `merge` | Đẩy `pr/<PKG>` (`--force-with-lease`), tạo hoặc dùng lại PR vào `dev`, chờ check, `gh pr merge --merge --match-head-commit <sha> --delete-branch`; bỏ hẳn `git push <nhánh>:dev`, không có công tắc push thẳng khẩn cấp | `rebase-merge.ps1` | — |
| Kiểu gộp PR | Merge commit (không squash/rebase) để giữ SHA gói cho nhánh xếp chồng `rebase --onto <sha cũ>` | `rebase-merge.ps1` | `--squash` nếu bỏ nhánh xếp chồng |
| Cổng review khi dùng chung một tài khoản GitHub | Bình luận `Review OK — <Claude/Codex> (review chéo tại <sha>)` của BÊN KIA trên issue hoặc PR; bên review nhận từ chữ trong bình luận; `CHANGES` mới nhất vô hiệu `OK` cũ hơn; bình luận của chính bên gộp bị bỏ qua | `workflow-lib.ps1` `Resolve-AgReviewGate` | Bắt buộc approval GitHub khi hai công cụ có hai tài khoản |
| SHA review khác SHA đầu PR vì rebase | Vẫn nhận khi phần thay đổi ngoài tệp sinh (diff `-U0` từ merge-base với `origin/dev`, bỏ dòng `index`/`@@`) giống hệt; ngược lại dừng (mã 4) và yêu cầu review lại ở SHA mới | `Test-AgReviewCovers` | So theo `git range-diff` nếu cần nhận cả rebase đổi ngữ cảnh |
| Vị trí cổng review | Kiểm sơ bộ trước khi lấy khóa (phán quyết mới nhất là OK); kiểm chính xác ngay sau rebase/sinh lại, trước cổng cục bộ và mọi thao tác từ xa | `ticket.ps1`, `rebase-merge.ps1` | — |
| Repo chưa có check bắt buộc (trước #132) | Chờ mọi check báo trên commit đầu PR; không có check nào sau 180 giây → cảnh báo, đi tiếp; check đỏ → dừng trừ khi khớp `-AllowFailingChecks` | `rebase-merge.ps1` | Khi đã bật bảo vệ nhánh, chỉ các check bắt buộc được chờ |
| Thời gian chờ check | 90 phút, giữ khóa gộp trong lúc chờ | `AG_PR_CHECKS_TIMEOUT_MIN` | Giảm khi CI nhanh hơn |
| Đường dẫn máy | Repo từ vị trí script (`--git-common-dir`); worktree `AG_WORKTREE_ROOT` (mặc định `%USERPROFILE%\agwt`); repo GitHub `gh repo view` hoặc `AG_GH_REPO`; log/báo cáo `AG_HANDOFF_DIR` (mặc định `kn-handoff` cạnh repo) | `Get-AgContext`, `progress-report.sh` | Biến môi trường |
| Bản `kn-handoff/` | Giữ nguyên tới khi gói này vào `dev`; sau đó chuyển theo README | — | — |
| Bảo vệ nhánh `dev`/`main` | Chỉ đề xuất JSON (PR bắt buộc, 0 approval, check bắt buộc = job CI, cấm force-push/xóa, áp cả admin, `strict: false`, không bắt lịch sử thẳng); CHƯA áp dụng, chờ #132 và người dùng duyệt | `scripts/workflow/README.md` | — |
| `-DryRun` | Không lấy khóa, không đổi nhãn, không push/PR/gộp/bình luận; rebase thử trong worktree tạm dưới `%TEMP%`, cổng review chạy thật (chỉ đọc) | `ticket.ps1`, `rebase-merge.ps1` | — |
| Kiểm thử | `selftest.ps1` (không Pester): phân tích bình luận, so SHA ngắn, bên review, quyết định "rebase chỉ đổi tệp sinh" trên repo git tạm | `scripts/workflow/selftest.ps1` | Chuyển sang Pester khi có |

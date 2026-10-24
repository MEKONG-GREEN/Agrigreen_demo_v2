# scripts/workflow — quy trình ticket hai công cụ (Claude Code, Codex)

Bản trong repo của các script từng nằm ngoài git ở `kn-handoff/` (#142, phương án (a) ngày 24/10/2026). Khác biệt chính:
bước cuối của `ticket.ps1 merge` **tạo Pull Request vào `dev`** và gộp PR đó, không còn `git push <nhánh>:dev`
(không có công tắc push thẳng khẩn cấp). Nhờ vậy có thể bật bảo vệ nhánh `dev`/`main`.

| Tệp | Việc |
|---|---|
| `ticket.ps1` | `list`, `claim`, `ready`, `review`, `release`, `merge`, `lock-status` (khóa `claim/<PKG>`, `lock/integration` tạo-chỉ-một-lần như cũ) |
| `rebase-merge.ps1` | chỉ chạy qua `ticket.ps1 merge`: rebase lên `origin/dev`, gỡ xung đột `.md` (union) và tệp sinh (lấy dev rồi sinh lại), cổng review chéo, cổng cục bộ, PR, chờ check, gộp |
| `workflow-lib.ps1` | hàm dùng chung (phân tích bình luận review, so SHA, quyết định cổng review, ngữ cảnh máy, lệnh in-ở-dry-run) |
| `union.ps1` | gỡ xung đột tài liệu kiểu giữ cả hai bên |
| `progress-report.sh` | báo cáo tiến độ 6 giờ/lần lên issue theo dõi |
| `selftest.ps1` | tự kiểm hàm thuần (không cần Pester, không gọi GitHub): `powershell -NoProfile -ExecutionPolicy Bypass -File scripts\workflow\selftest.ps1` |

## Không còn đường dẫn cứng

| Giá trị | Mặc định | Ghi đè |
|---|---|---|
| Repo | bản checkout chính chứa script (cha của `git rev-parse --git-common-dir`; chạy từ worktree cũng ra bản chính) | `AG_REPO_ROOT` |
| Thư mục worktree gói | `%USERPROFILE%\agwt` | `AG_WORKTREE_ROOT` |
| Repo GitHub | `gh repo view --json nameWithOwner` | `AG_GH_REPO` |
| Log gộp, báo cáo, `prototype-coverage.tsv` | `<cha repo>\kn-handoff` (nếu có), không thì `%TEMP%\ag-workflow` | `AG_HANDOFF_DIR` |
| Thời gian chờ check PR | 90 phút | `-ChecksTimeoutMin`, `AG_PR_CHECKS_TIMEOUT_MIN` |
| Thời gian chờ check xuất hiện trước khi coi là "không có CI" | 180 giây | `AG_PR_CHECKS_GRACE_SEC` |
| Issue theo dõi tiến độ | #65 | `AG_TRACK_ISSUE` |

## Luồng `merge`

```
ticket.ps1 merge -Pkg P -Agent <bên gộp> [-Worktree w] [-Tests a,b] [-DryRun]
```

1. Kiểm sơ bộ (trước khi lấy khóa): phán quyết mới nhất của bên kia trên issue/PR phải là `Review OK` (SHA nào cũng được).
2. Lấy khóa `lock/integration` (tạo-chỉ-một-lần), nhãn `st:review` → `st:merging`.
3. `rebase-merge.ps1`: rebase, gỡ xung đột `.md`/tệp sinh, sinh lại (modgen, `npm run gen:api`, `AG_UPDATE_WEB_FIXTURES=1 TestWebMenuFixture`) — y như cũ.
4. **Cổng review chéo** trên SHA đầu PR cuối cùng (HEAD sau rebase/sinh lại) — trước mọi cổng tốn thời gian và mọi thao tác từ xa. Quy tắc ở dưới.
5. Cổng cục bộ y như cũ: build, vet, `modgen -check`, migratecheck, archlint, knboundary, `-Tests`, cổng migration DB.
6. Đẩy `HEAD` lên `refs/heads/pr/<PKG>` với `--force-with-lease=refs/heads/pr/<PKG>:<giá trị hiện tại>`.
7. `gh pr create --base dev --head pr/<PKG>` (hoặc `gh pr edit` PR đang mở cùng head): tiêu đề `[<PKG>] <tiêu đề issue>`,
   thân có `Refs #n` (KHÔNG `Closes` — script tự đóng issue sau khi gộp), kết quả cổng và dòng Review OK đã dùng.
8. Chờ check: nếu `dev` có check bắt buộc (bảo vệ nhánh hoặc ruleset) → chờ đủ các check đó xanh; nếu chưa có → chờ mọi
   check báo trên commit đầu; không có check nào sau thời gian ân hạn → cảnh báo và đi tiếp. Check đỏ → dừng (mã 5), PR để mở.
   Khi `dev` chưa có check bắt buộc, `-AllowFailingChecks '<regex tên check>'` hạ các check đỏ khớp regex thành cảnh báo.
9. `gh pr merge <pr> --merge --match-head-commit <sha> --delete-branch` — merge commit giữ nguyên SHA của gói, nên nhánh
   xếp chồng vẫn `git rebase --onto origin/dev <sha cũ>` được. Nhánh `pr/<PKG>` bị xóa sau khi gộp; nhánh gói và worktree giữ lại.
10. Kiểm `origin/dev` chứa SHA đầu; xóa `claim/<PKG>`, nhãn `st:done`, bình luận, đóng issue — như cũ. Khóa luôn được trả (finally).

Mã thoát `rebase-merge.ps1`: 0 gộp xong; 1 cổng/kiểm thử/sinh lại; 2 xung đột mã; 4 review; 5 check PR; 6 gộp PR.
Mọi mã khác 0 → `ticket.ps1` trả nhãn `st:review` và bình luận đuôi log như cũ.

`-DryRun`: không lấy khóa từ xa, không đổi nhãn, không push/PR/gộp/bình luận — in từng lệnh `git`/`gh` có tác dụng phụ
(`[dry-run] …`) và lệnh chỉ đọc (`[read] …`). Rebase được chạy thử trong một worktree tạm tách rời dưới `%TEMP%` (nhánh và
worktree của gói không bị đụng) để tính SHA đầu PR thật và chạy thật cổng review; sinh lại và cổng cục bộ chỉ được in.

## Cổng review chéo (một tài khoản GitHub chung)

Claude và Codex dùng chung một tài khoản nên không bắt buộc GitHub approval được. Cổng là bình luận trên **issue hoặc PR**:

```
Review OK — Codex (review chéo tại 1a2b3c4d)
Review CHANGES — Claude (review chéo tại 1a2b3c4d)
```

Dùng `ticket.ps1 review -Pkg P -Agent <bên review> -Sha <sha> -Ok|-Changes [-Note "…"]` để đăng đúng định dạng
(script từ chối tự review khi issue mang nhãn `agent:<bên review>`).

- `-Agent claude` gộp cần review của **Codex** và ngược lại; bên review nhận từ chữ trong bình luận (`— Codex` / `— Claude`).
  Bình luận của chính bên gộp bị bỏ qua.
- Dòng phải đứng đầu dòng (cho phép `**`/`_`); câu giữa dòng hay dòng trích dẫn `>` không tính. Gạch nối `—`, `–`, `-`, `--` đều nhận.
- SHA: 7–64 ký tự hex, khớp khi là tiền tố của SHA đầy đủ (không phân biệt hoa thường).
- Phán quyết `CHANGES` mới nhất của bên review vô hiệu mọi `OK` cũ hơn nó.
- Một `OK` (sau `CHANGES` cuối) **phủ** SHA đầu PR khi:
  - **exact** — SHA review là SHA đầu PR (ngắn hoặc đầy đủ); hoặc
  - **equivalent** — rebase/sinh lại không đổi nội dung ngoài tệp sinh: so `git diff -U0 merge-base(R, origin/dev)..R` với
    `git diff -U0 merge-base(H, origin/dev)..H`, bỏ tệp sinh (cùng regex với bước gỡ xung đột), bỏ dòng `index`/`@@`
    (phụ thuộc base và số dòng), tệp nhị phân so bằng SHA blob sau. Giống hệt từng ký tự → phủ.
- Không phủ → dừng (mã 4) với thông điệp yêu cầu review lại ở SHA mới và lệnh `ticket.ps1 review … -Sha <đầu mới>` cần chạy.

Hệ quả: review ở SHA đầu nhánh trước khi gộp vẫn dùng được khi `dev` chỉ chạy thêm ở tệp khác, ở dòng xa, hoặc ở tệp sinh;
còn nếu rebase phải gỡ xung đột `.md` làm đổi dòng thêm/bớt của gói, hoặc gói có commit mới, thì phải review lại.

## Chuyển từ `kn-handoff/` sang bản trong repo

Các bản đang chạy ở `C:\Dev\claude-skills-main\kn-handoff\` **giữ nguyên** cho tới khi gói này vào `dev`. Sau đó:

1. Đặt biến môi trường (một lần, cho cả Claude Code và Codex):
   `setx AG_WORKTREE_ROOT C:\Users\manht\agwt` (mặc định đã là `%USERPROFILE%\agwt`; chỉ cần khi khác) và, nếu muốn giữ
   log ở chỗ cũ, `setx AG_HANDOFF_DIR C:\Dev\claude-skills-main\kn-handoff`.
2. Chạy từ đường dẫn repo:
   `powershell -ExecutionPolicy Bypass -File C:\Dev\claude-skills-main\Agrigreen_demo_v2\scripts\workflow\ticket.ps1 <lệnh> …`
   (bản checkout chính phải ở `dev` mới nhất; hoặc chạy từ một worktree đang ở `origin/dev`).
3. `merge` giờ bắt buộc `-Agent` (bên gộp). Bên review dùng `ticket.ps1 review` thay vì gõ tay bình luận.
4. Có thể thay `kn-handoff\ticket.ps1` bằng một dòng chuyển tiếp:
   `& 'C:\Dev\claude-skills-main\Agrigreen_demo_v2\scripts\workflow\ticket.ps1' @args; exit $LASTEXITCODE`.

## Bảo vệ nhánh (đề xuất — CHƯA áp dụng)

Bật sau khi #132 (CI chạy trên `dev` và PR vào `dev`) đã gộp và xanh trên `dev`. Script chạy được cả trước và sau khi bật.
`required_status_checks.checks[].context` = tên job CI (`name:` trong `.github/workflows/ci.yml`); chọn các job ổn định, ví dụ:

```json
{
  "required_status_checks": {
    "strict": false,
    "checks": [
      { "context": "Cổng tĩnh (ranh giới, float, PII, migration, hợp đồng)" },
      { "context": "Kiểm thử (sổ, RLS qua PgBouncer, idempotency)" },
      { "context": "Web vận hành (Next.js)" },
      { "context": "Bảo mật và giấy phép" }
    ]
  },
  "enforce_admins": true,
  "required_pull_request_reviews": {
    "required_approving_review_count": 0,
    "dismiss_stale_reviews": false,
    "require_code_owner_reviews": false
  },
  "restrictions": null,
  "required_linear_history": false,
  "allow_force_pushes": false,
  "allow_deletions": false,
  "required_conversation_resolution": false
}
```

Áp dụng (người dùng duyệt riêng; lặp lại cho `main`):

```
gh api -X PUT repos/MEKONG-GREEN/Agrigreen_demo_v2/branches/dev/protection --input protection-dev.json
```

- `strict: false` (không bắt "nhánh phải cập nhật với base"): script đã rebase dưới khóa gộp; bật lên sẽ buộc chạy lại CI mỗi khi `dev` nhích.
- `required_approving_review_count: 0`: bắt buộc qua PR nhưng không cần approval (một tài khoản không tự approve được); cổng review là bình luận ở trên.
- `required_linear_history: false`: script gộp bằng merge commit.
- `enforce_admins: true`: tài khoản dùng chung là admin, nên phải áp cả admin thì mới chặn được push thẳng.
- Các ref điều phối `claim/*`, `lock/integration`, `pr/*` không bị ảnh hưởng (chỉ bảo vệ `dev`, `main`).

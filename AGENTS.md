# AGENTS.md — hướng dẫn chung cho người và AI agent

Tệp này là **điểm vào duy nhất** cho mọi bên viết mã trong repo: Claude Code, Codex, đơn vị bảo trì ngoài (Ecombox) và dev mới. `CLAUDE.md` ở gốc chỉ trỏ về đây. Mọi quy ước chi tiết nằm ở tài liệu được liên kết; tệp này nói **phải làm gì, theo thứ tự nào, kiểm bằng gì**.

Bản rút gọn cho người: [CONTRIBUTING.md](CONTRIBUTING.md). Quy trình chi tiết: [docs/process/](docs/process/README.md). Chỉ mục quy ước: [docs/conventions/](docs/conventions/README.md).

Mục lục: [1. Bản đồ repo](#1-bản-đồ-repo-và-ranh-giới-module) · [2. Môi trường](#2-môi-trường-phát-triển) · [3. Vòng đời ticket](#3-vòng-đời-ticket) · [4. Nhánh và commit](#4-nhánh-và-commit) · [5. Gộp qua PR](#5-luồng-gộp-qua-pull-request-phương-án-a) · [6. Tệp sinh](#6-tệp-sinh-ra-không-merge-tay) · [7. Migration](#7-migration) · [8. Kiểm thử và cổng](#8-kiểm-thử-và-cổng) · [9. Lỗi bắt buộc tránh](#9-lỗi-bắt-buộc-tránh) · [10. Review chéo](#10-review-chéo) · [Còn thiếu](#còn-thiếu)

---

## 1. Bản đồ repo và ranh giới module

Go modular monolith + PostgreSQL 17/PostGIS + web Next.js + app Flutter. Tổng quan thư mục: [README.md](README.md#cấu-trúc).

| Thư mục | Nội dung |
|---|---|
| `cmd/` | binary (`api-platform`, `api-internal`, `worker`, …); `cmd/*/assembly` ghép module |
| `internal/shared/` | hạ tầng dùng chung: `db`, `tenant`, `authz`, `idempotency`, `outbox`, `problem`, `money`, `modreg`, `pgtest` … |
| `internal/platform/` | module nền tảng/HTX (`accounts`, `party`, `forms`, `masterdata`, `notify`, `reports`, `cooperative`, …) |
| `internal/kn/` | hệ Khuyến nông (`extension`, `hrm`, `price`, `contact`) — tách theo [ADR-0055](docs/adr/0055-tach-he-thong-khuyen-nong.md) |
| `migrations/` | SQL forward-only theo cụm/schema |
| `contracts/` | OpenAPI, view `_pub` (`pubview/`), sự kiện, sản phẩm dữ liệu |
| `apps/web-ops`, `apps/web-internal` | web vận hành / nội bộ MKG (Next.js — đọc [apps/web-ops/AGENTS.md](apps/web-ops/AGENTS.md)) |
| `apps/field-app`, `packages/agrigreen_mobile` | Flutter |
| `tools/` | cổng CI: `archlint`, `modgen`, `migratecheck`, `contractcheck`, `knboundary`, `docscheck`, `mfaflagcheck`, `docsql` |
| `scripts/` | môi trường dev, sinh mã web; `scripts/workflow/` chứa script ticket/gộp (sau [#142](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/142)) |
| `docs/` | ADR, quy ước, runbook, UAT, roadmap |

Quy tắc ranh giới (CI chặn):
- Mỗi module có `module.yaml` sinh bằng modgen; chỉ import module khác qua **port**; đọc dữ liệu module khác chỉ qua view `_pub` có hợp đồng trong `contracts/pubview/`. Chi tiết: [docs/dev/modules.md](docs/dev/modules.md), [docs/dev/g1-conventions.md](docs/dev/g1-conventions.md).
- KN là hệ độc lập về logic: không gọi in-process vào module HTX, không ghi dữ liệu HTX; điểm dính KN trong module dùng chung chỉ được **giảm** (`go run ./tools/knboundary`). Xem [ADR-0055](docs/adr/0055-tach-he-thong-khuyen-nong.md) và [docs/kn/tach-he-thong/kiem-ke.md](docs/kn/tach-he-thong/kiem-ke.md).
- `tools/archlint/modgraph_exceptions.yaml` và baseline `tools/knboundary/baseline.yaml` chỉ được thu nhỏ.

### Làn (lane) — ai được ghi ở đâu

| Nhãn | Phạm vi ghi |
|---|---|
| `lane:shared` | `internal/shared/authz`, `internal/platform/{accounts,party,forms,masterdata,notify,reports}`, `cmd/*/assembly` |
| `lane:kn` | `internal/kn/*`, module mới của KN, màn KN trong `apps/web-ops`, `docs/kn/*` |

Chủ hiện tại của mỗi làn ghi trong [docs/process/two-tool-workflow.md](docs/process/two-tool-workflow.md#2-làn). Cần sửa ngoài làn → mở **ticket con** cho làn kia. Ngoại lệ (vd tệp gốc repo, `.github/`, `scripts/workflow/`) phải ghi rõ "NGOẠI LỆ LÀN" trong issue và nhờ bên còn lại review.

## 2. Môi trường phát triển

| Cách | Tài liệu | Trạng thái |
|---|---|---|
| Docker Compose toàn bộ nền tảng | [docs/dev-setup.md](docs/dev-setup.md#cách-1-docker-compose--toàn-bộ-nền-tảng-linux-macos-windows-có-docker-desktop) | dùng trong CI/compose; **chưa kiểm lại trên máy sạch** |
| Windows không Docker (PostgreSQL cài cục bộ) | [docs/dev-setup.md](docs/dev-setup.md#cách-2-windows-không-docker-cách-đã-dùng-để-dựng-g0) | đang dùng hằng ngày |
| Biến môi trường kiểm thử | `. .\scripts\dev-env.ps1` (đọc mật khẩu superuser từ tệp **ngoài repo**, không in ra) | đang dùng |
| Cụm PostgreSQL tạm, riêng cho một lượt test | `.\scripts\pg-ephemeral.ps1 run go test …` (xem đầu tệp script) | đang dùng; khuyên dùng khi chạy song song |
| Web | `apps/web-ops`: `npm ci`, `npm run dev:mock` (không cần backend) | đang dùng |

Không commit bí mật: `.env`, chứng thư, mật khẩu đều sinh bằng `scripts/gen-dev-secrets.*` vào thư mục đã gitignore. Không ghi đường dẫn riêng của máy vào tài liệu — dùng `<repo>`, `<worktree>`, `%LOCALAPPDATA%`.

## 3. Vòng đời ticket

Ticket = **GitHub Issue**, tiêu đề `[<PKG>] …` (vd `[P0-POOL-KN] …`), tạo theo mẫu [.github/ISSUE_TEMPLATE/work-package.md](.github/ISSUE_TEMPLATE/work-package.md). Issue là nguồn sự thật duy nhất về ai làm gì.

| Nhãn | Giá trị |
|---|---|
| `st:` | `ready → claimed → review → merging → done` (+ `blocked`) |
| `lane:` | `kn`, `shared` |
| `agent:` | `claude`, `codex` (người: ghi tên trong bình luận nhận) |
| `phong:` | `tt` (Trồng trọt), `cnts`, `khtc`, `hcth`, `tttt`, `shared` |
| `size:` | `S`, `M`, `L`, `XL` |
| khác | `kn`, `g1` (phạm vi/mốc) |

> **Giai đoạn chuyển — tới khi chuyển sang luồng PR ([#142](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/142) đã vào `dev`, CI trên `dev` xanh, người dùng đã xác nhận bảo vệ nhánh):** `dev` chưa bật bảo vệ nhánh; `scripts/workflow/` dù đã có trong repo cũng chưa dùng. Trong thời gian này dùng bản `ticket.ps1` hiện hành do người điều phối cung cấp (ngoài repo) — xem [two-tool-workflow.md](docs/process/two-tool-workflow.md#giai-đoạn-chuyển-trước-142). Các lệnh và luồng PR dưới đây là quy trình **sau khi chuyển luồng PR**.

Lệnh (từ gốc repo, **sau khi chuyển luồng PR**):

```powershell
.\scripts\workflow\ticket.ps1 list  [-Lane kn|shared]
.\scripts\workflow\ticket.ps1 claim -Pkg <PKG> -Agent claude|codex   # exit 3 nếu đã có người nhận
.\scripts\workflow\ticket.ps1 ready -Pkg <PKG> -Note "<commit; lệnh kiểm; kết quả>"
.\scripts\workflow\ticket.ps1 review -Pkg <PKG> -Agent <bên review> -Sha <sha> -Ok|-Changes [-Note "…"]
.\scripts\workflow\ticket.ps1 release -Pkg <PKG> -Note "<lý do>"
.\scripts\workflow\ticket.ps1 merge -Pkg <PKG> -Agent <bên chạy gộp> -Worktree <thư mục> -Tests "./internal/...,./tests/..."
.\scripts\workflow\ticket.ps1 lock-status
```

- **Nhận** (`claim`): tạo nguyên tử `refs/heads/claim/<PKG>` trên remote → `st:claimed` + `agent:`. Rồi tạo worktree riêng từ `origin/dev`.
- **Sẵn sàng** (`ready`): `st:review` + bình luận bằng chứng (commit, lệnh, `--- PASS`).
- **Gộp** (`merge`): chỉ qua script, giữ khóa `refs/heads/lock/integration` (mục 5). Thành công → `st:done`, đóng issue, xóa `claim/<PKG>`; thất bại → về `st:review` kèm log.
- **Gói treo**: nhánh không có commit mới quá 12 giờ → bình luận trên issue rồi mới `release` + `claim` lại.

## 4. Nhánh và commit

- **Một nhánh mỗi ticket**, tạo từ `origin/dev`, trong worktree riêng: `git fetch origin +refs/heads/dev:refs/remotes/origin/dev` rồi `git worktree add -b <nhánh> <worktree> origin/dev`. Tên nhánh = mã gói viết thường bỏ dấu chấm (`p0poolkn`); Codex dùng tiền tố `codex/`.
- **Chỉ gộp vào `dev`. Không bao giờ push, fast-forward hay sửa `main`** — Tech Lead duyệt `dev → main`; demo triển khai từ `main`.
- Commit message **tiếng Việt**, tiền tố mã gói: `P0-POOL-KN: đọc phiên bản biểu ngoài transaction`. Commit sớm, nhiều lần (sau backend, sau web, sau docs).
- Commit do AI viết kết thúc bằng trailer `Co-Authored-By: <tên model> <noreply@…>` của công cụ đó.
- **Không xếp chồng nhánh** (nhánh B dựa trên nhánh A chưa gộp). Nếu bắt buộc: ghi **SHA gốc** của A trong issue của B; sau khi A gộp, chạy `git rebase --onto origin/dev <SHA gốc A> <nhánh B>` rồi chạy lại toàn bộ cổng.

## 5. Luồng gộp qua Pull Request (phương án (a))

Chốt ngày **24/10/2026** (ticket [#142 WORKFLOW-PR](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/142)). **Chỉ có hiệu lực khi đủ ba điều kiện: #142 đã vào `dev`, CI trên `dev` xanh, và người dùng đã xác nhận bật bảo vệ nhánh**; trước đó (kể cả khi #142 đã gộp nhưng chưa đủ hai điều kiện sau) xem [giai đoạn chuyển](docs/process/two-tool-workflow.md#giai-đoạn-chuyển-trước-142) — bản hiện hành gộp dưới khóa nhưng chưa qua PR, chưa chờ CI và không tự kiểm bình luận review. Khi luồng PR có hiệu lực, `ticket.ps1 merge`:

1. Giữ khóa gộp `refs/heads/lock/integration` (tạo nguyên tử; luôn nhả khi xong hoặc lỗi).
2. Rebase nhánh gói lên `origin/dev` (xung đột tài liệu → giữ cả hai; tệp sinh → sinh lại; xung đột mã → dừng, trả về chủ gói).
3. Kiểm **cổng review**: phải có bình luận của bên **khác chủ gói** (chủ gói = nhãn `agent:` của issue, không phụ thuộc ai chạy `merge`) dạng `Review OK — <công cụ> (review chéo tại <sha>)` ở đầu dòng, với `<sha>` là SHA đầu PR — hoặc SHA trước rebase khi nội dung các tệp nguồn của gói không đổi. `CHANGES` mới hơn vô hiệu `OK`. Claude và Codex dùng chung một tài khoản GitHub nên GitHub không thể bắt buộc approval; script kiểm bình luận này thay.
4. Chạy cổng cục bộ (mục 8, đúng toolchain Go của CI) và test DB thật của các gói khai báo trong `-Tests`.
5. Push nhánh PR, mở **Pull Request vào `dev`** (mô tả theo [mẫu PR](.github/pull_request_template.md)).
6. Chờ CI — fail-closed: mọi check (bắt buộc, hoặc mọi check báo về khi chưa bật bảo vệ nhánh) phải `pass`; không có check, check bị bỏ qua hay lỗi đọc → dừng.
7. Đọc lại cổng review ngay trước khi gộp, rồi gộp bằng `gh pr merge --merge --match-head-commit <sha>` (đầu PR đổi → từ chối).

**Bảo vệ nhánh** `dev` và `main` (chỉ bật khi #142 đã vào `dev`, CI trên `dev` xanh và người dùng xác nhận): bắt buộc PR, bắt buộc CI xanh, cấm push thẳng, cấm force-push, áp cả admin. Kể cả trước đó: đừng `git push origin <nhánh>:dev` bằng tay.

Chi tiết và lịch sử quy trình: [docs/process/two-tool-workflow.md](docs/process/two-tool-workflow.md).

## 6. Tệp sinh ra: không merge tay

Khi rebase/xung đột ở các tệp dưới đây: lấy một phía bất kỳ rồi **chạy lại lệnh sinh**, commit kết quả.

| Tệp | Lệnh sinh lại |
|---|---|
| `**/module.yaml`, `internal/shared/modreg/*_gen.go` | `go run ./tools/modgen` (kiểm: `-check`) |
| menu/mock/OpenAPI gộp của web (`scripts/gen-module-*.mjs`), `apps/web-ops/src/api/generated/*` | `npm run gen:api` trong `apps/web-ops` (và `apps/web-internal` nếu đổi `mkg-internal.yaml`); kiểm: `npm run check:api` |
| golden menu `apps/web-ops/src/api/mock/fixtures/kn-menu.json` | `AG_UPDATE_WEB_FIXTURES=1 go test ./internal/platform/party/handler -run TestWebMenuFixture` (cần PG thật) |
| baseline knboundary | `go run ./tools/knboundary -prune` (chỉ bỏ mục lỗi thời; không thêm) |

Quy ước đăng ký menu/mock/OpenAPI theo module: [docs/dev/module-registrations.md](docs/dev/module-registrations.md).

## 7. Migration

- Tên theo mốc UTC: `go run ./tools/migratecheck -new <cụm>_<module>_<tên>`. Không tự đặt số.
- **Forward-only**: không sửa tệp đã gộp; không UPDATE/DELETE cấp câu lệnh trong migration.
- Trước khi gộp và sau mỗi lần rebase: `go run ./tools/migratecheck -base origin/dev migrations` — tệp mới phải xếp **sau** migration mới nhất trên dev; nếu không, tạo lại tên bằng `-new` và sửa tham chiếu.
- Sổ append-only qua `shared.apply_ledger` (không UPDATE/DELETE; không ADD COLUMN vào sổ đã có dữ liệu — dùng sổ phụ).
- RLS theo tenant cho mỗi bảng qua `shared.apply_tenant_rls` ([ADR-0005](docs/adr/0005-rls-mot-policy-moi-bang.md)); schema + vai `r_<module>` riêng mỗi module ([modules.md — Vai DB](docs/dev/modules.md#vai-db-theo-module-w23)).
- Một tệp = một schema. Chi tiết: [docs/dev/parallel-packages.md](docs/dev/parallel-packages.md), [docs/dev/package-checklist.md](docs/dev/package-checklist.md).

## 8. Kiểm thử và cổng

Cổng tĩnh Go (từ gốc repo; `make gates` + `make gates-base` gói phần lớn):

```bash
gofmt -l $(git ls-files '*.go' | grep -v /testdata/)    # phải rỗng
go build ./...
go vet ./...
go run ./tools/archlint ./...
go run ./tools/modgen -check
go run ./tools/migratecheck -base origin/dev migrations
go run ./tools/contractcheck
go run ./tools/knboundary -base origin/dev
go run ./tools/docscheck
golangci-lint run ./...
```

Web (`apps/web-ops`, tương tự `apps/web-internal`): `npm run check:api`, `npm run lint`, `npx tsc --noEmit`, `npx vitest run` (hoặc `npm test`), `npm run build:mock`.

Flutter (`apps/field-app`, `packages/agrigreen_mobile`): `dart format --output=none --set-exit-if-changed lib test`, `flutter analyze --fatal-infos`, `flutter test`.

Test DB:
- Nạp `. .\scripts\dev-env.ps1` hoặc dùng `.\scripts\pg-ephemeral.ps1 run go test -count=1 -p 1 -timeout 55m <gói đụng tới + gói tiêu thụ trực tiếp>`.
- Thiếu `AG_TEST_PG_ADMIN_URL` thì test DB **SKIP âm thầm** và `ok` không có nghĩa gì. Xác nhận bằng `go test -v -run <Test>` thấy `--- PASS`. **SKIP tính là trượt.**
- `INFRA: PostgreSQL unavailable` là lỗi hạ tầng, không phải lỗi mã: chờ `pg_isready` rồi chạy lại, hoặc dùng cụm tạm. Không dừng/khởi động lại cụm dùng chung; không xóa database mình không tạo.
- Làm song song nhiều gói → mỗi gói một cụm `pg-ephemeral`.

## 9. Lỗi bắt buộc tránh

Rút ra từ đợt kiểm tra 22–24/10/2026 (báo cáo Ecombox và các ticket P0). Reviewer từ chối PR vi phạm.

| # | Quy tắc | Vì sao | Kiểm bằng |
|---|---|---|---|
| 1 | **Không gọi port module khác, không I/O mạng (email, lưu tệp, KMS/vault, HTTP) bên trong DB transaction.** Đọc trước tx, gửi sau commit hoặc qua outbox. | Mỗi tx giữ một kết nối; lời gọi lồng mở kết nối thứ hai → pool cạn → treo toàn hệ ở 20 CCU. [#125](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/125), [#126](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/126), [#139](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/139) | test pool nhỏ, gọi song song ≥ kích thước pool; review. [Quy ước](docs/conventions/transactions-and-io.md) |
| 2 | **Cấu hình fail-closed**: thiếu SMTP/FCM/URL công khai → kênh tắt, bản gửi `dropped` kèm lý do; cấu hình một phần/sai → không khởi động; bộ giả ghi log chỉ khi đặt rõ `AG_MAILER=log` trên máy phát triển. Không bao giờ ghi "Đã gửi" khi chưa gửi. | `LogMailer` mặc định khiến thông báo báo thành công giả. [#127](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/127) | test fail-closed theo môi trường. [Quy ước](docs/conventions/fail-closed-config.md) |
| 3 | **Một `Idempotency-Key` cho mỗi thao tác người dùng**, sinh khi mở form/bắt đầu thao tác, dùng lại khi thử lại, đổi sau khi thành công. | Sinh khóa mỗi request → bấm Lưu lần hai tạo 409 + bản ghi mồ côi. [#129](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/129) | vitest bấm lại cùng khóa. [Quy ước](docs/conventions/idempotency.md) |
| 4 | **Worker kiểm phụ thuộc khi khởi động** (pool DB, broker, kênh gửi); **nhận việc gửi nguyên tử** (`UPDATE … WHERE status='pending' RETURNING`), trả việc `sending` quá hạn về `pending`. | Worker chạy với DB nil → panic; gửi trùng 6 email; kẹt `sending`. [#128](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/128) | test không gửi trùng, sống qua restart broker |
| 5 | **RLS fail-closed**: policy không được bỏ qua kiểm khi bảng chiếu (`k_ref`, …) rỗng. | Tenant chưa có `k_ref` thì policy cho qua hết. [#130](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/130) | test RLS với chiếu rỗng và có dữ liệu |
| 6 | **Tệp sinh phải sinh lại**, không sửa/merge tay (mục 6). | Merge tay làm lệch golden/registry, CI đỏ trên dev. | `modgen -check`, `npm run check:api`, golden test |
| 7 | **Kiểm số ở server**, không chỉ ở UI (không âm, bắt buộc, phạm vi). | API gọi thẳng vượt kiểm UI. [#113](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/113) | test handler/app với số âm/thiếu trường |
| 8 | **UI dùng bộ UI kit chung** (NumberField, form bắt buộc, DetailPage, DataTable). Tài liệu `docs/kn/ui-kit.md` có sau khi [#97](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/97) gộp. | Mỗi màn tự làm lại → lệch hành vi kiểm nhập. | review |

Quy tắc mã chung khác (cấm float cho tiền/khối lượng, UUIDv7, mã lỗi `problem.*`, PII qua `vault/identity`, `AG_MFA_DISABLED` chỉ ở demo): [docs/process/package-rules.md](docs/process/package-rules.md).

## 10. Review chéo

Từ 23/10/2026 **mọi gói** phải được bên còn lại (bên khác chủ gói, theo nhãn `agent:`) review trước khi gộp. Reviewer:

1. Checkout đúng **SHA đầu PR** (`gh pr checkout <n>`; `git rev-parse HEAD`).
2. Đọc diff toàn bộ, đối chiếu tiêu chí nghiệm thu trong issue và [mục 9](#9-lỗi-bắt-buộc-tránh).
3. Chạy lại tối thiểu: cổng tĩnh của phần bị đụng (mục 8), test DB của gói với `-v` (thấy `--- PASS`), web/Flutter nếu đụng.
4. Kiểm: migration mới xếp sau dev, tệp sinh khớp lệnh sinh, không có bí mật/đường dẫn máy, tài liệu gói (`docs/kn/gia-dinh/<PKG>.md`, `docs/roadmap.d/…/<PKG>.md`).
5. Bình luận trên PR (hoặc issue) một dòng **ở đầu dòng** (không trong khối code hay trích dẫn — ví dụ trong khối code không được tính):

   ```
   Review OK — <Claude|Codex> (review chéo tại <sha đầy đủ>)
   Review CHANGES — <Claude|Codex> (review chéo tại <sha đầy đủ>)
   ```

   Sau khi chuyển luồng PR dùng `ticket.ps1 review -Pkg <PKG> -Agent <bên review> -Sha <sha> -Ok|-Changes` để đăng đúng mẫu. Có vấn đề → `Review CHANGES …` kèm danh sách cần sửa, không ghi "Review OK". Gặp xung đột mã thật thì không tự viết lại, trả về chủ gói.

Reviewer đã ghi "Review OK" có thể chạy `ticket.ps1 merge` cho gói đó.

## Còn thiếu

- **Cài môi trường trên máy sạch chưa kiểm lại** (cả hai cách trong [docs/dev-setup.md](docs/dev-setup.md)). TODO: một người mới chạy từ đầu và ghi lệch.
- **Tài liệu kiến trúc v4.3 chưa có trong repo** (repo chỉ có [v4.1](docs/kien-truc/AgriGreen-Thiet-ke-kien-truc-he-thong-v4_1.md)). TODO: đưa v4.3 vào `docs/kien-truc/` và cập nhật liên kết ở đây.
- Script `scripts/workflow/` và bảo vệ nhánh: phụ thuộc [#142](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/142); trước khi #142 gộp, lệnh ticket chạy từ bản do người điều phối cung cấp ([giai đoạn chuyển](docs/process/two-tool-workflow.md#giai-đoạn-chuyển-trước-142)).
- `docs/kn/ui-kit.md`: phụ thuộc [#97](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/97).
- Chủ sở hữu CODEOWNERS cho Ecombox/Tech Lead: chưa quyết.

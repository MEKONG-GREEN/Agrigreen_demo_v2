# Quy ước phát triển G1

Tài liệu này dành cho mọi người và agent phát triển module G1. Đọc cùng tài liệu kiến trúc v3.0 (`G:\My Drive\Development\AgriGreen-Thiet-ke-kien-truc-he-thong-v3_0.md`) và các ADR trong `docs/adr/`.

## 1. Cấu trúc một module

```
internal/platform/<module>/
├── port/        # interface + DTO công khai — thứ DUY NHẤT module khác được import (R-02)
├── domain/      # thực thể, quy tắc; không phụ thuộc hạ tầng; không time.Now() (R-12)
├── app/         # use case, mở giao dịch bằng tenant.WithTenantTx; kiểm vai và phạm vi
├── store/       # SQL, chỉ dùng pool của vai DB của module (R-05)
├── handler/     # HTTP (Gin) — Application API và Admin API
└── module.go    # New(...) + RegisterApp(rg) + RegisterAdmin(rg) — thỏa httpx.Module
```

Module mẫu là `internal/platform/party` (đồng ý theo mục đích). Chép cách làm của module này.

## 2. Số migration và vai DB đã cấp trước

Không đổi số đã cấp. Mỗi module chỉ tạo tệp migration mang số của mình.

| Cụm | Số | Module | Schema | Vai DB |
|---|---|---|---|---|
| pf | 000001 | party: đồng ý (đã có) | `pf_party` | `party_app` |
| pf | 000002 | masterdata | `pf_masterdata` | `masterdata_app` |
| pf | 000003 | party: tổ chức, HTX, nông hộ | `pf_party` | `party_app` |
| pf | 000004 | geo | `pf_geo` | `geo_app` |
| pf | 000005 | content | `pf_content` | `content_app` |
| pf | 000006 | extension | `pf_extension` | `extension_app` |
| pf | 000007 | mechanization | `pf_mechanization` | `mechanization_app` |
| pf | 000008 | mrv | `pf_mrv` | `mrv_app` |
| pf | 000009 | sync: thiết bị, con trỏ, xung đột | `pf_sync` | `sync_app` |
| pf | 000010 | tệp minh chứng (đối tượng mã hóa) | `pf_files` | `sync_app` |
| pf | 000011 | accounts: tài khoản (cầu nối Keycloak) | `pf_accounts` | `accounts_app` |
| pf | 000012 | notify: thông báo trong app, token đẩy, email | `pf_notify` | `notify_app` |
| pf | 000013 | reports: mẫu biểu, việc xuất, lịch gửi | `pf_reports` | `reports_app` |
| pf | 000014 | content: danh mục cá nhân (LIB-05) | `pf_content` | `content_app` |
| pf | 000015 | mechanization: dự báo nhu cầu (FN-09) | `pf_mechanization` | `mechanization_app` |
| pf | 000016 | extension: bổ sung báo cáo (FR-03) | `pf_extension` | `extension_app` |
| pf | 000017 | party: bổ sung mô hình đọc (G-03/G-04) | `pf_party` | `party_app` |
| vault | 000002 | vault/identity | `vault` | `vault_app` |

- Các vai đã khai trong `internal/shared/dbadmin/dbadmin.go`. **Không sửa tệp đó.** Cần vai mới thì ghi vào báo cáo.
- Migration **chỉ tiến**, không có tệp `.down.sql`. Không được có `UPDATE`, `DELETE`, `TRUNCATE` ở mức câu lệnh (R-25).
- Mọi bảng trong schema nghiệp vụ phải có `tenant_id` và gọi `SELECT shared.apply_tenant_rls('<schema.bảng>')`.
- Bảng sổ (chỉ ghi thêm) theo mẫu mục 8.1, rồi gọi `SELECT shared.apply_ledger('<schema.bảng>', '<schema.bảng>')`. Xem `migrations/pf/000001_pf_party_consent.up.sql`.
- Khóa duy nhất trên bảng sổ không làm được vì bảng phân vùng (ADR-0012). `fn_chain` đã chống trùng `idem_key`.
- Không dùng khóa ngoại xuyên schema, không join xuyên schema. Tham chiếu sang module khác là UUID thuần (R-06).
- Cuối migration: `GRANT` cho vai của module. Với bảng sổ, **không** GRANT UPDATE/DELETE.

## 3. Quy tắc dữ liệu

- Tiền và khối lượng dùng `shared/money`, `shared/mass`; cột `numeric` theo R-10. **Không float** ở mọi module có tiền hoặc khối lượng.
- Diện tích (ha) và tỷ lệ phần trăm dùng `decimal` (`github.com/shopspring/decimal`) và cột `numeric(14,4)`.
- Toạ độ lưu PostGIS `geometry(…, 4326)`.
- Định danh là UUIDv7 (`shared/ids`). Client ngoại tuyến được gửi `id` tự sinh, server kiểm bằng `ids.Parse`.
- Mọi sự việc có `event_time` (lúc xảy ra) và `recorded_at` (lúc ghi nhận) (R-13).
- **Không xóa cứng dữ liệu nghiệp vụ.** "Xóa" là trạng thái ngừng dùng hoặc bản ghi bù, luôn kèm lý do. Yêu cầu nào trong BRD đòi xóa cứng thì đổi theo ADR-0011.
- Dữ liệu cá nhân (họ tên, SĐT, CCCD, địa chỉ của nông hộ, người đại diện, chủ máy) **chỉ nằm trong vault**. Module khác chỉ giữ `*_ref` và gọi `internal/vault/identity/client` (R-04).
  - Trường Go chứa dữ liệu cá nhân phải có tag `pii:"true"`.
  - Không đưa trường đó vào log hay thông báo lỗi (R-14).

## 4. API

- Route Application API gắn qua `RegisterApp(rg)`. Nhóm `/v1` đã có xác thực OIDC và Idempotency-Key cho mọi POST.
- Route quản trị gắn qua `RegisterAdmin(rg)`. Nhóm này bắt buộc MFA.
  - Dữ liệu chủ, tham số và phê duyệt đi qua Admin API.
  - Thay đổi có phê duyệt dùng hai vai `param_setter` và `param_approver`. Người phê duyệt không được là người đặt (R-56): gọi `authz.EnsureDistinctApprover` ở `app/`.
- Kiểm vai bằng `authz.Require(ctx, vai...)`. Tên vai của module khai ở `port/`.
- Phạm vi theo cây TW → Tỉnh → Xã: lấy từ `party/port.ScopeResolver` (ADR-0009).
  - Lọc đọc theo `scope.Units`.
  - Kiểm ghi bằng `scope.Allows(unitCode)`.
- Lỗi trả `problem.*`. Mã lỗi riêng của module có dạng `<module>.<lỗi>`; thêm vào `docs/errors.md` ở mục của module.
- Phân trang bằng cursor (R-53), theo mẫu `ListBySubject` và handler `list` của party.
- Việc chạy quá 2 giây trả `202` + `/v1/jobs/{id}` (R-52): ghi `job` + outbox, worker xử lý. Việc dưới 2 giây làm đồng bộ.
- Sự kiện phát ra có tên dạng `pf.<module>.<thực thể>.<động từ quá khứ>`.
  - Viết schema tại `contracts/events/<tên>.v1.json`.
  - Struct payload trong `port/` có chú thích `//ag:event <tên> v1`.
- Cập nhật `contracts/openapi/application.yaml` hoặc `contracts/openapi/admin.yaml` cho route mới.

## 5. Kiểm thử

- Mỗi gói có kiểm thử tích hợp đặt `func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }`.
  - Lấy pool bằng `pgtest.Get(t).Pool(t, migrations.PF, "<vai>")`.
  - Token giả dùng `internal/shared/authz/authztest`, có `OrgRef` để thử phạm vi.
- **Bắt buộc có kiểm thử cho:**
  - cách ly tenant;
  - phạm vi theo cây phân cấp;
  - tự phê duyệt bị chặn (nếu module có phê duyệt);
  - gọi lặp POST cùng Idempotency-Key;
  - sửa sai bằng bản ghi bù (nếu có sổ);
  - quy tắc nghiệp vụ chính của BRD (ghi mã FN/BR/US trong tên hoặc chú thích kiểm thử).
- Trước khi báo xong, chạy trên Windows PowerShell tại gốc worktree:
  ```powershell
  . .\scripts\dev-env.ps1
  gofmt -l cmd internal tools
  go vet ./...
  go test -count=1 ./...
  go run ./tools/archlint ./...
  go run ./tools/migratecheck migrations
  go run ./tools/contractcheck
  & "$env:USERPROFILE\go\bin\golangci-lint.exe" run ./...
  ```
  Mọi lệnh phải sạch. PostgreSQL 17 + PostGIS chạy sẵn ở localhost:5432; mỗi gói kiểm thử có database riêng nên chạy song song không đụng nhau.

## 6. Không sửa các tệp dùng chung

Các tệp sau do người tích hợp giữ:
- `internal/shared/dbadmin/dbadmin.go`
- `cmd/api-platform/main.go` — gốc lắp ráp DUY NHẤT là `cmd/api-platform/assembly/modules.go`; agent được thêm phần nối module của mình vào đó, giữ thay đổi gọn
- `.github/workflows/ci.yml`
- `deploy/**`
- migration của module khác

Nếu cần đổi `internal/shared/*`, làm thay đổi nhỏ, tương thích ngược, và ghi rõ trong báo cáo. Được thêm dependency bằng `go get` (giấy phép phải thuộc `deploy/policies/allowed-licenses.txt`).

## 7. Tài liệu nguồn yêu cầu

Tất cả trong `G:\My Drive\Development\`:

| App | Tệp |
|---|---|
| App Khuyến nông | `BRD_App Khuyến nông_260729_v1.0` (là tệp .docx), `Backlog_UserStory_App_Khuyen_Nong_v1.0.docx`, `App_Khuyen_Nong_v5.0.html` |
| Bản đồ CGH | `BRD_BanDoCGH_v1.3.docx`, `Backlog_UserStory_BanDoCGH_v4.0.xlsx` |
| GIS | `BRD_GIS_v1.3` (là tệp .docx), `Backlog_UserStory_GIS_v1.1.xlsx`, `Mekong_Green_GIS_v1.3.html` |
| App HTX | `Backlog_UserStory_App_HopTacXa_v4.0.xlsx`, `App Hợp tác xã_v5.1 (1).html` |

Prototype chạy được `C:\Users\manht\Downloads\claude-skills-main\mekong-green` (TypeScript/SQLite) là **đặc tả**. Đọc quy tắc nghiệp vụ và kiểm thử ở đó, nhưng không chép mã.

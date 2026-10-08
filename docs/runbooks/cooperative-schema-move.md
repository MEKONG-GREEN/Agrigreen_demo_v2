# Chuyển dữ liệu HTX/hộ/thửa/vụ sang schema `pf_coop` (W3.3)

Áp **một lần** khi lên bản có W3.3 (pf 000131–000133, [thiết kế](../design/cooperative.md) mục 8). Sau bản này:

- 22 bảng (cùng phân vùng của 9 sổ), 5 chuỗi số và 4 hàm trigger nằm ở schema `pf_coop` của module `pf/cooperative`;
- mọi SQL trên các bảng đó chạy dưới vai `r_cooperative` (giao dịch `SET LOCAL ROLE r_cooperative`, qua `app_platform`, `app_worker`, `app_public`);
- `r_party`, `r_geo`, `r_cropcycle` **không còn** quyền nào trên các bảng này.

Đường dẫn HTTP, mã lỗi, tên sự kiện, tên thực thể đồng bộ và **tên sổ** (`TG_ARGV` của `trg_chain`, `shared.chain_head.ledger`) không đổi. Quyết định PO 09/10/2026: giữ tên sổ cũ (phương án A, mục 8.3), không chèn `chain_head` mới.

**Không tương thích ngược ở mức SQL.** Nhị phân cũ đọc `pf_geo.plot`, `pf_cropcycle.season_current`… sẽ lỗi ngay sau pf 000132. Phải chạy trong cửa sổ bảo trì (H-17, [câu hỏi tồn đọng](../cau-hoi-ton-dong.md)).

## Chuẩn bị (trước cửa sổ)

1. **Diễn tập trên bản sao UAT** (H-17 — chờ bản sao dữ liệu). Mỗi bước ghi kết quả vào biên bản diễn tập:
   1. `pg_dump` UAT → cụm tạm, `migrate bootstrap` + `migrate up` tới pf 000124 (nếu bản sao cũ hơn);
   2. ghi số liệu trước (truy vấn ở mục "Kiểm" dưới, chạy bằng tài khoản quản trị cụm):
      - đếm dòng và `sum(hashtext(row_hash::text))` từng sổ, `sum(hashtext(to_jsonb(x)::text))` từng bảng còn lại;
      - `SELECT ledger, count(*) FROM shared.chain_head GROUP BY 1`;
      - `SELECT max(last_value) FROM pf_geo.plot_sync_seq` (và 4 chuỗi số còn lại);
   3. `migrate verify-chains` phải xanh;
   4. chạy pf 000131–000133, **đo thời gian** (khóa `ACCESS EXCLUSIVE` giữ suốt 000132);
   5. `migrate verify-chains` xanh, số chuỗi bằng trước; đếm/tổng kiểm bằng trước;
   6. chạy bộ e2e G1 (`tests/e2e`) trên nhị phân mới trỏ vào cụm tạm;
   7. diễn tập đường lui (mục "Lùi bản") rồi lặp lại bước 5.

   Kiểm thử tích hợp `migrations/TestCooperativeSchemaMoveRehearsal_W33` làm đúng các bước 2–5 trên dữ liệu tổng hợp ở mọi lần CI (2 tenant, 22 bảng, sổ nhiều dòng) — diễn tập UAT xác nhận thêm thời gian khóa với số phân vùng và khối lượng thật.
2. **Hẹn cửa sổ bảo trì** (H-17): ước lượng ≤ 15 phút dừng ghi trên `api-platform`, `integration-gateway` (bí danh `api-public`), `worker`, `objsync`. Báo người dùng App KN/HTX/CGH: ứng dụng ngoại tuyến tiếp tục ghi trên máy, đồng bộ lại sau cửa sổ (con trỏ `sync_seq` giữ giá trị — không đồng bộ lại toàn bộ).
3. Sao lưu: xác nhận bản sao lưu WAL/PITR mới nhất của cụm pf (`docs/runbooks/backup.md`).

## Thứ tự trong cửa sổ

1. **Dừng ghi.** Scale về 0 (hoặc bật chế độ bảo trì) `api-platform`, `integration-gateway` (bí danh `api-public`), `worker`, `objsync`. Job `maintain`/`anchor` đang chạy thì chờ xong (000132 khóa theo thứ tự cố định, `lock_timeout = 5s` — nếu còn khóa, migration lỗi sạch, chạy lại).
2. **Bootstrap.** Chạy `migrate bootstrap` **bản W3.3** (Job `db-bootstrap` của release `data`). Bước này tạo vai nhóm `r_cooperative` (NOLOGIN, thành viên `ag_shared_user`) và cấp membership cho `app_platform`, `app_worker`, `app_public` theo `modreg/assembly_gen.go`. **Phải chạy trước bước 3** — `ag_migrator` không có CREATEROLE; pf 000131 lỗi rõ ràng nếu thiếu vai.
3. **Migrate.** `migrate up`:
   - 000131: schema `pf_coop`, `USAGE` + quyền `k_ref` cho `r_cooperative`;
   - 000132: khóa 22 bảng, `SET SCHEMA` chuỗi số, hàm, bảng cha, rồi từng phân vùng (vòng `pg_inherits`); đăng ký bù `shared.ledger_added_column` dưới tên bảng mới; thu hồi mọi quyền cũ, cấp ma trận quyền cho `r_cooperative`;
   - 000133: chỉ kiểm — lỗi (và lùi cả giao dịch của tệp) nếu còn bảng/phân vùng/hàm ở schema cũ, `trg_chain` đổi tên sổ, RLS không ép, `v_missing_rls`/`v_missing_immutability` không rỗng, hoặc còn quyền lạ trên `pf_coop`.
4. **Xác minh chuỗi.** `migrate verify-chains` phải xanh; số chuỗi theo sổ bằng số ghi ở diễn tập/bước chuẩn bị.
5. **Triển khai nhị phân W3.3** của mọi đơn vị (cùng lúc — không có bản trung gian chạy được trên cả hai lược đồ).
6. **Mở lại ghi** và kiểm (mục dưới).

## Kiểm sau cửa sổ

```sql
-- Không còn gì ở schema cũ (trừ phần ở lại: org, consent…, admin_boundary, facility…, collection_*).
SELECT n.nspname, c.relname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
 WHERE n.nspname IN ('pf_party', 'pf_geo', 'pf_cropcycle')
   AND c.relname ~ '^(cooperative|household|kn_profile_event|plot|boundary_conflict|cooperative_(boundary|area)|sop_|variety_sop|season_|farm_log|harvest_declaration|coop_season_area|yield_summary)';
-- Sổ giữ tên cũ.
SELECT c.oid::regclass, encode(t.tgargs, 'escape') FROM pg_trigger t JOIN pg_class c ON c.oid = t.tgrelid
 WHERE t.tgname = 'trg_chain' AND c.relnamespace = 'pf_coop'::regnamespace;
-- Quyền: chỉ r_cooperative.
SELECT DISTINCT g.rolname FROM pg_class c CROSS JOIN LATERAL aclexplode(c.relacl) e JOIN pg_roles g ON g.oid = e.grantee
 WHERE c.relnamespace = 'pf_coop'::regnamespace AND e.grantee <> c.relowner;
```

- `/readyz` của mọi đơn vị trả 200; nhật ký không có `permission denied for schema pf_coop` / `relation "pf_geo.plot" does not exist`.
- App HTX: mở danh sách vụ, ghi một nhật ký thử ở tenant kiểm thử; App KN: mở danh sách thửa, hồ sơ HTX; bề mặt đối tác `/v1/partner/{cooperatives,plots,season-harvest-dates}` trả 200 với tài khoản MKG.
- Đồng bộ ngoại tuyến: một máy có con trỏ cũ kéo tiếp, không nhận lại toàn bộ.

## Lùi bản

Repo không có `.down.sql` (R-25). Đường lui là migration **tiến** làm ngược (đánh số khi cần, không đưa vào `migrations/` trừ khi phải dùng), viết sẵn trong thư mục diễn tập:

```sql
SET LOCAL lock_timeout = '5s';
-- khóa cùng thứ tự với 000132 rồi chuyển ngược: phân vùng trước (vòng pg_inherits từ pf_coop về schema của bảng cha cũ),
-- bảng cha, hàm, chuỗi số về pf_party / pf_geo / pf_cropcycle như danh sách ở 000132;
-- REVOKE ALL … FROM r_cooperative; GRANT lại ma trận cũ cho r_party / r_geo / r_cropcycle (theo ACL ghi ở bước chuẩn bị).
```

Vì không đổi dữ liệu, OID, `chain_head` hay tên sổ (D-3), lùi chỉ là thao tác catalog. `shared.ledger_added_column` giữ cả hai tên — không cần làm gì. Sau khi lùi DB phải triển khai lại nhị phân **trước** W3.3.

## Liên quan

- [Thiết kế ranh giới `pf/cooperative`](../design/cooperative.md) mục 8, 17;
- [verify-chains](verify-chains.md) (tên sổ là định danh logic, không phải vị trí);
- [vai DB W2.6](db-roles-w26.md) (vai đăng nhập đơn vị, `migrate bootstrap`).

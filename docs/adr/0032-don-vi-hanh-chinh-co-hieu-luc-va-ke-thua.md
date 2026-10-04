# ADR-0032: Đơn vị hành chính có hiệu lực theo thời gian, ràng buộc loại trừ và bảng kế thừa

**Trạng thái:** Đề xuất — chờ MKG ký (A7) · **Ngày:** 04/10/2026 (gói W0.2) · **Phụ lục B v4.1:** mục 11 · **Xung đột:** X-17 · **Phát hiện:** DB-06 · **Câu hỏi:** C-9 / H-16 · **Thành phần:** `pf/masterdata` (`pf_masterdata.admin_unit`, bảng mới `admin_unit_succession`), `pf/kref` (`k_ref.admin_unit`), `pf/reports`, cổng tích hợp (feed tham chiếu `admin_units`) · **Liên quan:** [ADR-0027](0027-bon-tang-module-tep-khai-bao.md), [ADR-0029](0029-k-ref-ban-chieu-loi-moi-cum.md), [ADR-0033](0033-cong-tich-hop-mkg-nhu-moi-doi-tac.md), [ADR-0035](0035-cap-phep-tong-hop-theo-xa-nguong-k.md), kiến trúc v4.1 §19, §25, §33, §38.1

## Bối cảnh

Hiện trạng (migration pf 000002, `masterdata/domain/adminunit.go`):
- `pf_masterdata.admin_unit` có `valid_from`/`valid_to` (`[)`), `level` (`province | district | commune`), `parent_code`, `successor_codes text[]`, `source`.
- Khóa `(tenant_id, code, valid_from)`: cùng mã được dùng lại sau sắp xếp 2025 (vd. mã 91).
- Trigger `fn_admin_unit_guard` chặn sửa nội dung, chỉ cho đóng phiên bản đang mở, chặn chồng lấp bằng khóa tư vấn + `EXISTS`. Không xóa, không TRUNCATE.
- Đơn vị hành chính **theo tenant** (ADR-0009). ĐTK 1 có một tenant chương trình.

v4.1 §19 (R-DB-09):
- nguồn ở `k_orgtree.admin_unit` (cụm idp), **dùng chung** cho mọi đơn vị, không theo tenant;
- chống chồng lấp bằng `EXCLUDE USING gist` (btree_gist) — khai báo, không dựa vào trigger;
- bảng kế thừa `admin_unit_succession (predecessor, successor, effective_on, area_share, legal_basis)`;
- bản ghi nghiệp vụ trỏ phiên bản có hiệu lực tại `event_time`; báo cáo kỳ cũ nhóm theo đơn vị **tại thời điểm của kỳ**.

Khoảng cách (DB-06, X-17):
- `successor_codes` chỉ có mã, không có ngày hiệu lực của quan hệ, tỉ lệ diện tích khi tách một phần, căn cứ pháp lý.
- Chống chồng lấp phụ thuộc trigger; một lần ghi vòng qua trigger (vai chủ, `session_replication_role`) là đủ để hỏng.
- Bảng theo tenant thay vì một nguồn chung.
- Chưa có nguồn chính thức cho danh mục và bảng kế thừa sau sắp xếp 2025 (C-9).

## Quyết định

### 1. Giữ `pf_masterdata.admin_unit` làm nguồn ở ĐTK 1 (ghi lệch X-17)

- Không dựng `k_orgtree` / cụm idp lúc này. Bảng giữ ở `pf_masterdata`, giữ `tenant_id`, giữ RLS `p_tenant`.
- ĐTK 1 chỉ có một tenant chủ danh mục, nên "theo tenant" và "dùng chung" cho cùng kết quả.
- Lệch có hạn: chuyển về lõi khi có **đơn vị thứ hai cần danh mục** hoặc khi có cụm idp (W7.4), cái nào đến trước. Cách chuyển ở mục 6.

Khai trong `module.yaml` của `pf/masterdata`: `kind: capability` (ADR-0027). Bảng là một phần của `k/orgtree` theo nghĩa v4.1; vị trí vật lý ghi ở bảng ngoại lệ của W1.2 cho tới khi chuyển.

### 2. Ràng buộc loại trừ khai báo, giữ trigger

```sql
CREATE EXTENSION IF NOT EXISTS btree_gist;
ALTER TABLE pf_masterdata.admin_unit
  ADD CONSTRAINT admin_unit_no_overlap
  EXCLUDE USING gist (tenant_id WITH =, code WITH =,
                      daterange(valid_from, valid_to, '[)') WITH &&);
```

- `EXCLUDE` là lớp bảo đảm cuối, chạy cả khi trigger bị vòng qua. Trigger **giữ nguyên** vì nó làm việc mà `EXCLUDE` không làm: chỉ cho đóng phiên bản đang mở, cấm sửa nội dung.
- Khóa tư vấn trong trigger thành thừa cho việc chống chồng lấp nhưng vô hại; giữ một kỳ rồi gỡ nếu bài đo ghi thấy tranh chấp khóa.
- Mã lỗi giữ `exclusion_violation` như hiện nay, nên ánh xạ lỗi ở `masterdata/app` không đổi.

### 3. Bảng kế thừa

```sql
CREATE TABLE pf_masterdata.admin_unit_succession (
  tenant_id              uuid          NOT NULL,
  predecessor_code       text          NOT NULL,
  predecessor_valid_from date          NOT NULL,
  successor_code         text          NOT NULL,
  successor_valid_from   date          NOT NULL,
  effective_on           date          NOT NULL,
  area_share             numeric(9,6)  NULL CHECK (area_share > 0 AND area_share <= 1),
  legal_basis            text          NOT NULL CHECK (length(legal_basis) BETWEEN 1 AND 300),
  basis_status           text          NOT NULL CHECK (basis_status IN ('official','pending_c9')),
  actor_ref              uuid          NOT NULL,
  recorded_at            timestamptz   NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, predecessor_code, predecessor_valid_from, successor_code, effective_on),
  FOREIGN KEY (tenant_id, predecessor_code, predecessor_valid_from) REFERENCES pf_masterdata.admin_unit,
  FOREIGN KEY (tenant_id, successor_code, successor_valid_from)     REFERENCES pf_masterdata.admin_unit
);
```

Khác v4.1 §19 ở hai điểm, có lý do:
- Trỏ phiên bản bằng `(code, valid_from)` thay `uuid`: khóa hiện có của repo, không cần thêm cột định danh.
- Thêm `basis_status`: bản ghi điền từ `successor_codes` (mục 4) chưa có căn cứ pháp lý thật. Không để `legal_basis` rỗng, cũng không giả là chính thức.

Quy tắc:
- **Chỉ ghi thêm**: `shared.fn_immutable` cho UPDATE, DELETE, TRUNCATE; có trong `shared.v_missing_immutability` (R-DB-06).
- Sửa sai bằng bản ghi bù: một dòng `basis_status = 'official'` cùng cặp, `effective_on` đúng; dòng `pending_c9` được truy vấn bỏ qua khi có dòng `official` cùng cặp.
- `predecessor_valid_from` phải trỏ phiên bản đã đóng với `valid_to = effective_on`; `successor_valid_from ≤ effective_on`. Kiểm bằng trigger ghi (không phải RLS).
- Khi một đơn vị tách thành nhiều đơn vị, tổng `area_share` của các dòng cùng tiền thân và cùng `effective_on` ≤ 1. Kiểm trong `app/` khi ghi, kiểm lại bằng truy vấn trong `restore-drill`.
- RLS `p_tenant` như bảng gốc.

### 4. Điền dữ liệu từ `successor_codes`

Migration của W2.2 điền bằng `INSERT … SELECT` từ các phiên bản đã đóng có `successor_codes`, `legal_basis = 'successor_codes (pf 000002)'`, `basis_status = 'pending_c9'`. Không UPDATE dòng nào của `admin_unit`.

- `successor_codes` **giữ** một kỳ phát hành để mã cũ đọc được; `masterdata/app` ghi cả hai trong cùng giao dịch khi đóng phiên bản. Sau một kỳ, ngừng ghi cột này (không xóa cột: dữ liệu lịch sử).
- Production chỉ nạp danh mục và bảng kế thừa từ **nguồn chính thức** (C-9 / H-16). Trước khi có trả lời, `pending_c9` chỉ có trong dữ liệu tổng hợp, seed và UAT.

### 5. Tra theo thời điểm (R-DB-09)

Port của `pf/masterdata` có ba hàm, thay mọi truy vấn trực tiếp ở module khác:

| Hàm | Ý nghĩa |
|---|---|
| `AdminUnitAt(code, at date)` | Phiên bản có hiệu lực tại `at`; không có → `masterdata.admin_unit_not_effective` |
| `SuccessorsAt(code, from, to date)` | Chuỗi kế thừa giữa hai ngày, kèm `area_share` |
| `RollForward(code, at, target date)` | Ánh xạ một mã tại `at` sang các mã tại `target` (cho báo cáo so sánh nhiều kỳ) |

- Bản ghi nghiệp vụ lưu `(code, valid_from)` của phiên bản tại `event_time`, hoặc lưu `code` + `event_time` và tra lại. Không lưu tên.
- Báo cáo kỳ cũ (`pf/reports`, `pf/metrics`) nhóm theo đơn vị **tại kỳ**. W2.2 kiểm lại các truy vấn nhóm theo địa bàn hiện có và ghi danh sách chỗ phải sửa (bản đối chiếu, dòng R-DB-09).
- So sánh nhiều kỳ qua một lần sắp xếp dùng `RollForward`; khi tách một phần, số liệu chia theo `area_share` và **gắn cờ ước tính** trong kết quả.

### 6. Phân phối và chuyển về lõi

- Trong cụm: `k_ref.admin_unit` (ADR-0029) chiếu cả `admin_unit` và `admin_unit_succession`, dựng từ sự kiện `pf.masterdata.admin_unit.changed` (thêm trường kế thừa vào sự kiện, phiên bản mới của schema sự kiện).
- Ra ngoài: feed **tham chiếu dùng chung** `admin_units` qua cổng tích hợp (§38.1, ADR-0033), ảnh chụp theo phiên bản + thay đổi theo cursor. Mọi đối tác (MKG gồm cả) đọc cùng feed.
- Chuyển về lõi (W7.4 hoặc đơn vị thứ hai): `ALTER TABLE … SET SCHEMA` sang schema lõi, bỏ `tenant_id` khỏi khóa bằng bảng mới + `INSERT … SELECT` có kiểm đếm (không UPDATE), view tương thích tên cũ một kỳ. Mã chính thức và `valid_from` giữ nguyên, nên mọi tham chiếu `(code, valid_from)` trong bản ghi nghiệp vụ vẫn đúng.

## Phương án đã cân nhắc

| Phương án | Lý do không chọn |
|---|---|
| Dựng ngay `k_orgtree.admin_unit` ở cụm idp như v4.1 | Cụm idp chưa có (W7.4 chờ G-1); ĐTK 1 một tenant nên không có lợi ích, chỉ thêm chuyển dữ liệu trước 15/12/2026 |
| Chỉ dùng trigger (hiện trạng) | Không khai báo; vòng qua được bằng vai chủ hay `session_replication_role`; v4.1 yêu cầu `EXCLUDE` |
| Thay trigger bằng `EXCLUDE` | `EXCLUDE` không chặn sửa nội dung và mở lại phiên bản đã đóng |
| Mở rộng `successor_codes` thành `jsonb` có ngày, tỉ lệ, căn cứ | Phải UPDATE dòng đã đóng (bất biến); không có khóa ngoại; khó truy vấn ngược (đơn vị mới ← đơn vị cũ) |
| Bỏ `basis_status`, đặt `legal_basis` giả | Lẫn dữ liệu chưa kiểm với dữ liệu chính thức; vi phạm tinh thần C-9 |

## Hệ quả

- Hai đơn vị cùng mã chồng thời gian hiệu lực bị DB từ chối dù mã ghi đi đường nào (đúng lỗi CROSS-01).
- Báo cáo nhiều kỳ qua sắp xếp 2025 có số liệu ước tính khi tách một phần; giao diện phải hiển thị cờ ước tính.
- `btree_gist` thêm vào danh sách extension của cụm pf (compose, chart, `pgtest`, `tools/docsql` W0.4).
- Danh mục chưa có nguồn chính thức: không chặn mã, **chặn nạp production** (C-9).
- ADR-0009 không đổi; X-17 ghi lệch ở đây, có điều kiện kết thúc.

## Hiện thực

| Gói | Việc | Migration |
|---|---|---|
| W2.2 | `btree_gist`, `EXCLUDE`, `admin_unit_succession`, điền từ `successor_codes`, ba hàm port, kiểm truy vấn báo cáo theo kỳ | pf 000089 |
| W2.5 | `k_ref.admin_unit` gồm kế thừa; sự kiện `pf.masterdata.admin_unit.changed` phiên bản mới | pf 000091–000092 |
| W3.5 | Nạp danh mục và bảng kế thừa chính thức (chờ C-9) | — |
| W5.1, W5.3 | Hợp đồng và feed tham chiếu `admin_units` | pf 000100–000101 |
| W7.4 | Chuyển về lõi (hoặc sớm hơn khi có đơn vị thứ hai) | cụm `access` |

## Liên quan

- Kiến trúc v4.1: §19 (R-DB-09), §25 (`k/orgtree`, `AdminUnitAt`), §33 (dữ liệu tổng hợp có bảng kế thừa), §38.1 (tham chiếu dùng chung), Phụ lục B mục 11, Phụ lục C C-9.
- Bản đối chiếu: DB-06, X-17, dòng §19 và R-DB-09.
- Câu hỏi: [cau-hoi-ton-dong.md](../cau-hoi-ton-dong.md) H-16.
- [ADR-0009](0009-mo-hinh-tenant.md), [ADR-0027](0027-bon-tang-module-tep-khai-bao.md), [ADR-0029](0029-k-ref-ban-chieu-loi-moi-cum.md), [ADR-0033](0033-cong-tich-hop-mkg-nhu-moi-doi-tac.md), [ADR-0035](0035-cap-phep-tong-hop-theo-xa-nguong-k.md); `docs/requirements/danh-muc-hanh-chinh.md`.

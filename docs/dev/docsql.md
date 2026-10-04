# docsql — chạy DDL của tài liệu kiến trúc (R-OPS-04)

`tools/docsql` trích mọi khối ```sql của một tệp markdown theo thứ tự và chạy trên
PostgreSQL thật. Mục đích: DDL mẫu trong tài liệu kiến trúc luôn chạy được (bài học
DB-01 của v4.1 mục 12).

```sh
go run ./tools/docsql docs/kien-truc/AgriGreen-Thiet-ke-kien-truc-he-thong-v4_1.md
go run ./tools/docsql -list docs/kien-truc/…v4_1.md   # chỉ liệt kê khối + hash, không cần DB
make docsql
```

Windows: `. .\scripts\dev-env.ps1` trước (đặt `AG_TEST_PG_ADMIN_URL`).

**Cách chạy.** Tạo database tạm `ag_docsql_<hex>` trên cụm của `AG_TEST_PG_ADMIN_URL`
(hoặc `-admin`); trong **một giao dịch**: chạy `tools/docsql/preamble.sql` rồi từng khối,
mỗi khối một savepoint (khối lỗi được hoàn tác, khối sau vẫn chạy). Cuối lượt `ROLLBACK`
(vai do khối tạo, vốn là đối tượng toàn cụm, không còn lại) và xóa database.

**Preamble** dựng những gì tài liệu giả định mà không viết thành khối: extension
(postgis, btree_gist, pgcrypto — pgcrypto chỉ cố gắng vì `digest()` chỉ được phân giải
khi trigger chạy), schema, vai `r_<module>`, và bảng đứng thế tối thiểu mà chính sách RLS
trỏ tới (`k_ref.node_closure`, `dom_coop.plot`, `dom_coop.household`). Không định nghĩa
lại thứ tài liệu đã định nghĩa. `-preamble FILE` thay bản nhúng sẵn.

**Bỏ qua khối:** `tools/docsql/skip.yaml` (doc + số thứ tự + hash + lý do), không chèn
đánh dấu vào tài liệu gốc. Khối đổi nội dung → mục thành "thừa" → tool hỏng. Lỗi thật
của tài liệu: lý do `lỗi trong tài liệu v4.1: …` và câu hỏi ở `docs/cau-hoi-ton-dong.md` mục H.

**Kết quả:** mỗi khối `PASS`/`FAIL`/`SKIP` kèm dòng tài liệu, lỗi PostgreSQL (SQLSTATE,
DETAIL, HINT) và dòng xảy ra lỗi. Thoát 1 khi có khối lỗi không bỏ qua hoặc mục thừa;
2 khi lỗi hạ tầng (thiếu URL, không nối được, preamble lỗi).

**CI:** bước "Tài liệu kiến trúc: DDL chạy được" trong job `test` (cùng PostgreSQL 17 +
PostGIS). `go test ./tools/docsql/...` cũng chạy bản v4.1 (bỏ qua khi thiếu
`AG_TEST_PG_ADMIN_URL`, hỏng khi `CI=true`).

**Lần chạy đầu (04/10/2026, PostgreSQL 17 cục bộ):** 9 khối, 9 PASS, 0 SKIP, 0 FAIL.

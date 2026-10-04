-- Phần dựng sẵn cho docsql (R-OPS-04): những gì DDL mẫu trong tài liệu kiến trúc
-- v4.1 GIẢ ĐỊNH đã có, nhưng tài liệu không viết thành khối ```sql. Chạy trong
-- cùng giao dịch với các khối, trước khối đầu tiên; cuối lượt mọi thứ bị ROLLBACK
-- (kể cả vai — vai là đối tượng toàn cụm) rồi database bị xóa.
--
-- Nguyên tắc: chỉ dựng ĐỦ để khối chạy được — extension, schema, vai, và bảng
-- "đứng thế" tối thiểu cho các bảng mà tài liệu chỉ nhắc tới (chính sách RLS trỏ
-- vào). Không định nghĩa lại thứ mà chính tài liệu định nghĩa. Mỗi mục ghi mục
-- của v4.1 cần nó.

-- Extension (v4.1 mục 15.2: digest(); mục 18: geography; mục 19: EXCLUDE trên text).
-- pgcrypto: cố gắng, không bắt buộc — digest() chỉ được phân giải khi trigger
-- plpgsql chạy, không phải lúc CREATE FUNCTION, và bản PostgreSQL Windows cục bộ
-- có thể thiếu DLL OpenSSL cho pgcrypto. Image CI (deploy/compose/postgres) có đủ.
DO $$ BEGIN
  CREATE EXTENSION IF NOT EXISTS pgcrypto;
EXCEPTION WHEN OTHERS THEN
  RAISE WARNING 'docsql: không nạp được pgcrypto (%) — bỏ qua', SQLERRM;
END $$;
CREATE EXTENSION IF NOT EXISTS postgis;
CREATE EXTENSION IF NOT EXISTS btree_gist;

-- Schema theo R-DB-01 (mục 13) mà các khối dùng.
CREATE SCHEMA k_ledger;       -- 15.2 fn_chain
CREATE SCHEMA k_ref;          -- 17.2, 17.3
CREATE SCHEMA k_orgtree;      -- 19
CREATE SCHEMA mkg_fieldops;   -- 15.1–15.3
CREATE SCHEMA dom_coop;       -- 17.2, 17.3 (chính sách)
CREATE SCHEMA cap_evidence;   -- 18

-- Vai chạy của module (mục 14: r_<module>, NOLOGIN) mà khối GRANT ở mục 14 cấp.
CREATE ROLE r_cap_forms NOLOGIN;
CREATE ROLE r_dom_coop NOLOGIN;
CREATE ROLE r_cap_workflow NOLOGIN;

-- k_ref.node_closure (mục 17.1: cặp tổ tiên–hậu duệ của cây Nút) — chính sách
-- p_scope ở 17.2 đọc ancestor/descendant. Tài liệu không có DDL cho bảng này.
CREATE TABLE k_ref.node_closure (
  ancestor   uuid NOT NULL,
  descendant uuid NOT NULL,
  PRIMARY KEY (ancestor, descendant)
);

-- Bảng nghiệp vụ đứng thế cho chính sách ở 17.2 (dom_coop.plot) và 17.3
-- (dom_coop.household): chỉ các cột chính sách tham chiếu.
CREATE TABLE dom_coop.plot (
  id        uuid PRIMARY KEY,
  tenant_id uuid NOT NULL,
  node_id   uuid NOT NULL
);
CREATE TABLE dom_coop.household (
  id        uuid PRIMARY KEY,
  tenant_id uuid NOT NULL,
  node_id   uuid NOT NULL
);

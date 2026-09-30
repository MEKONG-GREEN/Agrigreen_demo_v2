-- Cụm mkg — nền móng G4 (G4-0, docs/dev/g4-conventions.md §2): schema của bốn module mới.
--
--   mkg_logistics   chuyến, chặng đường thủy/đường bộ, ảnh chụp kết quả định tuyến đã dùng (mkg 000015)
--   mkg_orders      đơn bán, giao nhận, đơn mua + phiếu nhận hàng (mkg 000016)
--   mkg_costing     sổ giá thành (mkg 000017), kỳ, đóng kỳ, quyết toán HTX (mkg 000018) — event sourcing mức B
--   mkg_acctbridge  bút toán tổng hợp theo kỳ, lần đẩy sang phần mềm kế toán pháp định (mkg 000019)
--
-- Chỉ tạo schema và cấp USAGE cho đúng vai của module (R-05: mỗi module một vai, chỉ quyền trên
-- schema của nó). Bảng, RLS (shared.apply_tenant_rls), sổ (shared.apply_ledger) và GRANT trên bảng
-- do migration của từng module tạo. Không có quyền xuyên schema: module mkg đọc nhau qua port/ trong
-- cùng tiến trình, mỗi bên bằng pool vai của chính mình (không join, không FK xuyên schema — R-06).
--
-- Vai logistics_app, orders_app, costing_app, acctbridge_app khai ở internal/shared/dbadmin (G4-0).

CREATE SCHEMA mkg_logistics;
REVOKE ALL ON SCHEMA mkg_logistics FROM PUBLIC;
GRANT USAGE ON SCHEMA mkg_logistics TO logistics_app;

CREATE SCHEMA mkg_orders;
REVOKE ALL ON SCHEMA mkg_orders FROM PUBLIC;
GRANT USAGE ON SCHEMA mkg_orders TO orders_app;

CREATE SCHEMA mkg_costing;
REVOKE ALL ON SCHEMA mkg_costing FROM PUBLIC;
GRANT USAGE ON SCHEMA mkg_costing TO costing_app;

CREATE SCHEMA mkg_acctbridge;
REVOKE ALL ON SCHEMA mkg_acctbridge FROM PUBLIC;
GRANT USAGE ON SCHEMA mkg_acctbridge TO acctbridge_app;

-- mkg_hub: công bố sức chứa định kỳ theo ngày qua van hub_capacity (GIS FN-06 / GIS US-WH-01
-- AC-2, Q-MKG-02).
--
-- Job hub_capacity_daily chạy ở api-internal (leader, khóa advisory trên kết nối riêng tới
-- cụm mkg) cho MỌI tenant có cơ sở đang dùng: vai ag_outbox_publisher chỉ đọc hai cột của
-- facility_current để tìm tenant; chụp và xuất bằng vai hub_app của module (R-05), qua đúng
-- cửa van (mkg_hub.valve_export + shared.outbox cùng giao dịch). Chống trùng theo idem_key
-- 'capacity-daily:<hub>:<YYYY-MM-DD>' của sổ valve_export — không cần bảng mới.

GRANT USAGE ON SCHEMA mkg_hub TO ag_outbox_publisher;
GRANT SELECT (tenant_id, status) ON mkg_hub.facility_current TO ag_outbox_publisher;

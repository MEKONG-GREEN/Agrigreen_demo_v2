-- pf/geo: khôi phục thửa đã ngừng dùng (GIS BR-17 "xóa mềm… khôi phục được").
-- Khôi phục là một SỰ KIỆN mới trong sổ plot_event (action 'restored', bắt buộc
-- lý do như ngừng dùng) — không sửa/xóa sự kiện 'retired' đã ghi (RB-1).
ALTER TABLE pf_geo.plot_event DROP CONSTRAINT plot_event_action_check;
ALTER TABLE pf_geo.plot_event ADD CONSTRAINT plot_event_action_check
  CHECK (action IN ('created', 'assigned', 'retired', 'restored'));
ALTER TABLE pf_geo.plot_event ADD CONSTRAINT plot_event_restored_reason
  CHECK (action <> 'restored' OR char_length(btrim(reason)) >= 10);
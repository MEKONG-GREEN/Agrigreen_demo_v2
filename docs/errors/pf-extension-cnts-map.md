### pf/extension (`dom/extension` — bản đồ vùng nuôi, vùng dịch CNTS, W11.52)

Dùng lại mã đã có của module: `extension.read_only` (ngữ cảnh chỉ xem vẽ/sửa vùng), `extension.out_of_scope` (xã ngoài
tỉnh/phạm vi người vẽ, sửa vùng có xã ngoài phạm vi), `extension.support_reason_required`, `extension.dependency_unavailable`
(chưa nối đơn vị hành chính; gửi `species_code`/`disease_code` khi chưa nối danh mục CNTS).

| HTTP | code | Khi nào |
|---|---|---|
| 404 | `extension.cnts_zone_not_found` | Không có vùng, ngoài phạm vi người gọi (Cấp Xã: vùng không có xã mình; tenant khác), hoặc vùng chưa có ở ngày xem (`/contents`) |
| 422 | `extension.cnts_zone_invalid` | Vùng sai: loại, tên, xã, cách vẽ không hợp loại; GeoJSON sai loại/vòng hở/ngoài Việt Nam/quá 5.000 đỉnh; hình không hợp lệ theo PostGIS (`ST_IsValidReason`, vd tự cắt); bán kính ngoài 10–50.000 m; ngày áp dụng sau hôm nay hoặc sớm hơn phiên bản trước; ổ dịch không có/khác tỉnh/gắn vào vùng nuôi; vùng cha không phải vùng dịch; loài/bệnh không có trong danh mục; ghi chú có SĐT/CCCD; khoanh từ ổ dịch với bán kính không tăng dần |
| 409 | `extension.cnts_zone_version_conflict` | `base_version` khác phiên bản hiện hành; ngưng/khôi phục vùng đã ở trạng thái đó |
| 409 | `extension.cnts_zone_retired` | Sửa vùng đã ngưng hiển thị (khôi phục trước) |

Cảnh báo không chặn (trường `warnings` của vùng vừa ghi): `outside_communes` — vùng nằm hoàn toàn ngoài địa giới các xã đã
chọn (FN-18 luồng ngoại lệ; chỉ khi đã nối ranh giới hành chính của `pf/geo` và xã có ranh giới).

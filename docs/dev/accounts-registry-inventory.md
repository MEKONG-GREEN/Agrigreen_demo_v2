# Kiểm kê và rollout registry tài khoản — KNSPLIT-2-ACCOUNTS-REGISTRY

## Nguồn gốc và phạm vi phần đầu

Khôi phục #52 từ dev `857934416860007cfb2e1b886f682dea27c3ec95` sau khi xác minh actual claim Codex cũ nhưng không có nhánh/WT/checkpoint trên máy mới; release/reclaim qua ticket.ps1 được ghi trên issue. Không reset/xóa. Đây là phần trọn vẹn đầu tiên, chưa đạt toàn bộ acceptance #52; phần còn lại ở child [#193 KNSPLIT-2-ACCOUNTS-REGISTRY-FINAL](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/193).

## Hiện trạng và thay đổi

| Mảnh | Hợp đồng/owner hiện có | Phần này |
|---|---|---|
| Catalog thuộc tính/nhãn/CSV và cờ quyền | accounts/port.AttributeCatalog (#78), extension.AttributeCatalog (#79) | API assembly dựng owner snapshot, lỗi dừng startup, truyền Attributes tường minh |
| Luật assignment/loại node | accounts/port.AssignmentRules (#68), extension.AssignmentRules (#72) | API truyền owner rules, giữ native normalization/scope checks |
| Vai realm/TOTP/MFA | RoleCatalog (#66), NativeRoleCatalog (#96), owner declarations (#95) | Giữ Roles production hiện hành; exact ordering ManagedRoles/LoginTOTPRoles/Grantable thuộc #193 trước retire #57 |
| Phiên/hỗ trợ/default unit | SessionRegistry/SessionProjection, AccountSessionPolicies (#60), Navigation/ScopedDutyHolders (#51/#63) | Giữ ghép nối hiện hành, không cộng quyền từ realm/phiên khác |
| Đơn vị nội bộ/ủy quyền/read-only | Sổ accounts, cờ AttributeCatalog, consumer KN đang chuyển | #193 kiểm kê và nối owner còn thiếu; chưa xóa bridge |
| Worker dormant/org-status | Luồng tenant/native hiện hành, không ghi assignment qua API | Không thêm registry giả/thay global lifecycle; membership KN/provenance còn #87/#90–94 |

Không thêm registry/module-specific branch/fallback; không sửa namespace/schema/migration, dữ liệu HTX, tenant/Principal, D2, golden, API/web hoặc bản khai KN. Cờ chỉ xem/đọc mọi phòng lấy từ catalog server, không suy từ nhãn. Giữ MFA D24 và thứ tự vai hiện hành; không sửa golden để che khác biệt.

## Nghiệm chứng và giới hạn

Test mới TestBuildAccountOwnerCatalogThroughHTTP đi qua Build thật và HTTP Admin: metadata/nhãn/cờ catalog owner; native platform_admin có MFA thành công; thiếu MFA hoặc chỉ có kn_province_admin/kn_user bị từ chối. Suite accounts/handler + tests/knaccounts kiểm create/grant/change/CSV, node/tenant/role, readonly/delegation/thu hồi/hết hạn/D24 trên PostgreSQL thật; contextdata/notify/worker kiểm consumer. Kết quả migration/static/mutation/CI Linux race được ghi trên đúng SHA gửi review tại issue #52 sau khi runner xong, không dùng PASS cũ.

Không coi #57/KNSPLIT-4b/G1 hoặc toàn acceptance #52 hoàn tất từ phần rollout snapshot này; role-order và remaining policies giữ ở child #193.
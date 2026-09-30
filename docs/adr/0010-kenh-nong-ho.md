# ADR-0010: Kênh nông hộ: Flutter cho hiện trường, Zalo Mini App chỉ online

**Trạng thái:** Chấp nhận — 30/09/2026, theo quyết định của chủ sản phẩm trong phiên làm việc · **Ngày:** 27/09/2026

## Bối cảnh (X3)
Backlog App HTX đòi nông dân: đăng nhập offline, vẽ ranh giới offline, ảnh có watermark GPS, nhật ký "chờ đồng bộ". Zalo Mini App không đáp ứng được các yêu cầu này.

## Đề xuất
- Nông dân và cán bộ hiện trường dùng **app Flutter** (`apps/field-app`) cho các luồng cần offline.
- Zalo Mini App chỉ cho các chức năng online: tin tức, thời tiết, xem vụ, nhận thông báo.
- Không gửi mật khẩu tạm qua email (GIS, CGH, HTX). Kích hoạt tài khoản bằng liên kết một lần của Keycloak. Nông dân kích hoạt bằng QR hoặc liên kết do quản lý HTX phát (g2-conventions §1).
- Vai quản trị (đánh dấu `[MFA]` trong realm) bắt buộc MFA: Admin API chặn token không có yếu tố thứ hai, và tài khoản mới có vai này phải cấu hình TOTP. Vai hiện trường đăng nhập ngoại tuyến bằng PIN trên app Flutter.
- Ranh giới trong mã: chỉ thiết bị `android` hoặc `ios` (app Flutter) đăng ký đồng bộ ngoại tuyến được. Zalo Mini App không có thiết bị đồng bộ.

## Triển khai
Đối chiếu từng cam kết với mã và kiểm thử: [trien-khai-g1.md](trien-khai-g1.md#adr-0010-kênh-nông-hộ).

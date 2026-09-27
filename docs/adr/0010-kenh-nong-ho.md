# ADR-0010: Kênh nông hộ: Flutter cho hiện trường, Zalo Mini App chỉ online

**Trạng thái:** Đề xuất — cần MKG ký · **Ngày:** 27/09/2026

## Bối cảnh (X3)
Backlog App HTX đòi nông dân: đăng nhập offline, vẽ ranh giới offline, ảnh có watermark GPS, nhật ký "chờ đồng bộ". Zalo Mini App không đáp ứng được các yêu cầu này.

## Đề xuất
- Nông dân và cán bộ hiện trường dùng **app Flutter** (`apps/field-app`) cho các luồng cần offline.
- Zalo Mini App chỉ cho các chức năng online: tin tức, thời tiết, xem vụ, nhận thông báo.
- Không gửi mật khẩu tạm qua email (GIS, CGH, HTX). Kích hoạt tài khoản bằng liên kết một lần của Keycloak.

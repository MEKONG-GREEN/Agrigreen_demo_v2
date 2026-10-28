# Chỉ mục quy ước kỹ thuật

Điểm vào: [AGENTS.md](../../AGENTS.md). Các tệp quy ước gốc giữ nguyên vị trí; trang này chỉ liên kết.

## Quy ước theo cổng

| Tệp | Nội dung |
|---|---|
| [g1-conventions.md](../dev/g1-conventions.md) | Cấu trúc module, dữ liệu (không float, UUIDv7, không xóa cứng, PII ở vault), API, kiểm thử bắt buộc, tệp dùng chung |
| [g2-conventions.md](../dev/g2-conventions.md) | Phần thêm của G2 |
| [g3-conventions.md](../dev/g3-conventions.md) | Phần thêm của G3 |
| [g4-conventions.md](../dev/g4-conventions.md) | Phần thêm của G4 |
| [g5-conventions.md](../dev/g5-conventions.md) | Phần thêm của G5 |

Một số mục của g1-conventions đã được thay bằng quy ước WH-B: xem bảng "Ghi đè" trong [package-rules.md](../process/package-rules.md#4-ghi-đè-mới-hơn-g1-conventions).

## Module, gói, song song

| Tệp | Nội dung |
|---|---|
| [modules.md](../dev/modules.md) | `module.yaml`, đồ thị module, vai DB theo module, biến ngữ cảnh DB, `k_ref`, view `_pub`, cổng kiến trúc |
| [module-registrations.md](../dev/module-registrations.md) | Đăng ký menu/mock/OpenAPI theo module (WH-A) |
| [package-checklist.md](../dev/package-checklist.md) | Danh sách việc khi thêm một gói |
| [parallel-packages.md](../dev/parallel-packages.md) | Tránh xung đột: migration mốc UTC, ADR giữ chỗ, tài liệu theo gói, lỗi hạ tầng test DB |
| [envelope-format.md](../dev/envelope-format.md), [sync-contract.md](../dev/sync-contract.md) | Định dạng phong bì, hợp đồng đồng bộ |

## Quy tắc bắt buộc mới (24/10/2026)

| Tệp | Nội dung |
|---|---|
| [transactions-and-io.md](transactions-and-io.md) | Không gọi port module khác / I-O ngoài trong DB transaction; mẫu test pool nhỏ |
| [idempotency.md](idempotency.md) | Một `Idempotency-Key` cho mỗi thao tác người dùng |
| [fail-closed-config.md](fail-closed-config.md) | Cấu hình kênh ngoài, worker, RLS fail-closed |
| `docs/kn/ui-kit.md` | Bộ UI kit chung của web (có sau khi [#97](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/97) gộp) |

## Quyết định kiến trúc

- [ADR — chỉ mục và số đã giữ chỗ](../adr/README.md). ADR hay cần: [0005 RLS](../adr/0005-rls-mot-policy-moi-bang.md), [0027 bốn tầng module](../adr/0027-bon-tang-module-tep-khai-bao.md), [0028 vai đăng nhập theo đơn vị](../adr/0028-vai-dang-nhap-theo-don-vi-trien-khai.md), [0038 migration theo module](../adr/0038-bo-chay-migration-theo-module.md), [0055 tách KN](../adr/0055-tach-he-thong-khuyen-nong.md).
- Tài liệu kiến trúc: [docs/kien-truc/](../kien-truc/README.md).
- Mã lỗi: [docs/errors.md](../errors.md) và `docs/errors/<cụm>-<module>.md`.

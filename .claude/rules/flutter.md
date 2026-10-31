---
paths:
  - "apps/field-app/**/*"
  - "apps/htx-app/**/*"
  - "apps/mkg-field-app/**/*"
  - "packages/agrigreen_mobile/**/*"
---
# Flutter / Dart

Nguồn: `analysis_options.yaml` của từng app/gói, AGENTS.md §8, [idempotency.md](../../docs/conventions/idempotency.md), [sync-contract.md](../../docs/dev/sync-contract.md). Khi lệch, nguồn thắng.

- Lint: `flutter_lints` + `strict-casts`, `strict-raw-types`; luật thêm: nháy đơn, khai kiểu trả về, `avoid_dynamic_calls`, `unawaited_futures`, `cancel_subscriptions`, `prefer_final_locals`.
- Mã dùng chung giữa các app (auth, đồng bộ, outbox, bản đồ, ảnh, mã hóa) ưu tiên đặt trong `packages/agrigreen_mobile` (lớp `core/`, `domain/`, `data/`, `application/`, `ui/`), không chép sang từng app.
- Ngoại tuyến: id bản ghi do client sinh (UUIDv7) và **giữ nguyên** khi đồng bộ lại — đóng vai trò khóa idempotency. Lệnh ghi theo mẫu hàng đợi có sẵn (`data/actions/action_queue.dart`, `data/outbox/outbox_repository.dart`).
- Hợp đồng đồng bộ theo `docs/dev/sync-contract.md`; đổi hợp đồng thì đổi cả phía Go và test hợp đồng (`http_contract_*`, `sync_*`).
- Dữ liệu nhạy cảm trên máy: SQLCipher, kho bảo mật (`secure_store`), ảnh mã hóa (`crypto/envelope`). Không log token/PII.
- Đồng hồ và id qua `core/clock.dart`, `core/ids.dart` để test được.
- **Kiểm:** `dart format --output=none --set-exit-if-changed lib test`, `flutter analyze --fatal-infos`, `flutter test`.

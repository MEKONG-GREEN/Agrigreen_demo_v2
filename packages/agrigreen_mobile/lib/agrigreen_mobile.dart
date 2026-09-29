/// Lõi di động AgriGreen dùng chung cho `apps/field-app` (khuyến nông) và
/// `apps/htx-app` (HTX/nông dân) — docs/dev/g2-conventions.md §1.
///
/// Gồm: đăng nhập OIDC + phiên ngoại tuyến 72 giờ + PIN; mở CSDL SQLCipher và
/// migration cục bộ; hàng đợi chiều lên (UUIDv7 + idem_key), push/pull theo
/// cursor, xung đột, lệnh API xếp hàng; phong bì mã hóa (sealed box); ảnh mã
/// hóa ngay khi chụp + watermark; hộp thư và kênh đẩy; gói bản đồ ngoại tuyến;
/// chủ đề và widget chung. Màn hình và luồng nghiệp vụ riêng nằm ở từng app.
///
/// Có thể import thư viện này, hoặc từng tệp (`package:agrigreen_mobile/data/sync/sync_api.dart`, …).
/// Bản giả cho kiểm thử: `package:agrigreen_mobile/testing.dart`.
library;

export 'application/auth_flow.dart';
export 'application/inbox_service.dart';
export 'application/push_coordinator.dart';
export 'application/tile_pack_service.dart';
export 'core/backoff.dart';
export 'core/clock.dart';
export 'core/config.dart';
export 'core/ids.dart';
export 'data/actions/action_queue.dart';
export 'data/auth/device_service.dart';
export 'data/auth/oidc_client.dart';
export 'data/auth/pin_service.dart';
export 'data/auth/session_service.dart';
export 'data/auth/wipe_service.dart';
export 'data/catalog/catalog_cache.dart';
export 'data/crypto/envelope.dart';
export 'data/db/local_db.dart';
export 'data/db/schema.dart';
export 'data/db/sqlcipher_opener.dart';
export 'data/dial/dialer.dart';
export 'data/files/blob_store.dart';
export 'data/files/evidence_repository.dart';
export 'data/map/tile_store.dart';
export 'data/outbox/outbox_repository.dart';
export 'data/photo/location_source.dart';
export 'data/photo/photo_pipeline.dart';
export 'data/push/firebase_push.dart';
export 'data/push/push_messaging.dart';
export 'data/push/push_token_registrar.dart';
export 'data/reference/reference_repository.dart';
export 'data/secure/secure_store.dart';
export 'data/sync/sync_api.dart';
export 'data/sync/sync_service.dart';
export 'domain/consent.dart';
export 'domain/deep_link.dart';
export 'domain/geo/boundary.dart';
export 'domain/geo/tiles.dart';
export 'domain/mrv.dart';
export 'domain/outbox_record.dart';
export 'ui/map/offline_tile_provider.dart';
export 'ui/theme.dart';
export 'ui/widgets/common.dart';

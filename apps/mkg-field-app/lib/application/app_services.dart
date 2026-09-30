import 'dart:io';

import 'package:agrigreen_mobile/core/clock.dart';
import 'package:agrigreen_mobile/core/config.dart';
import 'package:agrigreen_mobile/core/ids.dart';
import 'package:agrigreen_mobile/data/actions/action_queue.dart';
import 'package:agrigreen_mobile/data/auth/device_service.dart';
import 'package:agrigreen_mobile/data/auth/wipe_service.dart';
import 'package:agrigreen_mobile/data/db/local_db.dart';
import 'package:agrigreen_mobile/data/files/blob_store.dart';
import 'package:agrigreen_mobile/data/files/evidence_repository.dart';
import 'package:agrigreen_mobile/data/outbox/outbox_repository.dart';
import 'package:agrigreen_mobile/data/photo/location_source.dart';
import 'package:agrigreen_mobile/data/reference/reference_repository.dart';
import 'package:agrigreen_mobile/data/secure/secure_store.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:agrigreen_mobile/data/sync/sync_service.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';
import 'package:path/path.dart' as p;

import '../domain/roles.dart';
import 'field_service.dart';

/// Client OIDC công khai của App hiện trường MKG
/// (deploy/compose/keycloak/agrigreen-realm.json).
const mkgOidcClientId = 'agrigreen-mkg-field-app';
const mkgOidcRedirect = 'vn.agrigreen.mkgfield:/oauth2redirect';

/// Đồng bộ của vùng mkg (api-internal, `internal/mkg/fieldops/app/sync.go`) —
/// cùng hình dạng docs/dev/sync-contract.md nhưng KHÔNG phải pf/sync.
const mkgPushPath = '/v1/fieldops/sync/push';
const mkgPullPath = '/v1/fieldops/sync/pull';

/// HttpSyncApi của lõi trỏ về api-internal (vùng mkg).
SyncApi mkgHttpApi({
  required String baseUrl,
  required AccessTokenProvider token,
  required String deviceId,
  Duration timeout = const Duration(seconds: 30),
}) => HttpSyncApi(
  baseUrl: baseUrl,
  accessToken: token,
  deviceId: deviceId,
  timeout: timeout,
  pushPath: mkgPushPath,
  pullPath: mkgPullPath,
);

/// Phụ thuộc nền tảng, thay được trong kiểm thử.
class MkgPlatform {
  const MkgPlatform({
    required this.config,
    required this.store,
    required this.clock,
    required this.ids,
    required this.opener,
    required this.dataDir,
    required this.apiFactory,
    required this.location,
    this.appVersion = '0.1.0',
    this.blobStore,
    this.onlineChanges,
    this.autoSync = true,
    this.gpsTimeout = const Duration(seconds: 10),
  });

  final AppConfig config;
  final SecureStore store;
  final Clock clock;
  final IdGenerator ids;
  final DbOpener opener;

  /// Thư mục dữ liệu riêng của app (CSDL SQLCipher).
  final Directory dataDir;

  /// API cho thiết bị [deviceId] (header `X-Device-Id`).
  final SyncApi Function(AccessTokenProvider token, String deviceId) apiFactory;
  final LocationSource location;
  final String appVersion;
  final BlobStore? blobStore;

  /// Luồng "có mạng trở lại" (null = không theo dõi).
  final Stream<bool> Function()? onlineChanges;

  /// Tự đồng bộ theo chu kỳ (tắt trong kiểm thử widget).
  final bool autoSync;
  final Duration gpsTimeout;
}

/// Toàn bộ dịch vụ sau khi mở khóa (CSDL SQLCipher đã mở).
class MkgServices {
  MkgServices._({
    required this.platform,
    required this.db,
    required this.deviceId,
    required this.api,
    required this.outbox,
    required this.refs,
    required this.actions,
    required this.wipe,
    required this.sync,
    required this.field,
  });

  final MkgPlatform platform;
  final LocalDb db;
  final String deviceId;
  final SyncApi api;
  final OutboxRepository outbox;
  final ReferenceRepository refs;
  final ActionQueue actions;
  final WipeService wipe;
  final SyncService sync;
  final FieldService field;

  Future<String?> _token() => platform.store.read(SecureKeys.accessToken);

  /// Vai đọc từ access token đã lưu (dùng được khi ngoại tuyến).
  Future<FieldMode> mode() async => modeFor(rolesFromJwt(await _token()));

  static Future<MkgServices> open(
    MkgPlatform pf, {
    required AccessTokenProvider accessToken,
    void Function()? onWiped,
  }) async {
    final deviceId = await DeviceService(
      store: pf.store,
      ids: pf.ids,
    ).deviceId();
    final db = await LocalDb.open(
      opener: pf.opener,
      path: p.join(pf.dataDir.path, 'agrigreen_mkg_field.db'),
      keys: DbKeyProvider(pf.store),
    );
    final blobs =
        pf.blobStore ??
        BlobStore(Directory(p.join(pf.dataDir.path, 'evidence')));
    final outbox = OutboxRepository(
      db: db,
      deviceId: deviceId,
      clock: pf.clock,
      ids: pf.ids,
      entities: PushEntity.mkgFieldops,
    );
    final refs = ReferenceRepository(db, pf.clock);
    final actions = ActionQueue(db: db, clock: pf.clock, ids: pf.ids);
    final wipe = WipeService(
      db: db,
      blobs: blobs,
      store: pf.store,
      onWiped: onWiped,
    );
    final api = pf.apiFactory(accessToken, deviceId);
    final field = FieldService(
      db: db,
      outbox: outbox,
      refs: refs,
      actions: actions,
      api: api,
      location: pf.location,
      clock: pf.clock,
      deviceId: deviceId,
      subject: () async =>
          subjectFromJwt(await pf.store.read(SecureKeys.accessToken)),
      gpsTimeout: pf.gpsTimeout,
    );
    final sync = SyncService(
      db: db,
      api: api,
      outbox: outbox,
      refs: refs,
      // App MKG không chụp ảnh (không có /v1/files ở api-internal): hàng ảnh rỗng.
      evidence: EvidenceRepository(db: db, blobs: blobs, clock: pf.clock),
      wipe: wipe,
      clock: pf.clock,
      deviceId: deviceId,
      batchSize: pf.config.pushBatchSize,
      pullLimit: pf.config.pullLimit,
      retention: pf.config.ackRetention,
      actions: actions,
      // Số hiệu lực theo thửa/công đoạn không có entity pull: tải qua API.
      afterPull: field.refreshAllCounts,
    );
    return MkgServices._(
      platform: pf,
      db: db,
      deviceId: deviceId,
      api: api,
      outbox: outbox,
      refs: refs,
      actions: actions,
      wipe: wipe,
      sync: sync,
      field: field,
    );
  }

  Future<void> close() async {
    await sync.dispose();
    if (!wipe.wiped) await db.close();
  }
}

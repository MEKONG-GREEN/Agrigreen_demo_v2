import 'dart:io';

import 'package:agrigreen_mobile/application/inbox_service.dart';
import 'package:agrigreen_mobile/application/push_coordinator.dart';
import 'package:agrigreen_mobile/core/clock.dart';
import 'package:agrigreen_mobile/core/config.dart';
import 'package:agrigreen_mobile/core/ids.dart';
import 'package:agrigreen_mobile/data/actions/action_queue.dart';
import 'package:agrigreen_mobile/data/auth/device_service.dart';
import 'package:agrigreen_mobile/data/auth/wipe_service.dart';
import 'package:agrigreen_mobile/data/catalog/catalog_cache.dart';
import 'package:agrigreen_mobile/data/crypto/envelope.dart';
import 'package:agrigreen_mobile/data/db/local_db.dart';
import 'package:agrigreen_mobile/data/dial/dialer.dart';
import 'package:agrigreen_mobile/data/files/blob_store.dart';
import 'package:agrigreen_mobile/data/files/evidence_repository.dart';
import 'package:agrigreen_mobile/data/outbox/outbox_repository.dart';
import 'package:agrigreen_mobile/data/photo/location_source.dart';
import 'package:agrigreen_mobile/data/photo/photo_pipeline.dart';
import 'package:agrigreen_mobile/data/push/push_messaging.dart';
import 'package:agrigreen_mobile/data/push/push_token_registrar.dart';
import 'package:agrigreen_mobile/data/reference/reference_repository.dart';
import 'package:agrigreen_mobile/data/secure/secure_store.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:agrigreen_mobile/data/sync/sync_service.dart';
import 'package:path/path.dart' as p;

import 'crop_service.dart';
import 'manager_service.dart';
import 'news_service.dart';
import 'profile_service.dart';
import 'staff_service.dart';
import 'support_service.dart';

/// Client OIDC công khai của App HTX (deploy/compose/keycloak/agrigreen-realm.json).
const htxOidcClientId = 'agrigreen-htx-app';
const htxOidcRedirect = 'vn.agrigreen.htx:/oauth2redirect';

/// Phụ thuộc nền tảng, thay được trong kiểm thử.
class HtxPlatform {
  const HtxPlatform({
    required this.config,
    required this.store,
    required this.clock,
    required this.ids,
    required this.opener,
    required this.dataDir,
    required this.apiFactory,
    required this.camera,
    required this.location,
    required this.copyText,
    this.appVersion = '0.1.0',
    this.push = const NoopPushMessaging(),
    this.notifier = const NoopLocalNotifier(),
    this.watermarker = const ImageWatermarker(),
    this.dialer = const UrlLauncherDialer(),
    this.blobStore,
    this.onlineChanges,
    this.autoSync = true,
  });

  final AppConfig config;
  final SecureStore store;
  final Clock clock;
  final IdGenerator ids;
  final DbOpener opener;

  /// Thư mục dữ liệu riêng của app (CSDL SQLCipher + ciphertext ảnh).
  final Directory dataDir;

  /// API cho thiết bị [deviceId] (header `X-Device-Id`).
  final SyncApi Function(AccessTokenProvider token, String deviceId) apiFactory;
  final CameraSource camera;
  final LocationSource location;
  final String appVersion;
  final PushMessaging push;
  final LocalNotifier notifier;
  final Watermarker watermarker;

  /// Mở trình quay số (`tel:`) và trình duyệt (liên kết kích hoạt).
  final Dialer dialer;

  /// Chép vào bộ nhớ tạm (số điện thoại cán bộ — HTX US-SUPPORT-02 AC-1).
  final Future<void> Function(String text) copyText;

  /// Kho ciphertext ảnh; mặc định thư mục `evidence` trong [dataDir].
  final BlobStore? blobStore;

  /// Luồng "có mạng trở lại" (connectivity_plus trên máy; null = không theo dõi).
  final Stream<bool> Function()? onlineChanges;

  /// Tự đồng bộ theo chu kỳ (tắt trong kiểm thử widget).
  final bool autoSync;
}

/// Toàn bộ dịch vụ sau khi mở khóa (CSDL đã mở).
class HtxServices {
  HtxServices._({
    required this.platform,
    required this.db,
    required this.deviceId,
    required this.api,
    required this.outbox,
    required this.refs,
    required this.evidence,
    required this.wipe,
    required this.sync,
    required this.photos,
    required this.actions,
    required this.inbox,
    required this.pushTokens,
    required this.catalogs,
    required this.profile,
    required this.crop,
    required this.support,
    required this.staff,
    required this.news,
    required this.manager,
  });

  final HtxPlatform platform;
  final LocalDb db;
  final String deviceId;
  final SyncApi api;
  final OutboxRepository outbox;
  final ReferenceRepository refs;
  final EvidenceRepository evidence;
  final WipeService wipe;
  final SyncService sync;
  final PhotoPipeline photos;
  final ActionQueue actions;
  final InboxService inbox;
  final PushTokenRegistrar pushTokens;
  final CatalogCache catalogs;
  final ProfileService profile;
  final CropService crop;
  final SupportService support;
  final StaffService staff;
  final NewsService news;
  final ManagerService manager;

  late final PushHost pushHost = _ServicesPushHost(this);

  static Future<HtxServices> open(
    HtxPlatform pf, {
    required AccessTokenProvider accessToken,
    void Function()? onWiped,
  }) async {
    final devices = DeviceService(store: pf.store, ids: pf.ids);
    final deviceId = await devices.deviceId();
    final db = await LocalDb.open(
      opener: pf.opener,
      path: p.join(pf.dataDir.path, 'agrigreen_htx.db'),
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
    );
    final refs = ReferenceRepository(db, pf.clock);
    final actions = ActionQueue(db: db, clock: pf.clock, ids: pf.ids);
    final pushTokens = PushTokenRegistrar(db, startedAt: pf.clock.now());
    final catalogs = CatalogCache(db, pf.clock);
    final evidence = EvidenceRepository(db: db, blobs: blobs, clock: pf.clock);
    final wipe = WipeService(
      db: db,
      blobs: blobs,
      store: pf.store,
      onWiped: onWiped,
    );
    final profile = ProfileService(
      db: db,
      catalogs: catalogs,
      accessToken: () => pf.store.read(SecureKeys.accessToken),
    );
    final api = pf.apiFactory(accessToken, deviceId);
    final sync = SyncService(
      db: db,
      api: api,
      outbox: outbox,
      refs: refs,
      evidence: evidence,
      wipe: wipe,
      clock: pf.clock,
      deviceId: deviceId,
      batchSize: pf.config.pushBatchSize,
      pullLimit: pf.config.pullLimit,
      retention: pf.config.ackRetention,
      actions: actions,
      beforeSync: () async {
        await devices.ensureRegistered(api, appVersion: pf.appVersion);
        await pushTokens.flush(api, deviceId);
      },
      // Dữ liệu không có entity đồng bộ: tải qua API sau mỗi lượt pull.
      afterPull: () async {
        await catalogs.refresh(api);
        await profile.refresh(api);
      },
    );
    // Ảnh nhật ký / yêu cầu hỗ trợ: đóng dấu GPS + giờ, mã hóa phong bì bằng
    // khóa công khai của hộ ngay khi chụp (HTX US-LOG-01 AC-4). Nông dân là
    // chủ thể của ảnh nên không qua cổng đồng ý mrv.evidence của app KN.
    final photos = PhotoPipeline(
      camera: pf.camera,
      location: pf.location,
      clock: pf.clock,
      ids: pf.ids,
      cipher: EnvelopeCipher(),
      refs: refs,
      evidence: evidence,
      maxBytes: pf.config.maxFileBytes,
      watermarker: pf.watermarker,
    );
    final crop = CropService(
      outbox: outbox,
      refs: refs,
      catalogs: catalogs,
      profile: profile,
      location: pf.location,
      clock: pf.clock,
      ids: pf.ids,
      api: api,
      evidence: evidence,
    );
    return HtxServices._(
      platform: pf,
      db: db,
      deviceId: deviceId,
      api: api,
      outbox: outbox,
      refs: refs,
      evidence: evidence,
      wipe: wipe,
      sync: sync,
      photos: photos,
      actions: actions,
      inbox: InboxService(refs: refs, actions: actions, clock: pf.clock),
      pushTokens: pushTokens,
      catalogs: catalogs,
      profile: profile,
      crop: crop,
      support: SupportService(
        outbox: outbox,
        refs: refs,
        location: pf.location,
      ),
      staff: StaffService(api: api, dialer: pf.dialer),
      news: NewsService(refs: refs, actions: actions),
      manager: ManagerService(
        api: api,
        db: db,
        actions: actions,
        crop: crop,
        profile: profile,
        clock: pf.clock,
        ids: pf.ids,
      ),
    );
  }

  Future<void> close() async {
    await sync.dispose();
    if (!wipe.wiped) await db.close();
  }
}

class _ServicesPushHost implements PushHost {
  _ServicesPushHost(this._s);
  final HtxServices _s;

  @override
  Future<void> syncNow({Set<String>? entities}) async {
    await _s.sync.syncOnce(force: true, entities: entities);
  }

  @override
  Future<InboxItem?> notification(String id) => _s.inbox.byId(id);

  @override
  Future<void> setPushToken(String token) => _s.pushTokens.setToken(token);
}

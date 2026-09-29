import 'dart:io';

import 'package:agrigreen_mobile/application/inbox_service.dart';
import 'package:agrigreen_mobile/application/push_coordinator.dart';
import 'package:agrigreen_mobile/application/tile_pack_service.dart';
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
import 'package:agrigreen_mobile/data/map/tile_store.dart';
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

import 'boundary_service.dart';
import 'consent_service.dart';
import 'directive_service.dart';
import 'evidence_service.dart';
import 'field_report_service.dart';
import 'library_service.dart';
import 'roster_service.dart';
import 'task_service.dart';

/// Phụ thuộc nền tảng, thay được trong kiểm thử.
class AppPlatform {
  const AppPlatform({
    required this.config,
    required this.store,
    required this.clock,
    required this.ids,
    required this.opener,
    required this.dataDir,
    required this.apiFactory,
    required this.camera,
    required this.location,
    this.appVersion = '0.1.0',
    this.push = const NoopPushMessaging(),
    this.notifier = const NoopLocalNotifier(),
    this.watermarker = const ImageWatermarker(),
    this.dialer = const UrlLauncherDialer(),
  });

  final AppConfig config;
  final SecureStore store;
  final Clock clock;
  final IdGenerator ids;
  final DbOpener opener;

  /// Thư mục dữ liệu riêng của app (CSDL + ciphertext).
  final Directory dataDir;

  /// API cho thiết bị [deviceId] (header `X-Device-Id`).
  final SyncApi Function(AccessTokenProvider token, String deviceId) apiFactory;
  final CameraSource camera;
  final LocationSource location;
  final String appVersion;
  final PushMessaging push;
  final LocalNotifier notifier;
  final Watermarker watermarker;

  /// Mở ứng dụng gọi/nhắn tin (tel:/sms:) cho danh bạ trực.
  final Dialer dialer;
}

/// Toàn bộ dịch vụ sau khi mở khóa (CSDL đã mở).
class AppServices {
  AppServices._({
    required this.platform,
    required this.db,
    required this.deviceId,
    required this.outbox,
    required this.refs,
    required this.evidence,
    required this.wipe,
    required this.sync,
    required this.photos,
    required this.reports,
    required this.boundaries,
    required this.tasks,
    required this.actions,
    required this.inbox,
    required this.library,
    required this.directives,
    required this.consents,
    required this.tiles,
    required this.pushTokens,
    required this.catalogs,
    required this.mrv,
    required this.roster,
  });

  final AppPlatform platform;
  final LocalDb db;
  final String deviceId;
  final OutboxRepository outbox;
  final ReferenceRepository refs;
  final EvidenceRepository evidence;
  final WipeService wipe;
  final SyncService sync;
  final PhotoPipeline photos;
  final FieldReportService reports;
  final BoundaryService boundaries;
  final TaskService tasks;
  final ActionQueue actions;
  final InboxService inbox;
  final LibraryService library;
  final DirectiveService directives;
  final ConsentService consents;
  final TilePackService tiles;
  final PushTokenRegistrar pushTokens;
  final CatalogCache catalogs;
  final EvidenceService mrv;

  /// Danh bạ trực (trực tuyến; số điện thoại đọc lúc bấm, không lưu máy).
  final RosterService roster;

  late final PushHost pushHost = _ServicesPushHost(this);

  static Future<AppServices> open(
    AppPlatform pf, {
    required AccessTokenProvider accessToken,
    void Function()? onWiped,
  }) async {
    final devices = DeviceService(store: pf.store, ids: pf.ids);
    final deviceId = await devices.deviceId();
    final db = await LocalDb.open(
      opener: pf.opener,
      path: p.join(pf.dataDir.path, 'agrigreen_field.db'),
      keys: DbKeyProvider(pf.store),
    );
    final blobs = BlobStore(Directory(p.join(pf.dataDir.path, 'evidence')));
    final outbox = OutboxRepository(
      db: db,
      deviceId: deviceId,
      clock: pf.clock,
      ids: pf.ids,
    );
    final refs = ReferenceRepository(db, pf.clock);
    final actions = ActionQueue(db: db, clock: pf.clock, ids: pf.ids);
    final pushTokens = PushTokenRegistrar(db, startedAt: pf.clock.now());
    final consents = ConsentService(db: db, outbox: outbox, clock: pf.clock);
    final catalogs = CatalogCache(db, pf.clock);
    final evidence = EvidenceRepository(db: db, blobs: blobs, clock: pf.clock);
    final wipe = WipeService(
      db: db,
      blobs: blobs,
      store: pf.store,
      onWiped: onWiped,
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
        // Sau đăng ký thiết bị: gắn token đẩy (mỗi lần khởi động, khi đổi).
        await pushTokens.flush(api, deviceId);
      },
      // Danh mục không có entity đồng bộ: tải qua API sau mỗi lượt pull.
      afterPull: () => catalogs.refresh(api),
    );
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
      consent: consents,
    );
    return AppServices._(
      platform: pf,
      db: db,
      deviceId: deviceId,
      outbox: outbox,
      refs: refs,
      evidence: evidence,
      wipe: wipe,
      sync: sync,
      photos: photos,
      reports: FieldReportService(outbox: outbox, clock: pf.clock),
      boundaries: BoundaryService(outbox: outbox, refs: refs, ids: pf.ids),
      tasks: TaskService(outbox: outbox, refs: refs),
      actions: actions,
      inbox: InboxService(refs: refs, actions: actions, clock: pf.clock),
      library: LibraryService(refs: refs, actions: actions),
      directives: DirectiveService(
        refs: refs,
        actions: actions,
        clock: pf.clock,
      ),
      consents: consents,
      tiles: TilePackService(
        store: TileStore(db),
        urlTemplate: pf.config.tileUrlTemplate,
        clock: pf.clock,
        ids: pf.ids,
      ),
      pushTokens: pushTokens,
      catalogs: catalogs,
      mrv: EvidenceService(
        catalogs: catalogs,
        evidence: evidence,
        photos: photos,
        location: pf.location,
        consent: consents,
        clock: pf.clock,
        ids: pf.ids,
      ),
      roster: RosterService(api: api, dialer: pf.dialer, ids: pf.ids),
    );
  }

  Future<void> close() async {
    await sync.dispose();
    if (!wipe.wiped) await db.close();
  }
}

class _ServicesPushHost implements PushHost {
  _ServicesPushHost(this._s);
  final AppServices _s;

  @override
  Future<void> syncNow() async {
    await _s.sync.syncOnce(force: true);
  }

  @override
  Future<InboxItem?> notification(String id) => _s.inbox.byId(id);

  @override
  Future<void> setPushToken(String token) => _s.pushTokens.setToken(token);
}

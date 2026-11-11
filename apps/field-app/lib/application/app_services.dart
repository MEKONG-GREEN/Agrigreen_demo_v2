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
import 'package:agrigreen_mobile/data/db/local_db.dart';
import 'package:agrigreen_mobile/data/files/blob_store.dart';
import 'package:agrigreen_mobile/data/files/evidence_repository.dart';
import 'package:agrigreen_mobile/data/map/tile_store.dart';
import 'package:agrigreen_mobile/data/outbox/outbox_repository.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';
import 'package:agrigreen_mobile/data/photo/location_source.dart';
import 'package:agrigreen_mobile/data/photo/photo_pipeline.dart';
import 'package:agrigreen_mobile/data/push/push_messaging.dart';
import 'package:agrigreen_mobile/data/push/push_token_registrar.dart';
import 'package:agrigreen_mobile/data/reference/reference_repository.dart';
import 'package:agrigreen_mobile/data/secure/secure_store.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:agrigreen_mobile/data/sync/sync_service.dart';
import 'package:path/path.dart' as p;

import 'package:agrigreen_mobile/application/boundary_service.dart';

import 'cnts_period_service.dart';
import 'kn_collection_service.dart';
import 'kn_context_service.dart';
import 'kn_menu_service.dart';
import 'kn_report_service.dart';
import '../domain/cnts_period.dart';
import '../domain/kn_report.dart';
import 'work_schedule_service.dart';

/// Nguồn đồng bộ chiều xuống của App KN (KN-STANDALONE-9 #270): chỉ entity của
/// module KN và hạ tầng KN (danh mục, đơn vị hành chính, thông báo). Không kéo
/// nguồn HTX/MRV/nông dân (`plot`, `season`, `farm_log`, `sop_version`,
/// `mrv_catalog`, `support_request`, `pubkey`…) hay nguồn của màn cũ đã gỡ
/// (`task`, `directive`, `document`, `library_item`).
const knPullEntities = {
  RefEntity.catalogItem, // CNTS FN-06: danh mục CNTS ngoại tuyến
  RefEntity.adminUnit, // TT FN-XA-03.05: chọn xã cho thửa mới
  RefEntity.notification, // nhắc việc (pf/notify)
  RefEntity.knPlot, // TT FN-XA-03.05
  // TT FN-XA-03.02: report_task, form_layout, report_submission
  ...KnEntity.all,
  FormsEntity.task, // CNTS FN-08/09: forms_task
};

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
    required this.boundaries,
    required this.actions,
    required this.inbox,
    required this.tiles,
    required this.pushTokens,
    required this.catalogs,
    required this.schedules,
    required this.knContext,
    required this.knMenu,
    required this.knReports,
    required this.cntsPeriods,
    required this.knData,
  });

  final AppPlatform platform;
  final LocalDb db;
  final String deviceId;
  final OutboxRepository outbox;
  final ReferenceRepository refs;
  final EvidenceRepository evidence;
  final WipeService wipe;
  final SyncService sync;
  final BoundaryService boundaries;
  final ActionQueue actions;
  final InboxService inbox;
  final TilePackService tiles;
  final PushTokenRegistrar pushTokens;
  final CatalogCache catalogs;

  /// Lịch công tác tuần (KN v7.7 P5): bản đệm + nháp trên máy, lưu/gửi qua
  /// hàng đợi lệnh.
  final WorkScheduleService schedules;

  /// KN v7.7 P1: ngữ cảnh phiên (vai trò, phòng ban, xã phụ trách).
  final KnContextService knContext;

  /// KN v7.7 P2: menu Cấp Xã (đệm ngoại tuyến) + số đếm.
  final KnMenuService knMenu;

  /// KN v7.7 P7: công việc cần làm + nhập báo cáo ngoại tuyến.
  final KnReportService knReports;

  /// W11.42: biểu kỳ Cấp Xã CNTS (cap/forms native) ngoại tuyến.
  final CntsPeriodService cntsPeriods;

  /// KN v7.7 P7: thu thập dữ liệu (HTX, máy móc, thửa, vật tư).
  final KnCollectionService knData;

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
    final actions = ActionQueue(
      db: db,
      clock: pf.clock,
      ids: pf.ids,
      extraKinds: CntsActionKind.values,
    );
    final pushTokens = PushTokenRegistrar(db, startedAt: pf.clock.now());
    final catalogs = CatalogCache(db, pf.clock);
    final evidence = EvidenceRepository(db: db, blobs: blobs, clock: pf.clock);
    final wipe = WipeService(
      db: db,
      blobs: blobs,
      store: pf.store,
      onWiped: onWiped,
    );
    final api = pf.apiFactory(accessToken, deviceId);
    late final WorkScheduleService schedules;
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
      pullEntities: knPullEntities,
      beforeSync: () async {
        await devices.ensureRegistered(api, appVersion: pf.appVersion);
        // Sau đăng ký thiết bị: gắn token đẩy (mỗi lần khởi động, khi đổi).
        await pushTokens.flush(api, deviceId);
      },
      // Lệnh lưu lịch tuần vừa phát lại → tải bản mới, bỏ nháp đã lên máy chủ.
      afterPull: () => schedules.refreshAfterSync(),
    );
    schedules = WorkScheduleService(
      db: db,
      api: api,
      actions: actions,
      clock: pf.clock,
      ids: pf.ids,
      deviceId: deviceId,
      location: pf.location, // W11.46: check-in GPS lịch công tác
      camera: pf.camera, // CNTS v4.0 FN-12: ảnh check-in/check-out bắt buộc
      syncNow: () async {
        await sync.syncOnce(force: true);
      },
    );
    // KN v7.7 P1: mọi lời gọi mang X-AG-Context; ngữ cảnh bị từ chối → chọn
    // lại cùng phân công rồi gọi lại (HttpSyncApi; bản giả bỏ qua).
    final knContext = KnContextService(
      db: db,
      api: api,
      clock: pf.clock,
      ids: pf.ids,
    );
    if (api is HttpSyncApi) {
      api.contextId = knContext.contextId;
      api.onContextRejected = knContext.recover;
    }
    final knReports = KnReportService(
      db: db,
      refs: refs,
      outbox: outbox,
      api: api,
      clock: pf.clock,
      syncNow: (entities) async {
        await sync.syncOnce(force: true, entities: entities);
      },
    );
    final cntsPeriods = CntsPeriodService(
      db: db,
      refs: refs,
      outbox: outbox,
      clock: pf.clock,
      syncNow: (entities) async {
        await sync.syncOnce(force: true, entities: entities);
      },
    );
    final knMenu = KnMenuService(
      db: db,
      api: api,
      clock: pf.clock,
      localTodoCount: knReports.todoCount,
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
      // #271: thửa KN ở kho KN — đồng bộ kn_plot / kn_plot_boundary.
      boundaries: BoundaryService(
        outbox: outbox,
        refs: refs,
        ids: pf.ids,
        db: db,
        plotEntity: RefEntity.knPlot,
        pushEntity: PushEntity.knPlotBoundary,
      ),
      actions: actions,
      inbox: InboxService(refs: refs, actions: actions, clock: pf.clock),
      tiles: TilePackService(
        store: TileStore(db),
        urlTemplate: pf.config.tileUrlTemplate,
        clock: pf.clock,
        ids: pf.ids,
      ),
      pushTokens: pushTokens,
      catalogs: catalogs,
      schedules: schedules,
      knContext: knContext,
      knMenu: knMenu,
      knReports: knReports,
      cntsPeriods: cntsPeriods,
      knData: KnCollectionService(
        db: db,
        api: api,
        actions: actions,
        catalogs: catalogs,
        clock: pf.clock,
      ),
    );
  }

  Future<void> close() async {
    await sync.dispose();
    await knContext.dispose();
    if (!wipe.wiped) await db.close();
  }
}

class _ServicesPushHost implements PushHost {
  _ServicesPushHost(this._s);
  final AppServices _s;

  @override
  Future<void> syncNow({Set<String>? entities}) async {
    await _s.sync.syncOnce(force: true, entities: entities);
  }

  @override
  Future<InboxItem?> notification(String id) => _s.inbox.byId(id);

  @override
  Future<void> setPushToken(String token) => _s.pushTokens.setToken(token);
}

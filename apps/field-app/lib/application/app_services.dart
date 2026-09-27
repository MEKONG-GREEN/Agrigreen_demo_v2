import 'dart:io';

import 'package:path/path.dart' as p;

import '../core/clock.dart';
import '../core/config.dart';
import '../core/ids.dart';
import '../data/auth/device_service.dart';
import '../data/auth/wipe_service.dart';
import '../data/crypto/envelope.dart';
import '../data/db/local_db.dart';
import '../data/files/blob_store.dart';
import '../data/files/evidence_repository.dart';
import '../data/outbox/outbox_repository.dart';
import '../data/photo/location_source.dart';
import '../data/photo/photo_pipeline.dart';
import '../data/reference/reference_repository.dart';
import '../data/secure/secure_store.dart';
import '../data/sync/sync_api.dart';
import '../data/sync/sync_service.dart';
import 'boundary_service.dart';
import 'field_report_service.dart';
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
  });

  final AppConfig config;
  final SecureStore store;
  final Clock clock;
  final IdGenerator ids;
  final DbOpener opener;

  /// Thư mục dữ liệu riêng của app (CSDL + ciphertext).
  final Directory dataDir;
  final SyncApi Function(AccessTokenProvider token) apiFactory;
  final CameraSource camera;
  final LocationSource location;
  final String appVersion;
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
    final evidence = EvidenceRepository(db: db, blobs: blobs, clock: pf.clock);
    final wipe = WipeService(
      db: db,
      blobs: blobs,
      store: pf.store,
      onWiped: onWiped,
    );
    final api = pf.apiFactory(accessToken);
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
      beforeSync: () =>
          devices.ensureRegistered(api, appVersion: pf.appVersion),
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
      photos: PhotoPipeline(
        camera: pf.camera,
        location: pf.location,
        clock: pf.clock,
        ids: pf.ids,
        cipher: EnvelopeCipher(),
        refs: refs,
        evidence: evidence,
        maxBytes: pf.config.maxFileBytes,
      ),
      reports: FieldReportService(
        outbox: outbox,
        location: pf.location,
        clock: pf.clock,
      ),
      boundaries: BoundaryService(outbox: outbox, refs: refs, ids: pf.ids),
      tasks: TaskService(outbox: outbox, refs: refs),
    );
  }

  Future<void> close() async {
    await sync.dispose();
    if (!wipe.wiped) await db.close();
  }
}

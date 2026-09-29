import 'dart:io';
import 'dart:math';

import 'package:agrigreen_mobile/core/backoff.dart';
import 'package:agrigreen_mobile/core/clock.dart';
import 'package:agrigreen_mobile/core/ids.dart';
import 'package:agrigreen_mobile/data/actions/action_queue.dart';
import 'package:agrigreen_mobile/data/auth/wipe_service.dart';
import 'package:agrigreen_mobile/data/db/local_db.dart';
import 'package:agrigreen_mobile/data/files/blob_store.dart';
import 'package:agrigreen_mobile/data/files/evidence_repository.dart';
import 'package:agrigreen_mobile/data/outbox/outbox_repository.dart';
import 'package:agrigreen_mobile/data/reference/reference_repository.dart';
import 'package:agrigreen_mobile/data/secure/secure_store.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:agrigreen_mobile/data/sync/sync_service.dart';
import 'package:agrigreen_mobile/testing.dart';
import 'package:path/path.dart' as p;

import 'ffi_opener.dart';

const testDeviceId = '01920000-0000-7000-8000-00000000d001';

/// Dựng toàn bộ tầng dữ liệu trên một tệp SQLite thật trong thư mục tạm.
/// [reopen] mô phỏng app bị tắt đột ngột rồi mở lại: bỏ mọi đối tượng trong
/// bộ nhớ, chỉ còn lại những gì đã ghi xuống đĩa.
class Harness {
  Harness._(this.dir, this.store, this.clock, this.opener, this.server);

  final Directory dir;
  final MemorySecureStore store;
  final FakeClock clock;
  final FfiOpener opener;
  SyncApi server;
  final ids = UuidV7Generator();

  late LocalDb db;
  late OutboxRepository outbox;
  late ReferenceRepository refs;
  late EvidenceRepository evidence;
  late BlobStore blobs;
  late WipeService wipe;
  late SyncService sync;
  late ActionQueue actions;
  int wipedCallbacks = 0;

  String get dbPath => p.join(dir.path, 'field.db');

  FakeServer get fake => server as FakeServer;

  static Future<Harness> create({SyncApi? server, DateTime? now}) async {
    final dir = await Directory.systemTemp.createTemp('ag_field_');
    final h = Harness._(
      dir,
      MemorySecureStore(),
      FakeClock(now ?? DateTime.utc(2026, 12, 2, 1, 0)),
      FfiOpener(),
      server ?? FakeServer(),
    );
    await h._open();
    return h;
  }

  Future<void> _open() async {
    db = await LocalDb.open(
      opener: opener,
      path: dbPath,
      keys: DbKeyProvider(store),
    );
    blobs = BlobStore(Directory(p.join(dir.path, 'evidence')));
    outbox = OutboxRepository(
      db: db,
      deviceId: testDeviceId,
      clock: clock,
      ids: ids,
    );
    refs = ReferenceRepository(db, clock);
    evidence = EvidenceRepository(db: db, blobs: blobs, clock: clock);
    wipe = WipeService(
      db: db,
      blobs: blobs,
      store: store,
      onWiped: () => wipedCallbacks++,
    );
    actions = ActionQueue(db: db, clock: clock, ids: ids);
    sync = SyncService(
      actions: actions,
      db: db,
      api: server,
      outbox: outbox,
      refs: refs,
      evidence: evidence,
      wipe: wipe,
      clock: clock,
      deviceId: testDeviceId,
      batchSize: 100,
      backoff: const Backoff(jitter: 0),
      random: Random(1),
    );
  }

  /// "Crash": đóng CSDL mà không dọn dẹp gì thêm, rồi mở lại từ đĩa.
  Future<void> reopen() async {
    await sync.dispose();
    await db.close();
    await _open();
  }

  Future<void> dispose() async {
    await sync.dispose();
    try {
      await db.close();
    } catch (_) {}
    if (await dir.exists()) await dir.delete(recursive: true);
  }
}

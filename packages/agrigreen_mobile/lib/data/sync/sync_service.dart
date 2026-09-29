import 'dart:async';
import 'dart:convert';
import 'dart:math';

import '../../core/backoff.dart';
import '../../core/clock.dart';
import '../actions/action_queue.dart';
import '../auth/wipe_service.dart';
import '../db/local_db.dart';
import '../files/evidence_repository.dart';
import '../outbox/outbox_repository.dart';
import '../reference/reference_repository.dart';
import 'sync_api.dart';

enum SyncState { idle, running, offline, needsLogin, revoked, error }

class SyncStatus {
  const SyncStatus({
    this.state = SyncState.idle,
    this.lastSuccessAt,
    this.lastError,
    this.failures = 0,
    this.nextAttemptAt,
  });
  final SyncState state;
  final DateTime? lastSuccessAt;
  final String? lastError;
  final int failures;
  final DateTime? nextAttemptAt;

  String get label => switch (state) {
    SyncState.idle => 'Đã sẵn sàng',
    SyncState.running => 'Đang đồng bộ…',
    SyncState.offline => 'Mất kết nối — sẽ thử lại',
    SyncState.needsLogin => 'Cần đăng nhập lại khi có mạng',
    SyncState.revoked => 'Thiết bị đã bị thu hồi',
    SyncState.error => 'Lỗi đồng bộ — sẽ thử lại',
  };

  Map<String, Object?> toJson() => {
    'state': state.name,
    'last_success_at': lastSuccessAt?.millisecondsSinceEpoch,
    'last_error': lastError,
    'failures': failures,
    'next_attempt_at': nextAttemptAt?.millisecondsSinceEpoch,
  };

  factory SyncStatus.fromJson(Map<String, Object?> j) {
    DateTime? t(Object? v) =>
        v is int ? DateTime.fromMillisecondsSinceEpoch(v) : null;
    return SyncStatus(
      state: SyncState.values.firstWhere(
        (s) => s.name == j['state'],
        orElse: () => SyncState.idle,
      ),
      lastSuccessAt: t(j['last_success_at']),
      lastError: j['last_error'] as String?,
      failures: (j['failures'] as int?) ?? 0,
      nextAttemptAt: t(j['next_attempt_at']),
    );
  }
}

class SyncReport {
  int pushed = 0;
  int pulled = 0;
  int uploaded = 0;
  int purged = 0;
  int actions = 0;
}

/// Điều phối đồng bộ: tải tệp → push lô → pull theo cursor → purge.
/// Trạng thái lùi thử lại lưu bền trong bảng meta (sống qua khởi động lại).
class SyncService {
  SyncService({
    required LocalDb db,
    required SyncApi api,
    required OutboxRepository outbox,
    required ReferenceRepository refs,
    required EvidenceRepository evidence,
    required WipeService wipe,
    required Clock clock,
    required String deviceId,
    this.backoff = const Backoff(),
    this.batchSize = 100,
    this.pullLimit = 500,
    this.retention = const Duration(hours: 48),
    Random? random,
    this.beforeSync,
    this.afterPull,
    ActionQueue? actions,
  }) : _actions = actions,
       _db = db,
       _api = api,
       _outbox = outbox,
       _refs = refs,
       _evidence = evidence,
       _wipe = wipe,
       _clock = clock,
       _deviceId = deviceId,
       _random = random ?? Random();

  final LocalDb _db;
  final SyncApi _api;
  final OutboxRepository _outbox;
  final ReferenceRepository _refs;
  final EvidenceRepository _evidence;
  final WipeService _wipe;
  final Clock _clock;
  final String _deviceId;
  final Backoff backoff;
  final int batchSize;
  final int pullLimit;
  final Duration retention;
  final Random _random;
  final ActionQueue? _actions;

  /// Chạy trước mỗi lượt (ví dụ đăng ký thiết bị).
  final Future<void> Function()? beforeSync;

  /// Chạy sau pull (ví dụ làm mới danh mục tải qua API). Lỗi ném ra được xử lý
  /// như lỗi của lượt đồng bộ.
  final Future<void> Function()? afterPull;

  static const _statusKey = 'sync.status';

  final _statusCtl = StreamController<SyncStatus>.broadcast();
  Stream<SyncStatus> get statusStream => _statusCtl.stream;

  Future<SyncReport?>? _running;

  Future<SyncStatus> status() async {
    if (_wipe.wiped) return const SyncStatus(state: SyncState.revoked);
    final j = await _db.getMetaJson(_statusKey);
    return j == null ? const SyncStatus() : SyncStatus.fromJson(j);
  }

  Future<void> _save(SyncStatus s) async {
    if (!_wipe.wiped) {
      await _db.setMeta(_statusKey, jsonEncode(s.toJson()));
    }
    if (!_statusCtl.isClosed) _statusCtl.add(s);
  }

  /// Chạy một lượt đồng bộ. [force] bỏ qua thời điểm lùi (nút "Đồng bộ ngay").
  /// Gọi chồng nhau thì dùng chung lượt đang chạy.
  ///
  /// [entities] (KN US-SYNC-02, thông điệp `sync_now` có khóa `entities`):
  /// chiều lên vẫn gửi hết, chiều xuống chỉ pull các entity này
  /// (`?entities=`). Máy chủ giữ vị trí con trỏ của entity không đọc nên lượt
  /// đầy đủ sau đó không mất thay đổi. null/rỗng = pull toàn bộ.
  Future<SyncReport?> syncOnce({bool force = false, Set<String>? entities}) {
    if (_wipe.wiped) return Future.value(null);
    final running = _running;
    if (running != null) return running.then((_) => null);
    final only = entities == null || entities.isEmpty
        ? null
        : (entities.toList()..sort());
    final f = _run(force, only);
    _running = f;
    return f.whenComplete(() => _running = null);
  }

  Future<SyncReport?> _run(bool force, List<String>? entities) async {
    final prev = await status();
    if (!force &&
        prev.nextAttemptAt != null &&
        _clock.now().isBefore(prev.nextAttemptAt!)) {
      return null;
    }
    final report = SyncReport();
    try {
      await _save(
        SyncStatus(
          state: SyncState.running,
          lastSuccessAt: prev.lastSuccessAt,
          failures: prev.failures,
        ),
      );
      await beforeSync?.call();
      await _uploadFiles(report);
      await _evidence.flushForms(_outbox);
      await _pushAll(report);
      await _replayActions(report);
      await _pullAll(report, entities);
      await afterPull?.call();
      report.purged = await _outbox.purgeAcked(retention);
      await _evidence.purgeUploaded(retention);
      await _refs.purgeOldNotifications();
      await _actions?.purgeDone();
      await _save(
        SyncStatus(state: SyncState.idle, lastSuccessAt: _clock.now()),
      );
      return report;
    } on DeviceRevokedException {
      await _save(const SyncStatus(state: SyncState.revoked));
      await _wipe.wipe();
      return null;
    } on UnauthenticatedException catch (e) {
      await _save(
        SyncStatus(
          state: SyncState.needsLogin,
          lastSuccessAt: prev.lastSuccessAt,
          lastError: e.code,
        ),
      );
      return null;
    } on NetworkException {
      await _fail(prev, SyncState.offline, 'network');
      return null;
    } on ApiException catch (e) {
      await _fail(prev, SyncState.error, e.code);
      return null;
    }
  }

  Future<void> _fail(SyncStatus prev, SyncState state, String error) async {
    final failures = prev.failures + 1;
    await _save(
      SyncStatus(
        state: state,
        lastSuccessAt: prev.lastSuccessAt,
        lastError: error,
        failures: failures,
        nextAttemptAt: _clock.now().add(
          backoff.delayFor(failures, random: _random),
        ),
      ),
    );
  }

  Future<void> _uploadFiles(SyncReport report) async {
    for (final f in await _evidence.pending()) {
      try {
        final blob = await _evidence.blobs.read(f.blobPath);
        final ref = await _api.uploadFile(
          idempotencyKey: 'file:${f.id}',
          meta: f.meta,
          ciphertext: blob,
          fileRef: f.id,
        );
        await _evidence.markUploaded(f, ref);
        report.uploaded++;
      } on ApiException catch (e) {
        if (e is DeviceRevokedException || e is UnauthenticatedException) {
          rethrow;
        }
        if (e.retriable) rethrow;
        // Lỗi dữ liệu (413, 422…): đánh dấu hỏng, hiển thị, không chặn hàng đợi.
        await _evidence.recordFailure(f.id, e.code, fatal: true);
      }
    }
  }

  Future<void> _pushAll(SyncReport report) async {
    // Giới hạn vòng lặp để không quay vô hạn nếu máy chủ không trả kết quả.
    for (var guard = 0; guard < 1000; guard++) {
      final batch = await _outbox.claimBatch(batchSize);
      if (batch == null || batch.records.isEmpty) return;
      List<PushResult> results;
      try {
        results = await _api.push(
          batchKey: batch.batchKey,
          deviceId: _deviceId,
          records: batch.records,
        );
      } on ApiException catch (e) {
        if (e.status == 422 && e.code == 'idempotency_key_reused') {
          await _outbox.releaseBatch(batch.batchKey);
          continue;
        }
        rethrow; // lô giữ batch_key → lần sau gửi lại y nguyên
      }
      await _outbox.applyResults(batch.batchKey, results);
      report.pushed += results.length;
      if (results.isEmpty) return;
      // Module nhận lỗi hệ thống (sync.sink_failed): bản ghi vẫn chờ; dừng lượt
      // này và lùi thử lại thay vì gửi dồn liên tục.
      if (results.every(
        (r) =>
            r.status == 'rejected' &&
            OutboxRepository.retriableRejectCodes.contains(r.code),
      )) {
        throw ApiException(503, results.first.code ?? 'sync.sink_failed');
      }
    }
  }

  /// Phát lại lệnh API xếp hàng (đánh dấu đã đọc, xác nhận chỉ đạo, lưu tài
  /// liệu) theo thứ tự. Mỗi lệnh có Idempotency-Key cố định → gọi lặp an toàn.
  Future<void> _replayActions(SyncReport report) async {
    final q = _actions;
    if (q == null) return;
    for (final a in await q.pending()) {
      try {
        await _api.postAction(
          path: a.path,
          idempotencyKey: a.idempotencyKey,
          body: a.body,
        );
        await q.markDone(a.id);
        report.actions++;
      } on ApiException catch (e) {
        if (e is DeviceRevokedException || e is UnauthenticatedException) {
          rethrow;
        }
        if (e.retriable) {
          await q.recordAttempt(a.id, e.code);
          rethrow;
        }
        // 404 (không còn/ngoài phạm vi), 403, 422: bỏ lệnh, không chặn hàng đợi.
        await q.markFailed(a.id, e.code);
      }
    }
  }

  Future<void> _pullAll(SyncReport report, [List<String>? entities]) async {
    var restarted = false;
    for (var guard = 0; guard < 10000; guard++) {
      PullPage page;
      try {
        page = await _api.pull(
          cursor: await _refs.cursor(),
          limit: pullLimit,
          entities: entities,
        );
      } on ApiException catch (e) {
        // Con trỏ bị sửa / của thiết bị khác → pull lại từ đầu (một lần).
        if (e.status == 400 && e.code == 'sync.cursor_invalid' && !restarted) {
          restarted = true;
          await _db.setMeta(ReferenceRepository.cursorKey, null);
          continue;
        }
        rethrow;
      }
      if (entities != null && page.reset) {
        // Phạm vi đã đổi: trang `reset` chỉ mang một phần entity mà áp nó sẽ
        // thay TOÀN BỘ dữ liệu tham chiếu → bỏ trang, pull đầy đủ từ con trỏ cũ
        // (máy chủ trả lại `reset` cho lượt đầy đủ).
        entities = null;
        continue;
      }
      await _refs.applyPage(page);
      report.pulled += page.changes.length;
      if (!page.hasMore || page.nextCursor == null) return;
    }
  }

  Future<void> dispose() => _statusCtl.close();
}

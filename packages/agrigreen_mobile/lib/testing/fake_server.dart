import 'dart:convert';
import 'dart:typed_data';

import '../data/outbox/outbox_repository.dart';
import '../data/sync/sync_api.dart';
import '../domain/outbox_record.dart';

/// Máy chủ giả mô phỏng hành vi hợp đồng: Idempotency-Key theo lô (gọi lặp
/// cùng thân → phát lại phản hồi; khác thân → 422), chống trùng theo idem_key,
/// kết quả theo từng bản ghi.
class FakeServer implements SyncApi {
  /// idem_key → bản ghi đã ghi sổ.
  final Map<String, Map<String, Object?>> ledger = {};
  final Map<String, String> _batchBodies = {};
  final Map<String, List<PushResult>> _batchResponses = {};
  final List<String> pushBatchKeys = [];
  final List<String> pushBodies = [];
  final Map<String, Map<String, Object?>> files = {};
  final List<String> uploadKeys = [];
  final List<String> registeredDevices = [];

  /// device_id → token đẩy đã gắn; khóa idempotency đã dùng.
  final Map<String, String> pushTokens = {};
  final List<String> pushTokenKeys = [];
  int failPushToken = 0;

  /// Lệnh API đã nhận (path, khóa) — gọi lặp cùng khóa trả phản hồi cũ.
  final List<(String, String)> actionCalls = [];
  final Map<String, Map<String, Object?>> _actionResponses = {};
  int failNextActions = 0;

  /// path → mã lỗi 4xx trả về (ví dụ 404 notify.notification_not_found).
  final Map<String, (int, String)> actionErrors = {};
  final List<String?> uploadFileRefs = [];

  int failNextPushes = 0;
  int dropNextResponses = 0;
  int failNextUploads = 0;
  int dropNextUploadResponses = 0;
  bool revoked = false;
  bool unauthenticated = false;

  /// Bỏ kết quả của các id này khỏi phản hồi (máy chủ "quên" trả).
  Set<String> omitResultsFor = {};
  bool Function(Map<String, Object?> wire)? rejectIf;
  bool Function(Map<String, Object?> wire)? conflictIf;

  /// cursor (null = đầu) → trang pull.
  final Map<String?, PullPage> pages = {};
  final List<String?> pullCursors = [];

  /// `entities` của từng lời gọi pull (null = mọi entity).
  final List<List<String>?> pullEntities = [];

  int get pushCalls => pushBatchKeys.length;

  void _guard() {
    if (revoked) throw DeviceRevokedException();
    if (unauthenticated) throw UnauthenticatedException('unauthenticated');
  }

  @override
  Future<void> registerDevice({
    required String deviceId,
    required String platform,
    required String appVersion,
  }) async {
    _guard();
    registeredDevices.add(deviceId);
  }

  @override
  Future<List<PushResult>> push({
    required String batchKey,
    required String deviceId,
    required List<OutboxRecord> records,
  }) async {
    _guard();
    pushBatchKeys.add(batchKey);
    final body = HttpSyncApi.encodePushBody(deviceId, records);
    pushBodies.add(body);
    if (failNextPushes > 0) {
      failNextPushes--;
      throw NetworkException('timeout before server');
    }
    List<PushResult> results;
    final prev = _batchBodies[batchKey];
    if (prev != null) {
      if (prev != body) {
        throw ApiException(422, 'idempotency_key_reused');
      }
      results = _batchResponses[batchKey]!;
    } else {
      results = [];
      final decoded = (jsonDecode(body) as Map)['records'] as List;
      for (final w in decoded.cast<Map<String, Object?>>()) {
        final id = w['id']! as String;
        final key = w['idem_key']! as String;
        if (ledger.containsKey(key)) {
          results.add(PushResult(id: id, status: 'duplicate'));
        } else if (rejectIf?.call(w) ?? false) {
          results.add(
            PushResult(
              id: id,
              status: 'rejected',
              code: 'validation_failed',
              errors: const [
                {'field': 'payload.note', 'reason': 'required'},
              ],
            ),
          );
        } else if (conflictIf?.call(w) ?? false) {
          ledger[key] = w;
          results.add(
            PushResult(id: id, status: 'conflict', conflictId: 'c-$id'),
          );
        } else {
          ledger[key] = w;
          results.add(PushResult(id: id, status: 'accepted'));
        }
      }
      _batchBodies[batchKey] = body;
      _batchResponses[batchKey] = results;
    }
    if (dropNextResponses > 0) {
      dropNextResponses--;
      throw NetworkException('response lost');
    }
    return results.where((r) => !omitResultsFor.contains(r.id)).toList();
  }

  @override
  Future<PullPage> pull({
    String? cursor,
    required int limit,
    List<String>? entities,
  }) async {
    _guard();
    pullCursors.add(cursor);
    pullEntities.add(entities);
    final page =
        pages[cursor] ??
        PullPage(changes: const [], nextCursor: cursor, hasMore: false);
    if (entities == null || entities.isEmpty) return page;
    // Lọc theo entity như máy chủ (`?entities=`).
    return PullPage(
      changes: [
        for (final c in page.changes)
          if (entities.contains(c.entity)) c,
      ],
      nextCursor: page.nextCursor,
      hasMore: page.hasMore,
      serverTime: page.serverTime,
      reset: page.reset,
    );
  }

  @override
  Future<String> uploadFile({
    required String idempotencyKey,
    required Map<String, Object?> meta,
    required Uint8List ciphertext,
    String? fileRef,
  }) async {
    _guard();
    uploadKeys.add(idempotencyKey);
    uploadFileRefs.add(fileRef);
    if (failNextUploads > 0) {
      failNextUploads--;
      throw NetworkException('upload failed');
    }
    // Máy chủ dùng file_ref máy gửi (sync-contract); thiếu thì tự sinh.
    final ref = fileRef ?? 'fref-${idempotencyKey.hashCode.toUnsigned(32)}';
    files[idempotencyKey] = {
      'meta': meta,
      'size': ciphertext.length,
      'ref': ref,
    };
    if (dropNextUploadResponses > 0) {
      dropNextUploadResponses--;
      throw NetworkException('upload response lost');
    }
    return ref;
  }

  /// path → JSON trả cho GET (danh mục).
  final Map<String, Map<String, Object?>> getResponses = {};

  /// Bộ xử lý GET tùy biến (app kiểm thử API có tham số truy vấn). Trả null →
  /// dùng [getResponses]. Ném [ApiException]/[NetworkException] để mô phỏng lỗi.
  Future<Map<String, Object?>?> Function(
    String path,
    Map<String, String>? query,
  )?
  onGet;

  /// Bộ xử lý POST lệnh tùy biến (phản hồi theo nội dung). Trả null → mặc định.
  Future<Map<String, Object?>?> Function(
    String path,
    Map<String, Object?>? body,
  )?
  onPost;
  final List<String> gets = [];

  @override
  Future<Map<String, Object?>> getJson(
    String path, {
    Map<String, String>? query,
  }) async {
    _guard();
    final h = onGet;
    if (h != null) {
      gets.add(path);
      final r = await h(path, query);
      if (r != null) return r;
    }
    final key = query == null || query['evidence_type'] == null
        ? path
        : '$path?evidence_type=${query['evidence_type']}';
    gets.add(key);
    final r = getResponses[key];
    if (r == null) throw ApiException(404, 'not_found');
    return r;
  }

  @override
  Future<void> registerPushToken({
    required String deviceId,
    required String token,
    required String idempotencyKey,
  }) async {
    _guard();
    pushTokenKeys.add(idempotencyKey);
    if (failPushToken > 0) {
      failPushToken--;
      throw ApiException(503, 'notify.dependency_unavailable');
    }
    pushTokens[deviceId] = token;
  }

  @override
  Future<Map<String, Object?>> postAction({
    required String path,
    required String idempotencyKey,
    Map<String, Object?>? body,
  }) async {
    _guard();
    actionCalls.add((path, idempotencyKey));
    if (failNextActions > 0) {
      failNextActions--;
      throw NetworkException('action failed');
    }
    final err = actionErrors[path];
    if (err != null) throw ApiException(err.$1, err.$2);
    final prev = _actionResponses[idempotencyKey];
    if (prev != null) return prev;
    final h = onPost;
    if (h != null) {
      final r = await h(path, body);
      if (r != null) return _actionResponses[idempotencyKey] = r;
    }
    return _actionResponses.putIfAbsent(
      idempotencyKey,
      () => {'path': path, 'body': body},
    );
  }

  /// Số lần máy chủ thực hiện mỗi path (khóa trùng chỉ tính một).
  Map<String, int> get actionEffects {
    final m = <String, int>{};
    final seen = <String>{};
    for (final (path, key) in actionCalls) {
      if (seen.add(key)) m[path] = (m[path] ?? 0) + 1;
    }
    return m;
  }
}

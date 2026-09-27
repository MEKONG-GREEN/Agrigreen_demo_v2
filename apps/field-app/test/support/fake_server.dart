import 'dart:convert';
import 'dart:typed_data';

import 'package:agrigreen_field/data/outbox/outbox_repository.dart';
import 'package:agrigreen_field/data/sync/sync_api.dart';
import 'package:agrigreen_field/domain/outbox_record.dart';

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
  Future<PullPage> pull({String? cursor, required int limit}) async {
    _guard();
    pullCursors.add(cursor);
    return pages[cursor] ??
        PullPage(changes: const [], nextCursor: cursor, hasMore: false);
  }

  @override
  Future<String> uploadFile({
    required String idempotencyKey,
    required Map<String, Object?> meta,
    required Uint8List ciphertext,
  }) async {
    _guard();
    uploadKeys.add(idempotencyKey);
    if (failNextUploads > 0) {
      failNextUploads--;
      throw NetworkException('upload failed');
    }
    final ref = 'fref-${idempotencyKey.hashCode.toUnsigned(32)}';
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
}

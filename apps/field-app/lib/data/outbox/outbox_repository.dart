import 'dart:convert';

import 'package:sqflite_common/sqlite_api.dart';

import '../../core/clock.dart';
import '../../core/ids.dart';
import '../../domain/outbox_record.dart';
import '../db/local_db.dart';

class OutboxException implements Exception {
  OutboxException(this.message);
  final String message;
  @override
  String toString() => message;
}

/// Kết quả từng bản ghi trong phản hồi `POST /v1/sync/push`.
class PushResult {
  const PushResult({
    required this.id,
    required this.status,
    this.conflictId,
    this.code,
    this.errors,
  });

  final String id;

  /// accepted | duplicate | conflict | rejected
  final String status;
  final String? conflictId;
  final String? code;
  final List<Object?>? errors;

  factory PushResult.fromJson(Map<String, Object?> j) => PushResult(
    id: j['id']! as String,
    status: j['status']! as String,
    conflictId: j['conflict_id'] as String?,
    code: j['code'] as String?,
    errors: j['errors'] as List<Object?>?,
  );
}

class ClaimedBatch {
  const ClaimedBatch(this.batchKey, this.records);
  final String batchKey;
  final List<OutboxRecord> records;
}

/// Tóm tắt trạng thái hàng đợi cho màn hình chủ.
class OutboxCounts {
  const OutboxCounts({
    this.pending = 0,
    this.sent = 0,
    this.conflict = 0,
    this.rejected = 0,
    this.superseded = 0,
  });
  final int pending;
  final int sent;
  final int conflict;
  final int rejected;
  final int superseded;
}

/// Hàng đợi chiều lên. Bảo đảm:
/// - `seq` tăng đơn điệu theo bộ đếm lưu bền (không tái dùng sau khi xóa),
///   nên `idem_key = dev:<device_id>:<seq>` không bao giờ lặp;
/// - ghi bản ghi và tăng bộ đếm trong **cùng một giao dịch**;
/// - lô đang gửi dở được gửi lại **y nguyên** với cùng Idempotency-Key;
/// - không có API sửa nội dung — sửa sai = bản ghi bù (`op = correct`).
class OutboxRepository {
  OutboxRepository({
    required LocalDb db,
    required String deviceId,
    required Clock clock,
    required IdGenerator ids,
  }) : _db = db,
       _deviceId = deviceId,
       _clock = clock,
       _ids = ids;

  final LocalDb _db;
  final String _deviceId;
  final Clock _clock;
  final IdGenerator _ids;

  static const _seqKey = 'outbox.seq';

  Database get _sql => _db.db;

  Future<int> _nextSeq(DatabaseExecutor tx) async {
    final cur = int.tryParse(await _db.getMeta(_seqKey, tx: tx) ?? '') ?? 0;
    final next = cur + 1;
    await _db.setMeta(_seqKey, '$next', tx: tx);
    return next;
  }

  /// Tạo bản ghi mới (`op = create`). Truyền [tx] để ghi trong giao dịch khác.
  Future<OutboxRecord> enqueue({
    required String entity,
    required Map<String, Object?> payload,
    DateTime? eventTime,
    String? eventTimeRfc3339,
    DatabaseExecutor? tx,
  }) {
    return _insert(
      entity: entity,
      op: RecordOp.create,
      payload: payload,
      eventTime: eventTimeRfc3339 ?? formatRfc3339(eventTime ?? _clock.now()),
      tx: tx,
    );
  }

  /// Sửa sai bằng bản ghi bù. Bản gốc không bị đổi.
  ///
  /// [lockedPayloadKeys]: các khóa trong payload phải giữ nguyên như bản gốc
  /// (ví dụ tọa độ GPS và thời điểm chụp của nhật ký — US-LOG-01 AC-3).
  /// `event_time` luôn lấy theo bản gốc.
  Future<OutboxRecord> correct({
    required String originalId,
    required String reason,
    required Map<String, Object?> payload,
    Set<String> lockedPayloadKeys = const {},
  }) async {
    if (reason.trim().isEmpty) {
      throw OutboxException('Bản ghi bù bắt buộc có lý do sửa.');
    }
    return _sql.transaction((tx) async {
      final orig = await byId(originalId, tx: tx);
      if (orig == null) {
        throw OutboxException(
          'Không tìm thấy bản ghi gốc trên máy để tạo bản ghi bù.',
        );
      }
      for (final k in lockedPayloadKeys) {
        if (jsonEncode(orig.payload[k]) != jsonEncode(payload[k])) {
          throw LockedFieldException(k);
        }
      }
      return _insert(
        entity: orig.entity,
        op: RecordOp.correct,
        payload: payload,
        eventTime: orig.eventTime,
        supersedesId: orig.id,
        correctionReason: reason.trim(),
        tx: tx,
      );
    });
  }

  Future<OutboxRecord> _insert({
    required String entity,
    required RecordOp op,
    required Map<String, Object?> payload,
    required String eventTime,
    String? supersedesId,
    String? correctionReason,
    DatabaseExecutor? tx,
  }) async {
    if (!PushEntity.all.contains(entity)) {
      throw OutboxException('Thực thể không hợp lệ: $entity');
    }
    Future<OutboxRecord> run(DatabaseExecutor t) async {
      final seq = await _nextSeq(t);
      final now = _clock.now();
      final rec = OutboxRecord(
        id: _ids.newId(),
        seq: seq,
        idemKey: idemKeyFor(_deviceId, seq),
        entity: entity,
        op: op,
        eventTime: eventTime,
        recordedAt: now,
        payload: payload,
        status: OutboxStatus.pending,
        supersedesId: supersedesId,
        correctionReason: correctionReason,
      );
      await t.insert('outbox', {
        'id': rec.id,
        'seq': rec.seq,
        'idem_key': rec.idemKey,
        'entity': rec.entity,
        'op': rec.op.name,
        'event_time': rec.eventTime,
        'recorded_at': now.millisecondsSinceEpoch,
        'supersedes_id': supersedesId,
        'correction_reason': correctionReason,
        'payload': jsonEncode(payload),
        'status': OutboxStatus.pending.name,
      });
      return rec;
    }

    if (tx != null) return run(tx);
    return _sql.transaction(run);
  }

  Future<OutboxRecord?> byId(String id, {DatabaseExecutor? tx}) async {
    final rows = await (tx ?? _sql).query(
      'outbox',
      where: 'id = ?',
      whereArgs: [id],
    );
    return rows.isEmpty ? null : OutboxRecord.fromRow(rows.first);
  }

  Future<List<OutboxRecord>> list({
    Set<OutboxStatus>? statuses,
    String? entity,
    int? limit,
  }) async {
    final where = <String>[];
    final args = <Object?>[];
    if (statuses != null && statuses.isNotEmpty) {
      where.add('status IN (${List.filled(statuses.length, '?').join(',')})');
      args.addAll(statuses.map((s) => s.name));
    }
    if (entity != null) {
      where.add('entity = ?');
      args.add(entity);
    }
    final rows = await _sql.query(
      'outbox',
      where: where.isEmpty ? null : where.join(' AND '),
      whereArgs: args,
      orderBy: 'seq DESC',
      limit: limit,
    );
    return rows.map(OutboxRecord.fromRow).toList();
  }

  Future<OutboxCounts> counts() async {
    final rows = await _sql.rawQuery(
      'SELECT status, COUNT(*) AS n FROM outbox GROUP BY status',
    );
    final m = {for (final r in rows) r['status']! as String: r['n']! as int};
    return OutboxCounts(
      pending: m['pending'] ?? 0,
      sent: m['sent'] ?? 0,
      conflict: m['conflict'] ?? 0,
      rejected: m['rejected'] ?? 0,
      superseded: m['superseded'] ?? 0,
    );
  }

  /// Lấy lô kế tiếp để gửi. Nếu có lô gửi dở (đã gán batch_key nhưng chưa có
  /// kết quả — ví dụ app bị tắt giữa chừng), trả lại **đúng lô đó** để gửi lại
  /// với cùng Idempotency-Key và cùng thân.
  Future<ClaimedBatch?> claimBatch(int maxSize) {
    return _sql.transaction((tx) async {
      final inflight = await tx.query(
        'outbox',
        columns: ['batch_key'],
        where: "status = 'pending' AND batch_key IS NOT NULL",
        orderBy: 'seq',
        limit: 1,
      );
      String key;
      if (inflight.isNotEmpty) {
        key = inflight.first['batch_key']! as String;
      } else {
        final ids = await tx.query(
          'outbox',
          columns: ['id'],
          where: "status = 'pending' AND batch_key IS NULL",
          orderBy: 'seq',
          limit: maxSize,
        );
        if (ids.isEmpty) return null;
        key = _ids.newId();
        final list = ids.map((r) => r['id']! as String).toList();
        await tx.rawUpdate(
          'UPDATE outbox SET batch_key = ? WHERE id IN '
          '(${List.filled(list.length, '?').join(',')})',
          [key, ...list],
        );
      }
      await tx.rawUpdate(
        "UPDATE outbox SET attempts = attempts + 1 WHERE batch_key = ? AND status = 'pending'",
        [key],
      );
      final rows = await tx.query(
        'outbox',
        where: "batch_key = ? AND status = 'pending'",
        whereArgs: [key],
        orderBy: 'seq',
      );
      return ClaimedBatch(key, rows.map(OutboxRecord.fromRow).toList());
    });
  }

  /// Áp kết quả từng bản ghi. Cả lô trong một giao dịch.
  Future<void> applyResults(String batchKey, List<PushResult> results) {
    final now = _clock.now().millisecondsSinceEpoch;
    return _sql.transaction((tx) async {
      for (final r in results) {
        final rows = await tx.query(
          'outbox',
          columns: ['id', 'op', 'supersedes_id', 'status'],
          where: 'id = ? AND batch_key = ?',
          whereArgs: [r.id, batchKey],
        );
        if (rows.isEmpty) continue; // kết quả lạ — bỏ qua
        if (rows.first['status'] != OutboxStatus.pending.name) continue;
        switch (r.status) {
          case 'accepted':
          case 'duplicate':
            await tx.update(
              'outbox',
              {
                'status': OutboxStatus.sent.name,
                'acked_at': now,
                'error_code': null,
                'errors': null,
              },
              where: 'id = ?',
              whereArgs: [r.id],
            );
            final sup = rows.first['supersedes_id'] as String?;
            if (sup != null) {
              // Bản gốc bị từ chối đã được thay thế → cho phép purge sau 48 giờ.
              await tx.update(
                'outbox',
                {'status': OutboxStatus.superseded.name, 'acked_at': now},
                where: 'id = ? AND status = ?',
                whereArgs: [sup, OutboxStatus.rejected.name],
              );
            }
          case 'conflict':
            await tx.update(
              'outbox',
              {
                'status': OutboxStatus.conflict.name,
                'acked_at': now,
                'conflict_id': r.conflictId,
              },
              where: 'id = ?',
              whereArgs: [r.id],
            );
          case 'rejected':
            await tx.update(
              'outbox',
              {
                'status': OutboxStatus.rejected.name,
                'error_code': r.code ?? 'rejected',
                'errors': jsonEncode(r.errors ?? const []),
              },
              where: 'id = ?',
              whereArgs: [r.id],
            );
          default:
            // Trạng thái chưa biết: giữ pending, gửi lại ở lô sau.
            break;
        }
      }
      // Bản ghi không có kết quả: tách khỏi lô để lô sau mang theo (vẫn cùng
      // idem_key nên máy chủ chống trùng).
      await tx.rawUpdate(
        "UPDATE outbox SET batch_key = NULL WHERE batch_key = ? AND status = 'pending'",
        [batchKey],
      );
    });
  }

  /// Bỏ gán lô (khi máy chủ báo Idempotency-Key đã dùng cho thân khác). Các
  /// bản ghi vẫn `pending` và giữ nguyên `idem_key`, nên lô mới không gây trùng.
  Future<void> releaseBatch(String batchKey) => _sql.rawUpdate(
    "UPDATE outbox SET batch_key = NULL WHERE batch_key = ? AND status = 'pending'",
    [batchKey],
  );

  /// Xóa bản ghi đã được xác nhận quá [retention]. Không bao giờ xóa
  /// `pending` hoặc `rejected`.
  Future<int> purgeAcked(Duration retention) {
    final cutoff = _clock.now().subtract(retention).millisecondsSinceEpoch;
    return _sql.delete(
      'outbox',
      where: "status IN ('sent','conflict','superseded') AND acked_at <= ?",
      whereArgs: [cutoff],
    );
  }
}

/// Người dùng cố đổi trường bị khóa sau khi lưu.
class LockedFieldException extends OutboxException {
  LockedFieldException(this.field)
    : super('Tọa độ GPS và thời gian không thể chỉnh sửa sau khi lưu.');
  final String field;
}

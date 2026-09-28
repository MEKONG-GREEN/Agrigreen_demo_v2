import 'dart:convert';

/// Thực thể chiều lên G1 (hợp đồng đồng bộ).
abstract final class PushEntity {
  static const plotBoundary = 'plot_boundary';
  static const fieldReport = 'field_report';
  static const taskUpdate = 'task_update';
  static const consent = 'consent';
  static const evidence = 'evidence';
  static const all = {plotBoundary, fieldReport, taskUpdate, consent, evidence};
}

enum RecordOp {
  create,
  correct;

  static RecordOp parse(String s) =>
      RecordOp.values.firstWhere((e) => e.name == s);
}

/// Trạng thái cục bộ của một bản ghi trong hàng đợi.
enum OutboxStatus {
  /// Chưa được máy chủ xác nhận (kể cả đang gửi dở).
  pending,

  /// accepted/duplicate — đã gửi, chờ 48 giờ rồi xóa.
  sent,

  /// Máy chủ giữ cả hai bản ghi, chờ xử lý. Máy coi là đã gửi, vẫn hiển thị.
  conflict,

  /// Dữ liệu sai — giữ lại, hiển thị lỗi, người dùng tạo bản ghi bù.
  rejected,

  /// Bản ghi bị từ chối đã được thay bằng bản ghi bù được chấp nhận.
  superseded;

  static OutboxStatus parse(String s) =>
      OutboxStatus.values.firstWhere((e) => e.name == s);

  String get label => switch (this) {
    OutboxStatus.pending => 'Chờ đồng bộ',
    OutboxStatus.sent => 'Đã đồng bộ',
    OutboxStatus.conflict => 'Xung đột — chờ xử lý',
    OutboxStatus.rejected => 'Bị từ chối',
    OutboxStatus.superseded => 'Đã thay bằng bản ghi bù',
  };
}

class OutboxRecord {
  const OutboxRecord({
    required this.id,
    required this.seq,
    required this.idemKey,
    required this.entity,
    required this.op,
    required this.eventTime,
    required this.recordedAt,
    required this.payload,
    required this.status,
    this.supersedesId,
    this.correctionReason,
    this.batchKey,
    this.attempts = 0,
    this.ackedAt,
    this.conflictId,
    this.errorCode,
    this.errors,
    this.replacedBy,
  });

  final String id;
  final int seq;
  final String idemKey;
  final String entity;
  final RecordOp op;

  /// RFC 3339 có offset — lúc sự việc xảy ra (R-13).
  final String eventTime;

  /// Lúc ghi nhận trên máy.
  final DateTime recordedAt;
  final Map<String, Object?> payload;
  final OutboxStatus status;
  final String? supersedesId;
  final String? correctionReason;
  final String? batchKey;
  final int attempts;
  final DateTime? ackedAt;
  final String? conflictId;
  final String? errorCode;
  final List<Object?>? errors;

  /// Bản ghi bị từ chối đã được thay bằng bản ghi **mới** (op create) có id này
  /// (hợp đồng: bản bị từ chối không có ở máy chủ nên không dùng `correct`).
  final String? replacedBy;

  /// Mã lỗi `geo.*` và lỗi theo trường (để hiển thị).
  String get errorSummary {
    final parts = <String>[?errorCode];
    for (final e in errors ?? const <Object?>[]) {
      if (e is Map) {
        final f = e['field'];
        final r = e['reason'];
        parts.add([?f, ?r].join(': '));
      }
    }
    return parts.join(' · ');
  }

  /// Thân bản ghi gửi lên `POST /v1/sync/push`.
  Map<String, Object?> toWire() => {
    'id': id,
    'idem_key': idemKey,
    'entity': entity,
    'op': op.name,
    'event_time': eventTime,
    'supersedes_id': supersedesId,
    'correction_reason': correctionReason,
    'payload': payload,
  };

  factory OutboxRecord.fromRow(Map<String, Object?> r) => OutboxRecord(
    id: r['id']! as String,
    seq: r['seq']! as int,
    idemKey: r['idem_key']! as String,
    entity: r['entity']! as String,
    op: RecordOp.parse(r['op']! as String),
    eventTime: r['event_time']! as String,
    recordedAt: DateTime.fromMillisecondsSinceEpoch(r['recorded_at']! as int),
    payload: (jsonDecode(r['payload']! as String) as Map)
        .cast<String, Object?>(),
    status: OutboxStatus.parse(r['status']! as String),
    supersedesId: r['supersedes_id'] as String?,
    correctionReason: r['correction_reason'] as String?,
    batchKey: r['batch_key'] as String?,
    attempts: r['attempts']! as int,
    ackedAt: r['acked_at'] == null
        ? null
        : DateTime.fromMillisecondsSinceEpoch(r['acked_at']! as int),
    conflictId: r['conflict_id'] as String?,
    errorCode: r['error_code'] as String?,
    errors: r['errors'] == null
        ? null
        : (jsonDecode(r['errors']! as String) as List),
    replacedBy: r['replaced_by'] as String?,
  );
}

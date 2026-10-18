import 'dart:convert';

/// Thực thể chiều lên (hợp đồng đồng bộ): G1 và G2 (App HTX).
abstract final class PushEntity {
  static const plotBoundary = 'plot_boundary';
  static const fieldReport = 'field_report';
  static const taskUpdate = 'task_update';
  static const consent = 'consent';
  static const evidence = 'evidence';

  /// G2 pf/cropcycle (App HTX): mở/sửa/hủy vụ (HTX US-SEASON-01).
  static const seasonOpen = 'season_open';

  /// G2 pf/cropcycle: nhật ký canh tác (HTX US-LOG-01/02/04).
  static const farmLog = 'farm_log';

  /// G2 pf/cropcycle: nhật ký hàng loạt — một đầu lô (HTX US-LOG-03).
  static const farmLogBatch = 'farm_log_batch';

  /// G2 pf/cropcycle: khai báo sản lượng (HTX US-YIELD-01).
  static const harvest = 'harvest';

  /// G2 pf/extension: yêu cầu hỗ trợ của nông dân (HTX US-SUPPORT-01).
  static const supportRequest = 'support_request';

  /// G3 mkg/fieldops (App hiện trường MKG): đếm cuộn theo công đoạn —
  /// `POST /v1/fieldops/sync/push` của vùng mkg, KHÔNG phải pf/sync.
  static const rollCount = 'roll_count';

  /// G3-2 pf/cropcycle: quản lý HTX xác nhận / tranh chấp bảng kê số cuộn
  /// thu gom MKG (RB-6 chiều mua).
  static const htxCollectionResponse = 'htx_collection_response';

  /// KN v7.7 P7 (pf/extension reportcycle): lưu nháp / gửi báo cáo theo biểu
  /// mẫu khi ngoại tuyến (FN-XA-03.02). Chỉ `op: create`; `id` = id bài nộp.
  static const reportSubmission = 'report_submission';

  /// W11.42 (pf/forms `cap/forms`): một lần gửi biểu kỳ native (CNTS BM-01/04/
  /// 05/17) tạo ngoại tuyến. Chỉ `op: create`; `id` = mã phiên bản bài nộp;
  /// kỳ đã chốt lúc đồng bộ → máy chủ GIỮ "Chờ mở kỳ".
  static const formsSubmission = 'forms_submission';

  static const g1 = {plotBoundary, fieldReport, taskUpdate, consent, evidence};
  static const kn = {reportSubmission, formsSubmission};
  static const g3 = {htxCollectionResponse};
  static const g2 = {
    seasonOpen,
    farmLog,
    farmLogBatch,
    harvest,
    supportRequest,
  };

  /// Thực thể của nền tảng (`/v1/sync/push`) — mặc định của OutboxRepository.
  static const all = {...g1, ...g2, ...g3, ...kn};

  /// Thực thể của đồng bộ vùng mkg (`/v1/fieldops/sync/push`).
  static const mkgFieldops = {rollCount};
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

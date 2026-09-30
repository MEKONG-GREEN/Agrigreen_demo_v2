import 'dart:convert';

import 'package:sqflite_common/sqlite_api.dart';

import '../../core/clock.dart';
import '../../core/ids.dart';
import '../db/local_db.dart';

/// Lệnh API xếp hàng khi ngoại tuyến. Mọi lệnh là POST idempotent của
/// Application API (gọi lặp giữ kết quả lần đầu), nên phát lại an toàn.
enum ApiActionKind {
  /// `POST /v1/notifications/{id}/read`
  notificationRead('notification_read'),

  /// `POST /v1/notifications/read-all`
  notificationReadAll('notification_read_all'),

  /// `POST /v1/extension/directives/{id}/ack`
  directiveAck('directive_ack'),

  /// `POST /v1/documents/{ref}/save {note}`
  librarySave('library_save'),

  /// `POST /v1/documents/{ref}/unsave`
  libraryUnsave('library_unsave'),

  /// G2 (App HTX): `POST /v1/cropcycle/farm-logs/{id}/reviews {decision, note}`
  /// — quản lý HTX duyệt / yêu cầu bổ sung nhật ký (HTX US-LOG-05).
  farmLogReview('farm_log_review'),

  /// G3 (App hiện trường MKG, api-internal): `POST
  /// /v1/fieldops/orders/{id}/crew-shifts {crew_ref, kind, headcount,
  /// event_time, device_id}` — mở / đóng ca tổ gom.
  fieldopsCrewShift('fieldops_crew_shift'),

  /// G4 (App hiện trường MKG — tài xế, api-internal): `POST
  /// /v1/logistics/trips/{id}/milestones {kind, event_time, lat, lng}` — mốc
  /// xuất phát / đến của chuyến (chỉ tiến, L-02).
  logisticsMilestone('logistics_milestone');

  const ApiActionKind(this.code);
  final String code;

  static ApiActionKind parse(String c) => values.firstWhere((k) => k.code == c);

  String pathFor(String? target) => switch (this) {
    ApiActionKind.notificationRead => '/v1/notifications/$target/read',
    ApiActionKind.notificationReadAll => '/v1/notifications/read-all',
    ApiActionKind.directiveAck => '/v1/extension/directives/$target/ack',
    ApiActionKind.librarySave => '/v1/documents/$target/save',
    ApiActionKind.libraryUnsave => '/v1/documents/$target/unsave',
    ApiActionKind.farmLogReview => '/v1/cropcycle/farm-logs/$target/reviews',
    ApiActionKind.fieldopsCrewShift =>
      '/v1/fieldops/orders/$target/crew-shifts',
    ApiActionKind.logisticsMilestone =>
      '/v1/logistics/trips/$target/milestones',
  };
}

enum ApiActionStatus { pending, done, failed }

class ApiAction {
  const ApiAction({
    required this.id,
    required this.seq,
    required this.kind,
    required this.status,
    required this.createdAt,
    this.target,
    this.body,
    this.attempts = 0,
    this.lastError,
  });

  final String id;
  final int seq;
  final ApiActionKind kind;
  final String? target;
  final Map<String, Object?>? body;
  final ApiActionStatus status;
  final DateTime createdAt;
  final int attempts;
  final String? lastError;

  /// Idempotency-Key ổn định cho mọi lần phát lại (khớp regex của máy chủ).
  String get idempotencyKey => 'act:$id';
  String get path => kind.pathFor(target);

  factory ApiAction.fromRow(Map<String, Object?> r) => ApiAction(
    id: r['id']! as String,
    seq: r['seq']! as int,
    kind: ApiActionKind.parse(r['kind']! as String),
    target: r['target'] as String?,
    body: r['body'] == null
        ? null
        : (jsonDecode(r['body']! as String) as Map).cast<String, Object?>(),
    status: ApiActionStatus.values.byName(r['status']! as String),
    createdAt: DateTime.fromMillisecondsSinceEpoch(r['created_at']! as int),
    attempts: r['attempts']! as int,
    lastError: r['last_error'] as String?,
  );
}

/// Hàng đợi lệnh API (bảng `api_action`). Thứ tự phát lại theo `seq`.
class ActionQueue {
  ActionQueue({
    required LocalDb db,
    required Clock clock,
    required IdGenerator ids,
  }) : _db = db,
       _clock = clock,
       _ids = ids;

  final LocalDb _db;
  final Clock _clock;
  final IdGenerator _ids;

  static const _seqKey = 'api_action.seq';

  Database get _sql => _db.db;

  /// Giữ lệnh đã xong 48 giờ để lớp phủ trạng thái không "nháy" trước khi
  /// pull mang trạng thái mới xuống.
  static const doneRetention = Duration(hours: 48);

  Future<ApiAction> enqueue(
    ApiActionKind kind, {
    String? target,
    Map<String, Object?>? body,
  }) {
    return _sql.transaction((tx) async {
      final cur = int.tryParse(await _db.getMeta(_seqKey, tx: tx) ?? '') ?? 0;
      final seq = cur + 1;
      await _db.setMeta(_seqKey, '$seq', tx: tx);
      final now = _clock.now();
      final a = ApiAction(
        id: _ids.newId(),
        seq: seq,
        kind: kind,
        target: target,
        body: body,
        status: ApiActionStatus.pending,
        createdAt: now,
      );
      await tx.insert('api_action', {
        'id': a.id,
        'seq': seq,
        'kind': kind.code,
        'target': target,
        'body': body == null ? null : jsonEncode(body),
        'status': 'pending',
        'created_at': now.millisecondsSinceEpoch,
      });
      return a;
    });
  }

  Future<List<ApiAction>> pending() async {
    final rows = await _sql.query(
      'api_action',
      where: "status = 'pending'",
      orderBy: 'seq',
    );
    return rows.map(ApiAction.fromRow).toList();
  }

  /// Lệnh chưa thất bại (chờ hoặc đã xong) — dùng làm lớp phủ trạng thái cục bộ.
  Future<List<ApiAction>> active({ApiActionKind? kind, String? target}) async {
    final where = ["status != 'failed'"];
    final args = <Object?>[];
    if (kind != null) {
      where.add('kind = ?');
      args.add(kind.code);
    }
    if (target != null) {
      where.add('target = ?');
      args.add(target);
    }
    final rows = await _sql.query(
      'api_action',
      where: where.join(' AND '),
      whereArgs: args,
      orderBy: 'seq',
    );
    return rows.map(ApiAction.fromRow).toList();
  }

  /// Mọi lệnh của [kind] (kể cả đã thất bại, để hiển thị lỗi), theo `seq`.
  Future<List<ApiAction>> ofKind(ApiActionKind kind) async {
    final rows = await _sql.query(
      'api_action',
      where: 'kind = ?',
      whereArgs: [kind.code],
      orderBy: 'seq',
    );
    return rows.map(ApiAction.fromRow).toList();
  }

  Future<void> markDone(String id) => _sql.update(
    'api_action',
    {
      'status': 'done',
      'done_at': _clock.now().millisecondsSinceEpoch,
      'last_error': null,
    },
    where: 'id = ?',
    whereArgs: [id],
  );

  /// Lỗi dữ liệu (404, 422…): bỏ lệnh, không thử lại.
  Future<void> markFailed(String id, String code) => _sql.rawUpdate(
    "UPDATE api_action SET status = 'failed', attempts = attempts + 1, "
    'last_error = ? WHERE id = ?',
    [code, id],
  );

  Future<void> recordAttempt(String id, String code) => _sql.rawUpdate(
    'UPDATE api_action SET attempts = attempts + 1, last_error = ? WHERE id = ?',
    [code, id],
  );

  Future<int> pendingCount() async {
    final r = await _sql.rawQuery(
      "SELECT COUNT(*) AS n FROM api_action WHERE status = 'pending'",
    );
    return r.first['n']! as int;
  }

  Future<int> purgeDone() {
    final cutoff = _clock.now().subtract(doneRetention).millisecondsSinceEpoch;
    return _sql.delete(
      'api_action',
      where:
          "status IN ('done','failed') AND COALESCE(done_at, created_at) <= ?",
      whereArgs: [cutoff],
    );
  }
}

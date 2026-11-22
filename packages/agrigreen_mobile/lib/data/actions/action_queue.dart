import 'dart:convert';

import 'package:sqflite_common/sqlite_api.dart';

import '../../core/clock.dart';
import '../../core/ids.dart';
import '../db/local_db.dart';

/// Loại lệnh API xếp hàng: mã lưu trong bảng `api_action` + đường dẫn phát lại.
///
/// Lõi chỉ khai báo lệnh của Application API nền tảng ([ApiActionKind]).
/// Lệnh của vùng khác (vd api-internal vùng mkg: `/v1/fieldops`,
/// `/v1/logistics`) do CHÍNH app đó khai báo và đăng ký qua
/// `ActionQueue(kinds: ...)` — bản build của app khác (App hiện trường KN)
/// không chứa và không xếp hàng được các lệnh này (phân vùng mạng §29,
/// R-SEC-01).
abstract interface class ActionKind {
  /// Mã ổn định lưu trong cột `api_action.kind` (duy nhất trong một app).
  String get code;

  /// Phương thức HTTP khi phát lại.
  String get method;

  /// Đường dẫn phát lại cho [target].
  String pathFor(String? target);
}

/// Lệnh API xếp hàng khi ngoại tuyến. Mọi lệnh là POST idempotent của
/// Application API (gọi lặp giữ kết quả lần đầu), nên phát lại an toàn.
enum ApiActionKind implements ActionKind {
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

  // G3/G4 (mở/đóng ca tổ gom, mốc chuyến — api-internal vùng mkg) KHÔNG ở
  // đây: App hiện trường MKG tự khai báo (MkgActionKind) và đăng ký.

  /// KN v7.7 P5 (App hiện trường): `POST /v1/extension/work-schedules
  /// {id, week_start, action, base_version, items}` — lưu nháp / gửi duyệt
  /// lịch công tác tuần. Thân giữ nguyên giữa các lần phát lại.
  workScheduleSave('work_schedule_save'),

  /// FN-XA-03.03, KNSPLIT-2-KN-RECORDS (#67, #161): `POST
  /// /v1/kn/producer-records {kind, commune_code, …}` — thêm hồ sơ KN riêng
  /// HTX / Nông hộ / Tổ hợp tác (sổ `pf_extension.kn_producer`). KN không
  /// tạo/sửa HTX nền tảng: lệnh ghi `/v1/kn/cooperatives…` cũ đã bỏ khỏi app
  /// (xem [RetiredActionKind]).
  knRecordCreate('kn_record_create'),

  /// `POST /v1/kn/producer-records/{ref} {…, base_version}` — sửa hồ sơ (thay
  /// toàn bộ trường nhập được, khóa lạc quan theo phiên bản).
  knRecordUpdate('kn_record_update'),

  /// `POST /v1/kn/producer-records/{ref}/deactivate {reason, base_version}`.
  knRecordDeactivate('kn_record_deactivate'),

  /// `POST /v1/kn/producer-records/{ref}/reactivate {reason, base_version}`.
  knRecordReactivate('kn_record_reactivate'),

  /// FN-XA-03.04: `PUT /v1/kn/machine-counts/{ref}` — thay toàn
  /// bộ khai báo số lượng máy của HTX.
  knMachineCountsSave('kn_machine_counts_save', method: 'PUT'),

  /// FN-XA-03.06: `POST /v1/kn/materials` — ghi nhận vật tư.
  knMaterialsCreate('kn_materials_create'),

  /// `POST /v1/kn/materials/{id}/correct` — bản ghi bù có lý do.
  knMaterialCorrect('kn_material_correct'),

  /// FN-XA-03.05: `PUT /v1/kn/plots/{ref}` — sửa thuộc tính thửa.
  knPlotUpdate('kn_plot_update', method: 'PUT');

  const ApiActionKind(this.code, {this.method = 'POST'});
  @override
  final String code;

  /// Phương thức HTTP khi phát lại (mặc định POST).
  @override
  final String method;

  static ApiActionKind parse(String c) => values.firstWhere((k) => k.code == c);

  @override
  String pathFor(String? target) => switch (this) {
    ApiActionKind.notificationRead => '/v1/notifications/$target/read',
    ApiActionKind.notificationReadAll => '/v1/notifications/read-all',
    ApiActionKind.directiveAck => '/v1/extension/directives/$target/ack',
    ApiActionKind.librarySave => '/v1/documents/$target/save',
    ApiActionKind.libraryUnsave => '/v1/documents/$target/unsave',
    ApiActionKind.farmLogReview => '/v1/cropcycle/farm-logs/$target/reviews',
    ApiActionKind.workScheduleSave => '/v1/extension/work-schedules',
    ApiActionKind.knRecordCreate => '/v1/kn/producer-records',
    ApiActionKind.knRecordUpdate => '/v1/kn/producer-records/$target',
    ApiActionKind.knRecordDeactivate =>
      '/v1/kn/producer-records/$target/deactivate',
    ApiActionKind.knRecordReactivate =>
      '/v1/kn/producer-records/$target/reactivate',
    ApiActionKind.knMachineCountsSave => '/v1/kn/machine-counts/$target',
    ApiActionKind.knMaterialsCreate => '/v1/kn/materials',
    ApiActionKind.knMaterialCorrect => '/v1/kn/materials/$target/correct',
    ApiActionKind.knPlotUpdate => '/v1/kn/plots/$target',
  };
}

/// Lệnh đã bỏ khỏi app nhưng có thể còn trong `api_action` của máy cài bản
/// cũ: các lệnh ghi HTX / nông hộ nền tảng qua `/v1/kn/cooperatives…`
/// (KNSPLIT-2-KN-RECORDS #161 — KN không ghi dữ liệu HTX). Chỉ để đọc được
/// dòng cũ (lớp phủ, dọn 48 giờ); không xếp hàng mới, không bao giờ phát lại:
/// migration cục bộ v4 đánh dấu dòng còn chờ là `failed` (`retired`) và
/// [ActionQueue.pending] bỏ qua chúng.
final class RetiredActionKind implements ActionKind {
  const RetiredActionKind._(this.code);

  @override
  final String code;

  @override
  String get method => 'POST';

  @override
  String pathFor(String? target) =>
      throw StateError('api_action: lệnh "$code" đã bỏ, không phát lại');

  /// Mã lỗi ghi vào `last_error` khi bỏ lệnh cũ.
  static const retiredCode = 'retired';

  static const values = [
    RetiredActionKind._('kn_cooperative_create'),
    RetiredActionKind._('kn_cooperative_update'),
    RetiredActionKind._('kn_cooperative_deactivate'),
    RetiredActionKind._('kn_household_create'),
    RetiredActionKind._('kn_household_update'),
  ];

  static final codes = {for (final k in values) k.code};
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
  final ActionKind kind;
  final String? target;
  final Map<String, Object?>? body;
  final ApiActionStatus status;
  final DateTime createdAt;
  final int attempts;
  final String? lastError;

  /// Idempotency-Key ổn định cho mọi lần phát lại (khớp regex của máy chủ).
  String get idempotencyKey => 'act:$id';
  String get path => kind.pathFor(target);
  String get method => kind.method;

  /// [parseKind] tra mã `kind` → loại lệnh (mặc định chỉ lệnh của lõi).
  factory ApiAction.fromRow(
    Map<String, Object?> r, {
    ActionKind Function(String code) parseKind = ApiActionKind.parse,
  }) => ApiAction(
    id: r['id']! as String,
    seq: r['seq']! as int,
    kind: parseKind(r['kind']! as String),
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
///
/// Chỉ xếp hàng / đọc được các loại lệnh đã đăng ký: [ApiActionKind] của lõi
/// cộng [extraKinds] do app truyền vào (vd App hiện trường MKG đăng ký lệnh
/// api-internal của mình). Lệnh chưa đăng ký bị từ chối khi `enqueue`.
class ActionQueue {
  ActionQueue({
    required LocalDb db,
    required Clock clock,
    required IdGenerator ids,
    Iterable<ActionKind> extraKinds = const [],
  }) : _db = db,
       _clock = clock,
       _ids = ids,
       _kinds = _registry(extraKinds);

  final LocalDb _db;
  final Clock _clock;
  final IdGenerator _ids;
  final Map<String, ActionKind> _kinds;

  static Map<String, ActionKind> _registry(Iterable<ActionKind> extra) {
    final m = <String, ActionKind>{};
    for (final k in [
      ...ApiActionKind.values,
      ...RetiredActionKind.values,
      ...extra,
    ]) {
      final prev = m[k.code];
      if (prev != null && !identical(prev, k)) {
        throw ArgumentError.value(k.code, 'extraKinds', 'trùng mã loại lệnh');
      }
      m[k.code] = k;
    }
    return Map.unmodifiable(m);
  }

  /// Các loại lệnh hàng đợi này chấp nhận.
  Iterable<ActionKind> get kinds => _kinds.values;

  ActionKind _parseKind(String code) =>
      _kinds[code] ??
      (throw StateError('api_action: loại lệnh chưa đăng ký "$code"'));

  ApiAction _fromRow(Map<String, Object?> r) =>
      ApiAction.fromRow(r, parseKind: _parseKind);

  static const _seqKey = 'api_action.seq';

  Database get _sql => _db.db;

  /// Giữ lệnh đã xong 48 giờ để lớp phủ trạng thái không "nháy" trước khi
  /// pull mang trạng thái mới xuống.
  static const doneRetention = Duration(hours: 48);

  Future<ApiAction> enqueue(
    ActionKind kind, {
    String? target,
    Map<String, Object?>? body,
  }) {
    if (kind is RetiredActionKind || !identical(_kinds[kind.code], kind)) {
      return Future.error(
        ArgumentError.value(
          kind.code,
          'kind',
          'loại lệnh chưa đăng ký với ActionQueue',
        ),
      );
    }
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
    return [
      for (final a in rows.map(_fromRow))
        if (a.kind is! RetiredActionKind) a,
    ];
  }

  /// Lệnh chưa thất bại (chờ hoặc đã xong) — dùng làm lớp phủ trạng thái cục bộ.
  Future<List<ApiAction>> active({ActionKind? kind, String? target}) async {
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
    return rows.map(_fromRow).toList();
  }

  /// Mọi lệnh của [kind] (kể cả đã thất bại, để hiển thị lỗi), theo `seq`.
  Future<List<ApiAction>> ofKind(ActionKind kind) async {
    final rows = await _sql.query(
      'api_action',
      where: 'kind = ?',
      whereArgs: [kind.code],
      orderBy: 'seq',
    );
    return rows.map(_fromRow).toList();
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

  /// Bỏ một lệnh **còn chờ** vì đã có lệnh mới thay thế (ví dụ lưu lịch tuần
  /// lần sau thay lần trước khi cả hai chưa gửi). Trả false nếu lệnh đã gửi
  /// xong hoặc đã thất bại (không đổi gì). Lệnh bị bỏ mang `last_error =
  /// 'superseded'`.
  Future<bool> cancelPending(String id) async {
    final n = await _sql.rawUpdate(
      "UPDATE api_action SET status = 'failed', last_error = ?, done_at = ? "
      "WHERE id = ? AND status = 'pending'",
      [supersededCode, _clock.now().millisecondsSinceEpoch, id],
    );
    return n > 0;
  }

  static const supersededCode = 'superseded';

  /// Một lệnh theo id (null nếu đã dọn).
  Future<ApiAction?> byId(String id) async {
    final rows = await _sql.query(
      'api_action',
      where: 'id = ?',
      whereArgs: [id],
    );
    return rows.isEmpty ? null : _fromRow(rows.first);
  }

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

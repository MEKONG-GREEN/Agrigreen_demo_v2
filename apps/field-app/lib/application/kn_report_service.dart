import 'dart:async';
import 'dart:convert';

import 'package:agrigreen_mobile/core/clock.dart';
import 'package:agrigreen_mobile/data/db/local_db.dart';
import 'package:agrigreen_mobile/data/outbox/outbox_repository.dart';
import 'package:agrigreen_mobile/data/reference/reference_repository.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';

import '../domain/kn_report.dart';

/// Trạng thái đồng bộ của lần lưu/gửi gần nhất của một công việc.
enum KnSyncState {
  none(''),
  pending('Chờ đồng bộ'),
  accepted('Đã đồng bộ'),
  conflict(
    'Đã có bản mới hơn ở nơi khác — bản của bạn lưu thành phiên bản mới, kiểm tra lại',
  ),
  rejected('Bị từ chối');

  const KnSyncState(this.label);
  final String label;
}

class KnRecordStatus {
  const KnRecordStatus(this.state, {this.record, this.fieldErrors = const {}});
  final KnSyncState state;
  final OutboxRecord? record;

  /// `<row_key>.<chữ cái>` → lý do (từ `rejected`).
  final Map<String, String> fieldErrors;

  /// `draft` | `submitted` của bản ghi chờ/đã gửi.
  String? get kind => record?.payload['kind'] as String?;

  String get label {
    if (state != KnSyncState.rejected) return state.label;
    final code = record?.errorCode;
    return switch (code) {
      'extension.period_closed' => 'Bị từ chối: kỳ đã chốt, không gửi được',
      'extension.report_task_not_found' =>
        'Bị từ chối: công việc không còn (đã thu hồi)',
      'extension.read_only' => 'Bị từ chối: tài khoản chỉ được xem',
      'extension.submission_invalid' => 'Bị từ chối: có ô vi phạm yêu cầu của mẫu — sửa các ô báo đỏ rồi gửi lại',
      _ => 'Bị từ chối (${code ?? 'lỗi'})',
    };
  }
}

class KnValidationException implements Exception {
  KnValidationException(this.errors, [this.message]);
  final Map<String, String> errors;
  final String? message;
  @override
  String toString() =>
      message ?? 'Có ${errors.length} ô chưa hợp lệ — kiểm tra các ô báo đỏ.';
}

class KnTaskFilter {
  const KnTaskFilter({
    this.commune,
    this.periodType,
    this.status,
    this.formCode,
    this.adHoc,
  });
  final String? commune;
  final String? periodType;
  final String? status;
  final String? formCode;

  /// true = chỉ báo cáo đột xuất; false = chỉ định kỳ; null = tất cả.
  final bool? adHoc;

  KnTaskFilter copyWith({
    String? Function()? commune,
    String? Function()? periodType,
    String? Function()? status,
    String? Function()? formCode,
  }) => KnTaskFilter(
    commune: commune == null ? this.commune : commune(),
    periodType: periodType == null ? this.periodType : periodType(),
    status: status == null ? this.status : status(),
    formCode: formCode == null ? this.formCode : formCode(),
    adHoc: adHoc,
  );
}

class KnTaskRow {
  const KnTaskRow(this.task, this.display, this.action, this.sync);
  final KnReportTask task;
  final String display;
  final String action;
  final KnRecordStatus sync;
}

class KnTaskList {
  const KnTaskList({
    required this.rows,
    required this.todo,
    required this.overdue,
    required this.adjustment,
    required this.communes,
    required this.forms,
    required this.total,
  });
  final List<KnTaskRow> rows;
  final int todo;
  final int overdue;
  final int adjustment;

  /// Xã có công việc (mã → tên) và biểu (mã → tên) — lựa chọn bộ lọc.
  final Map<String, String> communes;
  final Map<String, String> forms;

  /// Tổng công việc trên máy (trước khi lọc).
  final int total;
}

/// Phiên nhập của một công việc (một tab xã).
class KnEntry {
  KnEntry({
    required this.task,
    required this.layout,
    required this.rows,
    required this.values,
    required this.serverSubmission,
    required this.siblingSubmissions,
    required this.sync,
    required this.serverIssues,
    required this.formulaPending,
    required this.now,
  });

  final KnReportTask task;
  final KnFormLayout layout;
  final List<KnRow> rows;

  /// Giá trị các ô nhập được: `<row_key>.<chữ cái>` → chuỗi.
  final Map<String, String> values;
  final KnSubmission? serverSubmission;

  /// Bài nộp của công việc cùng đợt, cùng biểu, cùng xã ở khe kỳ khác:
  /// khe → bài nộp (giá trị "kỳ khác chỉ xem").
  final Map<int, KnSubmission> siblingSubmissions;
  final KnRecordStatus sync;

  /// Vấn đề tại ô máy chủ ghi trên bài nộp đã pull: khóa ô → thông điệp.
  final Map<String, String> serverIssues;

  /// Còn thay đổi chưa được máy chủ tính → ô công thức "chưa tính".
  final bool formulaPending;
  final DateTime now;

  bool get readOnly => !task.openAt(now);

  bool editable(KnColumn c) => knEditable(c, task, now);

  KnCellRole role(KnColumn c) => knCellRole(c, task, now);

  /// Lỗi hiển thị tại ô (lỗi `rejected` ưu tiên, rồi vấn đề máy chủ ghi).
  String? errorAt(KnRow r, KnColumn c) {
    final k = knCellKey(r.key, c.letter);
    return sync.fieldErrors[k] ?? serverIssues[k];
  }

  /// Giá trị hiển thị cho ô chỉ xem; null = trống.
  String? displayValue(KnRow r, KnColumn c) {
    final k = knCellKey(r.key, c.letter);
    if (editable(c)) return values[k];
    switch (c.kind) {
      case 'fixed':
        switch (c.fixedSource) {
          case 'row_no':
            return '${r.index}';
          case 'commune_name':
            return task.communeName;
          case 'indicator_label':
            return r.label;
        }
        return serverSubmission?.value(r.key, c.letter);
      case 'provincial':
        return task.provincial[c.letter] ??
            serverSubmission?.value(r.key, c.letter);
      case 'formula':
        if (formulaPending) return null;
        return serverSubmission?.rows[r.key]?.cells[c.letter];
      case 'per_period':
        final own = serverSubmission?.value(r.key, c.letter);
        if (own != null && own.isNotEmpty) return own;
        final s = siblingSubmissions[c.periodSlot ?? 0];
        return s?.value(r.key, c.letter) ?? values[k];
      default:
        return values[k] ?? serverSubmission?.value(r.key, c.letter);
    }
  }
}

/// Công việc cần làm + nhập báo cáo ngoại tuyến (FN-XA-03.01 / 03.02).
///
/// - Công việc, bố cục, bài nộp đi xuống qua pull (`report_task`,
///   `form_layout`, `report_submission`); bố cục chỉ giải mã khi mở biểu.
/// - Bản đang nhập lưu trên máy (`catalog_cache`, kind `kn.report_work`),
///   tự lưu khi chuyển tab.
/// - "Lưu nháp" / "Gửi báo cáo" → bản ghi hàng đợi `report_submission`
///   (`id` UUIDv7 = id bài nộp, `base_version` = latest_version đã thấy);
///   kết quả push theo từng bản ghi: accepted / duplicate / conflict /
///   rejected (lỗi tại ô `rows.<row_key>.<chữ cái>`).
class KnReportService {
  KnReportService({
    required LocalDb db,
    required ReferenceRepository refs,
    required OutboxRepository outbox,
    required SyncApi api,
    required Clock clock,
    this.syncNow,
  }) : _db = db,
       _refs = refs,
       _outbox = outbox,
       _api = api,
       _clock = clock;

  final LocalDb _db;
  final ReferenceRepository _refs;
  final OutboxRepository _outbox;
  final SyncApi _api;
  final Clock _clock;

  /// Đồng bộ ngay sau khi xếp hàng (chiều xuống chỉ các entity này).
  Future<void> Function(Set<String> entities)? syncNow;

  static const _workKind = 'kn.report_work';

  /// Pull tập trung sau khi lưu/gửi (máy 3G): công việc + bài nộp.
  static const focusedEntities = {
    KnEntity.reportTask,
    KnEntity.reportSubmission,
  };

  final _layouts = <String, KnFormLayout>{};

  DateTime get now => _clock.now();

  Future<List<KnReportTask>> tasks() async => [
    for (final r in await _refs.list(KnEntity.reportTask)) KnReportTask(r.data),
  ];

  Future<KnReportTask?> task(String id) async {
    final r = await _refs.get(KnEntity.reportTask, id);
    return r == null ? null : KnReportTask(r.data);
  }

  // ---------------------------------------------------- trạng thái bản ghi

  Future<Map<String, OutboxRecord>> _latestRecords() async {
    final out = <String, OutboxRecord>{};
    // list() trả seq giảm dần → bản đầu tiên của mỗi công việc là mới nhất.
    for (final r in await _outbox.list(entity: PushEntity.reportSubmission)) {
      final id = '${r.payload['task_id']}';
      out.putIfAbsent(id, () => r);
    }
    return out;
  }

  KnRecordStatus _statusOf(OutboxRecord? r) {
    if (r == null) return const KnRecordStatus(KnSyncState.none);
    return switch (r.status) {
      OutboxStatus.pending => KnRecordStatus(KnSyncState.pending, record: r),
      OutboxStatus.sent || OutboxStatus.superseded => KnRecordStatus(
        KnSyncState.accepted,
        record: r,
      ),
      OutboxStatus.conflict => KnRecordStatus(KnSyncState.conflict, record: r),
      OutboxStatus.rejected => KnRecordStatus(
        KnSyncState.rejected,
        record: r,
        fieldErrors: knFieldErrors(r.errors),
      ),
    };
  }

  Future<KnRecordStatus> recordStatus(String taskId) async =>
      _statusOf((await _latestRecords())[taskId]);

  /// Trạng thái cục bộ phủ lên trạng thái máy chủ (bản chờ đồng bộ).
  static String? _localKind(KnRecordStatus s) =>
      s.state == KnSyncState.pending ? s.kind : null;

  // ------------------------------------------------------- danh sách

  Future<KnTaskList> list([KnTaskFilter f = const KnTaskFilter()]) async {
    final all = await tasks();
    final recs = await _latestRecords();
    final t = now;
    final rows = <KnTaskRow>[];
    var todo = 0, overdue = 0, adj = 0;
    final communes = <String, String>{};
    final forms = <String, String>{};
    for (final k in all) {
      if (f.adHoc != null && k.adHoc != f.adHoc) continue;
      communes[k.communeCode] = k.communeName;
      forms[k.formCode] = k.formName;
      final st = _statusOf(recs[k.id]);
      final lk = _localKind(st);
      final d = k.displayAt(t, localKind: lk);
      if (k.pendingAt(t, localKind: lk)) todo++;
      if (d == KnTaskStatus.overdue) overdue++;
      if (d == KnTaskStatus.adjustmentRequested) adj++;
      if (f.commune != null && k.communeCode != f.commune) continue;
      if (f.periodType != null && k.periodType != f.periodType) continue;
      if (f.formCode != null && k.formCode != f.formCode) continue;
      if (f.status != null && d != f.status) continue;
      rows.add(KnTaskRow(k, d, k.actionAt(t, localKind: lk), st));
    }
    // Hạn gần nhất trước; chưa có hạn cuối cùng.
    rows.sort((a, b) {
      final x = a.task.dueAt, y = b.task.dueAt;
      if (x == null && y == null) return a.task.id.compareTo(b.task.id);
      if (x == null) return 1;
      if (y == null) return -1;
      final c = x.compareTo(y);
      return c != 0 ? c : a.task.communeName.compareTo(b.task.communeName);
    });
    return KnTaskList(
      rows: rows,
      todo: todo,
      overdue: overdue,
      adjustment: adj,
      communes: communes,
      forms: forms,
      total: all.length,
    );
  }

  /// Số việc cần làm trên máy (badge khi ngoại tuyến).
  Future<int> todoCount() async => (await list()).todo;

  /// Tab xã của biểu: các công việc cùng đợt, cùng kỳ, cùng biểu trên các xã
  /// phụ trách, sắp theo tên xã.
  Future<List<KnReportTask>> tabsFor(KnReportTask t) async {
    final out = [
      for (final k in await tasks())
        if (k.groupKey == t.groupKey) k,
    ]..sort((a, b) => a.communeName.compareTo(b.communeName));
    return out.isEmpty ? [t] : out;
  }

  /// Bố cục của công việc (giải mã khi mở biểu, nhớ trong phiên). null = máy
  /// chưa có bố cục (chưa đồng bộ).
  Future<KnFormLayout?> layoutFor(KnReportTask t) async {
    final key = '${t.versionId}|${t.periodLabels.join(';')}|${t.seasonLabel}';
    final hit = _layouts[key];
    if (hit != null) return hit;
    final r = await _refs.get(KnEntity.formLayout, t.versionId);
    if (r == null) return null;
    final l = KnFormLayout.fromData(
      r.data,
      periodLabels: t.periodLabels,
      season: t.seasonLabel,
    );
    _layouts[key] = l;
    return l;
  }

  Future<KnSubmission?> _submission(String taskId) async {
    final r = await _refs.get(KnEntity.reportSubmission, taskId);
    return r == null ? null : KnSubmission(r.data);
  }

  // -------------------------------------------------------- bản đang nhập

  Future<Map<String, String>?> _work(String taskId) async {
    final rows = await _db.db.query(
      'catalog_cache',
      columns: ['json'],
      where: 'kind = ? AND key = ?',
      whereArgs: [_workKind, taskId],
    );
    if (rows.isEmpty) return null;
    final j = jsonDecode(rows.first['json']! as String) as Map;
    return {for (final e in j.entries) '${e.key}': '${e.value}'};
  }

  /// Lưu bản đang nhập trên máy (tự lưu — không gửi).
  Future<void> saveWork(String taskId, Map<String, String> values) =>
      _db.db.rawInsert(
        'INSERT OR REPLACE INTO catalog_cache (kind, key, json, fetched_at) '
        'VALUES (?, ?, ?, ?)',
        [_workKind, taskId, jsonEncode(values), now.millisecondsSinceEpoch],
      );

  Future<void> discardWork(String taskId) => _db.db.delete(
    'catalog_cache',
    where: 'kind = ? AND key = ?',
    whereArgs: [_workKind, taskId],
  );

  static Map<String, String> _valuesOfPayload(
    KnReportTask t,
    Map<String, Object?> payload,
  ) => {
    for (final r in (payload['rows'] as List?) ?? const [])
      if (r is Map)
        for (final e in ((r['input'] as Map?) ?? const {}).entries)
          knCellKey(
            knRowKey(t.communeCode, r['indicator'] as String?),
            '${e.key}',
          ): '${e.value}',
  };

  /// Giá trị gốc (không tính bản đang nhập): bản ghi hàng đợi mới nhất nếu
  /// máy chủ chưa phản ánh, nếu không thì bài nộp đã pull.
  Map<String, String> _base(
    KnReportTask t,
    KnFormLayout l,
    KnSubmission? sub,
    OutboxRecord? rec,
  ) {
    if (rec != null &&
        rec.status != OutboxStatus.rejected &&
        sub?.submissionId != rec.id) {
      return _valuesOfPayload(t, rec.payload);
    }
    if (rec != null && rec.status == OutboxStatus.rejected) {
      // Bản bị từ chối không có trên máy chủ: giữ giá trị để người dùng sửa.
      return _valuesOfPayload(t, rec.payload);
    }
    final out = <String, String>{};
    if (sub == null) return out;
    for (final row in knRowsFor(t, l)) {
      final s = sub.rows[row.key];
      if (s == null) continue;
      for (final c in l.columns) {
        if (!knEditable(c, t, now) &&
            c.kind != 'input' &&
            c.kind != 'per_period') {
          continue;
        }
        final v = s.input[c.letter];
        if (v != null) out[knCellKey(row.key, c.letter)] = v;
      }
    }
    return out;
  }

  /// Mở phiên nhập cho công việc [t]. null = máy chưa có bố cục mẫu.
  Future<KnEntry?> entry(KnReportTask t) async {
    final l = await layoutFor(t);
    if (l == null) return null;
    final sub = await _submission(t.id);
    final rec = (await _latestRecords())[t.id];
    final st = _statusOf(rec);
    final base = _base(t, l, sub, rec);
    final work = await _work(t.id);
    final values = {...base, ...?work};
    final dirty = work != null && !_sameValues(work, base);
    final unreflected =
        rec != null &&
        rec.status != OutboxStatus.rejected &&
        sub?.submissionId != rec.id;
    final siblings = <int, KnSubmission>{};
    for (final k in await tasks()) {
      if (k.id == t.id ||
          k.batchId != t.batchId ||
          k.templateId != t.templateId ||
          k.communeCode != t.communeCode) {
        continue;
      }
      final s = await _submission(k.id);
      if (s != null) siblings[k.slot] = s;
    }
    final issues = <String, String>{};
    for (final i in sub?.issues ?? const <Map<String, Object?>>[]) {
      final rk = '${i['row_key'] ?? ''}';
      final letter = i['letter'] as String?;
      issues[letter == null ? rk : knCellKey(rk, letter)] =
          '${i['message'] ?? i['code'] ?? ''}';
    }
    return KnEntry(
      task: t,
      layout: l,
      rows: knRowsFor(t, l),
      values: values,
      serverSubmission: sub,
      siblingSubmissions: siblings,
      sync: st,
      serverIssues: issues,
      formulaPending: dirty || unreflected || st.state == KnSyncState.rejected,
      now: now,
    );
  }

  static bool _sameValues(Map<String, String> a, Map<String, String> b) {
    String n(String? v) => (v ?? '').trim();
    for (final k in {...a.keys, ...b.keys}) {
      if (n(a[k]) != n(b[k])) return false;
    }
    return true;
  }

  /// Bản đang nhập khác bản gốc (cần lưu/gửi).
  Future<bool> isDirty(KnReportTask t) async {
    final work = await _work(t.id);
    if (work == null) return false;
    final l = await layoutFor(t);
    if (l == null) return false;
    final rec = (await _latestRecords())[t.id];
    return !_sameValues(work, _base(t, l, await _submission(t.id), rec));
  }

  /// Kiểm các ô nhập được của [e] với giá trị [values].
  Map<String, String> validate(
    KnEntry e,
    Map<String, String> values, {
    required bool submit,
  }) {
    final errs = <String, String>{};
    for (final r in e.rows) {
      for (final c in e.layout.columns) {
        if (!e.editable(c)) continue;
        final k = knCellKey(r.key, c.letter);
        final m = knValidateCell(c, values[k] ?? '', submit: submit);
        if (m != null) errs[k] = m;
      }
    }
    return errs;
  }

  /// "Lưu nháp" ([submit] false) / "Gửi báo cáo" ([submit] true): kiểm trên
  /// máy rồi xếp bản ghi `report_submission` vào hàng đợi (gửi được khi
  /// ngoại tuyến). Ném [KnValidationException] khi có ô sai hoặc kỳ đã chốt.
  Future<OutboxRecord> queue(
    KnEntry e,
    Map<String, String> values, {
    required bool submit,
  }) async {
    if (e.readOnly) {
      throw KnValidationException(const {}, 'Kỳ đã chốt — chỉ xem.');
    }
    final errs = validate(e, values, submit: submit);
    if (errs.isNotEmpty) throw KnValidationException(errs);
    final rows = [
      for (final r in e.rows)
        {
          'indicator': ?r.indicator,
          'input': {
            for (final c in e.layout.columns)
              if (e.editable(c) &&
                  knNormalize(
                    c,
                    values[knCellKey(r.key, c.letter)] ?? '',
                  ).isNotEmpty)
                c.letter: knNormalize(
                  c,
                  values[knCellKey(r.key, c.letter)] ?? '',
                ),
          },
        },
    ];
    final rec = await _outbox.enqueue(
      entity: PushEntity.reportSubmission,
      payload: {
        'task_id': e.task.id,
        'kind': submit ? 'submitted' : 'draft',
        'rows': rows,
        'base_version': e.task.latestVersion,
      },
    );
    await discardWork(e.task.id);
    final f = syncNow;
    // Không chờ: ngoại tuyến thì lượt đồng bộ tự lùi thử lại.
    if (f != null) unawaited(f(focusedEntities).catchError((Object _) {}));
    return rec;
  }

  /// "Lịch sử sửa" (trực tuyến): `GET /v1/extension/report-tasks/{id}/history`.
  Future<List<Map<String, Object?>>> history(String taskId) async {
    final j = await _api.getJson('/v1/extension/report-tasks/$taskId/history');
    return [
      for (final i in (j['items'] as List?) ?? const [])
        if (i is Map) i.cast<String, Object?>(),
    ];
  }

  /// "Báo cáo của xã" (FN-XA-03.07, trực tuyến; đệm để xem ngoại tuyến).
  Future<(List<Map<String, Object?>>, bool)> communeReports({
    String? commune,
  }) async {
    final key = 'commune_reports:${commune ?? ''}';
    try {
      final j = await _api.getJson(
        '/v1/extension/commune-reports',
        query: {'commune': ?commune},
      );
      await _db.db.rawInsert(
        'INSERT OR REPLACE INTO catalog_cache (kind, key, json, fetched_at) '
        'VALUES (?, ?, ?, ?)',
        ['kn.cache', key, jsonEncode(j), now.millisecondsSinceEpoch],
      );
      return (_items(j), false);
    } on DeviceRevokedException {
      rethrow;
    } on Object {
      final rows = await _db.db.query(
        'catalog_cache',
        columns: ['json'],
        where: 'kind = ? AND key = ?',
        whereArgs: ['kn.cache', key],
      );
      if (rows.isEmpty) rethrow;
      return (
        _items(
          (jsonDecode(rows.first['json']! as String) as Map)
              .cast<String, Object?>(),
        ),
        true,
      );
    }
  }

  static List<Map<String, Object?>> _items(Map<String, Object?> j) => [
    for (final i in (j['items'] as List?) ?? const [])
      if (i is Map) i.cast<String, Object?>(),
  ];
}

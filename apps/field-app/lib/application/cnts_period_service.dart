import 'dart:async';
import 'dart:convert';

import 'package:agrigreen_mobile/core/clock.dart';
import 'package:agrigreen_mobile/data/db/local_db.dart';
import 'package:agrigreen_mobile/data/outbox/outbox_repository.dart';
import 'package:agrigreen_mobile/data/reference/reference_repository.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';

import '../domain/cnts_period.dart';

/// Trạng thái đồng bộ của lần gửi gần nhất của một công việc biểu kỳ.
enum FormsSyncState {
  none(''),
  pending('Chờ đồng bộ'),
  accepted('Đã đồng bộ'),
  conflict('Đã có người cập nhật — tải bản mới rồi sửa lại'),
  rejected('Bị từ chối');

  const FormsSyncState(this.label);
  final String label;
}

class FormsRecordStatus {
  const FormsRecordStatus(
    this.state, {
    this.record,
    this.fieldErrors = const {},
  });
  final FormsSyncState state;
  final OutboxRecord? record;
  final Map<String, String> fieldErrors;

  String get label {
    if (state != FormsSyncState.rejected) return state.label;
    return switch (record?.errorCode) {
      'forms.period_closed' => 'Bị từ chối: kỳ đã chốt',
      'forms.period_cancelled' => 'Bị từ chối: kỳ đã hủy',
      'forms.values_invalid' =>
        'Bị từ chối: có ô sai — sửa ô báo đỏ rồi gửi lại',
      'forms.late_reason_required' =>
        'Bị từ chối: gửi sau hạn phải ghi lý do trễ',
      final c => 'Bị từ chối (${c ?? 'lỗi'})',
    };
  }
}

class FormsValidationException implements Exception {
  FormsValidationException(this.errors, [this.message]);
  final Map<String, String> errors;
  final String? message;
  @override
  String toString() =>
      message ?? 'Có ${errors.length} ô chưa hợp lệ — kiểm tra các ô báo đỏ.';
}

/// Một dòng danh sách công việc.
class FormsTaskRow {
  const FormsTaskRow(this.task, this.status, this.sync);
  final FormsTask task;

  /// Trạng thái hiển thị (máy chủ, tính lại "Trễ hạn" theo giờ máy).
  final String status;
  final FormsRecordStatus sync;
}

/// Bản đang nhập trên máy.
class FormsWork {
  FormsWork({
    required this.values,
    this.nilReport = false,
    this.note = '',
    this.explanation = '',
  });
  Map<String, String> values;
  bool nilReport;
  String note;
  String explanation;

  Map<String, Object?> toJson() => {
    'values': values,
    'nil_report': nilReport,
    'note': note,
    'explanation': explanation,
  };

  static FormsWork fromJson(Map<String, Object?> j) => FormsWork(
    values: {
      for (final e in ((j['values'] as Map?) ?? const {}).entries)
        '${e.key}': '${e.value}',
    },
    nilReport: j['nil_report'] == true,
    note: '${j['note'] ?? ''}',
    explanation: '${j['explanation'] ?? ''}',
  );
}

/// Biểu kỳ Cấp Xã CNTS ngoại tuyến (W11.42): công việc đi xuống qua pull
/// (`forms_task`), bản đang nhập lưu trên máy (`catalog_cache`, kind
/// `cnts.forms_work`), "Gửi báo cáo" xếp bản ghi `forms_submission` vào hàng
/// đợi (`id` UUIDv7 = mã phiên bản, `base_version` = phiên bản đã thấy — khóa
/// lạc quan: phiên bản đã đổi ở nơi khác → máy chủ từ chối
/// `forms.submission_conflict`, máy báo tải bản mới; KHÔNG ghi đè). Bản gửi
/// đến sau khi kỳ đã chốt được máy chủ giữ "Chờ mở kỳ" (lượt pull sau trả công
/// việc trạng thái `awaiting_reopen`). "Sao chép kỳ trước" lấy bài nộp của kỳ
/// gần nhất trước đó của cùng biểu, cùng đơn vị ĐÃ CÓ TRÊN MÁY (chạy ngoại
/// tuyến).
class CntsPeriodService {
  CntsPeriodService({
    required LocalDb db,
    required ReferenceRepository refs,
    required OutboxRepository outbox,
    required Clock clock,
    this.syncNow,
  }) : _db = db,
       _refs = refs,
       _outbox = outbox,
       _clock = clock;

  final LocalDb _db;
  final ReferenceRepository _refs;
  final OutboxRepository _outbox;
  final Clock _clock;

  Future<void> Function(Set<String> entities)? syncNow;

  static const _workKind = 'cnts.forms_work';
  static const focusedEntities = {FormsEntity.task};

  DateTime get now => _clock.now();

  Future<List<FormsTask>> tasks() async => [
    for (final r in await _refs.list(FormsEntity.task)) FormsTask(r.data),
  ];

  Future<FormsTask?> task(String id) async {
    final r = await _refs.get(FormsEntity.task, id);
    return r == null ? null : FormsTask(r.data);
  }

  Future<Map<String, OutboxRecord>> _latestRecords() async {
    final out = <String, OutboxRecord>{};
    for (final r in await _outbox.list(entity: PushEntity.formsSubmission)) {
      out.putIfAbsent('${r.payload['task_id']}', () => r);
    }
    return out;
  }

  FormsRecordStatus _statusOf(OutboxRecord? r) {
    if (r == null) return const FormsRecordStatus(FormsSyncState.none);
    if (r.status == OutboxStatus.rejected &&
        r.errorCode == 'forms.submission_conflict') {
      return FormsRecordStatus(FormsSyncState.conflict, record: r);
    }
    return switch (r.status) {
      OutboxStatus.pending => FormsRecordStatus(
        FormsSyncState.pending,
        record: r,
      ),
      OutboxStatus.sent || OutboxStatus.superseded => FormsRecordStatus(
        FormsSyncState.accepted,
        record: r,
      ),
      OutboxStatus.conflict => FormsRecordStatus(
        FormsSyncState.conflict,
        record: r,
      ),
      OutboxStatus.rejected => FormsRecordStatus(
        FormsSyncState.rejected,
        record: r,
        fieldErrors: {
          for (final e in r.errors ?? const <Object?>[])
            if (e is Map) '${e['field']}': '${e['reason']}',
        },
      ),
    };
  }

  Future<FormsRecordStatus> recordStatus(String taskId) async =>
      _statusOf((await _latestRecords())[taskId]);

  /// Danh sách công việc (lọc theo mã biểu), sắp: chưa xong trước, hạn gần trước.
  Future<List<FormsTaskRow>> list({String? code}) async {
    final recs = await _latestRecords();
    final t = now;
    final rows = <FormsTaskRow>[
      for (final x in await tasks())
        if (code == null || x.templateCode == code)
          FormsTaskRow(x, x.statusAt(t), _statusOf(recs[x.id])),
    ];
    int rank(FormsTaskRow r) => switch (r.status) {
      FormsStatus.overdue || FormsStatus.returned => 0,
      FormsStatus.notSubmitted => 1,
      FormsStatus.awaitingReopen => 2,
      _ => 3,
    };
    rows.sort((a, b) {
      final c = rank(a).compareTo(rank(b));
      if (c != 0) return c;
      final da = a.task.dueAt, db = b.task.dueAt;
      if (da == null || db == null) return 0;
      return rank(a) == 3 ? db.compareTo(da) : da.compareTo(db);
    });
    return rows;
  }

  /// Số việc cần làm (chưa nhập, trễ hạn, yêu cầu điều chỉnh) — badge.
  Future<int> todoCount() async => (await list())
      .where(
        (r) =>
            r.task.open &&
            {
              FormsStatus.notSubmitted,
              FormsStatus.overdue,
              FormsStatus.returned,
            }.contains(r.status),
      )
      .length;

  // ------------------------------------------------------- bản đang nhập

  Future<FormsWork> work(FormsTask t) async {
    final rows = await _db.db.query(
      'catalog_cache',
      columns: ['json'],
      where: 'kind = ? AND key = ?',
      whereArgs: [_workKind, '${t.id}:${t.version}'],
    );
    if (rows.isNotEmpty) {
      return FormsWork.fromJson(
        Map<String, Object?>.from(
          jsonDecode(rows.first['json']! as String) as Map,
        ),
      );
    }
    return FormsWork(
      values: t.values,
      nilReport: t.nilReport,
      note: t.note,
      explanation: t.explanation,
    );
  }

  /// Lưu bản đang nhập trên máy (tự lưu — không gửi). Khóa theo phiên bản: bản
  /// mới từ máy chủ không bị bản nháp cũ đè lên.
  Future<void> saveWork(FormsTask t, FormsWork w) => _db.db.rawInsert(
    'INSERT OR REPLACE INTO catalog_cache (kind, key, json, fetched_at) '
    'VALUES (?, ?, ?, ?)',
    [
      _workKind,
      '${t.id}:${t.version}',
      jsonEncode(w.toJson()),
      now.millisecondsSinceEpoch,
    ],
  );

  Future<void> discardWork(FormsTask t) => _db.db.delete(
    'catalog_cache',
    where: 'kind = ? AND key = ?',
    whereArgs: [_workKind, '${t.id}:${t.version}'],
  );

  /// "Sao chép kỳ trước": bài cuối của kỳ gần nhất trước kỳ của [t], cùng biểu,
  /// cùng đơn vị, đã có trên máy (null nếu không có).
  Future<FormsTask?> previous(FormsTask t) async {
    FormsTask? best;
    for (final x in await tasks()) {
      if (x.id == t.id ||
          x.templateCode != t.templateCode ||
          x.unit != t.unit ||
          x.version == 0) {
        continue;
      }
      final a = x.periodFrom ?? '', b = t.periodFrom ?? '';
      if (a.compareTo(b) >= 0) continue;
      if (best == null || a.compareTo(best.periodFrom ?? '') > 0) best = x;
    }
    return best;
  }

  /// "Gửi báo cáo": kiểm trên máy rồi xếp `forms_submission` vào hàng đợi
  /// (gửi được khi ngoại tuyến).
  Future<OutboxRecord> submit(FormsTask t, FormsWork w) async {
    if (t.periodStatus == 'cancelled') {
      throw FormsValidationException(const {}, 'Kỳ đã hủy — không gửi được.');
    }
    final errs = formsFieldErrors(t.fields, w.values, nilReport: w.nilReport);
    if (t.isLateAt(now) && w.explanation.trim().isEmpty) {
      errs['explanation'] = 'Gửi sau hạn: bắt buộc ghi lý do trễ';
    }
    if (errs.isNotEmpty) throw FormsValidationException(errs);
    final rec = await _outbox.enqueue(
      entity: PushEntity.formsSubmission,
      payload: {
        'task_id': t.id,
        'profile': t.profile,
        'period_id': t.periodId,
        'unit': t.unit,
        'values': formsPayloadValues(
          t.fields,
          w.values,
          nilReport: w.nilReport,
        ),
        'base_version': t.version,
        'nil_report': w.nilReport,
        'note': w.note.trim(),
        'explanation': w.explanation.trim(),
      },
    );
    await discardWork(t);
    final f = syncNow;
    if (f != null) unawaited(f(focusedEntities).catchError((Object _) {}));
    return rec;
  }
}

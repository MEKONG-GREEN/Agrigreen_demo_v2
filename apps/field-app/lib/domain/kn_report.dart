/// Báo cáo theo biểu mẫu KN v7.7 trên máy (FN-XA-03.01 / 03.02): công việc
/// (`report_task`), bố cục mẫu (`form_layout`), bài nộp mới nhất
/// (`report_submission`) đi xuống qua pull; nhập ngoại tuyến theo luật cột.
///
/// Công thức KHÔNG chạy trên máy: ô công thức hiển thị giá trị máy chủ đã tính
/// (bài nộp đã pull) hoặc "chưa tính" khi còn thay đổi chưa đồng bộ.
library;

/// Entity đồng bộ chiều xuống của KN v7.7 P7.
abstract final class KnEntity {
  static const reportTask = 'report_task';
  static const formLayout = 'form_layout';
  static const reportSubmission = 'report_submission';
  static const all = {reportTask, formLayout, reportSubmission};
}

/// Trạng thái hiển thị (reportcycle/domain DisplayStatuses).
abstract final class KnTaskStatus {
  static const notStarted = 'not_started';
  static const draft = 'draft';
  static const submitted = 'submitted';
  static const overdue = 'overdue';
  static const adjustmentRequested = 'adjustment_requested';
  static const confirmed = 'confirmed';
  static const closed = 'closed';

  static const all = [
    notStarted,
    draft,
    submitted,
    overdue,
    adjustmentRequested,
    confirmed,
    closed,
  ];

  static String label(String s) => switch (s) {
    notStarted => 'Chưa nhập',
    draft => 'Lưu nháp',
    submitted => 'Đã nộp',
    overdue => 'Trễ hạn',
    adjustmentRequested => 'Yêu cầu điều chỉnh',
    confirmed => 'Đã xác nhận',
    closed => 'Đã chốt',
    _ => s,
  };
}

/// Loại kỳ của đợt phát hành.
abstract final class KnPeriodType {
  static const week = 'week';
  static const month = 'month';
  static const season = 'season';
  static String label(String s) => switch (s) {
    week => 'Tuần',
    month => 'Tháng',
    season => 'Vụ',
    _ => s,
  };
}

/// Loại báo cáo đột xuất (party/domain ReportTypeAdHoc).
const knReportTypeAdHoc = 'dot_xuat';

DateTime? _t(Object? v) => v is String ? DateTime.tryParse(v) : null;

Map<String, String> _strMap(Object? v) => {
  if (v is Map)
    for (final e in v.entries)
      if (e.value != null) '${e.key}': '${e.value}',
};

/// Công việc nộp báo cáo của một xã (entity `report_task`).
class KnReportTask {
  KnReportTask(this.data)
    : id = '${data['id']}',
      batchId = '${data['batch_id']}',
      periodId = '${data['period_id']}',
      templateId = '${data['template_id']}',
      versionId = '${data['version_id']}',
      formCode = '${data['form_code'] ?? ''}',
      formName = '${data['form_name'] ?? data['form_code'] ?? ''}',
      communeCode = '${data['commune_code'] ?? ''}',
      communeName = '${data['commune_name'] ?? data['commune_code'] ?? ''}',
      reportType = data['report_type'] as String?,
      periodType = '${data['period_type'] ?? ''}',
      slot = (data['slot'] as num?)?.toInt() ?? 1,
      periodBlock = data['period_block'] == true,
      periodLabel = '${data['period_label'] ?? ''}',
      periodLabels = [
        for (final l in (data['period_labels'] as List?) ?? const []) '$l',
      ],
      seasonLabel = '${data['season_label'] ?? ''}',
      dueAt = _t(data['due_at']),
      autoCloseAt = _t(data['auto_close_at']),
      periodClosed = data['period_closed'] == true,
      reopenedUntil = _t(data['reopened_until']),
      status = '${data['status'] ?? KnTaskStatus.notStarted}',
      openSlots = [
        for (final s in (data['open_slots'] as List?) ?? const [])
          if (s is num) s.toInt(),
      ],
      adjustmentReason = data['adjustment_reason'] as String?,
      latestVersion = (data['latest_version'] as num?)?.toInt() ?? 0,
      latestSubmissionId = data['latest_submission_id'] as String?,
      submittedAt = _t(data['submitted_at']),
      provincial = _strMap(data['provincial']),
      updatedAt = _t(data['updated_at']);

  final Map<String, Object?> data;
  final String id;
  final String batchId;
  final String periodId;
  final String templateId;
  final String versionId;
  final String formCode;
  final String formName;
  final String communeCode;
  final String communeName;
  final String? reportType;
  final String periodType;
  final int slot;
  final bool periodBlock;
  final String periodLabel;
  final List<String> periodLabels;
  final String seasonLabel;
  final DateTime? dueAt;
  final DateTime? autoCloseAt;
  final bool periodClosed;
  final DateTime? reopenedUntil;

  /// Trạng thái lưu trên máy chủ (not_started|draft|submitted|…).
  final String status;
  final List<int> openSlots;
  final String? adjustmentReason;
  final int latestVersion;
  final String? latestSubmissionId;
  final DateTime? submittedAt;

  /// Ô "Tỉnh giao" của xã: chữ cái → giá trị.
  final Map<String, String> provincial;
  final DateTime? updatedAt;

  bool get adHoc => reportType == knReportTypeAdHoc;

  /// Còn nhập/nộp được: kỳ chưa chốt (máy coi kỳ đã chốt khi qua giờ tự chốt
  /// — bi quan khi ngoại tuyến), hoặc đã mở lại và chưa hết hạn mở.
  bool openAt(DateTime now) {
    final reopened = reopenedUntil != null && now.isBefore(reopenedUntil!);
    if (periodClosed) return reopened;
    if (autoCloseAt != null && !now.isBefore(autoCloseAt!)) return reopened;
    return true;
  }

  /// Trạng thái hiển thị tại [now] — như `domain.TaskState.Display` của máy
  /// chủ, có phủ trạng thái cục bộ ([localKind]: bản `draft`/`submitted` đang
  /// chờ đồng bộ).
  String displayAt(DateTime now, {String? localKind}) {
    if (!openAt(now)) return KnTaskStatus.closed;
    var s = status;
    if (localKind == 'submitted') {
      s = KnTaskStatus.submitted;
    } else if (localKind == 'draft' &&
        (s == KnTaskStatus.notStarted || s == KnTaskStatus.draft)) {
      s = KnTaskStatus.draft;
    }
    if ((s == KnTaskStatus.notStarted || s == KnTaskStatus.draft) &&
        dueAt != null &&
        !now.isBefore(dueAt!)) {
      return KnTaskStatus.overdue;
    }
    return s;
  }

  /// Nút trên dòng công việc: Nhập / Tiếp tục / Sửa / Xem.
  String actionAt(DateTime now, {String? localKind}) {
    final d = displayAt(now, localKind: localKind);
    if (d == KnTaskStatus.closed) return 'Xem';
    final hasDraft =
        localKind == 'draft' ||
        status == KnTaskStatus.draft ||
        (latestSubmissionId != null && status == KnTaskStatus.notStarted);
    if (d == KnTaskStatus.notStarted ||
        (d == KnTaskStatus.overdue && !hasDraft)) {
      return 'Nhập';
    }
    if (d == KnTaskStatus.draft || d == KnTaskStatus.overdue) {
      return 'Tiếp tục';
    }
    return 'Sửa';
  }

  /// Việc còn phải gửi (đếm "Cần làm").
  bool pendingAt(DateTime now, {String? localKind}) {
    final d = displayAt(now, localKind: localKind);
    return d == KnTaskStatus.notStarted ||
        d == KnTaskStatus.draft ||
        d == KnTaskStatus.overdue ||
        d == KnTaskStatus.adjustmentRequested;
  }

  /// Nhóm tab xã: cùng biểu + cùng kỳ của cùng đợt.
  String get groupKey => '$batchId|$periodId|$templateId|$formCode';
}

// ---------------------------------------------------------------- bố cục

class KnColumn {
  const KnColumn({
    required this.letter,
    required this.title,
    required this.kind,
    required this.dtype,
    this.unit,
    this.required = false,
    this.fixedSource,
    this.periodBlock,
    this.periodSlot,
    this.min,
    this.max,
    this.decimals,
    this.group,
    this.inactive = false,
    this.placeholder = false,
  });

  final String letter;
  final String title;

  /// fixed | provincial | input | per_period | formula
  final String kind;

  /// number | integer | text | date | catalog
  final String dtype;
  final String? unit;
  final bool required;
  final String? fixedSource;
  final String? periodBlock;
  final int? periodSlot;
  final double? min;
  final double? max;
  final int? decimals;
  final String? group;
  final bool inactive;
  final bool placeholder;

  bool get isFormula => kind == 'formula';
  bool get isNumeric => dtype == 'number' || dtype == 'integer';

  KnColumn copyWith({String? title, bool? inactive}) => KnColumn(
    letter: letter,
    title: title ?? this.title,
    kind: kind,
    dtype: dtype,
    unit: unit,
    required: required,
    fixedSource: fixedSource,
    periodBlock: periodBlock,
    periodSlot: periodSlot,
    min: min,
    max: max,
    decimals: decimals,
    group: group,
    inactive: inactive ?? this.inactive,
    placeholder: placeholder,
  );

  static double? _d(Object? v) =>
      v == null ? null : double.tryParse('$v'.replaceAll(',', '.'));

  factory KnColumn.fromJson(Map<String, Object?> j) => KnColumn(
    letter: '${j['letter']}',
    title: '${j['title'] ?? ''}',
    kind: '${j['kind'] ?? 'input'}',
    dtype: '${j['dtype'] ?? 'text'}',
    unit: (j['unit'] as String?)?.isEmpty ?? true ? null : j['unit'] as String,
    required: j['required'] == true,
    fixedSource: j['fixed_source'] as String?,
    periodBlock: j['period_block'] as String?,
    periodSlot: (j['period_slot'] as num?)?.toInt(),
    min: _d(j['min']),
    max: _d(j['max']),
    decimals: (j['decimals'] as num?)?.toInt(),
    group: j['group'] as String?,
    inactive: j['inactive'] == true,
    placeholder: j['placeholder'] == true,
  );
}

class KnColumnGroup {
  const KnColumnGroup(this.id, this.title, this.from, this.to);
  final String id;
  final String title;
  final String from;
  final String to;
}

class KnIndicator {
  const KnIndicator(this.code, this.label);
  final String code;
  final String label;
}

/// Số thứ tự cột Excel (A=1, Z=26, AA=27…).
int letterIndex(String l) {
  var n = 0;
  for (final c in l.toUpperCase().codeUnits) {
    n = n * 26 + (c - 64);
  }
  return n;
}

/// Bố cục một phiên bản mẫu (entity `form_layout`, `definition` =
/// FormDefinition) sau khi áp nhãn kỳ và vụ như `reports/form.Layout`.
class KnFormLayout {
  const KnFormLayout({
    required this.versionId,
    required this.code,
    required this.name,
    required this.title,
    required this.sheetName,
    required this.requirements,
    required this.columns,
    required this.groups,
    required this.indicators,
    required this.freezeThrough,
  });

  final String versionId;
  final String code;
  final String name;
  final String title;
  final String sheetName;
  final String requirements;
  final List<KnColumn> columns;
  final List<KnColumnGroup> groups;
  final List<KnIndicator> indicators;
  final String freezeThrough;

  KnColumn? column(String letter) {
    for (final c in columns) {
      if (c.letter == letter) return c;
    }
    return null;
  }

  /// Cột định danh (cố định tới `freeze_through`, hoặc mọi cột fixed).
  List<KnColumn> get identifying => [
    for (final c in columns)
      if (freezeThrough.isNotEmpty
          ? letterIndex(c.letter) <= letterIndex(freezeThrough) &&
                c.kind == 'fixed'
          : c.kind == 'fixed')
        c,
  ];

  /// Cột của nhóm "Đi tới" [g] (null = mọi cột).
  List<KnColumn> columnsOf(KnColumnGroup? g) {
    if (g == null) return columns;
    final a = letterIndex(g.from), b = letterIndex(g.to);
    return [
      for (final c in columns)
        if (c.group == g.id ||
            (letterIndex(c.letter) >= a && letterIndex(c.letter) <= b))
          c,
    ];
  }

  /// Giải mã `data` của `form_layout` và áp nhãn kỳ [periodLabels] + vụ
  /// [season] (cột theo kỳ rộng 1 → tiêu đề = nhãn kỳ; ô tiêu đề có khe kỳ →
  /// nhãn kỳ; khe vượt số kỳ của đợt → inactive; `{season}` → nhãn vụ).
  factory KnFormLayout.fromData(
    Map<String, Object?> data, {
    List<String> periodLabels = const [],
    String season = '',
  }) {
    final def = ((data['definition'] as Map?) ?? const {})
        .cast<String, Object?>();
    String ss(String s) =>
        season.isEmpty ? s : s.replaceAll('{season}', season);
    final blocks = <String, (int, String)>{
      for (final b in (def['period_blocks'] as List?) ?? const [])
        if (b is Map)
          '${b['id']}': (
            (b['width'] as num?)?.toInt() ?? 1,
            '${b['title_pattern'] ?? ''}',
          ),
    };
    String? label(String? block, int slot) {
      if (periodLabels.isEmpty) {
        final p = blocks[block]?.$2 ?? '';
        return p.isEmpty ? null : p.replaceAll('{n}', '$slot');
      }
      return slot <= periodLabels.length ? periodLabels[slot - 1] : null;
    }

    // Tiêu đề hàng tiêu đề nhiều tầng: ô tiêu đề của cột (dòng cuối) thay tiêu
    // đề trống; ô có khe kỳ mang nhãn kỳ.
    final headerByLetter = <String, String>{};
    for (final h
        in (((def['header'] as Map?)?['cells']) as List?) ?? const []) {
      if (h is! Map) continue;
      var text = '${h['text'] ?? ''}';
      final slot = (h['period_slot'] as num?)?.toInt() ?? 0;
      if (slot > 0) {
        text = label(h['period_block'] as String?, slot) ?? text;
      }
      headerByLetter['${h['letter']}'] = ss(text);
    }
    final cols = <KnColumn>[];
    for (final raw in (def['columns'] as List?) ?? const []) {
      if (raw is! Map) continue;
      var c = KnColumn.fromJson(raw.cast<String, Object?>());
      if (c.kind == 'per_period') {
        final slot = c.periodSlot ?? 1;
        if (periodLabels.isNotEmpty && slot > periodLabels.length) {
          c = c.copyWith(inactive: true);
        } else if ((blocks[c.periodBlock]?.$1 ?? 1) == 1) {
          final l = label(c.periodBlock, slot);
          if (l != null) c = c.copyWith(title: l);
        } else if (c.title.isEmpty) {
          final l = label(c.periodBlock, slot);
          if (l != null) c = c.copyWith(title: l);
        }
      }
      if (c.title.isEmpty && headerByLetter[c.letter] != null) {
        c = c.copyWith(title: headerByLetter[c.letter]);
      }
      cols.add(c.copyWith(title: ss(c.title)));
    }
    return KnFormLayout(
      versionId: '${data['version_id'] ?? ''}',
      code: '${data['code'] ?? def['code'] ?? ''}',
      name: ss('${data['name'] ?? ''}'),
      title: ss('${def['title'] ?? ''}'),
      sheetName: ss('${def['sheet_name'] ?? ''}'),
      requirements: '${def['requirements'] ?? ''}',
      columns: cols,
      groups: [
        for (final g in (def['groups'] as List?) ?? const [])
          if (g is Map)
            KnColumnGroup(
              '${g['id']}',
              ss('${g['title'] ?? g['id']}'),
              '${g['from']}',
              '${g['to']}',
            ),
      ],
      indicators: [
        for (final i
            in (((def['rows'] as Map?)?['indicators']) as List?) ?? const [])
          if (i is Map) KnIndicator('${i['code']}', '${i['label'] ?? ''}'),
      ],
      freezeThrough: '${def['freeze_through'] ?? ''}',
    );
  }
}

// ---------------------------------------------------------------- dòng / ô

/// Khóa dòng như máy chủ (`reportcycle/app.rowKey`): mã xã hoặc mã xã/chỉ tiêu.
String knRowKey(String commune, String? indicator) =>
    indicator == null || indicator.isEmpty ? commune : '$commune/$indicator';

/// Một dòng của xã trên biểu.
class KnRow {
  const KnRow(this.key, this.indicator, this.label, this.index);
  final String key;
  final String? indicator;
  final String label;

  /// STT (từ 1).
  final int index;
}

List<KnRow> knRowsFor(KnReportTask t, KnFormLayout l) {
  if (l.indicators.isEmpty) {
    return [KnRow(knRowKey(t.communeCode, null), null, t.communeName, 1)];
  }
  return [
    for (var i = 0; i < l.indicators.length; i++)
      KnRow(
        knRowKey(t.communeCode, l.indicators[i].code),
        l.indicators[i].code,
        l.indicators[i].label,
        i + 1,
      ),
  ];
}

/// Loại ô theo luật màu của BRD: xám = cố định, trắng viền = nhập, viền xanh
/// đậm = cột kỳ đang mở, xám nhạt = kỳ khác chỉ xem, xanh lá nhạt = công thức.
enum KnCellRole { fixed, input, openPeriod, otherPeriod, formula, inactive }

/// Vai trò ô [c] của công việc [t] tại [now].
KnCellRole knCellRole(KnColumn c, KnReportTask t, DateTime now) {
  if (c.inactive) return KnCellRole.inactive;
  switch (c.kind) {
    case 'formula':
      return KnCellRole.formula;
    case 'per_period':
      final slot = c.periodSlot ?? 1;
      final open = t.openSlots.isNotEmpty
          ? t.openSlots.contains(slot)
          : (!t.periodBlock || slot == t.slot);
      return open ? KnCellRole.openPeriod : KnCellRole.otherPeriod;
    case 'input':
      return KnCellRole.input;
    default:
      return KnCellRole.fixed; // fixed, provincial
  }
}

bool knEditable(KnColumn c, KnReportTask t, DateTime now) {
  if (!t.openAt(now)) return false;
  final r = knCellRole(c, t, now);
  return r == KnCellRole.input || r == KnCellRole.openPeriod;
}

/// Bài nộp đã pull (`report_submission`).
class KnSubmission {
  KnSubmission(Map<String, Object?> d)
    : taskId = '${d['task_id']}',
      submissionId = '${d['submission_id']}',
      versionNo = (d['version_no'] as num?)?.toInt() ?? 0,
      kind = '${d['kind'] ?? 'draft'}',
      rows = {
        for (final r in (d['rows'] as List?) ?? const [])
          if (r is Map)
            '${r['key']}': (
              input: _strMap(r['input']),
              cells: _strMap(r['cells']),
            ),
      },
      issues = [
        for (final i in (d['issues'] as List?) ?? const [])
          if (i is Map) i.cast<String, Object?>(),
      ];

  final String taskId;
  final String submissionId;
  final int versionNo;
  final String kind;
  final Map<String, ({Map<String, String> input, Map<String, String> cells})>
  rows;
  final List<Map<String, Object?>> issues;

  String? value(String rowKey, String letter) {
    final r = rows[rowKey];
    return r?.cells[letter] ?? r?.input[letter];
  }
}

/// Chuẩn hóa số nhập ("1,5" → "1.5"; bỏ khoảng trắng).
String knNormalize(KnColumn c, String v) {
  final t = v.trim();
  if (c.isNumeric) return t.replaceAll(' ', '').replaceAll(',', '.');
  return t;
}

final _dateRe = RegExp(r'^\d{4}-\d{2}-\d{2}$');

/// Kiểm một ô như máy chủ kiểm được trên máy (kiểu dữ liệu, bắt buộc khi gửi,
/// min/max của mẫu). null = hợp lệ. Không áp luật ngoài yêu cầu của mẫu.
String? knValidateCell(KnColumn c, String raw, {required bool submit}) {
  final v = knNormalize(c, raw);
  if (v.isEmpty) {
    return submit && c.required ? 'Bắt buộc nhập' : null;
  }
  switch (c.dtype) {
    case 'integer':
      final n = int.tryParse(v);
      if (n == null) return 'Nhập số nguyên';
      return _range(c, n.toDouble());
    case 'number':
      final n = double.tryParse(v);
      if (n == null || !RegExp(r'^-?\d+(\.\d+)?$').hasMatch(v)) {
        return 'Nhập số (dấu thập phân là dấu phẩy hoặc chấm)';
      }
      return _range(c, n);
    case 'date':
      if (!_dateRe.hasMatch(v) || DateTime.tryParse(v) == null) {
        return 'Ngày dạng YYYY-MM-DD';
      }
      return null;
    default:
      return null;
  }
}

String? _range(KnColumn c, double n) {
  String f(double x) =>
      x == x.roundToDouble() ? x.toInt().toString() : x.toString();
  if (c.min != null && n < c.min!) return 'Tối thiểu ${f(c.min!)}';
  if (c.max != null && n > c.max!) return 'Tối đa ${f(c.max!)}';
  return null;
}

/// Lỗi tại ô từ kết quả push `rejected` (`errors[].field` dạng
/// `rows.<row_key>.<chữ cái>`) → khóa `<row_key>.<chữ cái>` (hoặc `<row_key>`).
Map<String, String> knFieldErrors(List<Object?>? errors) {
  final out = <String, String>{};
  for (final e in errors ?? const <Object?>[]) {
    if (e is! Map) continue;
    final f = '${e['field'] ?? ''}';
    final reason = '${e['reason'] ?? e['message'] ?? 'không hợp lệ'}';
    if (!f.startsWith('rows.')) {
      out[f.isEmpty ? '_' : f] = reason;
      continue;
    }
    out[f.substring(5)] = reason;
  }
  return out;
}

/// Khóa ô dùng chung cho lỗi và giá trị: `<row_key>.<chữ cái>`.
String knCellKey(String rowKey, String letter) => '$rowKey.$letter';

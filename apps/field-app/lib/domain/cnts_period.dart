// Biểu kỳ Cấp Xã CNTS trên kỳ native `cap/forms` (W11.42, BRD CNTS v3.7
// UR-XA-06…09, 13): công việc `forms_task` đi xuống qua pull (một công việc =
// kỳ × đơn vị trong phạm vi, kèm định nghĩa biểu và bài nộp cuối), lần gửi
// `forms_submission` đi lên qua hàng đợi (docs/dev/sync-contract.md mục
// "Biểu kỳ cap/forms").

/// Thực thể đồng bộ của biểu kỳ cap/forms.
abstract final class FormsEntity {
  static const task = 'forms_task';
  static const submission = 'forms_submission';
}

/// Trạng thái hiển thị của công việc (khớp FormsTask.status).
abstract final class FormsStatus {
  static const notSubmitted = 'not_submitted';
  static const overdue = 'overdue';
  static const submitted = 'submitted';
  static const accepted = 'accepted';
  static const returned = 'returned';
  static const missed = 'missed';
  static const closedUnadjusted = 'closed_unadjusted';
  static const awaitingReopen = 'awaiting_reopen';

  static String label(String s) => switch (s) {
    notSubmitted => 'Chưa nhập',
    overdue => 'Trễ hạn',
    submitted => 'Đã nộp',
    accepted => 'Đã xác thực',
    returned => 'Yêu cầu điều chỉnh',
    missed => 'Không nộp',
    closedUnadjusted => 'Đã chốt – chưa điều chỉnh',
    awaitingReopen => 'Chờ mở kỳ',
    _ => s,
  };
}

/// Mã biểu kỳ CNTS và nhãn "Báo không phát sinh" (null = biểu không có).
abstract final class CntsForms {
  static const bm01 = 'CNTS-BM01';
  static const bm04 = 'CNTS-BM04';
  static const bm17 = 'CNTS-BM17';
  static const bm05i = 'CNTS-BM05-I';
  static const bm05ii = 'CNTS-BM05-II';

  static const order = [bm01, bm04, bm17, bm05i, bm05ii];

  static String short(String code) => switch (code) {
    bm01 => 'BM-01 KSGM ngày',
    bm04 => 'BM-04 Nhập liệu tuần',
    bm17 => 'BM-17 Khai thác thủy sản',
    bm05i => 'BM-05 Kết quả tuần',
    bm05ii => 'BM-05 Kế hoạch tuần',
    _ => code,
  };

  static String? nilLabel(String code) => switch (code) {
    bm01 => 'Cơ sở không hoạt động trong ngày',
    bm04 => 'Không phát sinh trong tuần',
    bm17 => 'Không phát sinh khai thác trong tuần',
    _ => null,
  };
}

/// Một trường của biểu (khớp forms/domain.Field).
class FormsField {
  FormsField(this.raw);
  final Map<String, Object?> raw;

  String get key => '${raw['key']}';
  String get label => '${raw['label'] ?? raw['key']}';
  String get type => '${raw['type']}';
  bool get required => raw['required'] == true;
  bool get nonNegative => raw['non_negative'] == true;
  String? get unit => raw['unit'] as String?;
  String get group => (raw['group'] as String?) ?? '';
  List<String> get options => [
    for (final o in (raw['options'] as List?) ?? const []) '$o',
  ];
  bool get numeric => type == 'integer' || type == 'decimal';
}

final _int = RegExp(r'^-?\d{1,18}$');
final _dec = RegExp(r'^-?\d{1,18}(\.\d{1,6})?$');

/// Công việc biểu kỳ (dữ liệu `forms_task`).
class FormsTask {
  FormsTask(this.data);
  final Map<String, Object?> data;

  String get id => '${data['id']}';
  String get profile => '${data['profile']}';
  String get periodId => '${data['period_id']}';
  String get unit => '${data['unit']}';
  String get unitKind => '${data['unit_kind']}';
  String get parent => '${data['parent']}';
  String get templateCode => '${data['template_code']}';
  String get templateTitle => '${data['template_title']}';
  String get label => '${data['label']}';
  String? get periodFrom => data['period_from'] as String?;
  DateTime? get dueAt => DateTime.tryParse('${data['due_at']}');
  String get periodStatus => '${data['period_status']}';
  bool get open => data['open'] == true;
  String get status => '${data['status']}';
  int get version => (data['version'] as num?)?.toInt() ?? 0;
  bool get nilReport => data['nil_report'] == true;
  String get note => '${data['note'] ?? ''}';
  String get explanation => '${data['explanation'] ?? ''}';
  bool get late => data['late'] == true;
  String? get returnNote => data['return_note'] as String?;
  Map<String, String> get values => {
    for (final e in ((data['values'] as Map?) ?? const {}).entries)
      '${e.key}': '${e.value}',
  };
  List<FormsField> get fields => [
    for (final f in (data['fields'] as List?) ?? const [])
      FormsField(Map<String, Object?>.from(f as Map)),
  ];

  /// Trạng thái tính lại theo giờ máy: chưa nộp, kỳ còn mở và quá hạn → Trễ hạn.
  String statusAt(DateTime now) {
    final due = dueAt;
    if (status == FormsStatus.notSubmitted &&
        open &&
        due != null &&
        now.isAfter(due)) {
      return FormsStatus.overdue;
    }
    return status;
  }

  /// Gợi ý "gửi sau hạn" theo đồng hồ máy. Máy chủ quyết định trễ hạn theo giờ
  /// NHẬN bài (D8): bản ghi trước hạn nhưng đồng bộ sau hạn vẫn bị từ chối
  /// `forms.late_reason_required` — ghi lý do trễ rồi gửi lại.
  bool isLateAt(DateTime now) => dueAt != null && now.isAfter(dueAt!);
}

/// Lỗi trên máy trước khi gửi (máy chủ kiểm lại): khóa trường → lý do.
Map<String, String> formsFieldErrors(
  List<FormsField> fields,
  Map<String, String> values, {
  required bool nilReport,
}) {
  final out = <String, String>{};
  for (final f in fields) {
    final v = (values[f.key] ?? '').trim();
    if (nilReport && f.numeric) {
      if (v.isNotEmpty && (double.tryParse(v) ?? 1) != 0) {
        out[f.key] = 'Báo không phát sinh: phải bằng 0';
      }
      continue;
    }
    if (v.isEmpty) {
      if (f.required) out[f.key] = 'Bắt buộc';
      continue;
    }
    if (f.type == 'integer' && !_int.hasMatch(v)) {
      out[f.key] = 'Số nguyên';
    } else if (f.type == 'decimal' && !_dec.hasMatch(v)) {
      out[f.key] = 'Số thập phân (dấu chấm)';
    } else if (f.nonNegative && f.numeric && v.startsWith('-')) {
      out[f.key] = 'Không được âm';
    } else if (f.type == 'choice' && !f.options.contains(v)) {
      out[f.key] = 'Chọn trong danh sách';
    }
  }
  return out;
}

/// Giá trị gửi đi: bỏ ô trống; báo không phát sinh → ô số = 0.
Map<String, String> formsPayloadValues(
  List<FormsField> fields,
  Map<String, String> values, {
  required bool nilReport,
}) => {
  for (final f in fields)
    if ((nilReport && f.numeric) || (values[f.key] ?? '').trim().isNotEmpty)
      f.key: nilReport && f.numeric ? '0' : values[f.key]!.trim(),
};

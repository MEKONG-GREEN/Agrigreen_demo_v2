/// Báo cáo hiện trường `field_report` — **máy chủ là hợp đồng**:
/// `internal/platform/extension/app/payload.go` (`ReportPayload`) và
/// `internal/platform/extension/domain/report.go` (`Report.Validate`).
///
/// Hai loại (KN FR-01, FR-02):
/// - `sowing_progress`: diện tích đã sạ theo giống × vụ của một HTX trong xã;
/// - `growth_pest`: giai đoạn sinh trưởng, sâu bệnh, mức độ, diện tích nhiễm.
///
/// Số là chuỗi decimal (R-10); ngày gieo `YYYY-MM-DD` theo giờ Việt Nam (R-12).
/// Không có GPS trong payload (máy chủ không nhận).
library;

enum FieldReportKind {
  sowing('sowing_progress', 'Tiến độ gieo sạ'),
  growthPest('growth_pest', 'Sinh trưởng – sâu bệnh');

  const FieldReportKind(this.code, this.label);
  final String code;
  final String label;

  static FieldReportKind? parse(String? c) {
    for (final k in values) {
      if (k.code == c) return k;
    }
    return null;
  }
}

/// Mức độ nhiễm (`domain.Severities`).
enum PestSeverity {
  nhe('nhe', 'Nhẹ'),
  trungBinh('trung_binh', 'Trung bình'),
  nang('nang', 'Nặng');

  const PestSeverity(this.code, this.label);
  final String code;
  final String label;
}

class FieldReportError implements Exception {
  const FieldReportError(this.field, this.reason);
  final String field;
  final String reason;
  String get message => '$field: $reason';
  @override
  String toString() => message;
}

/// Trường giữ nguyên khi sửa sai (máy chủ lấy từ bản bị thay; `SubmitReport`).
const fieldReportLockedKeys = {
  'kind',
  'commune_code',
  'cooperative_ref',
  'period',
};

/// Thêm cho `sowing_progress` (cùng khóa phiên bản xã × HTX × kỳ × vụ × giống).
const sowingLockedKeys = {'season_code', 'variety_code'};

final _periodRe = RegExp(r'^[0-9]{4}-(0[1-9]|1[0-2])$');
final _codeRe = RegExp(r'^[A-Za-z0-9_]{1,40}$');
final _uuidRe = RegExp(
  r'^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$',
);
final _decimalRe = RegExp(r'^-?[0-9]+(\.[0-9]+)?$');
final _dateRe = RegExp(r'^[0-9]{4}-[0-9]{2}-[0-9]{2}$');

// `domain.phoneRe`, `domain.nationalIDRe` (task.go): ghi chú không chứa SĐT/CCCD.
final _phoneRe = RegExp(r'(?:\+?84|\b0)[\s.-]?(?:\d[\s.-]?){8,9}\d\b');
final _nationalIdRe = RegExp(r'\b\d{12}\b');

/// Ghi chú có SĐT hoặc số CCCD (máy chủ từ chối: `ErrPIIInText`).
bool containsPii(String s) => _phoneRe.hasMatch(s) || _nationalIdRe.hasMatch(s);

/// Giờ Việt Nam (UTC+7, không có giờ mùa hè) — kỳ báo cáo và ngày nghiệp vụ.
DateTime vietnamTime(DateTime t) => t.toUtc().add(const Duration(hours: 7));

/// Kỳ báo cáo `YYYY-MM` chứa thời điểm [t] theo giờ Việt Nam (`domain.PeriodOf`).
String periodOf(DateTime t) {
  final v = vietnamTime(t);
  return '${v.year.toString().padLeft(4, '0')}-${v.month.toString().padLeft(2, '0')}';
}

String _todayVn(DateTime now) {
  final v = vietnamTime(now);
  String two(int x) => x.toString().padLeft(2, '0');
  return '${v.year}-${two(v.month)}-${two(v.day)}';
}

/// Diện tích (ha, chuỗi decimal): > 0 (hoặc ≥ 0), ≤ 100 000, ≤ 4 chữ số lẻ.
void _area(String field, String? s, {required bool positive}) {
  if (s == null || s.isEmpty) {
    throw FieldReportError(field, 'bắt buộc (ha, số thập phân)');
  }
  if (!_decimalRe.hasMatch(s)) {
    throw FieldReportError(field, 'phải là số thập phân dạng chuỗi');
  }
  final d = double.parse(s);
  if (d < 0 || (positive && d == 0)) {
    throw FieldReportError(field, 'phải lớn hơn 0');
  }
  if (d > 100000) throw FieldReportError(field, 'vượt giới hạn hợp lý (ha)');
  final dot = s.indexOf('.');
  if (dot >= 0 && s.length - dot - 1 > 4) {
    throw FieldReportError(field, 'tối đa 4 chữ số thập phân');
  }
}

/// Chuẩn hóa số người dùng nhập ("1,25" → "1.25"; bỏ số 0 thừa ở cuối).
String? normalizeDecimal(String? s) {
  var t = (s ?? '').trim().replaceAll(',', '.');
  if (t.isEmpty) return null;
  if (t.contains('.')) {
    t = t.replaceFirst(RegExp(r'0+$'), '');
    if (t.endsWith('.')) t = t.substring(0, t.length - 1);
  }
  return t;
}

/// Bản nháp báo cáo — tạo payload đúng `ReportPayload` và kiểm như máy chủ.
class FieldReportDraft {
  const FieldReportDraft({
    required this.kind,
    required this.communeCode,
    required this.period,
    this.cooperativeRef,
    this.seasonCode = '',
    this.varietyCode = '',
    this.sownAreaHa,
    this.sownOn,
    this.growthStage = '',
    this.pestCode = '',
    this.severity,
    this.affectedAreaHa,
    this.urgent = false,
    this.note = '',
  });

  final FieldReportKind kind;
  final String communeCode;
  final String period;
  final String? cooperativeRef;
  final String seasonCode;
  final String varietyCode;
  final String? sownAreaHa;
  final String? sownOn;
  final String growthStage;
  final String pestCode;
  final PestSeverity? severity;
  final String? affectedAreaHa;
  final bool urgent;
  final String note;

  /// Kiểm như `Report.Validate` (không gồm tra danh mục/phạm vi — máy chủ làm).
  /// [now] để chặn kỳ/ngày ở tương lai.
  void validate(DateTime now) {
    final n = note.trim();
    if (communeCode.trim().isEmpty) {
      throw const FieldReportError('commune_code', 'bắt buộc');
    }
    if (!_periodRe.hasMatch(period)) {
      throw const FieldReportError('period', 'định dạng YYYY-MM');
    }
    if (period.compareTo(periodOf(now)) > 0) {
      throw const FieldReportError('period', 'không báo cáo cho kỳ tương lai');
    }
    if (n.runes.length > 2000) {
      throw const FieldReportError('note', 'tối đa 2000 ký tự');
    }
    if (containsPii(n)) {
      throw const FieldReportError(
        'note',
        'không ghi số điện thoại hoặc số CCCD vào ghi chú',
      );
    }
    final coop = cooperativeRef?.trim() ?? '';
    if (coop.isNotEmpty && !_uuidRe.hasMatch(coop)) {
      throw const FieldReportError('cooperative_ref', 'phải là UUID');
    }
    switch (kind) {
      case FieldReportKind.sowing:
        if (coop.isEmpty) {
          throw const FieldReportError(
            'cooperative_ref',
            'bắt buộc với báo cáo gieo sạ (FR-01)',
          );
        }
        if (!_codeRe.hasMatch(seasonCode.trim())) {
          throw const FieldReportError(
            'season_code',
            'bắt buộc, mã vụ theo danh mục',
          );
        }
        if (varietyCode.trim().isEmpty) {
          throw const FieldReportError(
            'variety_code',
            'bắt buộc, chọn từ danh mục giống (BR-01)',
          );
        }
        _area('sown_area_ha', sownAreaHa, positive: true);
        final d = sownOn;
        if (d != null && d.isNotEmpty) {
          if (!_dateRe.hasMatch(d) || DateTime.tryParse(d) == null) {
            throw const FieldReportError('sown_on', 'định dạng YYYY-MM-DD');
          }
          if (d.compareTo(_todayVn(now)) > 0) {
            throw const FieldReportError('sown_on', 'không ở tương lai');
          }
        }
      case FieldReportKind.growthPest:
        if (!_codeRe.hasMatch(growthStage.trim())) {
          throw const FieldReportError(
            'growth_stage',
            'bắt buộc, mã giai đoạn [A-Za-z0-9_]',
          );
        }
        if (pestCode.trim().isEmpty) {
          throw const FieldReportError(
            'pest_code',
            'bắt buộc, chọn từ danh mục sâu bệnh',
          );
        }
        if (severity == null) {
          throw const FieldReportError(
            'severity',
            'phải là nhe, trung_binh hoặc nang',
          );
        }
        final a = affectedAreaHa;
        if (a != null && a.isNotEmpty) {
          _area('affected_area_ha', a, positive: false);
        }
    }
  }

  /// Payload đúng thứ tự/tên trường của `ReportPayload`. Trường không thuộc
  /// loại báo cáo gửi rỗng/null (máy chủ cũng xóa chúng khi kiểm).
  Map<String, Object?> toPayload() {
    final sowing = kind == FieldReportKind.sowing;
    String? opt(String? s) => (s == null || s.isEmpty) ? null : s;
    final coop = cooperativeRef?.trim() ?? '';
    return {
      'kind': kind.code,
      'commune_code': communeCode.trim(),
      'cooperative_ref': coop.isEmpty ? null : coop,
      'period': period,
      'season_code': sowing ? seasonCode.trim() : '',
      'variety_code': sowing ? varietyCode.trim() : '',
      'sown_area_ha': sowing ? opt(sownAreaHa) : null,
      'sown_on': sowing ? opt(sownOn) : null,
      'growth_stage': sowing ? '' : growthStage.trim(),
      'pest_code': sowing ? '' : pestCode.trim(),
      'severity': sowing ? '' : (severity?.code ?? ''),
      'affected_area_ha': sowing ? null : opt(affectedAreaHa),
      'urgent': urgent,
      'note': note.trim(),
    };
  }
}

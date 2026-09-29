/// Mô hình pf/cropcycle trên máy (docs/dev/sync-contract.md, mục pf/cropcycle)
/// và các quy tắc kiểm trước khi lưu — trùng với máy chủ để người dùng thấy lỗi
/// ngay khi ngoại tuyến; máy chủ vẫn là nơi quyết định (R-55).
library;

import 'package:agrigreen_mobile/data/photo/location_source.dart';

/// Hoạt động canh tác (`CropFarmLogContent.activity_code`, HTX US-LOG-01).
enum FarmActivity {
  lamDat('lam_dat', 'Làm đất'),
  gieoSa('gieo_sa', 'Gieo sạ'),
  tuoiNuoc('tuoi_nuoc', 'Tưới nước'),
  bonPhan('bon_phan', 'Bón phân'),
  phunThuoc('phun_thuoc', 'Phun thuốc'),
  kiemTra('kiem_tra', 'Kiểm tra'),
  thuHoach('thu_hoach', 'Thu hoạch');

  const FarmActivity(this.code, this.label);
  final String code;
  final String label;

  static FarmActivity? parse(String? code) {
    for (final a in values) {
      if (a.code == code) return a;
    }
    return null;
  }
}

/// Nhãn hoạt động theo mã (mã lạ giữ nguyên).
String activityLabel(String? code) =>
    FarmActivity.parse(code)?.label ?? '$code';

// ---------------------------------------------------------------------------
// Ngày nghiệp vụ theo giờ Việt Nam (UTC+7, không có giờ mùa hè)
// ---------------------------------------------------------------------------

const _vnOffset = Duration(hours: 7);

/// Ngày hôm nay theo giờ Việt Nam, dạng `DateTime.utc(y, m, d)`.
DateTime vnToday(DateTime now) {
  final v = now.toUtc().add(_vnOffset);
  return DateTime.utc(v.year, v.month, v.day);
}

/// `YYYY-MM-DD` → ngày (UTC nửa đêm). null nếu sai dạng.
DateTime? parseDay(String? s) {
  if (s == null) return null;
  final m = RegExp(r'^(\d{4})-(\d{2})-(\d{2})$').firstMatch(s.trim());
  if (m == null) return null;
  final d = DateTime.utc(
    int.parse(m.group(1)!),
    int.parse(m.group(2)!),
    int.parse(m.group(3)!),
  );
  return formatDay(d) == s.trim() ? d : null;
}

String formatDay(DateTime d) {
  String two(int v) => v.toString().padLeft(2, '0');
  return '${d.year.toString().padLeft(4, '0')}-${two(d.month)}-${two(d.day)}';
}

/// `dd/mm/yyyy` để hiển thị.
String displayDay(DateTime? d) {
  if (d == null) return '—';
  String two(int v) => v.toString().padLeft(2, '0');
  return '${two(d.day)}/${two(d.month)}/${d.year}';
}

int daysBetween(DateTime from, DateTime to) => DateTime.utc(
  to.year,
  to.month,
  to.day,
).difference(DateTime.utc(from.year, from.month, from.day)).inDays;

DateTime addDays(DateTime d, int n) =>
    DateTime.utc(d.year, d.month, d.day).add(Duration(days: n));

// ---------------------------------------------------------------------------
// Mở vụ (HTX US-SEASON-01)
// ---------------------------------------------------------------------------

const sowingPastDays = 15;
const sowingFutureDays = 30;

/// Thông báo của HTX US-SEASON-01 AC-2 (trùng `domain.MsgSowingWindow`).
const msgSowingWindow =
    'Ngày gieo sạ phải trong khoảng 15 ngày trước đến 30 ngày sau ngày hiện tại.';

/// null = hợp lệ; ngược lại là thông báo lỗi (HTX US-SEASON-01 AC-2).
String? checkSowingDate(DateTime sowing, DateTime today) {
  final d = daysBetween(today, sowing);
  return d < -sowingPastDays || d > sowingFutureDays ? msgSowingWindow : null;
}

// ---------------------------------------------------------------------------
// SOP đã phát hành (entity sop_version)
// ---------------------------------------------------------------------------

class SopStep {
  const SopStep({
    required this.seq,
    required this.name,
    required this.activityCode,
    required this.dayOffset,
    required this.windowDays,
    this.stageCode = '',
    this.mandatory = false,
    this.instruction = '',
    this.phiDays,
  });

  final int seq;
  final String name;
  final String stageCode;
  final String activityCode;
  final int dayOffset;
  final int windowDays;
  final bool mandatory;
  final String instruction;
  final int? phiDays;

  static int _int(Object? v) => v is num ? v.toInt() : int.tryParse('$v') ?? 0;

  factory SopStep.fromJson(Map<String, Object?> j) => SopStep(
    seq: _int(j['seq']),
    name: '${j['name'] ?? ''}',
    stageCode: '${j['stage_code'] ?? ''}',
    activityCode: '${j['activity_code'] ?? ''}',
    dayOffset: _int(j['day_offset']),
    windowDays: _int(j['window_days']),
    mandatory: j['mandatory'] == true,
    instruction: '${j['instruction'] ?? ''}',
    phiDays: j['phi_days'] == null ? null : _int(j['phi_days']),
  );
}

class SopVersion {
  const SopVersion({
    required this.id,
    required this.name,
    required this.steps,
    this.templateRef = '',
    this.code = '',
    this.versionNo = 0,
    this.templateStatus = 'active',
    this.harvestDayOffset,
    this.harvestWindowDays,
  });

  final String id;
  final String templateRef;
  final String code;
  final String name;
  final int versionNo;
  final String templateStatus;
  final List<SopStep> steps;
  final int? harvestDayOffset;
  final int? harvestWindowDays;

  factory SopVersion.fromJson(Map<String, Object?> j) {
    final steps = [
      for (final s in (j['steps'] as List?) ?? const [])
        if (s is Map) SopStep.fromJson(s.cast<String, Object?>()),
    ]..sort((a, b) => a.seq.compareTo(b.seq));
    int? opt(Object? v) => v is num ? v.toInt() : null;
    return SopVersion(
      id: '${j['id']}',
      templateRef: '${j['template_ref'] ?? ''}',
      code: '${j['code'] ?? ''}',
      name: '${j['name'] ?? ''}',
      versionNo: opt(j['version_no']) ?? 0,
      templateStatus: '${j['template_status'] ?? 'active'}',
      steps: steps,
      harvestDayOffset: opt(j['harvest_day_offset']),
      harvestWindowDays: opt(j['harvest_window_days']),
    );
  }

  /// Bước thu hoạch đầu tiên theo seq (như `domain.HarvestStep`).
  SopStep? get harvestStep {
    for (final s in steps) {
      if (s.activityCode == FarmActivity.thuHoach.code) return s;
    }
    return null;
  }

  /// Cổng thu hoạch: sạ + offset − cửa sổ, không sớm hơn ngày sạ (như
  /// `domain.DatesFor`; g2-conventions: theo SOP đã ghim, không cứng 60 ngày).
  DateTime? harvestOpenFor(DateTime sowing) {
    final off = harvestDayOffset ?? harvestStep?.dayOffset;
    if (off == null) return null;
    final win = harvestWindowDays ?? harvestStep?.windowDays ?? 0;
    final d = addDays(sowing, off - win);
    return d.isBefore(sowing) ? sowing : d;
  }

  DateTime? expectedHarvestFor(DateTime sowing) {
    final off = harvestDayOffset ?? harvestStep?.dayOffset;
    if (off == null) return null;
    final d = addDays(sowing, off);
    return d.isBefore(sowing) ? sowing : d;
  }

  /// Bước SOP hiện tại theo số ngày kể từ sạ: bước cuối cùng đã tới mốc
  /// (`day_offset − window_days`). Trước bước đầu → bước đầu (sắp tới).
  SopStep? currentStep(DateTime sowing, DateTime today) {
    if (steps.isEmpty) return null;
    final elapsed = daysBetween(sowing, today);
    SopStep? cur;
    for (final s in steps) {
      if (s.dayOffset - s.windowDays <= elapsed) cur = s;
    }
    return cur ?? steps.first;
  }
}

// ---------------------------------------------------------------------------
// Vụ (entity season) và nhật ký (entity farm_log)
// ---------------------------------------------------------------------------

class Season {
  const Season({
    required this.seasonRef,
    required this.plotRef,
    required this.seasonCode,
    required this.seasonYear,
    required this.varietyCode,
    required this.sowingDate,
    required this.status,
    this.plotCode = '',
    this.householdRef,
    this.cooperativeRef,
    this.communeCode = '',
    this.sopVersionId,
    this.expectedHarvestDate,
    this.harvestOpenDate,
    this.areaHa,
    this.currentEventId,
    this.harvestId,
    this.localPending = false,
  });

  final String seasonRef;
  final String plotRef;
  final String plotCode;
  final String? householdRef;
  final String? cooperativeRef;
  final String communeCode;
  final String seasonCode;
  final int seasonYear;
  final String varietyCode;
  final String? sopVersionId;
  final DateTime? sowingDate;
  final DateTime? expectedHarvestDate;
  final DateTime? harvestOpenDate;

  /// Diện tích (chuỗi decimal, ha) — không dùng số thực.
  final String? areaHa;

  /// open | completed | cancelled
  final String status;
  final String? currentEventId;
  final String? harvestId;

  /// Vụ mở trên máy, chưa được máy chủ xác nhận ("Chờ đồng bộ").
  final bool localPending;

  bool get isOpen => status == 'open';

  factory Season.fromJson(Map<String, Object?> j, {bool localPending = false}) {
    String? s(Object? v) => v == null || '$v'.isEmpty ? null : '$v';
    return Season(
      seasonRef: '${j['season_ref']}',
      plotRef: '${j['plot_ref']}',
      plotCode: '${j['plot_code'] ?? ''}',
      householdRef: s(j['household_ref']),
      cooperativeRef: s(j['cooperative_ref']),
      communeCode: '${j['commune_code'] ?? ''}',
      seasonCode: '${j['season_code'] ?? ''}',
      seasonYear: j['season_year'] is num
          ? (j['season_year']! as num).toInt()
          : int.tryParse('${j['season_year']}') ?? 0,
      varietyCode: '${j['variety_code'] ?? ''}',
      sopVersionId: s(j['sop_version_id']),
      sowingDate: parseDay(s(j['sowing_date'])),
      expectedHarvestDate: parseDay(s(j['expected_harvest_date'])),
      harvestOpenDate: parseDay(s(j['harvest_open_date'])),
      areaHa: s(j['area_ha']),
      status: '${j['status'] ?? 'open'}',
      currentEventId: s(j['current_event_id']),
      harvestId: s(j['harvest_id']),
      localPending: localPending,
    );
  }
}

/// Thông báo chặn khai sản lượng (HTX US-YIELD-01 AC-1).
const msgHarvestTooEarly =
    'Lô ruộng chưa đến giai đoạn thu hoạch, chưa thể khai báo sản lượng.';

/// Thông báo chặn ghi nhật ký khi chưa mở vụ (HTX US-LOG-02 AC-2).
const msgSeasonNotOpen =
    'Lô ruộng này chưa được mở vụ. Vui lòng mở vụ trước khi ghi nhật ký.';

/// Thông báo khi cố sửa GPS/thời gian (HTX US-LOG-01 AC-3).
const msgLockedGpsTime =
    'Tọa độ GPS và thời gian không thể chỉnh sửa sau khi lưu.';

/// Ngày mở cổng thu hoạch: ưu tiên mốc máy chủ đã tính (`harvest_open_date`),
/// không có (vụ mở trên máy) → tính từ SOP đã ghim.
DateTime? harvestOpenDate(Season s, SopVersion? sop) {
  if (s.harvestOpenDate != null) return s.harvestOpenDate;
  final sow = s.sowingDate;
  if (sow == null || sop == null) return null;
  return sop.harvestOpenFor(sow);
}

/// null = được khai; ngược lại là lý do chặn (HTX US-YIELD-01 AC-1).
String? harvestBlockReason(Season s, SopVersion? sop, DateTime today) {
  if (!s.isOpen) return 'Vụ không còn mở — không khai sản lượng được.';
  final open = harvestOpenDate(s, sop);
  if (open != null && today.isBefore(open)) return msgHarvestTooEarly;
  return null;
}

class FarmLogView {
  const FarmLogView({
    required this.id,
    required this.logRef,
    required this.seasonRef,
    required this.plotRef,
    required this.activityCode,
    required this.eventTime,
    required this.status,
    this.sopStepSeq,
    this.gpsSource = '',
    this.locationStatus = '',
    this.machineCodes = const [],
    this.inputs = const [],
    this.note = '',
    this.fileRefs = const [],
    this.onBehalf = false,
    this.reviewNote,
  });

  /// id phiên bản (dùng làm `supersedes_id` khi bổ sung).
  final String id;
  final String logRef;
  final String seasonRef;
  final String plotRef;
  final String activityCode;
  final int? sopStepSeq;
  final String eventTime;
  final String gpsSource;

  /// match | mismatch | missing
  final String locationStatus;
  final List<String> machineCodes;
  final List<Map<String, Object?>> inputs;
  final String note;
  final List<String> fileRefs;
  final bool onBehalf;

  /// pending | approved | needs_supplement
  final String status;
  final String? reviewNote;

  factory FarmLogView.fromJson(Map<String, Object?> j) => FarmLogView(
    id: '${j['id']}',
    logRef: '${j['log_ref'] ?? j['id']}',
    seasonRef: '${j['season_ref'] ?? ''}',
    plotRef: '${j['plot_ref'] ?? ''}',
    activityCode: '${j['activity_code'] ?? ''}',
    sopStepSeq: j['sop_step_seq'] is num
        ? (j['sop_step_seq']! as num).toInt()
        : null,
    eventTime: '${j['event_time'] ?? ''}',
    gpsSource: '${j['gps_source'] ?? ''}',
    locationStatus: '${j['location_status'] ?? ''}',
    machineCodes: [
      for (final m in (j['machine_codes'] as List?) ?? const []) '$m',
    ],
    inputs: [
      for (final i in (j['inputs'] as List?) ?? const [])
        if (i is Map) i.cast<String, Object?>(),
    ],
    note: '${j['note'] ?? ''}',
    fileRefs: [for (final f in (j['file_refs'] as List?) ?? const []) '$f'],
    onBehalf: j['on_behalf'] == true,
    status: '${j['status'] ?? 'pending'}',
    reviewNote: j['review_note'] as String?,
  );
}

String reviewStatusLabel(String s) => switch (s) {
  'approved' => 'Đã duyệt',
  'needs_supplement' => 'Cần bổ sung',
  'superseded' => 'Đã thay bằng bản bổ sung',
  _ => 'Chờ duyệt',
};

/// Nhãn vị trí cho người duyệt (HTX US-LOG-05 AC-1, US-GPS-01 AC-3/4).
String? locationLabel(String locationStatus, String gpsSource) {
  if (locationStatus == 'mismatch') return 'Vị trí không khớp';
  if (locationStatus == 'missing' ||
      gpsSource == GpsSourceCode.denied ||
      gpsSource == GpsSourceCode.noDevice) {
    return 'Thiếu GPS';
  }
  return null;
}

// ---------------------------------------------------------------------------
// GPS (hợp đồng: `gps: {lon, lat, accuracy_m, source}`, decimal dạng chuỗi)
// ---------------------------------------------------------------------------

abstract final class GpsSourceCode {
  static const device = 'device';
  static const denied = 'denied';
  static const noDevice = 'no_device';
}

/// GPS gửi lên theo kết quả lấy vị trí. Bị từ chối quyền → `denied`; không có
/// GPS thiết bị → `no_device` — **không bao giờ chặn lưu** (HTX US-GPS-01 AC-4).
Map<String, Object?> gpsPayload(LocationResult r) {
  final f = r.fix;
  if (f == null || r.status == GpsStatus.denied) {
    return {
      'source': r.status == GpsStatus.unavailable
          ? GpsSourceCode.noDevice
          : GpsSourceCode.denied,
    };
  }
  return {
    'lat': f.lat.toStringAsFixed(7),
    'lon': f.lon.toStringAsFixed(7),
    if (f.accuracyM != null) 'accuracy_m': f.accuracyM!.toStringAsFixed(1),
    'source': GpsSourceCode.device,
  };
}

String gpsLabel(LocationResult r) => switch (r.status) {
  GpsStatus.ok => 'Đã lấy GPS',
  GpsStatus.weak => 'GPS yếu (sai số lớn)',
  GpsStatus.denied => 'Thiếu GPS — chưa cấp quyền vị trí',
  GpsStatus.unavailable => 'Thiếu GPS — máy không có/tắt định vị',
};

// ---------------------------------------------------------------------------
// Sản lượng (HTX US-YIELD-01/03): kg nhập vào, hiển thị t/ha — số nguyên có
// tỉ lệ cố định, không dùng số thực cho khối lượng/diện tích.
// ---------------------------------------------------------------------------

/// Mặc định ngưỡng bất thường (t/ha) khi giống không có ngưỡng riêng — trùng
/// `domain.DefaultYieldAnomalyMax`.
const defaultYieldAnomalyMaxTPerHa = '12';

class YieldError implements Exception {
  YieldError(this.message);
  final String message;
  @override
  String toString() => message;
}

/// Giá trị thập phân không âm với tối đa [scale] chữ số lẻ, dạng số nguyên
/// đã nhân 10^scale. Nhận dấu phẩy thập phân kiểu Việt Nam.
BigInt? parseScaled(String input, int scale) {
  final t = input.trim().replaceAll(' ', '').replaceAll(',', '.');
  final m = RegExp(r'^(\d{1,12})(?:\.(\d+))?$').firstMatch(t);
  if (m == null) return null;
  final frac = m.group(2) ?? '';
  if (frac.length > scale) return null;
  return BigInt.parse('${m.group(1)}${frac.padRight(scale, '0')}');
}

String formatScaled(BigInt v, int scale, {int? digits}) {
  final neg = v.isNegative;
  var a = v.abs();
  var sc = scale;
  if (digits != null && digits < scale) {
    final div = BigInt.from(10).pow(scale - digits);
    a = (a + div ~/ BigInt.two) ~/ div; // làm tròn nửa lên
    sc = digits;
  }
  final s = a.toString().padLeft(sc + 1, '0');
  final ip = s.substring(0, s.length - sc);
  final fp = s.substring(s.length - sc);
  return '${neg ? '-' : ''}$ip${sc > 0 ? ',$fp' : ''}';
}

/// Khối lượng kg đã kiểm: chuỗi decimal gửi máy chủ (`mass_kg`, ≤ 3 chữ số lẻ).
class MassKg {
  MassKg._(this.grams);
  final BigInt grams;

  static MassKg parse(String input) {
    final t = input.trim();
    if (t.startsWith('-')) {
      throw YieldError('Sản lượng không được âm.');
    }
    if (t.isEmpty) throw YieldError('Nhập sản lượng (kg).');
    final g = parseScaled(t, 3);
    if (g == null) {
      throw YieldError('Sản lượng là số kg, tối đa 3 chữ số thập phân.');
    }
    return MassKg._(g);
  }

  bool get isZero => grams == BigInt.zero;

  /// Chuỗi decimal dạng máy chủ đọc (`mass.ParseKg`): dấu chấm, bỏ số 0 thừa.
  String get wire {
    final s = formatScaled(grams, 3).replaceAll(',', '.');
    if (!s.contains('.')) return s;
    final trimmed = s.replaceFirst(RegExp(r'0+$'), '');
    return trimmed.endsWith('.')
        ? trimmed.substring(0, trimmed.length - 1)
        : trimmed;
  }
}

/// Năng suất t/ha × 10⁴ (4 chữ số lẻ như `domain.YieldTPerHa`); null nếu
/// thiếu diện tích.
BigInt? yieldTPerHaX4(MassKg m, String? areaHa) {
  final a = areaHa == null ? null : parseScaled(areaHa, 4);
  if (a == null || a == BigInt.zero) return null;
  // kg/1000/ha = (g/1000)/1000/(a/10⁴) = g·10⁴/(10⁶·a); ×10⁴ → g·100/a.
  final n = m.grams * BigInt.from(100);
  return (n + a ~/ BigInt.two) ~/ a;
}

/// "6,25 t/ha" (2 chữ số lẻ) hoặc null.
String? formatYield(MassKg m, String? areaHa) {
  final y = yieldTPerHaX4(m, areaHa);
  return y == null ? null : '${formatScaled(y, 4, digits: 2)} t/ha';
}

enum YieldAnomaly { none, zero, above }

/// Như `domain.Anomaly`: 0 kg → zero; > ngưỡng t/ha → above.
YieldAnomaly yieldAnomaly(MassKg m, String? areaHa, String thresholdTPerHa) {
  if (m.isZero) return YieldAnomaly.zero;
  final y = yieldTPerHaX4(m, areaHa);
  final thr = parseScaled(thresholdTPerHa, 4);
  if (y != null && thr != null && thr > BigInt.zero && y > thr) {
    return YieldAnomaly.above;
  }
  return YieldAnomaly.none;
}

String anomalyMessage(
  YieldAnomaly a,
  MassKg m,
  String? areaHa,
  String thr,
) => switch (a) {
  YieldAnomaly.zero =>
    'Sản lượng bằng 0 kg. Bạn có chắc chắn muốn lưu số liệu này?',
  YieldAnomaly.above =>
    'Năng suất ${formatYield(m, areaHa)} vượt ngưỡng bất thường '
        '${thr.replaceAll('.', ',')} t/ha của giống. Bạn có chắc chắn số liệu đúng?',
  YieldAnomaly.none => '',
};

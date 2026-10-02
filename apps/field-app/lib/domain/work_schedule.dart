import 'field_report.dart' show containsPii, vietnamTime;

/// Lịch công tác tuần (KN v7.7 FN-XA-03.08, OpenAPI `ExtWorkSchedule*`).
///
/// Ngày nghiệp vụ tính theo giờ Việt Nam (UTC+7, không giờ mùa hè). Ngày
/// trong lịch là `DateTime.utc(y, m, d)` (chỉ phần ngày), dạng dây `YYYY-MM-DD`.

// ---------------------------------------------------------------- tuần

String two(int v) => v.toString().padLeft(2, '0');

/// `YYYY-MM-DD` của một ngày (phần ngày của [d]).
String ymd(DateTime d) =>
    '${d.year.toString().padLeft(4, '0')}-${two(d.month)}-${two(d.day)}';

/// Phân tích `YYYY-MM-DD` → `DateTime.utc` (null nếu sai dạng).
DateTime? parseYmd(String? s) {
  if (s == null) return null;
  final m = RegExp(r'^(\d{4})-(\d{2})-(\d{2})$').firstMatch(s.trim());
  if (m == null) return null;
  final d = DateTime.utc(int.parse(m[1]!), int.parse(m[2]!), int.parse(m[3]!));
  return ymd(d) == s.trim() ? d : null;
}

/// Ngày (giờ Việt Nam) chứa thời điểm [t].
DateTime vnDate(DateTime t) {
  final v = vietnamTime(t);
  return DateTime.utc(v.year, v.month, v.day);
}

/// Thứ Hai của tuần chứa ngày [d].
DateTime mondayOf(DateTime d) => DateTime.utc(
  d.year,
  d.month,
  d.day,
).subtract(Duration(days: d.weekday - 1));

/// Thứ Hai tuần hiện tại (giờ Việt Nam).
DateTime currentWeekStart(DateTime now) => mondayOf(vnDate(now));

/// Tuần mặc định khi mở màn: **tuần kế tiếp** (FN-XA-03.08).
DateTime nextWeekStart(DateTime now) =>
    currentWeekStart(now).add(const Duration(days: 7));

/// Thời điểm 00:00 giờ Việt Nam của ngày [d].
DateTime vnMidnight(DateTime d) =>
    DateTime.utc(d.year, d.month, d.day).subtract(const Duration(hours: 7));

/// Hạn đăng ký mặc định khi chưa có số liệu máy chủ: 00:00 Thứ Hai − 55 giờ
/// = 17:00 Thứ Sáu tuần trước (`ExtWorkScheduleSettings.submit_due_offset_hours`).
DateTime defaultSubmitDue(DateTime weekStart) =>
    vnMidnight(weekStart).subtract(const Duration(hours: 55));

/// Tuần đã kết thúc (sau 24:00 Chủ Nhật giờ Việt Nam) → khóa, chỉ xem.
bool weekEnded(DateTime weekStart, DateTime now) =>
    !now.isBefore(vnMidnight(weekStart.add(const Duration(days: 7))));

const weekdayShort = ['T2', 'T3', 'T4', 'T5', 'T6', 'T7', 'CN'];
const weekdayLong = [
  'Thứ Hai',
  'Thứ Ba',
  'Thứ Tư',
  'Thứ Năm',
  'Thứ Sáu',
  'Thứ Bảy',
  'Chủ Nhật',
];

/// `dd/MM`.
String dayMonth(DateTime d) => '${two(d.day)}/${two(d.month)}';

/// `08/09 – 14/09/2026`.
String weekLabel(DateTime weekStart) {
  final end = weekStart.add(const Duration(days: 6));
  return '${dayMonth(weekStart)} – ${dayMonth(end)}/${end.year}';
}

/// Giờ Việt Nam của một thời điểm: `Thứ Sáu 12/09 17:00`.
String vnDateTimeLabel(DateTime t) {
  final v = vietnamTime(t);
  return '${weekdayLong[v.weekday - 1]} ${two(v.day)}/${two(v.month)} '
      '${two(v.hour)}:${two(v.minute)}';
}

// ---------------------------------------------------------------- mục

enum WsSession {
  morning('morning', 'Sáng'),
  afternoon('afternoon', 'Chiều');

  const WsSession(this.code, this.label);
  final String code;
  final String label;

  static WsSession? tryParse(Object? c) {
    for (final s in values) {
      if (s.code == c) return s;
    }
    return null;
  }
}

enum WsStatus {
  draft('draft', 'Nháp'),
  pending('pending', 'Chờ duyệt'),
  approved('approved', 'Đã duyệt'),
  changesRequested('changes_requested', 'Yêu cầu chỉnh sửa');

  const WsStatus(this.code, this.label);
  final String code;
  final String label;

  static WsStatus? tryParse(Object? c) {
    for (final s in values) {
      if (s.code == c) return s;
    }
    return null;
  }
}

/// Một mục công tác (`ExtWorkScheduleItem`).
class WsItem {
  const WsItem({
    required this.id,
    required this.day,
    required this.session,
    required this.content,
    this.startTime,
    this.endTime,
    this.communeCode,
    this.cooperativeRef,
    this.location,
    this.participants,
    this.note,
    this.changed = false,
    this.conflict = false,
  });

  final String id;
  final DateTime day;
  final WsSession session;
  final String? startTime;
  final String? endTime;
  final String content;
  final String? communeCode;
  final String? cooperativeRef;
  final String? location;
  final String? participants;
  final String? note;

  /// Khác bản đã duyệt (máy chủ tính).
  final bool changed;

  /// Trùng thời gian với mục khác (máy chủ hoặc máy tính).
  final bool conflict;

  WsItem copyWith({bool? conflict, DateTime? day, String? id}) => WsItem(
    id: id ?? this.id,
    day: day ?? this.day,
    session: session,
    content: content,
    startTime: startTime,
    endTime: endTime,
    communeCode: communeCode,
    cooperativeRef: cooperativeRef,
    location: location,
    participants: participants,
    note: note,
    changed: changed,
    conflict: conflict ?? this.conflict,
  );

  /// "Sáng 07:30–11:00" / "Chiều".
  String get timeLabel {
    final t = startTime == null
        ? ''
        : ' $startTime${endTime == null ? '' : '–$endTime'}';
    return '${session.label}$t';
  }

  /// Địa điểm hiển thị: xã · HTX · nơi cụ thể.
  String get placeLabel =>
      [?communeCode, ?location].where((s) => s.trim().isNotEmpty).join(' · ');

  static String? _s(Object? v) {
    if (v is! String) return null;
    final t = v.trim();
    return t.isEmpty ? null : t;
  }

  static WsItem? fromJson(Map<String, Object?> j) {
    final day = parseYmd(j['day'] as String?);
    final session = WsSession.tryParse(j['session']);
    final id = j['id'];
    if (day == null || session == null || id is! String) return null;
    return WsItem(
      id: id,
      day: day,
      session: session,
      startTime: _s(j['start_time']),
      endTime: _s(j['end_time']),
      content: (j['content'] as String?) ?? '',
      communeCode: _s(j['commune_code']),
      cooperativeRef: _s(j['cooperative_ref']),
      location: _s(j['location']),
      participants: _s(j['participants']),
      note: _s(j['note']),
      changed: j['changed'] == true,
      conflict: j['conflict'] == true,
    );
  }

  /// Dạng lưu máy (kèm cờ hiển thị).
  Map<String, Object?> toJson() => {
    ...toInput(),
    'changed': changed,
    'conflict': conflict,
  };

  /// `ExtWorkScheduleItemInput` gửi máy chủ (đủ khóa, null khi trống, thứ tự
  /// cố định để thân lệnh ổn định).
  Map<String, Object?> toInput() => {
    'id': id,
    'day': ymd(day),
    'session': session.code,
    'start_time': startTime,
    'end_time': endTime,
    'content': content,
    'commune_code': communeCode,
    'cooperative_ref': cooperativeRef,
    'location': location,
    'participants': participants,
    'note': note,
  };
}

/// Lỗi kiểm dữ liệu của một mục (giống quy tắc máy chủ, 422).
class WorkScheduleError implements Exception {
  WorkScheduleError(this.field, this.message);
  final String field;
  final String message;
  @override
  String toString() => message;
}

final _hhmm = RegExp(r'^([01][0-9]|2[0-3]):[0-5][0-9]$');
final _uuid = RegExp(
  r'^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$',
);

int _minutes(String hhmm) =>
    int.parse(hhmm.substring(0, 2)) * 60 + int.parse(hhmm.substring(3, 5));

/// Số mục tối đa của một lịch tuần (`ExtWorkScheduleSave.items.maxItems`).
const maxScheduleItems = 100;

/// Kiểm một mục như máy chủ: nội dung 3..500 ký tự, ngày trong tuần, buổi,
/// giờ `HH:MM`, giờ kết thúc ≥ giờ bắt đầu, độ dài trường, không ghi SĐT/CCCD
/// trong thành phần phối hợp, ghi chú (RB-3).
void validateItem(WsItem it, DateTime weekStart) {
  final c = it.content.trim();
  if (c.length < 3) {
    throw WorkScheduleError('content', 'Nội dung công việc tối thiểu 3 ký tự.');
  }
  if (c.length > 500) {
    throw WorkScheduleError('content', 'Nội dung công việc tối đa 500 ký tự.');
  }
  final offset = it.day.difference(weekStart).inDays;
  if (it.day.isBefore(weekStart) || offset > 6) {
    throw WorkScheduleError('day', 'Ngày phải nằm trong tuần đang đăng ký.');
  }
  for (final (f, v) in [
    ('start_time', it.startTime),
    ('end_time', it.endTime),
  ]) {
    if (v != null && !_hhmm.hasMatch(v)) {
      throw WorkScheduleError(f, 'Giờ phải có dạng HH:MM (ví dụ 07:30).');
    }
  }
  if (it.startTime == null && it.endTime != null) {
    throw WorkScheduleError(
      'start_time',
      'Nhập giờ bắt đầu trước giờ kết thúc.',
    );
  }
  if (it.startTime != null &&
      it.endTime != null &&
      _minutes(it.endTime!) < _minutes(it.startTime!)) {
    throw WorkScheduleError(
      'end_time',
      'Giờ kết thúc phải sau hoặc bằng giờ bắt đầu.',
    );
  }
  if (it.cooperativeRef != null && !_uuid.hasMatch(it.cooperativeRef!)) {
    throw WorkScheduleError('cooperative_ref', 'Mã HTX không hợp lệ.');
  }
  if ((it.location?.length ?? 0) > 200) {
    throw WorkScheduleError('location', 'Địa điểm tối đa 200 ký tự.');
  }
  if ((it.participants?.length ?? 0) > 500) {
    throw WorkScheduleError(
      'participants',
      'Thành phần phối hợp tối đa 500 ký tự.',
    );
  }
  if ((it.note?.length ?? 0) > 1000) {
    throw WorkScheduleError('note', 'Ghi chú tối đa 1000 ký tự.');
  }
  for (final (f, label, v) in [
    ('participants', 'Thành phần phối hợp', it.participants),
    ('note', 'Ghi chú', it.note),
  ]) {
    if (v != null && containsPii(v)) {
      throw WorkScheduleError(
        f,
        '$label không được ghi số điện thoại hoặc số CCCD.',
      );
    }
  }
}

/// Khoảng thời gian dùng để dò trùng: giờ đã nhập, hoặc cả buổi khi chưa nhập
/// giờ (Sáng 07:00–11:30, Chiều 13:00–17:00). Chưa có giờ kết thúc → 1 giờ.
/// Cùng quy tắc với máy chủ (extension/domain.Conflicts) và web-ops.
(int, int) _span(WsItem it) {
  final s = it.startTime == null
      ? (it.session == WsSession.morning ? 7 * 60 : 13 * 60)
      : _minutes(it.startTime!);
  final e = it.startTime == null
      ? (it.session == WsSession.morning ? 11 * 60 + 30 : 17 * 60)
      : it.endTime == null
      ? s + 60
      : _minutes(it.endTime!);
  return (s, e <= s ? s + 60 : e);
}

/// Id các mục trùng thời gian với mục khác cùng ngày (cảnh báo, không chặn).
Set<String> overlappingIds(List<WsItem> items) {
  final out = <String>{};
  for (var i = 0; i < items.length; i++) {
    for (var j = i + 1; j < items.length; j++) {
      final a = items[i], b = items[j];
      if (a.day != b.day) continue;
      final (aStart, aEnd) = _span(a);
      final (bStart, bEnd) = _span(b);
      if (aStart < bEnd && bStart < aEnd) out.addAll([a.id, b.id]);
    }
  }
  return out;
}

/// Đánh dấu `conflict` theo dò trùng trên máy.
List<WsItem> markOverlaps(List<WsItem> items) {
  final ids = overlappingIds(items);
  return [for (final it in items) it.copyWith(conflict: ids.contains(it.id))];
}

/// Sắp theo ngày, buổi, giờ bắt đầu.
List<WsItem> sortItems(Iterable<WsItem> items) {
  final l = items.toList();
  l.sort((a, b) {
    final d = a.day.compareTo(b.day);
    if (d != 0) return d;
    final s = a.session.index.compareTo(b.session.index);
    if (s != 0) return s;
    return _span(a).$1.compareTo(_span(b).$1);
  });
  return l;
}

// ---------------------------------------------------------------- log

/// Một dòng Log (`KNLogEntry`).
class WsLogEntry {
  const WsLogEntry({
    required this.event,
    required this.at,
    this.actor,
    this.reason,
  });
  final String event;
  final DateTime? at;
  final String? actor;
  final String? reason;

  String get label => switch (event) {
    'draft_saved' => 'Lưu nháp',
    'submitted' => 'Gửi duyệt',
    'approved' => 'Đã duyệt',
    'change_requested' || 'changes_requested' => 'Yêu cầu chỉnh sửa',
    'amended' => 'Điều chỉnh lịch đã duyệt',
    'locked' => 'Khóa tuần',
    'copied' || 'copied_previous' => 'Sao chép tuần trước',
    'created' => 'Tạo lịch tuần',
    _ => event,
  };

  static WsLogEntry fromJson(Map<String, Object?> j) => WsLogEntry(
    event: '${j['event'] ?? ''}',
    at: j['at'] is String ? DateTime.tryParse(j['at']! as String) : null,
    actor: j['actor_display'] as String?,
    reason: j['reason'] as String?,
  );
}

/// Nguồn thời gian có thể thay trong kiểm thử (không gọi DateTime.now() rải rác).
abstract class Clock {
  DateTime now();
}

class SystemClock implements Clock {
  const SystemClock();
  @override
  DateTime now() => DateTime.now();
}

/// Đồng hồ giả cho kiểm thử: đứng yên cho tới khi [advance].
class FakeClock implements Clock {
  FakeClock(this._now);
  DateTime _now;

  @override
  DateTime now() => _now;

  void advance(Duration d) => _now = _now.add(d);
  void set(DateTime t) => _now = t;
}

/// RFC 3339 kèm độ lệch múi giờ của máy, ví dụ `2026-12-02T08:15:00+07:00`
/// (hợp đồng đồng bộ yêu cầu event_time có offset).
String formatRfc3339(DateTime t) {
  final local = t.toLocal();
  String two(int v) => v.toString().padLeft(2, '0');
  final off = local.timeZoneOffset;
  final sign = off.isNegative ? '-' : '+';
  final abs = off.abs();
  final oh = two(abs.inHours);
  final om = two(abs.inMinutes.remainder(60));
  return '${local.year.toString().padLeft(4, '0')}-${two(local.month)}-${two(local.day)}'
      'T${two(local.hour)}:${two(local.minute)}:${two(local.second)}$sign$oh:$om';
}

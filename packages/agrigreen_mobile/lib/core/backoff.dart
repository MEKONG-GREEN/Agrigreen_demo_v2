import 'dart:math';

/// Lùi lũy thừa có trần và jitter ("full jitter" một nửa) cho việc thử lại đồng bộ.
class Backoff {
  const Backoff({
    this.base = const Duration(seconds: 5),
    this.max = const Duration(minutes: 30),
    this.jitter = 0.2,
  });

  final Duration base;
  final Duration max;

  /// Tỷ lệ dao động ±jitter quanh giá trị lũy thừa.
  final double jitter;

  /// Độ trễ sau [failures] lần thất bại liên tiếp (failures ≥ 1).
  Duration delayFor(int failures, {Random? random}) {
    if (failures <= 0) return Duration.zero;
    final exp = min(failures - 1, 20);
    final raw = base.inMilliseconds * pow(2, exp);
    final capped = min(raw.toDouble(), max.inMilliseconds.toDouble());
    final r = random ?? Random();
    final factor = 1 + jitter * (r.nextDouble() * 2 - 1);
    final ms = (capped * factor).round();
    return Duration(milliseconds: min(ms, max.inMilliseconds));
  }
}

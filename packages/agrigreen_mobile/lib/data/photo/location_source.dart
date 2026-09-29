import 'package:geolocator/geolocator.dart';

import '../crypto/envelope.dart';

enum GpsStatus {
  ok,

  /// Sai số lớn (> ngưỡng) — cảnh báo độ chính xác thấp (HTX US-PLOT-02 AC-4).
  weak,

  /// Người dùng từ chối quyền — không chặn lưu, gắn nhãn "Thiếu GPS".
  denied,

  /// Tắt định vị / không có GPS thiết bị.
  unavailable;

  String get label => switch (this) {
    GpsStatus.ok => 'GPS tốt',
    GpsStatus.weak => 'GPS yếu',
    GpsStatus.denied => 'Thiếu GPS',
    GpsStatus.unavailable => 'Không có GPS thiết bị',
  };
}

class LocationResult {
  const LocationResult(this.status, [this.fix]);
  final GpsStatus status;
  final GpsFix? fix;
}

abstract class LocationSource {
  Future<LocationResult> current();
}

/// Ngưỡng sai số coi là GPS yếu.
const double weakAccuracyM = 20;

class GeolocatorLocationSource implements LocationSource {
  const GeolocatorLocationSource();

  @override
  Future<LocationResult> current() async {
    if (!await Geolocator.isLocationServiceEnabled()) {
      return const LocationResult(GpsStatus.unavailable);
    }
    var perm = await Geolocator.checkPermission();
    if (perm == LocationPermission.denied) {
      // Luôn hỏi, không tự bật GPS (US-GPS-01 AC-1).
      perm = await Geolocator.requestPermission();
    }
    if (perm == LocationPermission.denied ||
        perm == LocationPermission.deniedForever) {
      return const LocationResult(GpsStatus.denied);
    }
    try {
      final p = await Geolocator.getCurrentPosition(
        locationSettings: const LocationSettings(
          accuracy: LocationAccuracy.high,
          timeLimit: Duration(seconds: 20),
        ),
      );
      final fix = GpsFix(
        lat: p.latitude,
        lon: p.longitude,
        accuracyM: p.accuracy,
      );
      return LocationResult(
        p.accuracy > weakAccuracyM ? GpsStatus.weak : GpsStatus.ok,
        fix,
      );
    } catch (_) {
      return const LocationResult(GpsStatus.unavailable);
    }
  }
}

/// Nguồn vị trí cố định cho kiểm thử.
class FixedLocationSource implements LocationSource {
  const FixedLocationSource(this.result);
  final LocationResult result;
  @override
  Future<LocationResult> current() async => result;
}

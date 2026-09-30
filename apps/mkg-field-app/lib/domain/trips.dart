/// Chuyến của tài xế (mkg/logistics, `contracts/openapi/mkg-internal.yaml` tag
/// logistics). Trên máy chỉ giữ phần tài xế cần — **không** lưu cước, giá,
/// tên người (RB-3: tài xế/phương tiện chỉ là `*_ref`).
library;

import 'fieldops.dart' show shortRef;

/// Trạng thái chuyến — chỉ tiến `planned → departed → arrived → completed`,
/// hủy từ `planned`/`departed` (Go cưỡng chế, L-02; app phản chiếu).
abstract final class TripStatus {
  static const planned = 'planned';
  static const departed = 'departed';
  static const arrived = 'arrived';
  static const completed = 'completed';
  static const cancelled = 'cancelled';

  /// Trạng thái tài xế còn việc (kéo về máy khi đồng bộ).
  static const active = [planned, departed, arrived];

  static String label(String s) => switch (s) {
    planned => 'Chờ xuất phát',
    departed => 'Đang chạy',
    arrived => 'Đã đến nơi',
    completed => 'Đã hoàn tất',
    cancelled => 'Đã hủy',
    _ => s,
  };

  /// Bậc trong chuỗi chỉ tiến (hủy / lạ = -1).
  static int rank(String s) => switch (s) {
    planned => 0,
    departed => 1,
    arrived => 2,
    completed => 3,
    _ => -1,
  };
}

/// Mốc tài xế ghi trên app.
enum Milestone {
  departed('departed', 'Xuất phát', 'Ghi xuất phát'),
  arrived('arrived', 'Đến nơi', 'Ghi đã đến');

  const Milestone(this.code, this.label, this.action);
  final String code;
  final String label;
  final String action;

  /// Trạng thái chuyến sau mốc.
  String get target => code;

  static Milestone? tryParse(String? c) {
    for (final m in values) {
      if (m.code == c) return m;
    }
    return null;
  }
}

/// Mốc kế tiếp được phép từ trạng thái [status] — từng bậc, không nhảy, không
/// lùi (phản chiếu `domain.CanTransition` của máy chủ). null = không còn mốc
/// của tài xế (đã đến → chờ cân / điều phối hoàn tất; hoàn tất; hủy).
Milestone? nextMilestone(String status) => switch (status) {
  TripStatus.planned => Milestone.departed,
  TripStatus.departed => Milestone.arrived,
  _ => null,
};

/// Lỗi chuyển trạng thái trên máy (trước khi xếp hàng) — null = hợp lệ.
String? checkMilestone(String status, Milestone m) {
  if (status == TripStatus.completed || status == TripStatus.cancelled) {
    return logisticsMessage('logistics.trip_closed');
  }
  if (nextMilestone(status) != m) {
    return TripStatus.rank(m.target) <= TripStatus.rank(status)
        ? 'Chuyến đã qua mốc "${m.label}" — không ghi lùi được.'
        : 'Phải ghi "${nextMilestone(status)?.label ?? '—'}" trước.';
  }
  return null;
}

String modeLabel(String? m) => switch (m) {
  'road' => 'Đường bộ',
  'waterway' => 'Đường thủy',
  _ => m ?? '—',
};

/// Loại điểm đầu/cuối chặng (`validKind` của mkg/logistics/app/legs.go).
String endpointLabel(String? kind) => switch (kind) {
  'plot' => 'Thửa ruộng',
  'collection_point' => 'Điểm tập kết',
  'facility' => 'Kho / nhà máy',
  'customer_site' => 'Điểm giao khách',
  'point' => 'Điểm',
  _ => kind ?? 'Điểm',
};

/// kg (chuỗi thập phân của máy chủ) → "12,5 tấn". Chỉ để hiển thị.
String tonnesLabel(String? kg) {
  if (kg == null) return '—';
  final v = double.tryParse(kg);
  if (v == null) return '—';
  var s = (v / 1000).toStringAsFixed(2);
  s = s.replaceFirst(RegExp(r'\.?0+$'), '');
  return '${s.replaceAll('.', ',')} tấn';
}

class TripPoint {
  const TripPoint({required this.kind, this.ref, this.lat, this.lng});
  final String kind;
  final String? ref;
  final String? lat;
  final String? lng;

  String get label => endpointLabel(kind);
  String get coords => lat == null || lng == null ? '' : '$lat, $lng';

  factory TripPoint.fromJson(Map<String, Object?>? j) => TripPoint(
    kind: '${j?['kind'] ?? ''}',
    ref: j?['ref'] as String?,
    lat: j?['lat'] as String?,
    lng: j?['lng'] as String?,
  );

  Map<String, Object?> toJson() => {
    'kind': kind,
    'ref': ref,
    'lat': lat,
    'lng': lng,
  };
}

class TripLeg {
  const TripLeg({
    required this.seq,
    required this.mode,
    required this.from,
    required this.to,
    this.distanceKm,
  });
  final int seq;
  final String mode;
  final TripPoint from;
  final TripPoint to;
  final String? distanceKm;

  String get distanceLabel {
    final d = double.tryParse(distanceKm ?? '');
    if (d == null) return '';
    return '${d.toStringAsFixed(1).replaceAll('.', ',')} km';
  }

  factory TripLeg.fromJson(Map<String, Object?> j) => TripLeg(
    seq: (j['seq'] as num?)?.toInt() ?? 0,
    mode: '${j['mode'] ?? ''}',
    from: TripPoint.fromJson((j['from'] as Map?)?.cast<String, Object?>()),
    to: TripPoint.fromJson((j['to'] as Map?)?.cast<String, Object?>()),
    distanceKm: j['distance_km'] as String?,
  );

  Map<String, Object?> toJson() => {
    'seq': seq,
    'mode': mode,
    'from': from.toJson(),
    'to': to.toJson(),
    'distance_km': distanceKm,
  };
}

/// Chuyến như máy lưu. Dựng từ `LogisticsTrip` của máy chủ qua [Trip.fromApi]
/// — bỏ cước kế hoạch/thực, lô hàng, phiếu cân, người lập (tối thiểu hóa dữ
/// liệu trên máy).
class Trip {
  const Trip({
    required this.tripId,
    required this.code,
    required this.status,
    required this.mode,
    required this.planRevision,
    this.vehicleRef,
    this.driverRef,
    this.plannedKg,
    this.plannedAt,
    this.departedAt,
    this.arrivedAt,
    this.legs = const [],
    this.flags = const [],
    this.hasLegs = false,
  });

  final String tripId;
  final String code;
  final String status;
  final String mode;
  final int planRevision;
  final String? vehicleRef;
  final String? driverRef;
  final String? plannedKg;
  final String? plannedAt;
  final String? departedAt;
  final String? arrivedAt;
  final List<TripLeg> legs;
  final List<String> flags;

  /// Đã tải chi tiết (chặng) chưa — danh sách của máy chủ không kèm chặng.
  final bool hasLegs;

  String get vehicleLabel => vehicleRef == null
      ? 'Chưa gán phương tiện'
      : 'PT ${shortRef(vehicleRef!)}';

  /// Điểm đến = điểm cuối chặng cuối của kế hoạch hiệu lực.
  TripPoint? get destination {
    if (legs.isEmpty) return null;
    final sorted = [...legs]..sort((a, b) => a.seq.compareTo(b.seq));
    return sorted.last.to;
  }

  static Trip fromApi(Map<String, Object?> j, {bool withLegs = false}) {
    final legs = (j['legs'] as List?) ?? const [];
    return Trip(
      tripId: '${j['trip_id']}',
      code: '${j['code'] ?? ''}',
      status: '${j['status'] ?? ''}',
      mode: '${j['mode'] ?? ''}',
      planRevision: (j['plan_revision'] as num?)?.toInt() ?? 0,
      vehicleRef: j['vehicle_ref'] as String?,
      driverRef: j['driver_ref'] as String?,
      plannedKg: j['planned_kg'] as String?,
      plannedAt: j['planned_at'] as String?,
      departedAt: j['departed_at'] as String?,
      arrivedAt: j['arrived_at'] as String?,
      legs: [
        for (final l in legs)
          TripLeg.fromJson((l as Map).cast<String, Object?>()),
      ],
      flags: [for (final f in (j['flags'] as List?) ?? const []) '$f'],
      hasLegs: withLegs,
    );
  }

  /// Bản lưu trên máy (không PII, không tiền).
  Map<String, Object?> toJson() => {
    'trip_id': tripId,
    'code': code,
    'status': status,
    'mode': mode,
    'plan_revision': planRevision,
    'vehicle_ref': vehicleRef,
    'driver_ref': driverRef,
    'planned_kg': plannedKg,
    'planned_at': plannedAt,
    'departed_at': departedAt,
    'arrived_at': arrivedAt,
    'legs': [for (final l in legs) l.toJson()],
    'flags': flags,
    'has_legs': hasLegs,
  };

  factory Trip.fromJson(Map<String, Object?> j) {
    final t = fromApi(j);
    return Trip(
      tripId: t.tripId,
      code: t.code,
      status: t.status,
      mode: t.mode,
      planRevision: t.planRevision,
      vehicleRef: t.vehicleRef,
      driverRef: t.driverRef,
      plannedKg: t.plannedKg,
      plannedAt: t.plannedAt,
      departedAt: t.departedAt,
      arrivedAt: t.arrivedAt,
      legs: t.legs,
      flags: t.flags,
      hasLegs: j['has_legs'] == true,
    );
  }
}

/// Một mốc trên máy (đã xếp hàng — chờ gửi, đã gửi, hoặc bị máy chủ từ chối).
class LocalMilestone {
  const LocalMilestone({
    required this.actionId,
    required this.tripId,
    required this.milestone,
    required this.eventTime,
    required this.state,
    this.lat,
    this.lng,
    this.accuracyM,
    this.errorCode,
  });
  final String actionId;
  final String tripId;
  final Milestone milestone;
  final DateTime eventTime;
  final MilestoneState state;
  final String? lat;
  final String? lng;
  final double? accuracyM;
  final String? errorCode;

  String get gpsLabel {
    if (lat == null || lng == null) return 'Không có GPS';
    final acc = accuracyM == null ? '' : ' (±${accuracyM!.round()} m)';
    return '$lat, $lng$acc';
  }
}

enum MilestoneState {
  /// Chờ gửi (ngoại tuyến hoặc chưa tới lượt đồng bộ).
  pending,

  /// Máy chủ đã nhận (giữ 48 giờ để lớp phủ không "nháy").
  sent,

  /// Máy chủ từ chối (409 `logistics.*`…) — xung đột, hiển thị rõ.
  conflict;

  String get label => switch (this) {
    MilestoneState.pending => 'Chờ đồng bộ',
    MilestoneState.sent => 'Đã đồng bộ',
    MilestoneState.conflict => 'Xung đột',
  };
}

/// Trạng thái hiệu lực trên máy = trạng thái máy chủ (lần tải gần nhất) phủ
/// bởi mốc chờ gửi / vừa gửi (không tính mốc bị từ chối). Không bao giờ lùi.
String effectiveStatus(String serverStatus, Iterable<LocalMilestone> local) {
  var s = serverStatus;
  if (s == TripStatus.completed || s == TripStatus.cancelled) return s;
  for (final m in local) {
    if (m.state == MilestoneState.conflict) continue;
    if (TripStatus.rank(m.milestone.target) > TripStatus.rank(s)) {
      s = m.milestone.target;
    }
  }
  return s;
}

/// Mã lỗi máy chủ (`logistics.*`, dùng chung) → câu tiếng Việt cho tài xế.
String logisticsMessage(String? code) => switch (code) {
  'logistics.trip_transition' => 'Mốc không hợp lệ với trạng thái chuyến trên máy chủ (chỉ ghi tiến từng bậc: xuất phát → đến nơi). Có thể mốc đã được ghi ở nơi khác — kéo xuống để làm mới.',
  'logistics.trip_closed' =>
    'Chuyến đã hoàn tất hoặc đã bị hủy — không ghi mốc được nữa.',
  'logistics.trip_not_found' =>
    'Không còn chuyến này (đã bị xóa khỏi phân công hoặc ngoài phạm vi).',
  'validation_failed' => 'Dữ liệu mốc không hợp lệ (thời điểm ở tương lai hoặc tọa độ sai) — kiểm tra giờ máy và GPS.',
  'forbidden' => 'Tài khoản không có vai tài xế — liên hệ điều phối.',
  'idempotency_key_reused' =>
    'Mã gửi trùng với thao tác khác — liên hệ quản trị.',
  null => 'Bị từ chối.',
  _ => 'Bị từ chối ($code).',
};

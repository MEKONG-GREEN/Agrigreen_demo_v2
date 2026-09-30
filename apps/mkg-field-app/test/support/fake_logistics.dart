import 'package:agrigreen_mobile/data/sync/sync_api.dart';

/// mkg/logistics giả (api-internal) theo `internal/mkg/logistics/handler` +
/// `app/trip.go`:
/// - `GET /v1/logistics/trips?status=&cursor=&limit=` mới nhất trước, con trỏ
///   mờ (`next_cursor` khi trang đầy), con trỏ hỏng → 422 `validation_failed`;
/// - `GET /v1/logistics/trips/{id}` (kèm chặng) — 404 `logistics.trip_not_found`;
/// - `POST /v1/logistics/trips/{id}/milestones` — Idempotency-Key (gọi lặp →
///   phản hồi lần đầu), chỉ tiến từng bậc (409 `logistics.trip_transition`),
///   chuyến đóng (409 `logistics.trip_closed`), lat/lng bắt buộc (422).
class FakeLogistics {
  /// trip_id → `LogisticsTrip` (kể cả cước — để kiểm máy KHÔNG lưu).
  final Map<String, Map<String, Object?>> trips = {};

  /// Khóa → phản hồi đã trả (phát lại).
  final Map<String, Map<String, Object?>> _idem = {};

  /// Mốc đã ghi sổ (đúng một lần mỗi khóa).
  final List<Map<String, Object?>> milestones = [];
  int replays = 0;

  /// Trần kích thước trang của máy chủ (bỏ qua `limit` lớn hơn).
  int pageCap = 200;
  final List<Map<String, String>?> listQueries = [];
  final List<String> detailGets = [];

  /// Ghi sổ xong nhưng "mất" phản hồi (máy thấy lỗi mạng) n lần.
  int dropResponses = 0;

  /// Trả `next_cursor` hỏng n lần (máy phải đọc lại từ đầu).
  int corruptCursors = 0;

  void add(Map<String, Object?> trip) => trips['${trip['trip_id']}'] = trip;

  List<Map<String, Object?>> _sorted(String? status) {
    final l = [
      for (final t in trips.values)
        if (status == null || t['status'] == status) t,
    ];
    l.sort((a, b) {
      final c = '${b['planned_at']}'.compareTo('${a['planned_at']}');
      return c != 0 ? c : '${b['trip_id']}'.compareTo('${a['trip_id']}');
    });
    return l;
  }

  Map<String, Object?> _summary(Map<String, Object?> t) =>
      {...t}..remove('legs');

  Map<String, Object?>? get(String path, Map<String, String>? q) {
    if (path == '/v1/logistics/trips') {
      listQueries.add(q);
      final limit = int.tryParse(q?['limit'] ?? '') ?? 50;
      final n = limit < pageCap ? limit : pageCap;
      var start = 0;
      final c = q?['cursor'];
      if (c != null) {
        final m = RegExp(r'^c:(\d+)$').firstMatch(c);
        if (m == null) {
          throw ApiException(422, 'validation_failed');
        }
        start = int.parse(m.group(1)!);
      }
      final all = _sorted(q?['status']);
      final page = all.skip(start).take(n).toList();
      String? next = page.length == n ? 'c:${start + n}' : null;
      if (next != null && corruptCursors > 0) {
        corruptCursors--;
        next = 'hỏng';
      }
      return {
        'items': [for (final t in page) _summary(t)],
        'next_cursor': next,
      };
    }
    final m = RegExp(r'^/v1/logistics/trips/([^/]+)$').firstMatch(path);
    if (m != null) {
      detailGets.add(m.group(1)!);
      final t = trips[m.group(1)];
      if (t == null) throw ApiException(404, 'logistics.trip_not_found');
      return {'trip': t};
    }
    return null;
  }

  static int _rank(Object? s) => switch (s) {
    'planned' => 0,
    'departed' => 1,
    'arrived' => 2,
    'completed' => 3,
    _ => -1,
  };

  Map<String, Object?> milestone(
    String id,
    String key,
    Map<String, Object?>? body,
  ) {
    final cached = _idem[key];
    if (cached != null) {
      replays++;
      return cached;
    }
    final t = trips[id];
    if (t == null) throw ApiException(404, 'logistics.trip_not_found');
    final kind = body?['kind'];
    if (kind != 'departed' && kind != 'arrived') {
      throw ApiException(422, 'validation_failed');
    }
    if (body?['lat'] is! String || body?['lng'] is! String) {
      throw ApiException(422, 'validation_failed');
    }
    if (DateTime.tryParse('${body?['event_time']}') == null) {
      throw ApiException(422, 'validation_failed');
    }
    final st = t['status'];
    if (st == 'completed' || st == 'cancelled') {
      throw ApiException(409, 'logistics.trip_closed');
    }
    if (_rank(kind) != _rank(st) + 1) {
      throw ApiException(409, 'logistics.trip_transition');
    }
    t['status'] = kind;
    t['${kind}_at'] = body!['event_time'];
    milestones.add({'trip_id': id, 'key': key, ...body});
    final resp = {
      'trip': t,
      'warnings': const <String>[],
      'event': {'kind': kind, 'status': kind},
    };
    _idem[key] = resp;
    if (dropResponses > 0) {
      dropResponses--;
      throw NetworkException('response lost');
    }
    return resp;
  }
}

const tripA = '01920000-0000-7000-8000-00000000d0a1';
const tripB = '01920000-0000-7000-8000-00000000d0a2';
const tripC = '01920000-0000-7000-8000-00000000d0a3';
const vehicleRef = '01920000-0000-7000-8000-00000000f0f1';
const facilityRef = '01920000-0000-7000-8000-00000000f0e1';

/// `LogisticsTrip` của máy chủ (có cả cước / lô — máy phải bỏ).
Map<String, Object?> tripData({
  String id = tripA,
  String code = 'CH000001',
  String status = 'planned',
  String mode = 'waterway',
  String? driver,
  String plannedAt = '2026-10-01T01:00:00Z',
  String plannedKg = '12500.000',
  int planRevision = 1,
  bool legs = true,
}) => {
  'trip_id': id,
  'code': code,
  'status': status,
  'mode': mode,
  'vehicle_ref': vehicleRef,
  'driver_ref': driver,
  'collection_order_ref': null,
  'delivery_ref': null,
  'cargo': [
    {'lot_ref': '01920000-0000-7000-8000-00000000c0c1', 'mass_kg': plannedKg},
  ],
  'planned_kg': plannedKg,
  'actual_kg': null,
  'planned_cost': '11250000.00',
  'actual_cost': null,
  'cost_source': null,
  'plan_revision': planRevision,
  'planned_at': plannedAt,
  'departed_at': null,
  'arrived_at': null,
  'completed_at': null,
  'cancelled_at': null,
  'cancel_reason': null,
  'ticket_ref': null,
  'source_ref': null,
  'flags': const <String>[],
  'rule_version': 'logistics.trip.v1',
  'recorded_at': plannedAt,
  if (legs)
    'legs': [
      {
        'leg_id': '01920000-0000-7000-8000-00000000e0e1',
        'seq': 1,
        'plan_revision': planRevision,
        'mode': mode,
        'from': {
          'kind': 'collection_point',
          'ref': null,
          'lat': '10.0451234',
          'lng': '105.7701234',
        },
        'to': {
          'kind': 'facility',
          'ref': facilityRef,
          'lat': '10.2001000',
          'lng': '105.9002000',
        },
        'distance_km': '42.315',
        'route_source': 'waterway_network',
        'route_snapshot_ref': null,
        'network_version': 3,
        'manual_reason': null,
        'mass_kg': plannedKg,
        'rate_vnd_per_t_km': '900.0000',
        'planned_cost': '11250000.00',
        'param_version': '#0',
        'flags': const <String>[],
        'rule_version': 'logistics.leg.v1',
      },
    ],
};

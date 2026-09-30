import 'dart:async';
import 'dart:convert';

import 'package:agrigreen_mobile/core/clock.dart';
import 'package:agrigreen_mobile/data/actions/action_queue.dart';
import 'package:agrigreen_mobile/data/db/local_db.dart';
import 'package:agrigreen_mobile/data/photo/location_source.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';

import '../domain/roles.dart';
import '../domain/trips.dart';

/// Lỗi nghiệp vụ chuyến hiển thị cho tài xế.
class TripError implements Exception {
  TripError(this.message);
  final String message;
  @override
  String toString() => message;
}

/// Chuyến như tài xế thấy: bản máy chủ (lần tải gần nhất) + mốc trên máy.
class TripView {
  const TripView(this.trip, this.local);
  final Trip trip;

  /// Mốc đã xếp hàng trên máy của chuyến, theo thứ tự ghi.
  final List<LocalMilestone> local;

  /// Trạng thái hiệu lực (chỉ tiến): máy chủ phủ bởi mốc chờ gửi / vừa gửi.
  String get status => effectiveStatus(trip.status, local);
  Milestone? get next => nextMilestone(status);
  bool get pending => local.any((m) => m.state == MilestoneState.pending);
  List<LocalMilestone> get conflicts => [
    for (final m in local)
      if (m.state == MilestoneState.conflict) m,
  ];

  /// Mốc [m] mới nhất trên máy (không tính mốc bị từ chối).
  LocalMilestone? localOf(Milestone m) {
    LocalMilestone? out;
    for (final l in local) {
      if (l.milestone == m && l.state != MilestoneState.conflict) out = l;
    }
    return out;
  }

  /// Thời điểm mốc [m]: máy chủ nếu đã có, không thì bản trên máy.
  DateTime? timeOf(Milestone m) {
    final server = switch (m) {
      Milestone.departed => trip.departedAt,
      Milestone.arrived => trip.arrivedAt,
    };
    final t = server == null ? null : DateTime.tryParse(server);
    return t ?? localOf(m)?.eventTime;
  }
}

/// Mốc chuyến của tài xế (G4 mkg/logistics) trên máy — ngoại tuyến trước:
///
/// - chiều xuống: sau mỗi lượt pull của đồng bộ, tải chuyến `planned` /
///   `departed` / `arrived` (`GET /v1/logistics/trips?status=…`, con trỏ) chỉ
///   khi tài khoản có vai `driver`; giữ chuyến giao cho tôi (`driver_ref` =
///   `sub`) — máy chủ chưa có bộ lọc "của tôi" nên lọc ở máy;
/// - chiều lên: mốc xuất phát / đến xếp vào hàng lệnh API (`api_action`,
///   Idempotency-Key cố định `act:<id>`), `event_time` theo đồng hồ máy lúc
///   bấm, tọa độ GPS; gửi khi có mạng (`SyncService._replayActions`), phát
///   lại an toàn; 409 `logistics.*` → xung đột hiển thị rõ, không thử lại.
class TripService {
  TripService({
    required LocalDb db,
    required ActionQueue actions,
    required SyncApi api,
    required LocationSource location,
    required Clock clock,
    required Future<String?> Function() subject,
    required Future<List<String>> Function() roles,
    this.gpsTimeout = const Duration(seconds: 10),
    this.pageSize = 200,
  }) : _db = db,
       _actions = actions,
       _api = api,
       _location = location,
       _clock = clock,
       _subject = subject,
       _roles = roles;

  final LocalDb _db;
  final ActionQueue _actions;
  final SyncApi _api;
  final LocationSource _location;
  final Clock _clock;
  final Future<String?> Function() _subject;
  final Future<List<String>> Function() _roles;
  final Duration gpsTimeout;
  final int pageSize;

  static const tripsKey = 'logistics.trips';

  /// actionId → dấu GPS phụ (sai số, trạng thái) — hợp đồng máy chủ chưa có
  /// trường sai số nên giữ trên máy để hiển thị.
  static const gpsKey = 'logistics.milestone_gps';

  Future<bool> isDriverAccount() async => isDriver(await _roles());

  // ------------------------------------------------------------ chiều xuống

  /// Tải chuyến của tôi (sau pull của đồng bộ). Không có vai `driver` → không
  /// gọi máy chủ. Lỗi mạng / 5xx ném ra (lượt đồng bộ lùi thử lại); lỗi dữ
  /// liệu (403 mất vai, 422…) giữ bản cũ trên máy.
  Future<void> refresh() async {
    if (!await isDriverAccount()) return;
    final me = await _subject();
    if (me == null) return;
    final prev = {for (final t in await _cached()) t.tripId: t};
    // Chuyến đổi trạng thái giữa hai lần đọc có thể xuất hiện ở hai danh
    // sách: giữ bản ở bậc cao hơn (mới hơn — trạng thái chỉ tiến).
    final mine = <String, Trip>{};
    try {
      for (final st in TripStatus.active) {
        await _page(st, (t) {
          if (t.driverRef != me) return;
          final had = mine[t.tripId];
          if (had == null ||
              TripStatus.rank(t.status) > TripStatus.rank(had.status)) {
            mine[t.tripId] = t;
          }
        });
      }
    } on ApiException catch (e) {
      if (e is UnauthenticatedException || e.retriable) rethrow;
      return;
    }
    final out = <Trip>[];
    for (final t in mine.values) {
      final old = prev[t.tripId];
      if (old != null && old.hasLegs && old.planRevision == t.planRevision) {
        out.add(_withLegs(t, old.legs));
        continue;
      }
      try {
        final j = await _api.getJson('/v1/logistics/trips/${t.tripId}');
        final raw = (j['trip'] as Map?)?.cast<String, Object?>();
        out.add(raw == null ? t : Trip.fromApi(raw, withLegs: true));
      } on ApiException catch (e) {
        if (e is UnauthenticatedException || e.retriable) rethrow;
        // 404 (vừa bị hủy/xóa khỏi phạm vi): bỏ chuyến.
      }
    }
    await _db.setMeta(
      tripsKey,
      jsonEncode({
        'fetched_at': _clock.now().millisecondsSinceEpoch,
        'trips': [for (final t in out) t.toJson()],
      }),
    );
    await _pruneGps();
  }

  /// Đọc hết các trang của một trạng thái theo `next_cursor`. Con trỏ hỏng
  /// (422) → đọc lại từ đầu một lần.
  Future<void> _page(String status, void Function(Trip) onTrip) async {
    String? cursor;
    var restarted = false;
    final seen = <String>{};
    for (var guard = 0; guard < 1000; guard++) {
      Map<String, Object?> j;
      try {
        j = await _api.getJson(
          '/v1/logistics/trips',
          query: {'status': status, 'limit': '$pageSize', 'cursor': ?cursor},
        );
      } on ApiException catch (e) {
        if (e.status == 422 && cursor != null && !restarted) {
          restarted = true;
          cursor = null;
          continue;
        }
        rethrow;
      }
      for (final raw in (j['items'] as List?) ?? const []) {
        final t = Trip.fromApi((raw as Map).cast<String, Object?>());
        if (seen.add(t.tripId)) onTrip(t);
      }
      final next = j['next_cursor'];
      if (next is! String || next.isEmpty || next == cursor) return;
      cursor = next;
    }
  }

  Trip _withLegs(Trip t, List<TripLeg> legs) => Trip.fromJson({
    ...t.toJson(),
    'legs': [for (final l in legs) l.toJson()],
    'has_legs': true,
  });

  Future<List<Trip>> _cached() async {
    final j = await _db.getMetaJson(tripsKey);
    return [
      for (final t in (j?['trips'] as List?) ?? const [])
        Trip.fromJson((t as Map).cast<String, Object?>()),
    ];
  }

  /// Lần tải chuyến gần nhất (null = chưa từng).
  Future<DateTime?> fetchedAt() async {
    final ms = (await _db.getMetaJson(tripsKey))?['fetched_at'];
    return ms is int ? DateTime.fromMillisecondsSinceEpoch(ms) : null;
  }

  // ------------------------------------------------------------ đọc trên máy

  Future<Map<String, Map<String, Object?>>> _gpsMeta() async {
    final j = await _db.getMetaJson(gpsKey);
    return {
      for (final e in (j ?? const <String, Object?>{}).entries)
        if (e.value is Map) e.key: (e.value! as Map).cast<String, Object?>(),
    };
  }

  Future<void> _pruneGps() async {
    final meta = await _gpsMeta();
    if (meta.isEmpty) return;
    final live = {
      for (final a in await _actions.ofKind(ApiActionKind.logisticsMilestone))
        a.id,
    };
    meta.removeWhere((k, _) => !live.contains(k));
    await _db.setMeta(gpsKey, jsonEncode(meta));
  }

  Future<List<LocalMilestone>> _local() async {
    final gps = await _gpsMeta();
    final out = <LocalMilestone>[];
    for (final a in await _actions.ofKind(ApiActionKind.logisticsMilestone)) {
      final m = Milestone.tryParse(a.body?['kind'] as String?);
      final t = DateTime.tryParse('${a.body?['event_time']}');
      if (m == null || t == null || a.target == null) continue;
      out.add(
        LocalMilestone(
          actionId: a.id,
          tripId: a.target!,
          milestone: m,
          eventTime: t,
          state: switch (a.status) {
            ApiActionStatus.pending => MilestoneState.pending,
            ApiActionStatus.done => MilestoneState.sent,
            ApiActionStatus.failed => MilestoneState.conflict,
          },
          lat: a.body?['lat'] as String?,
          lng: a.body?['lng'] as String?,
          accuracyM: (gps[a.id]?['accuracy_m'] as num?)?.toDouble(),
          errorCode: a.status == ApiActionStatus.failed ? a.lastError : null,
        ),
      );
    }
    return out;
  }

  static int _order(String status) => switch (status) {
    TripStatus.departed => 0,
    TripStatus.planned => 1,
    TripStatus.arrived => 2,
    _ => 3,
  };

  /// Chuyến của tôi trên máy: đang chạy trước, rồi chờ xuất phát, rồi đã đến.
  Future<List<TripView>> trips() async {
    final local = await _local();
    final list = [
      for (final t in await _cached())
        TripView(t, [
          for (final m in local)
            if (m.tripId == t.tripId) m,
        ]),
    ];
    list.sort((a, b) {
      final c = _order(a.status).compareTo(_order(b.status));
      if (c != 0) return c;
      return (b.trip.plannedAt ?? '').compareTo(a.trip.plannedAt ?? '');
    });
    return list;
  }

  Future<TripView?> trip(String tripId) async {
    for (final v in await trips()) {
      if (v.trip.tripId == tripId) return v;
    }
    return null;
  }

  /// Mốc bị máy chủ từ chối (mọi chuyến, kể cả chuyến không còn trên máy).
  Future<List<LocalMilestone>> conflicts() async => [
    for (final m in await _local())
      if (m.state == MilestoneState.conflict) m,
  ];

  Future<int> pendingCount() async => [
    for (final m in await _local())
      if (m.state == MilestoneState.pending) m,
  ].length;

  // ------------------------------------------------------------ chiều lên

  Future<LocationResult> _gps() async {
    try {
      return await _location.current().timeout(gpsTimeout);
    } catch (_) {
      return const LocationResult(GpsStatus.unavailable);
    }
  }

  static String gpsMissingMessage(GpsStatus s) => switch (s) {
    GpsStatus.denied => 'App chưa được cấp quyền vị trí. Bật quyền vị trí rồi thử lại — mốc chuyến cần tọa độ GPS.',
    _ => 'Chưa lấy được vị trí GPS. Bật định vị, ra chỗ thoáng rồi thử lại — mốc chuyến cần tọa độ GPS.',
  };

  /// Ghi mốc [m] cho chuyến [tripId]: kiểm chỉ tiến từng bậc trên máy (máy
  /// chủ kiểm lần hai), lấy GPS, xếp hàng — gửi khi có mạng.
  Future<LocalMilestone> record(String tripId, Milestone m) async {
    if (!await isDriverAccount()) {
      throw TripError(logisticsMessage('forbidden'));
    }
    final v = await trip(tripId);
    if (v == null) {
      throw TripError(logisticsMessage('logistics.trip_not_found'));
    }
    final err = checkMilestone(v.status, m);
    if (err != null) throw TripError(err);
    final gps = await _gps();
    final fix = gps.fix;
    if (fix == null) throw TripError(gpsMissingMessage(gps.status));
    final now = _clock.now();
    final a = await _actions.enqueue(
      ApiActionKind.logisticsMilestone,
      target: tripId,
      body: {
        'kind': m.code,
        'event_time': formatRfc3339(now),
        'lat': fix.lat.toStringAsFixed(7),
        'lng': fix.lon.toStringAsFixed(7),
      },
    );
    final meta = await _gpsMeta();
    meta[a.id] = {'gps_status': gps.status.name, 'accuracy_m': ?fix.accuracyM};
    await _db.setMeta(gpsKey, jsonEncode(meta));
    return (await _local()).firstWhere((l) => l.actionId == a.id);
  }
}

import 'dart:async';
import 'dart:convert';

import 'package:agrigreen_mobile/core/clock.dart';
import 'package:agrigreen_mobile/data/actions/action_queue.dart';
import 'package:agrigreen_mobile/data/crypto/envelope.dart';
import 'package:agrigreen_mobile/data/db/local_db.dart';
import 'package:agrigreen_mobile/data/photo/location_source.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';

import '../domain/roles.dart';
import '../domain/trips.dart';
import 'mkg_action_kind.dart';

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
/// - chiều xuống: sau mỗi lượt pull của đồng bộ, chỉ khi tài khoản có vai
///   `driver`, tải "chuyến của tôi" do MÁY CHỦ lọc
///   (`GET /v1/logistics/trips?assigned_to_me=true`, `driver_ref` = `sub`):
///   lần đầu (hoặc định kỳ [TripService.fullEvery], đổi tài khoản, con trỏ bị
///   từ chối) đọc đầy đủ theo trạng thái `planned` / `departed` / `arrived`;
///   các lần sau đọc delta `updated_since=<con trỏ>` (chuyến có sự kiện ghi
///   sau mốc, kể cả hủy / hoàn tất → rời máy), phân trang `next_cursor`. Con
///   trỏ delta = `recorded_at` lớn nhất đã thấy (máy chủ so
///   `recorded_at > mốc`), lưu trên máy; gửi lùi [TripService.deltaOverlap]
///   để không sót giao dịch ghi xong muộn (bản trùng gộp theo `trip_id`);
/// - chiều lên: mốc xuất phát / đến xếp vào hàng lệnh API (`api_action`,
///   Idempotency-Key cố định `act:<id>`), `event_time` theo đồng hồ máy lúc
///   bấm, tọa độ GPS + `accuracy_m` nếu có — KHÔNG có GPS vẫn ghi (bỏ
///   lat/lng, máy chủ lưu NULL, không bao giờ 0,0) kèm ghi chú "không có GPS";
///   gửi khi có mạng (`SyncService._replayActions`), phát lại an toàn;
///   409 `logistics.*` → xung đột hiển thị rõ, không thử lại.
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
    this.fullEvery = const Duration(hours: 6),
    this.deltaOverlap = const Duration(minutes: 2),
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

  /// Đọc đầy đủ lại sau khoảng này — delta `assigned_to_me` không báo chuyến
  /// bị giao sang tài xế khác, lượt đầy đủ dọn các chuyến đó khỏi máy.
  final Duration fullEvery;

  /// Gửi `updated_since` lùi khoảng này so với con trỏ đã lưu.
  final Duration deltaOverlap;

  /// `{fetched_at, full_at, since, sub, trips}` — `since` là con trỏ delta
  /// (`recorded_at` lớn nhất đã thấy), `sub` là tài khoản của con trỏ.
  static const tripsKey = 'logistics.trips';

  /// actionId → trạng thái GPS lúc ghi (ok / denied / unavailable…) để hiển
  /// thị; tọa độ + `accuracy_m` nằm trong thân lệnh gửi máy chủ.
  static const gpsKey = 'logistics.milestone_gps';

  Future<bool> isDriverAccount() async => isDriver(await _roles());

  // ------------------------------------------------------------ chiều xuống

  /// Tải chuyến của tôi (sau pull của đồng bộ). Không có vai `driver` → không
  /// gọi máy chủ. Lỗi mạng / 5xx ném ra (lượt đồng bộ lùi thử lại); lỗi dữ
  /// liệu (403 mất vai…) giữ bản cũ trên máy; 422 ở chế độ delta (con trỏ /
  /// `updated_since` bị từ chối) → đọc đầy đủ lại.
  Future<void> refresh() async {
    if (!await isDriverAccount()) return;
    final me = await _subject();
    if (me == null) return;
    final meta = await _db.getMetaJson(tripsKey);
    final sameSub = meta?['sub'] == me;
    final prevList = await _cached();
    final prev = {for (final t in prevList) t.tripId: t};
    final rawSince = meta?['since'];
    final since = sameSub && rawSince is String
        ? DateTime.tryParse(rawSince)
        : null;
    final rawFull = meta?['full_at'];
    final fullAt = sameSub && rawFull is int
        ? DateTime.fromMillisecondsSinceEpoch(rawFull)
        : null;
    final now = _clock.now();

    _Read? read;
    try {
      if (since != null &&
          fullAt != null &&
          !now.isBefore(fullAt) &&
          now.difference(fullAt) < fullEvery) {
        try {
          read = await _readDelta(since);
        } on ApiException catch (e) {
          if (e.status != 422) rethrow;
          read = null; // con trỏ / updated_since bị từ chối → đọc đầy đủ
        }
      }
      read ??= await _readFull();
    } on ApiException catch (e) {
      if (e is UnauthenticatedException || e.retriable) rethrow;
      return;
    }

    // Gộp: lượt đầy đủ thay toàn bộ; delta phủ lên bản trên máy — chuyến
    // không còn hoạt động (hoàn tất / hủy) rời máy.
    final merged = <String, Trip>{
      if (!read.full)
        for (final t in prevList) t.tripId: t,
    };
    for (final t in read.trips.values) {
      if (TripStatus.active.contains(t.status)) {
        merged[t.tripId] = t;
      } else {
        merged.remove(t.tripId);
      }
    }
    final out = <Trip>[];
    for (final t in merged.values) {
      final old = prev[t.tripId];
      if (t.hasLegs) {
        out.add(t); // bản trên máy không đổi ở lượt delta
        continue;
      }
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
    final nextSince = _later(since, read.maxRecordedAt);
    await _db.setMeta(
      tripsKey,
      jsonEncode({
        'fetched_at': now.millisecondsSinceEpoch,
        'full_at': read.full
            ? now.millisecondsSinceEpoch
            : fullAt?.millisecondsSinceEpoch,
        'since': nextSince?.toUtc().toIso8601String(),
        'sub': me,
        'trips': [for (final t in out) t.toJson()],
      }),
    );
    await _pruneGps();
  }

  static DateTime? _later(DateTime? a, DateTime? b) {
    if (a == null) return b;
    if (b == null) return a;
    return b.isAfter(a) ? b : a;
  }

  /// Đọc đầy đủ chuyến của tôi còn hoạt động, theo từng trạng thái. Chuyến
  /// đổi trạng thái giữa hai lần đọc có thể xuất hiện ở hai danh sách: giữ
  /// bản ở bậc cao hơn (mới hơn — trạng thái chỉ tiến).
  Future<_Read> _readFull() async {
    final r = _Read(full: true);
    for (final st in TripStatus.active) {
      await _page({'status': st}, (t, recordedAt) {
        r.see(recordedAt);
        final had = r.trips[t.tripId];
        if (had == null ||
            TripStatus.rank(t.status) > TripStatus.rank(had.status)) {
          r.trips[t.tripId] = t;
        }
      });
    }
    return r;
  }

  /// Đọc delta sau [since] (mọi trạng thái, `recorded_at` tăng dần — bản sau
  /// của cùng chuyến là bản mới hơn). 422 ném ra để nơi gọi đọc đầy đủ.
  Future<_Read> _readDelta(DateTime since) async {
    final r = _Read(full: false);
    final from = since.subtract(deltaOverlap).toUtc().toIso8601String();
    await _page({'updated_since': from}, (t, recordedAt) {
      r.see(recordedAt);
      r.trips[t.tripId] = t;
    }, restartOnBadCursor: false);
    return r;
  }

  /// Đọc hết các trang theo `next_cursor` (`assigned_to_me=true`). Con trỏ
  /// hỏng (422) → đọc lại từ đầu một lần (hoặc ném ra nếu
  /// [restartOnBadCursor] = false).
  Future<void> _page(
    Map<String, String> filter,
    void Function(Trip trip, DateTime? recordedAt) onTrip, {
    bool restartOnBadCursor = true,
  }) async {
    String? cursor;
    var restarted = false;
    final seen = <String>{};
    for (var guard = 0; guard < 1000; guard++) {
      Map<String, Object?> j;
      try {
        j = await _api.getJson(
          '/v1/logistics/trips',
          query: {
            ...filter,
            'assigned_to_me': 'true',
            'limit': '$pageSize',
            'cursor': ?cursor,
          },
        );
      } on ApiException catch (e) {
        if (e.status == 422 &&
            cursor != null &&
            !restarted &&
            restartOnBadCursor) {
          restarted = true;
          cursor = null;
          continue;
        }
        rethrow;
      }
      for (final raw in (j['items'] as List?) ?? const []) {
        final m = (raw as Map).cast<String, Object?>();
        final t = Trip.fromApi(m);
        if (seen.add('${t.tripId}|${m['recorded_at']}')) {
          onTrip(t, DateTime.tryParse('${m['recorded_at']}'));
        }
      }
      final next = j['next_cursor'];
      if (next is! String || next.isEmpty || next == cursor) return;
      cursor = next;
    }
  }

  /// Con trỏ delta đã lưu (`recorded_at` lớn nhất đã thấy; null = chưa có).
  Future<DateTime?> deltaCursor() async {
    final s = (await _db.getMetaJson(tripsKey))?['since'];
    return s is String ? DateTime.tryParse(s) : null;
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
      for (final a in await _actions.ofKind(MkgActionKind.logisticsMilestone))
        a.id,
    };
    meta.removeWhere((k, _) => !live.contains(k));
    await _db.setMeta(gpsKey, jsonEncode(meta));
  }

  Future<List<LocalMilestone>> _local() async {
    final gps = await _gpsMeta();
    final out = <LocalMilestone>[];
    for (final a in await _actions.ofKind(MkgActionKind.logisticsMilestone)) {
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
          // Thân lệnh (bản mới) hoặc sổ phụ trên máy (lệnh xếp trước G4).
          accuracyM:
              double.tryParse('${a.body?['accuracy_m']}') ??
              (gps[a.id]?['accuracy_m'] as num?)?.toDouble(),
          gpsStatus: gps[a.id]?['gps_status'] as String?,
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

  /// Mốc trên máy chưa bị từ chối (chờ gửi / vừa gửi), mới nhất trước — cho
  /// hàng đợi đồng bộ (kèm ghi chú "không có GPS" nếu thiếu tọa độ).
  Future<List<LocalMilestone>> queued() async => [
    for (final m in (await _local()).reversed)
      if (m.state != MilestoneState.conflict) m,
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

  /// Tọa độ dùng được (trong khoảng, không phải 0,0 — không bao giờ là vị
  /// trí ở VN; máy chủ coi 0,0 là không có GPS).
  static bool _usable(GpsFix f) =>
      f.lat.isFinite &&
      f.lon.isFinite &&
      f.lat.abs() <= 90 &&
      f.lon.abs() <= 180 &&
      !(f.lat == 0 && f.lon == 0);

  /// Số thập phân không âm dạng chuỗi (hợp đồng `accuracy_m`: string).
  static String? _decimal(double? v) {
    if (v == null || !v.isFinite || v < 0) return null;
    var s = v.toStringAsFixed(2);
    s = s.replaceFirst(RegExp(r'\.?0+$'), '');
    return s.isEmpty ? '0' : s;
  }

  /// Ghi mốc [m] cho chuyến [tripId]: kiểm chỉ tiến từng bậc trên máy (máy
  /// chủ kiểm lần hai), lấy GPS (nếu có), xếp hàng — gửi khi có mạng.
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
    final raw = gps.fix;
    // Không có GPS vẫn ghi (tài xế không bị chặn giữa đường): bỏ lat/lng —
    // máy chủ lưu NULL, không bao giờ gửi 0,0; ghi chú "không có GPS".
    final fix = raw != null && _usable(raw) ? raw : null;
    final now = _clock.now();
    final a = await _actions.enqueue(
      MkgActionKind.logisticsMilestone,
      target: tripId,
      body: {
        'kind': m.code,
        'event_time': formatRfc3339(now),
        if (fix != null) ...{
          'lat': fix.lat.toStringAsFixed(7),
          'lng': fix.lon.toStringAsFixed(7),
          'accuracy_m': ?_decimal(fix.accuracyM),
        },
      },
    );
    final meta = await _gpsMeta();
    meta[a.id] = {
      'gps_status': fix == null && gps.status == GpsStatus.ok
          ? GpsStatus.unavailable.name
          : gps.status.name,
    };
    await _db.setMeta(gpsKey, jsonEncode(meta));
    return (await _local()).firstWhere((l) => l.actionId == a.id);
  }
}

/// Kết quả một lượt đọc danh sách chuyến (đầy đủ hoặc delta).
class _Read {
  _Read({required this.full});
  final bool full;
  final Map<String, Trip> trips = {};
  DateTime? maxRecordedAt;

  void see(DateTime? at) {
    if (at != null && (maxRecordedAt == null || at.isAfter(maxRecordedAt!))) {
      maxRecordedAt = at;
    }
  }
}

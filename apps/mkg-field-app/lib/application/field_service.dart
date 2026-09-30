import 'dart:async';
import 'dart:convert';

import 'package:agrigreen_mobile/core/clock.dart';
import 'package:agrigreen_mobile/data/actions/action_queue.dart';
import 'package:agrigreen_mobile/data/db/local_db.dart';
import 'package:agrigreen_mobile/data/outbox/outbox_repository.dart';
import 'package:agrigreen_mobile/data/photo/location_source.dart';
import 'package:agrigreen_mobile/data/reference/reference_repository.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';

import '../domain/fieldops.dart';

/// Lỗi nghiệp vụ hiển thị cho người dùng.
class FieldError implements Exception {
  FieldError(this.message, {this.errors = const []});
  final String message;
  final List<CountRuleError> errors;
  @override
  String toString() => message;
}

/// Bảng số của một lệnh gom: số hiệu lực theo (thửa, công đoạn) và bản ghi bị
/// máy chủ từ chối còn chờ xử lý.
class OrderBoard {
  OrderBoard(this.order, this.counts, this.rejected, this.fetchedAt);
  final CollectionOrder order;

  /// plot_ref → công đoạn → số hiệu lực.
  final Map<String, Map<Stage, StageCount>> counts;

  /// plot_ref → công đoạn → bản ghi bị từ chối chưa được thay.
  final Map<String, Map<Stage, OutboxRecord>> rejected;

  /// Lần cuối tải số từ máy chủ (null = chưa từng).
  final DateTime? fetchedAt;

  StageCount? count(String plot, Stage s) => counts[plot]?[s];
  OutboxRecord? rejectedOf(String plot, Stage s) => rejected[plot]?[s];

  /// Tổng số cuộn theo công đoạn của cả lệnh.
  int totalRolls(Stage s) =>
      [for (final p in order.plotRefs) counts[p]?[s]?.rolls ?? 0]
          .fold(0, (a, b) => a + b);

  /// Số thửa đã có số ở công đoạn [s].
  int plotsDone(Stage s) =>
      order.plotRefs.where((p) => counts[p]?[s] != null).length;
}

/// Kết quả ghi một số (bản ghi đã xếp hàng + trạng thái GPS gắn kèm).
class CountOutcome {
  const CountOutcome(this.record, this.gps);
  final OutboxRecord record;
  final GpsStatus gps;
}

class ShiftEvent {
  const ShiftEvent({
    required this.kind,
    required this.eventTime,
    this.headcount,
    this.pending = false,
  });
  final String kind; // opened | closed
  final DateTime eventTime;
  final int? headcount;
  final bool pending;
}

class ShiftState {
  const ShiftState(this.events);
  final List<ShiftEvent> events;
  ShiftEvent? get last => events.isEmpty ? null : events.last;
  bool get open => last?.kind == 'opened';
}

class ReconRow {
  const ReconRow({
    required this.plotRef,
    required this.mkgRolls,
    required this.status,
    this.htxRolls,
    this.outcome,
    this.orderId,
  });
  final String plotRef;
  final String? orderId;
  final int mkgRolls;
  final int? htxRolls;
  final String? outcome;

  /// pending | match | mismatch
  final String status;

  static String statusLabel(String s) => switch (s) {
    'pending' => 'Chờ HTX xác nhận',
    'match' => 'Khớp',
    'mismatch' => 'Lệch',
    _ => s,
  };

  factory ReconRow.fromJson(Map<String, Object?> j) => ReconRow(
    plotRef: '${j['plot_ref']}',
    orderId: j['order_id'] as String?,
    mkgRolls: (j['mkg_rolls'] as num?)?.toInt() ?? 0,
    htxRolls: (j['htx_rolls'] as num?)?.toInt(),
    outcome: j['outcome'] as String?,
    status: '${j['status'] ?? 'pending'}',
  );
}

/// Dữ liệu trực tuyến kèm thời điểm tải; [stale] = đang xem bản lưu trên máy
/// vì không gọi được máy chủ.
class Cached<T> {
  const Cached(this.value, this.fetchedAt, {this.stale = false});
  final T value;
  final DateTime? fetchedAt;
  final bool stale;
}

/// Nghiệp vụ hiện trường MKG trên máy: lệnh gom (pull), đếm cuộn theo công
/// đoạn qua hàng đợi (push `roll_count` — đúng một lần nhờ `idem_key`), bản
/// ghi bù, ca tổ gom (lệnh API xếp hàng), duyệt lệnh và đối soát (trực tuyến).
class FieldService {
  FieldService({
    required LocalDb db,
    required OutboxRepository outbox,
    required ReferenceRepository refs,
    required ActionQueue actions,
    required SyncApi api,
    required LocationSource location,
    required Clock clock,
    required String deviceId,
    required Future<String?> Function() subject,
    this.gpsTimeout = const Duration(seconds: 10),
  }) : _db = db,
       _outbox = outbox,
       _refs = refs,
       _actions = actions,
       _api = api,
       _location = location,
       _clock = clock,
       _deviceId = deviceId,
       _subject = subject;

  final LocalDb _db;
  final OutboxRepository _outbox;
  final ReferenceRepository _refs;
  final ActionQueue _actions;
  final SyncApi _api;
  final LocationSource _location;
  final Clock _clock;
  final String _deviceId;
  final Future<String?> Function() _subject;

  /// GPS chỉ là dấu phụ: quá thời gian này thì lưu không GPS (không chặn).
  final Duration gpsTimeout;

  static String _countsKey(String order) => 'fieldops.counts.$order';
  static String _shiftsKey(String order) => 'fieldops.shifts.$order';
  static String _reconKey(String? order) => 'fieldops.recon.${order ?? '*'}';
  static const _plannedKey = 'fieldops.planned';

  // ---------------------------------------------------------------- lệnh gom

  /// Lệnh gom đã duyệt / đang làm có trên máy (pull `collection_order`).
  Future<List<CollectionOrder>> orders() async {
    final rows = await _refs.list(RefEntity.collectionOrder);
    final list = [for (final r in rows) CollectionOrder.fromJson(r.data)];
    list.sort((a, b) {
      final c = a.windowFrom.compareTo(b.windowFrom);
      return c != 0 ? c : a.orderId.compareTo(b.orderId);
    });
    return list;
  }

  Future<CollectionOrder?> order(String id) async {
    final r = await _refs.get(RefEntity.collectionOrder, id);
    return r == null ? null : CollectionOrder.fromJson(r.data);
  }

  // ------------------------------------------------------------ bảng số

  /// Tải số hiệu lực của lệnh từ máy chủ (`GET …/roll-counts`) và lưu trên máy.
  Future<void> refreshCounts(String orderId) async {
    final j = await _api.getJson('/v1/fieldops/orders/$orderId/roll-counts');
    await _db.setMeta(
      _countsKey(orderId),
      jsonEncode({
        'fetched_at': _clock.now().millisecondsSinceEpoch,
        'totals': j['totals'] ?? const [],
      }),
    );
  }

  /// Làm mới số của mọi lệnh trên máy (sau pull). Lỗi dữ liệu của một lệnh
  /// (404 hết phạm vi…) không làm hỏng lượt đồng bộ; lỗi mạng thì ném.
  Future<void> refreshAllCounts() async {
    for (final o in await orders()) {
      try {
        await refreshCounts(o.orderId);
      } on ApiException catch (e) {
        if (e is UnauthenticatedException || e.retriable) rethrow;
      }
    }
  }

  Future<List<OutboxRecord>> _recordsOf(String orderId) async {
    final all = await _outbox.list(entity: PushEntity.rollCount);
    return [
      for (final r in all.reversed)
        if (r.payload['order_id'] == orderId) r,
    ];
  }

  /// Số hiệu lực = số máy chủ (lần tải gần nhất) phủ bởi bản ghi trên máy chưa
  /// có trong lần tải đó (chờ gửi, hoặc gửi sau lần tải).
  Future<OrderBoard> board(CollectionOrder order) async {
    final counts = <String, Map<Stage, StageCount>>{};
    final snap = await _db.getMetaJson(_countsKey(order.orderId));
    final fetchedMs = snap?['fetched_at'] as int?;
    for (final t in (snap?['totals'] as List?) ?? const []) {
      if (t is! Map) continue;
      final stage = Stage.tryParse(t['stage'] as String?);
      if (stage == null) continue;
      counts.putIfAbsent('${t['plot_ref']}', () => {})[stage] = StageCount(
        rolls: (t['rolls'] as num?)?.toInt() ?? 0,
        looseKg: t['loose_kg'] as String?,
        discrepancyReason: t['discrepancy_reason'] as String?,
        id: t['roll_count_id'] as String?,
      );
    }
    final rejected = <String, Map<Stage, OutboxRecord>>{};
    for (final r in await _recordsOf(order.orderId)) {
      final p = r.payload;
      final stage = Stage.tryParse(p['stage'] as String?);
      final plot = p['plot_ref'] as String?;
      if (stage == null || plot == null) continue;
      switch (r.status) {
        case OutboxStatus.rejected:
          if (r.replacedBy == null) {
            rejected.putIfAbsent(plot, () => {})[stage] = r;
          }
          continue;
        case OutboxStatus.superseded:
          continue;
        case OutboxStatus.sent || OutboxStatus.conflict:
          final acked = r.ackedAt?.millisecondsSinceEpoch;
          if (fetchedMs != null && acked != null && fetchedMs >= acked) {
            continue; // lần tải số đã phản ánh bản ghi này
          }
        case OutboxStatus.pending:
          break;
      }
      // Bản ghi mới hơn (theo seq) thay bản bị từ chối cùng ô.
      rejected[plot]?.remove(stage);
      counts.putIfAbsent(plot, () => {})[stage] = StageCount(
        rolls: (p['rolls'] as num?)?.toInt() ?? 0,
        looseKg: p['loose_kg'] as String?,
        discrepancyReason: p['discrepancy_reason'] as String?,
        id: r.id,
        local: true,
        pending: r.status == OutboxStatus.pending,
      );
    }
    return OrderBoard(
      order,
      counts,
      rejected,
      fetchedMs == null ? null : DateTime.fromMillisecondsSinceEpoch(fetchedMs),
    );
  }

  /// Đọc GPS để đóng dấu bản ghi — tùy chọn, không bao giờ chặn việc lưu.
  Future<LocationResult> _gps() async {
    try {
      return await _location.current().timeout(gpsTimeout);
    } catch (_) {
      return const LocationResult(GpsStatus.unavailable);
    }
  }

  Map<String, Object?> _payload(
    CollectionOrder order,
    String plotRef,
    Stage stage,
    StageCount c,
    LocationResult gps, {
    String? vesselCode,
  }) => {
    'order_id': order.orderId,
    'plot_ref': plotRef,
    'stage': stage.code,
    'rolls': c.rolls,
    'loose_kg': ?c.looseKg,
    'discrepancy_reason': ?c.discrepancyReason,
    // Dấu GPS tùy chọn (không bắt buộc, không chặn lưu).
    'gps_status': gps.status.name,
    if (gps.fix != null) 'gps': gps.fix!.toJson(),
    // G4 (FM-05): số hiệu ghe tùy chọn — chỉ xếp ghe, đã chuẩn hóa (GH-01);
    // trống → không gửi (bản bù không gửi = giữ số hiệu cũ).
    if (stage.takesVesselCode) 'vessel_code': ?normalizeVesselCode(vesselCode),
  };

  /// Số hiệu ghe sai (FM-05) → lỗi theo trường, không xếp hàng.
  static void _checkVessel(String? vesselCode, Stage stage) {
    final err = checkVesselCode(vesselCode, stage);
    if (err != null) {
      throw FieldError(
        err,
        errors: [CountRuleError('FM-05', 'vessel_code', err)],
      );
    }
  }

  void _requireOpen(CollectionOrder order, String plotRef) {
    if (!order.countable) {
      throw FieldError(rejectMessage('fieldops.order_not_open'));
    }
    if (!order.plotRefs.contains(plotRef)) {
      throw FieldError(rejectMessage('fieldops.plot_not_in_order'));
    }
  }

  /// Ghi số một công đoạn (bản gốc). Kiểm FM-03/04/09 với số hiệu lực trên máy.
  Future<CountOutcome> recordCount({
    required CollectionOrder order,
    required String plotRef,
    required Stage stage,
    required String rollsText,
    String? looseKgText,
    String? discrepancyReason,
    String? vesselCode,
  }) async {
    _requireOpen(order, plotRef);
    _checkVessel(vesselCode, stage);
    final b = await board(order);
    if (b.count(plotRef, stage) != null) {
      throw FieldError(rejectMessage('fieldops.stage_already_counted'));
    }
    final prev = stage.previous;
    final (errs, c) = checkCount(
      stage: stage,
      rollsText: rollsText,
      looseKgText: looseKgText,
      discrepancyReason: discrepancyReason,
      previous: prev == null ? null : b.count(plotRef, prev),
    );
    if (c == null) throw FieldError(errs.first.message, errors: errs);
    final gps = await _gps();
    final payload = _payload(
      order,
      plotRef,
      stage,
      c,
      gps,
      vesselCode: vesselCode,
    );
    final rej = b.rejectedOf(plotRef, stage);
    // Bản bị từ chối không có trên máy chủ → thay bằng bản ghi MỚI (create).
    final rec = rej != null && rej.op == RecordOp.create
        ? await _outbox.replaceRejected(originalId: rej.id, payload: payload)
        : await _outbox.enqueue(entity: PushEntity.rollCount, payload: payload);
    if (rej != null && rej.op != RecordOp.create) {
      await _outbox.linkReplacement(rej.id, rec.id);
    }
    return CountOutcome(rec, gps.status);
  }

  /// Sửa số bằng bản ghi bù (RB-1): bắt buộc lý do ≥ 10 ký tự; công đoạn trước
  /// và sau vẫn phải thỏa FM-04 với giá trị mới.
  Future<CountOutcome> correctCount({
    required CollectionOrder order,
    required String plotRef,
    required Stage stage,
    required String rollsText,
    String? looseKgText,
    String? discrepancyReason,
    required String correctionReason,
    String? vesselCode,
  }) async {
    _requireOpen(order, plotRef);
    _checkVessel(vesselCode, stage);
    final b = await board(order);
    final cur = b.count(plotRef, stage);
    if (cur == null || cur.id == null) {
      throw FieldError('Công đoạn này chưa có số — dùng "Ghi số".');
    }
    final prev = stage.previous;
    final next = stage.next;
    final (errs, c) = checkCount(
      stage: stage,
      rollsText: rollsText,
      looseKgText: looseKgText,
      discrepancyReason: discrepancyReason,
      previous: prev == null ? null : b.count(plotRef, prev),
      correction: true,
      correctionReason: correctionReason,
      next: next == null ? null : b.count(plotRef, next),
    );
    if (c == null) throw FieldError(errs.first.message, errors: errs);
    final gps = await _gps();
    final payload = _payload(
      order,
      plotRef,
      stage,
      c,
      gps,
      vesselCode: vesselCode,
    );
    final OutboxRecord rec;
    if (cur.local && await _outbox.byId(cur.id!) != null) {
      rec = await _outbox.correct(
        originalId: cur.id!,
        reason: correctionReason,
        payload: payload,
      );
    } else {
      rec = await _outbox.correctRemote(
        entity: PushEntity.rollCount,
        supersedesId: cur.id!,
        reason: correctionReason,
        payload: payload,
      );
    }
    return CountOutcome(rec, gps.status);
  }

  // ------------------------------------------------------------- ca tổ gom

  /// Tải ca của lệnh từ máy chủ và lưu trên máy.
  Future<void> refreshShifts(String orderId) async {
    final j = await _api.getJson('/v1/fieldops/orders/$orderId/crew-shifts');
    await _db.setMeta(_shiftsKey(orderId), jsonEncode(j['items'] ?? const []));
  }

  Future<ShiftState> shifts(String orderId) async {
    final events = <ShiftEvent>[];
    final cached = await _db.getMeta(_shiftsKey(orderId));
    final server = cached == null ? const [] : jsonDecode(cached) as List;
    for (final s in server) {
      if (s is! Map) continue;
      final t = DateTime.tryParse('${s['event_time']}');
      if (t == null) continue;
      events.add(
        ShiftEvent(
          kind: '${s['kind']}',
          eventTime: t,
          headcount: (s['headcount'] as num?)?.toInt(),
        ),
      );
    }
    for (final a in await _actions.active(
      kind: ApiActionKind.fieldopsCrewShift,
      target: orderId,
    )) {
      final t = DateTime.tryParse('${a.body?['event_time']}');
      if (t == null) continue;
      if (events.any((e) => e.eventTime.isAtSameMomentAs(t))) continue;
      events.add(
        ShiftEvent(
          kind: '${a.body?['kind']}',
          eventTime: t,
          headcount: (a.body?['headcount'] as num?)?.toInt(),
          pending: a.status == ApiActionStatus.pending,
        ),
      );
    }
    events.sort((a, b) => a.eventTime.compareTo(b.eventTime));
    return ShiftState(events);
  }

  /// Mở ca (bắt buộc số người 1..200) — xếp hàng, gửi khi có mạng.
  Future<void> openShift(CollectionOrder order, String headcountText) async {
    final crew = _crewOf(order);
    final n = int.tryParse(headcountText.trim());
    if (n == null || n < 1 || n > 200) {
      throw FieldError('Số người của tổ từ 1 đến 200.');
    }
    if ((await shifts(order.orderId)).open) {
      throw FieldError('Ca đang mở — đóng ca trước khi mở ca mới.');
    }
    await _actions.enqueue(
      ApiActionKind.fieldopsCrewShift,
      target: order.orderId,
      body: {
        'crew_ref': crew,
        'kind': 'opened',
        'headcount': n,
        'event_time': formatRfc3339(_clock.now()),
        'device_id': _deviceId,
      },
    );
  }

  Future<void> closeShift(CollectionOrder order) async {
    final crew = _crewOf(order);
    if (!(await shifts(order.orderId)).open) {
      throw FieldError('Chưa có ca đang mở.');
    }
    await _actions.enqueue(
      ApiActionKind.fieldopsCrewShift,
      target: order.orderId,
      body: {
        'crew_ref': crew,
        'kind': 'closed',
        'event_time': formatRfc3339(_clock.now()),
        'device_id': _deviceId,
      },
    );
  }

  String _crewOf(CollectionOrder order) {
    if (!order.countable) {
      throw FieldError(rejectMessage('fieldops.order_not_open'));
    }
    final crew = order.crewRef;
    if (crew == null) {
      throw FieldError('Lệnh gom chưa giao tổ gom — chưa mở/đóng ca được.');
    }
    return crew;
  }

  /// Lệnh mở/đóng ca bị máy chủ từ chối (hiển thị ở hàng đợi).
  Future<List<ApiAction>> failedShiftActions() async => [
    for (final a in await _actions.ofKind(ApiActionKind.fieldopsCrewShift))
      if (a.status == ApiActionStatus.failed) a,
  ];

  // --------------------------------------------------------------- giám sát

  Future<Cached<T>> _online<T>(
    String key,
    Future<Object?> Function() fetch,
    T Function(Object? raw) parse,
  ) async {
    try {
      final raw = await fetch();
      final now = _clock.now();
      await _db.setMeta(
        key,
        jsonEncode({'fetched_at': now.millisecondsSinceEpoch, 'data': raw}),
      );
      return Cached(parse(raw), now);
    } on NetworkException {
      final c = await _db.getMetaJson(key);
      if (c == null) {
        throw FieldError('Cần có mạng để tải dữ liệu này lần đầu.');
      }
      return Cached(
        parse(c['data']),
        DateTime.fromMillisecondsSinceEpoch(c['fetched_at']! as int),
        stale: true,
      );
    } on ApiException catch (e) {
      throw FieldError(rejectMessage(e.code));
    }
  }

  /// Lệnh gom chờ duyệt (`GET /v1/fieldops/orders?status=planned`).
  Future<Cached<List<CollectionOrder>>> plannedOrders() => _online(
    _plannedKey,
    () async => (await _api.getJson(
      '/v1/fieldops/orders',
      query: const {'status': OrderStatus.planned, 'limit': '200'},
    ))['items'],
    (raw) => [
      for (final o in (raw as List?) ?? const [])
        CollectionOrder.fromJson((o as Map).cast<String, Object?>()),
    ],
  );

  /// actor_ref của người đang đăng nhập (`sub`).
  Future<String?> me() => _subject();

  /// Người lập lệnh không được tự duyệt (R-56) — kiểm trên máy để ẩn nút;
  /// máy chủ chặn lần hai (`403 self_approval_forbidden`).
  Future<bool> isOwn(CollectionOrder o) async {
    final sub = await me();
    return sub != null && o.createdBy != null && o.createdBy == sub;
  }

  /// Duyệt / từ chối lệnh gom — trực tuyến (cần phản hồi ngay).
  Future<void> decide(
    CollectionOrder o, {
    required bool approve,
    String? reason,
  }) async {
    if (await isOwn(o)) {
      throw FieldError(rejectMessage('self_approval_forbidden'));
    }
    final r = (reason ?? '').trim();
    if (!approve && r.runes.length < reasonMinLength) {
      throw FieldError('Lý do từ chối ít nhất $reasonMinLength ký tự.');
    }
    final decision = approve ? 'approved' : 'rejected';
    try {
      await _api.postAction(
        path: '/v1/fieldops/orders/${o.orderId}/decision',
        idempotencyKey: 'decide:${o.orderId}:${o.revision}:$decision',
        body: {'decision': decision, if (!approve) 'reason': r},
      );
    } on NetworkException {
      throw FieldError('Cần có mạng để duyệt lệnh gom.');
    } on ApiException catch (e) {
      throw FieldError(rejectMessage(e.code));
    }
  }

  /// Đối soát chiều mua MKG ↔ HTX theo thửa (RB-6) — trực tuyến, lưu bản gần
  /// nhất để xem khi mất sóng.
  Future<Cached<List<ReconRow>>> reconciliation({String? orderId}) => _online(
    _reconKey(orderId),
    () async => (await _api.getJson(
      '/v1/fieldops/reconciliation',
      query: {'order_id': ?orderId, 'limit': '500'},
    ))['items'],
    (raw) => [
      for (final r in (raw as List?) ?? const [])
        ReconRow.fromJson((r as Map).cast<String, Object?>()),
    ],
  );
}

import 'dart:convert';

import 'package:agrigreen_mobile/core/clock.dart';
import 'package:agrigreen_mobile/core/ids.dart';
import 'package:agrigreen_mobile/data/actions/action_queue.dart';
import 'package:agrigreen_mobile/data/db/local_db.dart';
import 'package:agrigreen_mobile/data/photo/location_source.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';

import '../domain/work_schedule.dart';

/// Quyền thao tác máy chủ trả (`ExtWorkScheduleDetail.can`).
class WsCan {
  const WsCan({this.edit = true, this.submit = true});
  final bool edit;
  final bool submit;
}

/// Lịch tuần như màn hình hiển thị: dữ liệu máy chủ (mới tải hoặc bản đệm)
/// phủ bởi bản nháp trên máy (nếu có).
class WeekSchedule {
  const WeekSchedule({
    required this.weekStart,
    required this.submitDueAt,
    required this.locked,
    required this.lateNow,
    required this.items,
    this.reviewDueAt,
    this.previousAvailable = false,
    this.scheduleId,
    this.status,
    this.version,
    this.approvedVersion,
    this.amendment = false,
    this.late = false,
    this.lastReason,
    this.reviewer,
    this.can = const WsCan(),
    this.approvedItems = const [],
    this.serverWarnings = const [],
    this.hasLocalDraft = false,
    this.dirty = false,
    this.pendingAction,
    this.syncError,
    this.offline = false,
    this.fetchedAt,
    this.loadError,
    this.communeCodes = const [],
    this.communeNames = const {},
    this.ownerLevel,
    this.checkinItems = const [],
    this.checkinEnabled = false,
  });

  /// W11.46: phòng ban dùng check-in GPS (CNTS) — hiện nút "Check-in".
  final bool checkinEnabled;

  /// Mục của bản ĐÃ DUYỆT còn hiệu lực (máy chủ đối chiếu check-in với bản này):
  /// lịch đã duyệt → mục máy chủ của bản hiện hành; đang điều chỉnh chờ duyệt lại
  /// → `approved_items`; chưa có bản nào được duyệt → rỗng.
  final List<WsItem> checkinItems;

  /// Mục đã duyệt tương ứng [i] (cùng `id`), null khi mục chỉ có ở bản điều chỉnh
  /// hoặc ở nháp trên máy.
  WsItem? approvedOf(WsItem i) {
    for (final a in checkinItems) {
      if (a.id == i.id) return a;
    }
    return null;
  }

  /// Mục được check-in lúc [now]: phòng bật, lịch ĐÃ DUYỆT (hoặc đang điều chỉnh
  /// nhưng còn bản đã duyệt hiệu lực — log bug TT-07/CN-06, như máy chủ), mục có
  /// trong bản đã duyệt đó, và theo dữ liệu ĐÃ DUYỆT (không theo bản điều chỉnh):
  /// ngoài cơ quan, của hôm nay; chưa check-in.
  bool canCheckin(WsItem i, DateTime now) {
    if (!checkinEnabled || scheduleId == null || i.checkin != null) {
      return false;
    }
    if (status != WsStatus.approved && (approvedVersion ?? 0) <= 0) {
      return false;
    }
    final a = approvedOf(i);
    return a != null && a.fieldWork && ymd(a.day) == ymd(now);
  }

  /// Lý do chưa check-in được mục ngoài cơ quan hôm nay (R-B3), null khi không
  /// cần báo (check-in được, đã check-in, phòng không dùng check-in, mục khác ngày).
  String? checkinBlockedReason(WsItem i, DateTime now) {
    if (!checkinEnabled || i.checkin != null || canCheckin(i, now)) return null;
    final a = approvedOf(i);
    final today =
        ymd(i.day) == ymd(now) || (a != null && ymd(a.day) == ymd(now));
    if (!today || !(i.fieldWork || (a?.fieldWork ?? false))) return null;
    if (scheduleId == null ||
        status == null ||
        status == WsStatus.draft ||
        (status != WsStatus.approved && (approvedVersion ?? 0) <= 0)) {
      return 'Chưa check-in được: lịch chưa được duyệt.';
    }
    if (a == null) {
      return 'Chưa check-in được: mục mới trong phần điều chỉnh, chờ duyệt.';
    }
    return 'Chưa check-in được: theo bản đã duyệt, mục này không phải hôm nay.';
  }

  final DateTime weekStart;
  final DateTime submitDueAt;
  final DateTime? reviewDueAt;

  /// Tuần đã kết thúc (hoặc máy chủ báo khóa) → chỉ xem.
  final bool locked;

  /// Gửi lúc này sẽ mang nhãn "Đăng ký muộn".
  final bool lateNow;
  final bool previousAvailable;

  final String? scheduleId;

  /// Trạng thái trên máy chủ (null = chưa đăng ký).
  final WsStatus? status;
  final int? version;
  final int? approvedVersion;

  /// Đang chờ duyệt phần điều chỉnh của lịch đã duyệt.
  final bool amendment;

  /// Lịch mang nhãn "Đăng ký muộn".
  final bool late;

  /// Lý do "Yêu cầu chỉnh sửa" gần nhất.
  final String? lastReason;
  final String? reviewer;
  final WsCan can;

  /// Mục hiển thị (bản nháp trên máy nếu có, nếu không là của máy chủ),
  /// đã sắp và đánh dấu trùng giờ.
  final List<WsItem> items;
  final List<WsItem> approvedItems;
  final List<String> serverWarnings;

  final bool hasLocalDraft;

  /// Có sửa trên máy chưa lưu/gửi.
  final bool dirty;

  /// Lệnh lưu đang chờ đồng bộ: `draft` | `submit`.
  final String? pendingAction;

  /// Mã lỗi máy chủ trả cho lần lưu gần nhất (409/422…).
  final String? syncError;

  /// Không tải được từ máy chủ (mất mạng) — đang dùng bản đệm.
  final bool offline;
  final DateTime? fetchedAt;

  /// Lỗi tải (403…) khi không có bản đệm.
  final String? loadError;

  /// Xã chọn được khi đăng ký lịch (`mine.commune_codes`, có cả khi tuần chưa
  /// có lịch): cán bộ xã → xã phụ trách; cán bộ tỉnh → mọi xã của tỉnh (quyết
  /// định PO 25/10/2026). Bản đệm cũ chưa có trường này → xã của lịch.
  /// Xã/phường luôn bắt buộc (R-B1, như máy chủ).
  final List<String> communeCodes;

  /// Tên xã theo mã (`mine.communes`); thiếu → hiện mã.
  final Map<String, String> communeNames;

  /// Cấp đăng ký lịch (`mine.owner_level`): `commune` | `province`; null khi
  /// chưa biết (chưa từng tải được từ máy chủ).
  final String? ownerLevel;

  bool get versionConflict =>
      syncError == WorkScheduleService.versionConflictCode;

  bool get pendingSync => pendingAction != null;

  Set<String> get overlapIds => {
    for (final i in items)
      if (i.conflict) i.id,
  };

  bool get editable => !locked && can.edit && loadError == null;

  /// "Lưu nháp" chỉ khi lịch chưa gửi hoặc bị yêu cầu chỉnh sửa.
  bool get canSaveDraft =>
      editable &&
      !versionConflict &&
      (status == null ||
          status == WsStatus.draft ||
          status == WsStatus.changesRequested) &&
      pendingAction != 'submit';

  bool get canSubmit =>
      editable &&
      !versionConflict &&
      items.isNotEmpty &&
      (dirty || hasLocalDraft || can.submit);

  /// Nhãn trạng thái hiển thị.
  String get statusLabel {
    if (pendingAction == 'submit') return 'Chờ đồng bộ để gửi duyệt';
    final s = status;
    if (s == null) return hasLocalDraft ? 'Nháp (trên máy)' : 'Chưa đăng ký';
    if (s == WsStatus.pending && amendment) return 'Điều chỉnh chờ duyệt lại';
    return s.label;
  }
}

/// Lịch công tác tuần trên app hiện trường (KN v7.7 P5, FN-XA-03.08).
///
/// - Đọc `GET /v1/extension/work-schedules/mine?week_start=` khi có mạng và
///   lưu bản đệm (bảng `catalog_cache`, SQLCipher; kind [_srvKind]).
/// - Sửa mục trên máy (bản nháp cục bộ, kind [_draftKind]); kiểm như máy chủ,
///   dò trùng giờ (cảnh báo).
/// - "Lưu nháp" / "Gửi duyệt" xếp lệnh idempotent
///   [ApiActionKind.workScheduleSave] (thân cố định: `id` UUIDv7 máy sinh cho
///   lịch mới, `base_version`), phát lại khi có mạng; sau đồng bộ tải lại.
/// - 409 `extension.schedule_version_conflict`: giữ bản nháp, người dùng chọn
///   tải lại hoặc gửi lại bản của mình trên phiên bản mới.
class WorkScheduleService {
  WorkScheduleService({
    required LocalDb db,
    required SyncApi api,
    required ActionQueue actions,
    required Clock clock,
    required IdGenerator ids,
    this.deviceId,
    this.syncNow,
    this.location,
  }) : _db = db,
       _api = api,
       _actions = actions,
       _clock = clock,
       _ids = ids;

  final LocalDb _db;
  final SyncApi _api;
  final ActionQueue _actions;
  final Clock _clock;
  final IdGenerator _ids;
  final String? deviceId;

  /// Nguồn GPS cho check-in (W11.46). null → không check-in được.
  final LocationSource? location;

  /// Đồng bộ ngay sau khi xếp lệnh (app: `SyncService.syncOnce(force: true)`).
  Future<void> Function()? syncNow;

  static const versionConflictCode = 'extension.schedule_version_conflict';
  static const weekLockedCode = 'extension.week_locked';
  static const _srvKind = 'ext_work_schedule';
  static const _draftKind = 'ext_work_schedule_draft';
  static const _checkinKind = 'ext_work_schedule_checkin';
  static const _base = '/v1/extension/work-schedules';

  DateTime defaultWeek() => nextWeekStart(_clock.now());

  // ------------------------------------------------------------ lưu trữ

  Future<void> _put(String kind, DateTime week, Object json) =>
      _db.db.rawInsert(
        'INSERT OR REPLACE INTO catalog_cache (kind, key, json, fetched_at) '
        'VALUES (?, ?, ?, ?)',
        [
          kind,
          ymd(week),
          jsonEncode(json),
          _clock.now().millisecondsSinceEpoch,
        ],
      );

  Future<(Map<String, Object?>, DateTime)?> _get(
    String kind,
    DateTime week,
  ) async {
    final rows = await _db.db.query(
      'catalog_cache',
      where: 'kind = ? AND key = ?',
      whereArgs: [kind, ymd(week)],
    );
    if (rows.isEmpty) return null;
    final j = jsonDecode(rows.first['json']! as String);
    if (j is! Map) return null;
    return (
      j.cast<String, Object?>(),
      DateTime.fromMillisecondsSinceEpoch(rows.first['fetched_at']! as int),
    );
  }

  Future<void> _delete(String kind, DateTime week) => _db.db.delete(
    'catalog_cache',
    where: 'kind = ? AND key = ?',
    whereArgs: [kind, ymd(week)],
  );

  Future<_Draft?> _draft(DateTime week) async {
    final r = await _get(_draftKind, week);
    return r == null ? null : _Draft.fromJson(r.$1);
  }

  Future<void> _saveDraft(DateTime week, _Draft d) =>
      _put(_draftKind, week, d.toJson());

  // ------------------------------------------------------------ tải

  /// Tải lịch tuần [week] (Thứ Hai). [refresh] = thử máy chủ trước; mất mạng
  /// thì dùng bản đệm.
  Future<WeekSchedule> load(DateTime week, {bool refresh = true}) async {
    week = mondayOf(week);
    var offline = false;
    String? loadError;
    if (refresh) {
      try {
        final j = await _api.getJson(
          '$_base/mine',
          query: {'week_start': ymd(week)},
        );
        await _put(_srvKind, week, j);
      } on NetworkException {
        offline = true;
      } on DeviceRevokedException {
        rethrow;
      } on UnauthenticatedException {
        offline = true;
      } on ApiException catch (e) {
        if (e.retriable) {
          offline = true;
        } else {
          loadError = _loadMessage(e.code);
        }
      }
    }
    final cached = await _get(_srvKind, week);
    final srv = cached?.$1;
    final draft = await _reconcile(week, await _draft(week), srv);
    final v = _view(
      week,
      srv,
      draft,
      scope: srv?['owner_level'] is String ? srv : await _lastScope(),
      offline: offline,
      fetchedAt: cached?.$2,
      loadError: srv == null ? loadError : null,
    );
    return _withCheckins(v, srv, await _get(_checkinKind, week));
  }

  // ------------------------------------------------------------ check-in (W11.46)

  /// Gắn check-in máy chủ trả và check-in đang chờ đồng bộ (bấm khi mất mạng) vào mục.
  WeekSchedule _withCheckins(
    WeekSchedule v,
    Map<String, Object?>? srv,
    (Map<String, Object?>, DateTime)? pending,
  ) {
    final detail = _map(srv?['schedule']);
    final enabled =
        srv?['checkin_enabled'] == true || detail?['checkin_enabled'] == true;
    final byItem = <String, WsCheckin>{};
    if (pending != null) {
      for (final e in pending.$1.entries) {
        final at = e.value is String
            ? DateTime.tryParse(e.value! as String)
            : null;
        if (at != null) {
          byItem[e.key] = WsCheckin(capturedAt: at, pending: true);
        }
      }
    }
    if (detail?['checkins'] is List) {
      for (final c in detail!['checkins']! as List) {
        if (c is! Map) continue;
        final m = c.cast<String, Object?>();
        final ck = WsCheckin.fromJson(m);
        if (ck != null && m['item_id'] is String) {
          byItem[m['item_id']! as String] = ck;
        }
      }
    }
    if (!enabled && byItem.isEmpty) return v;
    return WeekSchedule(
      weekStart: v.weekStart,
      submitDueAt: v.submitDueAt,
      reviewDueAt: v.reviewDueAt,
      locked: v.locked,
      lateNow: v.lateNow,
      previousAvailable: v.previousAvailable,
      scheduleId: v.scheduleId,
      status: v.status,
      version: v.version,
      approvedVersion: v.approvedVersion,
      amendment: v.amendment,
      late: v.late,
      lastReason: v.lastReason,
      reviewer: v.reviewer,
      can: v.can,
      items: [
        for (final i in v.items)
          byItem[i.id] == null ? i : i.copyWith(checkin: byItem[i.id]),
      ],
      approvedItems: v.approvedItems,
      serverWarnings: v.serverWarnings,
      hasLocalDraft: v.hasLocalDraft,
      dirty: v.dirty,
      pendingAction: v.pendingAction,
      syncError: v.syncError,
      offline: v.offline,
      fetchedAt: v.fetchedAt,
      loadError: v.loadError,
      communeCodes: v.communeCodes,
      communeNames: v.communeNames,
      ownerLevel: v.ownerLevel,
      checkinItems: v.checkinItems,
      checkinEnabled: enabled,
    );
  }

  /// Bấm "Check-in" tại địa điểm công tác (CV-07): lấy GPS (độ chính xác, thời điểm thiết bị), xếp lệnh idempotent
  /// [CntsActionKind.workScheduleCheckin] (phát lại khi có mạng, máy chủ nhận bản gửi muộn tối đa 7 ngày). Tọa độ chỉ
  /// nằm trong thân lệnh chờ gửi — không lưu riêng trên máy.
  Future<WeekSchedule> checkin(DateTime week, WsItem item) async {
    week = mondayOf(week);
    final v = await load(week, refresh: false);
    final now = _clock.now();
    if (!v.canCheckin(item, now)) {
      throw WorkScheduleError(
        'checkin',
        v.checkinBlockedReason(item, now) ?? 'Chỉ check-in mục công tác ngoài cơ quan của hôm nay, theo lịch đã duyệt.',
      );
    }
    final src = location;
    if (src == null) throw WorkScheduleError('gps', 'Thiết bị không có GPS.');
    final r = await src.current();
    final fix = r.fix;
    if (fix == null) {
      throw WorkScheduleError(
        'gps',
        r.status == GpsStatus.denied
            ? 'Cần cho phép truy cập vị trí để check-in.'
            : 'Không lấy được vị trí GPS — bật định vị rồi thử lại.',
      );
    }
    await _actions.enqueue(
      CntsActionKind.workScheduleCheckin,
      target: '${v.scheduleId}/items/${item.id}',
      body: {
        'lat': fix.lat,
        'lon': fix.lon,
        'accuracy_m': fix.accuracyM,
        'captured_at': now.toUtc().toIso8601String(),
        'mock_location': false,
        'device_id': deviceId,
      },
    );
    final pending = (await _get(_checkinKind, week))?.$1 ?? <String, Object?>{};
    await _put(_checkinKind, week, {
      ...pending,
      item.id: now.toUtc().toIso8601String(),
    });
    final sync = syncNow;
    if (sync != null) {
      try {
        await sync();
      } catch (_) {
        // lệnh vẫn chờ
      }
    }
    return load(week, refresh: sync != null);
  }

  static String _loadMessage(String code) => switch (code) {
    'forbidden' || 'http_403' || 'extension.out_of_scope' => 'Tài khoản chưa được gán phòng ban hoặc xã phụ trách để đăng ký lịch công tác.',
    _ => 'Không tải được lịch công tác ($code).',
  };

  static Map<String, Object?>? _map(Object? v) =>
      v is Map ? v.cast<String, Object?>() : null;

  static List<WsItem> _items(Object? v) => [
    if (v is List)
      for (final e in v)
        if (e is Map) ?WsItem.fromJson(e.cast<String, Object?>()),
  ];

  static List<Object?>? _list(Object? v) => v is List ? v : null;

  static int? _int(Object? v) => v is num ? v.toInt() : null;

  static DateTime? _time(Object? v) =>
      v is String ? DateTime.tryParse(v) : null;

  /// Đối chiếu bản nháp với lệnh đã xếp và dữ liệu máy chủ.
  Future<_Draft?> _reconcile(
    DateTime week,
    _Draft? d,
    Map<String, Object?>? srv,
  ) async {
    if (d == null) return null;
    final actionId = d.actionId;
    if (actionId != null) {
      final a = await _actions.byId(actionId);
      if (a == null || a.status == ApiActionStatus.done) {
        d
          ..actionId = null
          ..action = null
          ..ownDone = true
          ..error = null;
      } else if (a.status == ApiActionStatus.failed) {
        d
          ..actionId = null
          ..action = null
          ..dirty = true
          ..error = a.lastError ?? 'failed';
      }
    }
    final sched = _map(_map(srv?['schedule'])?['schedule']);
    final srvVersion = _int(sched?['version']);
    if (d.ownDone && srvVersion != null && srvVersion > (d.baseVersion ?? 0)) {
      if (!d.dirty) {
        // Máy chủ đã có bản của mình: bỏ bản nháp trên máy.
        await _delete(_draftKind, week);
        return null;
      }
      d
        ..baseVersion = srvVersion
        ..scheduleId = sched?['id'] as String? ?? d.scheduleId
        ..ownDone = false;
    }
    if (!d.dirty && d.actionId == null && d.error == null && !d.ownDone) {
      await _delete(_draftKind, week);
      return null;
    }
    await _saveDraft(week, d);
    return d;
  }

  /// Phạm vi (cấp, xã phụ trách) từ bản đệm `mine` gần nhất có trường này —
  /// dùng khi tuần đang mở chưa tải được (ngoại tuyến, tuần mới).
  Future<Map<String, Object?>?> _lastScope() async {
    final rows = await _db.db.query(
      'catalog_cache',
      columns: ['json'],
      where: 'kind = ?',
      whereArgs: [_srvKind],
      orderBy: 'fetched_at DESC',
      limit: 20,
    );
    for (final r in rows) {
      final j = jsonDecode(r['json']! as String);
      if (j is Map && j['owner_level'] is String) {
        return j.cast<String, Object?>();
      }
    }
    return null;
  }

  WeekSchedule _view(
    DateTime week,
    Map<String, Object?>? srv,
    _Draft? d, {
    Map<String, Object?>? scope,
    bool offline = false,
    DateTime? fetchedAt,
    String? loadError,
  }) {
    final now = _clock.now();
    final detail = _map(srv?['schedule']);
    final sched = _map(detail?['schedule']);
    final can = _map(detail?['can']);
    final submitDue =
        _time(srv?['submit_due_at']) ??
        _time(sched?['submit_due_at']) ??
        defaultSubmitDue(week);
    final srvItems = _items(detail?['items']);
    final shown = d?.items ?? srvItems;
    final local = overlappingIds(shown);
    final items = sortItems([
      for (final i in shown) i.copyWith(conflict: local.contains(i.id)),
    ]);
    final locked =
        srv?['locked'] == true ||
        sched?['locked'] == true ||
        weekEnded(week, now) ||
        d?.error == weekLockedCode;
    return WeekSchedule(
      weekStart: week,
      submitDueAt: submitDue,
      reviewDueAt: _time(srv?['review_due_at']),
      locked: locked,
      lateNow: srv?['late_now'] is bool
          ? srv!['late_now']! as bool
          : now.isAfter(submitDue),
      previousAvailable: srv?['previous_available'] == true,
      scheduleId: sched?['id'] as String? ?? d?.scheduleId,
      status: WsStatus.tryParse(sched?['status']),
      version: _int(sched?['version']),
      approvedVersion: _int(sched?['approved_version']),
      amendment: sched?['amendment'] == true,
      late: sched?['late'] == true,
      lastReason: sched?['last_reason'] as String?,
      reviewer: sched?['reviewer_display'] as String?,
      can: can == null
          ? const WsCan()
          : WsCan(edit: can['edit'] != false, submit: can['submit'] != false),
      items: items,
      approvedItems: sortItems(_items(detail?['approved_items'])),
      serverWarnings: [
        if (detail?['warnings'] is List)
          for (final w in detail!['warnings']! as List)
            if (w is Map && w['message'] is String) w['message']! as String,
      ],
      hasLocalDraft: d != null,
      dirty: d?.dirty ?? false,
      pendingAction: d?.action,
      syncError: d?.error,
      offline: offline,
      fetchedAt: fetchedAt,
      loadError: loadError,
      communeCodes: [
        for (final c
            in _list(scope?['commune_codes']) ??
                _list(sched?['commune_codes']) ??
                const [])
          if (c is String) c,
      ],
      communeNames: {
        for (final c in _list(scope?['communes']) ?? const [])
          if (c is Map && c['code'] is String && c['name'] is String)
            c['code']! as String: c['name']! as String,
      },
      ownerLevel:
          scope?['owner_level'] as String? ?? sched?['owner_level'] as String?,
      checkinItems: switch (WsStatus.tryParse(sched?['status'])) {
        WsStatus.approved => srvItems,
        WsStatus.draft || null => const <WsItem>[],
        _ => _items(detail?['approved_items']),
      },
    );
  }

  // ------------------------------------------------------------ sửa

  Future<(WeekSchedule, _Draft)> _editable(DateTime week) async {
    week = mondayOf(week);
    final v = await load(week, refresh: false);
    if (v.locked) {
      throw WorkScheduleError('week', 'Tuần đã kết thúc — lịch chỉ xem.');
    }
    if (!v.editable) {
      throw WorkScheduleError('week', 'Không được sửa lịch này.');
    }
    final d =
        await _draft(week) ??
        _Draft(
          scheduleId: v.scheduleId,
          baseVersion: v.version,
          items: [for (final i in v.items) i.copyWith(conflict: false)],
        );
    return (v, d);
  }

  /// Mã mục mới (UUIDv7).
  String newItemId() => _ids.newId();

  /// Thêm hoặc sửa (theo `id`) một mục. Ném [WorkScheduleError] khi sai.
  Future<WeekSchedule> upsertItem(DateTime week, WsItem item) async {
    week = mondayOf(week);
    validateItem(item, week);
    final (_, d) = await _editable(week);
    final idx = d.items.indexWhere((i) => i.id == item.id);
    if (idx < 0 && d.items.length >= maxScheduleItems) {
      throw WorkScheduleError(
        'items',
        'Lịch tuần tối đa $maxScheduleItems mục công tác.',
      );
    }
    final clean = item.copyWith(conflict: false);
    if (idx < 0) {
      d.items.add(clean);
    } else {
      d.items[idx] = clean;
    }
    _touch(d);
    await _saveDraft(week, d);
    return load(week, refresh: false);
  }

  Future<WeekSchedule> removeItem(DateTime week, String itemId) async {
    week = mondayOf(week);
    final (_, d) = await _editable(week);
    d.items.removeWhere((i) => i.id == itemId);
    _touch(d);
    await _saveDraft(week, d);
    return load(week, refresh: false);
  }

  void _touch(_Draft d) {
    d.dirty = true;
    if (d.error != versionConflictCode) d.error = null;
  }

  /// "Sao chép tuần trước": có mạng và tuần chưa có lịch → máy chủ tạo nháp
  /// (`POST …/copy-previous`); còn lại sao chép trên máy từ bản đệm tuần trước
  /// (ngày dời +7). Tuần đã có mục → chỉ thay khi [replace].
  Future<WeekSchedule> copyPrevious(
    DateTime week, {
    bool replace = false,
  }) async {
    week = mondayOf(week);
    final (v, d) = await _editable(week);
    if (v.items.isNotEmpty && !replace) {
      throw WorkScheduleError(
        'items',
        'Tuần này đã có mục công tác. Xác nhận thay bằng lịch tuần trước?',
      );
    }
    if (v.scheduleId == null && !v.hasLocalDraft) {
      try {
        final detail = await _api.postAction(
          path: '$_base/copy-previous',
          idempotencyKey: 'wscopy:${ymd(week)}:${_ids.newId()}',
          body: {'week_start': ymd(week)},
        );
        if (detail['schedule'] is Map) {
          final cached = (await _get(_srvKind, week))?.$1 ?? {};
          await _put(_srvKind, week, {...cached, 'schedule': detail});
          return await load(week, refresh: false);
        }
      } on NetworkException {
        // ngoại tuyến → sao chép trên máy
      } on DeviceRevokedException {
        rethrow;
      } on ApiException catch (e) {
        if (e.status == 404) {
          throw WorkScheduleError(
            'previous',
            'Tuần trước chưa có lịch để sao chép.',
          );
        }
        if (e.code == weekLockedCode) {
          throw WorkScheduleError('week', 'Tuần đã kết thúc — lịch chỉ xem.');
        }
        // 409 khác (đã có lịch), lỗi tạm → sao chép trên máy.
      }
    }
    final prevWeek = week.subtract(const Duration(days: 7));
    final prev = await load(prevWeek);
    if (prev.items.isEmpty) {
      throw WorkScheduleError(
        'previous',
        prev.offline || prev.fetchedAt == null
            ? 'Chưa có lịch tuần trước trên máy. Cần có mạng để tải.'
            : 'Tuần trước chưa có lịch để sao chép.',
      );
    }
    d.items
      ..clear()
      ..addAll([
        for (final i in prev.items)
          WsItem(
            id: _ids.newId(),
            day: i.day.add(const Duration(days: 7)),
            session: i.session,
            startTime: i.startTime,
            endTime: i.endTime,
            content: i.content,
            communeCode: i.communeCode,
            cooperativeRef: i.cooperativeRef,
            location: i.location,
            participants: i.participants,
            note: i.note,
          ),
      ]);
    _touch(d);
    await _saveDraft(week, d);
    return load(week, refresh: false);
  }

  /// "Lưu nháp" ([submit] = false) hoặc "Gửi duyệt": kiểm toàn bộ, xếp lệnh
  /// idempotent, đồng bộ ngay nếu có mạng rồi tải lại.
  Future<WeekSchedule> save(DateTime week, {required bool submit}) async {
    week = mondayOf(week);
    final (v, d) = await _editable(week);
    if (v.versionConflict) {
      throw WorkScheduleError(
        'version',
        'Lịch đã thay đổi trên máy chủ — tải lại trước khi lưu.',
      );
    }
    if (!submit && !v.canSaveDraft) {
      throw WorkScheduleError(
        'action',
        'Lịch đã gửi duyệt — dùng "Gửi duyệt" để gửi phần điều chỉnh.',
      );
    }
    if (submit && d.items.isEmpty) {
      throw WorkScheduleError('items', 'Lịch chưa có mục công tác nào.');
    }
    if (d.items.length > maxScheduleItems) {
      throw WorkScheduleError(
        'items',
        'Lịch tuần tối đa $maxScheduleItems mục công tác.',
      );
    }
    for (final i in d.items) {
      validateItem(i, week);
    }
    // Lệnh trước chưa gửi → thay bằng lệnh mới (một POST cho trạng thái cuối).
    final prevId = d.actionId;
    if (prevId != null && !await _actions.cancelPending(prevId)) {
      final a = await _actions.byId(prevId);
      if (a == null || a.status == ApiActionStatus.done) {
        // Lệnh trước đã ghi phiên bản mới trên máy chủ (mỗi lần lưu +1).
        d.baseVersion = (d.baseVersion ?? 0) + 1;
      }
    }
    d.scheduleId ??= _ids.newId();
    final body = <String, Object?>{
      'id': d.scheduleId,
      'week_start': ymd(week),
      'action': submit ? 'submit' : 'draft',
      'base_version': d.baseVersion,
      'items': [for (final i in sortItems(d.items)) i.toInput()],
      'device_id': deviceId,
    };
    final a = await _actions.enqueue(
      ApiActionKind.workScheduleSave,
      target: ymd(week),
      body: body,
    );
    d
      ..actionId = a.id
      ..action = submit ? 'submit' : 'draft'
      ..dirty = false
      ..ownDone = false
      ..error = null;
    await _saveDraft(week, d);
    final sync = syncNow;
    if (sync == null) return load(week, refresh: false);
    try {
      await sync();
    } catch (_) {
      // lỗi đồng bộ đã ghi ở trạng thái đồng bộ; lệnh vẫn chờ
    }
    return load(week);
  }

  /// Bỏ bản nháp trên máy và tải lại từ máy chủ ("Tải lại").
  Future<WeekSchedule> discardLocal(DateTime week) async {
    week = mondayOf(week);
    final d = await _draft(week);
    if (d?.actionId != null) await _actions.cancelPending(d!.actionId!);
    await _delete(_draftKind, week);
    return load(week);
  }

  /// Giữ bản trên máy, đặt lại trên phiên bản hiện hành của máy chủ để gửi lại
  /// (sau 409 version conflict). Cần có mạng để biết phiên bản mới.
  Future<WeekSchedule> keepMine(DateTime week) async {
    week = mondayOf(week);
    final v = await load(week);
    if (v.offline) {
      throw WorkScheduleError(
        'network',
        'Cần có mạng để tải phiên bản mới của lịch.',
      );
    }
    final d = await _draft(week);
    if (d == null) return v;
    d
      ..baseVersion = v.version
      ..scheduleId = v.scheduleId ?? d.scheduleId
      ..error = null
      ..dirty = true;
    await _saveDraft(week, d);
    return load(week, refresh: false);
  }

  /// Khung Log (`GET …/{id}/log`) — chỉ khi có mạng.
  Future<List<WsLogEntry>> log(String scheduleId) async {
    try {
      final j = await _api.getJson(
        '$_base/$scheduleId/log',
        query: {'limit': '50'},
      );
      return [
        if (j['items'] is List)
          for (final e in j['items']! as List)
            if (e is Map) WsLogEntry.fromJson(e.cast<String, Object?>()),
      ];
    } on NetworkException {
      throw WorkScheduleError('network', 'Cần có mạng để xem lịch sử.');
    } on DeviceRevokedException {
      rethrow;
    } on ApiException catch (e) {
      throw WorkScheduleError('log', 'Không tải được lịch sử (${e.code}).');
    }
  }

  /// Sau mỗi lượt đồng bộ: tải lại các tuần có lệnh lưu vừa gửi/thất bại để
  /// bỏ bản nháp đã lên máy chủ hoặc hiện lỗi. Lỗi tải bỏ qua (giữ bản đệm).
  Future<void> refreshAfterSync() async {
    final rows = await _db.db.query(
      'catalog_cache',
      columns: ['key'],
      where: 'kind = ?',
      whereArgs: [_draftKind],
    );
    for (final r in rows) {
      final week = parseYmd(r['key'] as String?);
      if (week == null) continue;
      final d = await _draft(week);
      if (d == null || (d.actionId == null && !d.ownDone)) continue;
      if (d.actionId != null) {
        final a = await _actions.byId(d.actionId!);
        if (a != null && a.status == ApiActionStatus.pending) continue;
      }
      try {
        await load(week);
      } on DeviceRevokedException {
        rethrow;
      } catch (_) {
        // giữ bản đệm
      }
    }
  }
}

/// Bản nháp lịch tuần trên máy (JSON trong `catalog_cache`).
class _Draft {
  _Draft({
    this.scheduleId,
    this.baseVersion,
    List<WsItem>? items,
    this.dirty = false,
    this.actionId,
    this.action,
    this.error,
    this.ownDone = false,
  }) : items = items ?? [];

  String? scheduleId;
  int? baseVersion;
  final List<WsItem> items;
  bool dirty;

  /// Lệnh lưu đã xếp (chưa đối chiếu kết quả).
  String? actionId;
  String? action;
  String? error;

  /// Lệnh của mình đã gửi xong, chờ tải phiên bản mới từ máy chủ.
  bool ownDone;

  Map<String, Object?> toJson() => {
    'schedule_id': scheduleId,
    'base_version': baseVersion,
    'items': [for (final i in items) i.toJson()],
    'dirty': dirty,
    'action_id': actionId,
    'action': action,
    'error': error,
    'own_done': ownDone,
  };

  static _Draft fromJson(Map<String, Object?> j) => _Draft(
    scheduleId: j['schedule_id'] as String?,
    baseVersion: (j['base_version'] as num?)?.toInt(),
    items: WorkScheduleService._items(j['items']),
    dirty: j['dirty'] == true,
    actionId: j['action_id'] as String?,
    action: j['action'] as String?,
    error: j['error'] as String?,
    ownDone: j['own_done'] == true,
  );
}

/// Lệnh API riêng của App hiện trường KN (W11.46): check-in lịch công tác
/// `POST /v1/extension/work-schedules/{id}/items/{item_id}/checkin` — đăng ký qua
/// `ActionQueue(extraKinds: CntsActionKind.values)`.
enum CntsActionKind implements ActionKind {
  workScheduleCheckin('work_schedule_checkin');

  const CntsActionKind(this.code);
  @override
  final String code;

  @override
  String get method => 'POST';

  @override
  String pathFor(String? target) => switch (this) {
    CntsActionKind.workScheduleCheckin =>
      '/v1/extension/work-schedules/$target/checkin',
  };
}

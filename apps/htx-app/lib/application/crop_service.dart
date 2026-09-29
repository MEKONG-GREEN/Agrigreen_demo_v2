import 'package:agrigreen_mobile/core/clock.dart';
import 'package:agrigreen_mobile/core/ids.dart';
import 'package:agrigreen_mobile/data/catalog/catalog_cache.dart';
import 'package:agrigreen_mobile/data/outbox/outbox_repository.dart';
import 'package:agrigreen_mobile/data/photo/location_source.dart';
import 'package:agrigreen_mobile/data/reference/reference_repository.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';

import '../domain/cropcycle.dart';
import 'profile_service.dart';

class CropError implements Exception {
  CropError(this.message);
  final String message;
  @override
  String toString() => message;
}

/// Sản lượng bất thường chưa xác nhận (HTX US-YIELD-03): UI hỏi lại rồi gọi
/// lại với `confirmAnomaly: true`.
class YieldNeedsConfirm implements Exception {
  YieldNeedsConfirm(this.message, this.kind);
  final String message;
  final YieldAnomaly kind;
  @override
  String toString() => message;
}

/// Thửa hiển thị trên máy (entity `plot`, hoặc suy từ `season` khi máy chủ
/// chưa gửi thửa cho vai farmer).
class PlotItem {
  const PlotItem({
    required this.ref,
    required this.code,
    this.name,
    this.areaHa,
    this.householdRef,
    this.cooperativeRef,
    this.communeCode,
  });
  final String ref;
  final String code;
  final String? name;
  final String? areaHa;
  final String? householdRef;
  final String? cooperativeRef;
  final String? communeCode;

  String get title =>
      name == null || name!.isEmpty ? 'Lô $code' : '$code · $name';
}

/// Danh mục chọn (vụ, giống, máy).
class Choice {
  const Choice(this.code, this.name);
  final String code;
  final String name;
}

/// Nhật ký hiển thị: bản từ máy chủ (kèm trạng thái duyệt) hoặc bản trên máy.
class LogEntry {
  const LogEntry({required this.view, this.local});
  final FarmLogView view;

  /// Bản ghi hàng đợi (chưa có trên máy chủ, hoặc máy chủ chưa gửi xuống).
  final OutboxRecord? local;

  String get statusLabel => local != null && local!.status != OutboxStatus.sent
      ? local!.status.label
      : reviewStatusLabel(view.status);
}

/// Mùa vụ + nhật ký + sản lượng (pf/cropcycle), ghi qua hàng đợi đồng bộ nên
/// **mọi thao tác ghi đều làm được khi ngoại tuyến** (HTX US-LOG-04,
/// US-YIELD-01 AC-3). Quy tắc kiểm trên máy trùng máy chủ; máy chủ vẫn quyết.
class CropService {
  CropService({
    required OutboxRepository outbox,
    required ReferenceRepository refs,
    required CatalogCache catalogs,
    required ProfileService profile,
    required LocationSource location,
    required Clock clock,
    required IdGenerator ids,
  }) : _outbox = outbox,
       _refs = refs,
       _catalogs = catalogs,
       _profile = profile,
       _location = location,
       _clock = clock,
       _ids = ids;

  final OutboxRepository _outbox;
  final ReferenceRepository _refs;
  final CatalogCache _catalogs;
  final ProfileService _profile;
  final LocationSource _location;
  final Clock _clock;
  final IdGenerator _ids;

  DateTime today() => vnToday(_clock.now());

  // ---- danh mục ----

  Future<List<Choice>> _catalog(String c) async => [
    for (final e in await _catalogs.entries(c)) Choice(e.code, e.name),
  ];

  Future<List<Choice>> seasonsCatalog() => _catalog('season');
  Future<List<Choice>> varieties() => _catalog('rice_variety');
  Future<List<Choice>> machines() => _catalog('machine_type');

  Future<String> nameOf(String catalog, String code) async {
    for (final e in await _catalogs.entries(catalog)) {
      if (e.code == code) return e.name;
    }
    return code;
  }

  /// Ngưỡng bất thường t/ha của giống (thuộc tính `yield_anomaly_max_t_per_ha`
  /// của danh mục giống; không có → 12 như máy chủ). HTX US-YIELD-03 AC-2.
  Future<String> anomalyThreshold(String varietyCode) async {
    for (final e in await _catalogs.entries('rice_variety')) {
      if (e.code == varietyCode) {
        final v = e.attrs['yield_anomaly_max_t_per_ha'];
        if (v != null && parseScaled(v, 4) != null) return v;
      }
    }
    return defaultYieldAnomalyMaxTPerHa;
  }

  // ---- thửa, vụ, SOP ----

  Future<List<Season>> _pulledSeasons() async => [
    for (final r in await _refs.list(RefEntity.season)) Season.fromJson(r.data),
  ];

  /// Vụ mở trên máy mà máy chủ chưa gửi xuống (chờ đồng bộ / vừa gửi).
  Future<List<Season>> _localSeasons(Set<String> known) async {
    final out = <Season>[];
    final plotsByRef = {for (final p in await plots()) p.ref: p};
    for (final r in await _outbox.list(entity: PushEntity.seasonOpen)) {
      if (r.op != RecordOp.create) continue;
      if (r.status == OutboxStatus.rejected ||
          r.status == OutboxStatus.superseded) {
        continue;
      }
      final p = r.payload;
      final ref = '${p['season_ref'] ?? r.id}';
      if (known.contains(ref)) continue;
      final plot = plotsByRef[p['plot_ref']];
      out.add(
        Season.fromJson({
          ...p,
          'season_ref': ref,
          'status': 'open',
          'plot_code': plot?.code ?? '',
          'area_ha': plot?.areaHa,
          'household_ref': plot?.householdRef,
          'cooperative_ref': plot?.cooperativeRef,
        }, localPending: r.status == OutboxStatus.pending),
      );
    }
    return out;
  }

  Future<List<Season>> seasons() async {
    final pulled = await _pulledSeasons();
    final local = await _localSeasons({for (final s in pulled) s.seasonRef});
    final all = [...local, ...pulled]
      ..sort(
        (a, b) => (b.sowingDate ?? DateTime.utc(0)).compareTo(
          a.sowingDate ?? DateTime.utc(0),
        ),
      );
    return all;
  }

  Future<Season?> seasonByRef(String ref) async {
    for (final s in await seasons()) {
      if (s.seasonRef == ref) return s;
    }
    return null;
  }

  Future<Season?> openSeasonFor(String plotRef) async {
    for (final s in await seasons()) {
      if (s.plotRef == plotRef && s.isOpen) return s;
    }
    return null;
  }

  /// Thửa của hộ mình (nông dân) hoặc của HTX (quản lý). Nguồn: entity `plot`;
  /// thửa chỉ biết qua `season` được bổ sung (mã, diện tích từ vụ).
  Future<List<PlotItem>> plots() async {
    final households = {for (final h in await _profile.households()) h.ref};
    final byRef = <String, PlotItem>{};
    for (final r in await _refs.list(RefEntity.plot)) {
      final d = r.data;
      final hh = d['household_ref'] as String?;
      if (households.isNotEmpty && hh != null && !households.contains(hh)) {
        continue;
      }
      byRef[r.id] = PlotItem(
        ref: r.id,
        code: '${d['code'] ?? r.id.substring(0, 8)}',
        name: d['name'] as String?,
        areaHa: d['area_ha'] as String?,
        householdRef: hh,
        cooperativeRef: d['cooperative_ref'] as String?,
        communeCode: d['commune_code'] as String?,
      );
    }
    for (final s in await _pulledSeasons()) {
      byRef.putIfAbsent(
        s.plotRef,
        () => PlotItem(
          ref: s.plotRef,
          code: s.plotCode.isEmpty ? s.plotRef.substring(0, 8) : s.plotCode,
          areaHa: s.areaHa,
          householdRef: s.householdRef,
          cooperativeRef: s.cooperativeRef,
          communeCode: s.communeCode,
        ),
      );
    }
    final out = byRef.values.toList()..sort((a, b) => a.code.compareTo(b.code));
    return out;
  }

  Future<PlotItem?> plot(String ref) async {
    for (final p in await plots()) {
      if (p.ref == ref) return p;
    }
    return null;
  }

  Future<SopVersion?> sopById(String? id) async {
    if (id == null) return null;
    final r = await _refs.get(RefEntity.sopVersion, id);
    return r == null ? null : SopVersion.fromJson(r.data);
  }

  /// SOP tự gán theo giống (HTX US-SEASON-01 AC-1): mẫu gắn với giống →
  /// phiên bản phát hành mới nhất của mẫu đó. null → máy chủ tự gán khi nhận.
  Future<SopVersion?> sopForVariety(String varietyCode) async {
    final tmpl = await _profile.templateForVariety(varietyCode);
    if (tmpl == null) return null;
    SopVersion? best;
    for (final r in await _refs.list(RefEntity.sopVersion)) {
      final v = SopVersion.fromJson(r.data);
      if (v.templateRef != tmpl || v.templateStatus == 'retired') continue;
      if (best == null || v.versionNo > best.versionNo) best = v;
    }
    return best;
  }

  /// SOP đã ghim của vụ (máy chủ gửi `sop_version_id`; vụ mở trên máy dùng
  /// phiên bản đã chọn lúc mở).
  Future<SopVersion?> sopFor(Season s) async =>
      await sopById(s.sopVersionId) ?? await sopForVariety(s.varietyCode);

  // ---- mở vụ (HTX US-SEASON-01, mở hộ US-SEASON-02) ----

  Future<OutboxRecord> openSeason({
    required PlotItem plot,
    required String seasonCode,
    required int seasonYear,
    required String varietyCode,
    required DateTime sowingDate,
  }) async {
    final err = checkSowingDate(sowingDate, today());
    if (err != null) throw CropError(err);
    if (seasonCode.isEmpty || varietyCode.isEmpty) {
      throw CropError('Chọn vụ và giống lúa.');
    }
    if (await openSeasonFor(plot.ref) != null) {
      throw CropError('Lô ${plot.code} đang canh tác vụ khác.');
    }
    final sop = await sopForVariety(varietyCode);
    final loc = await _location.current();
    return _outbox.enqueue(
      entity: PushEntity.seasonOpen,
      payload: {
        // season_ref máy sinh: nhật ký ghi ngay sau khi mở (ngoại tuyến)
        // tham chiếu được vụ trước khi máy chủ nhận.
        'season_ref': _ids.newId(),
        'plot_ref': plot.ref,
        'season_code': seasonCode,
        'season_year': seasonYear,
        'variety_code': varietyCode,
        'sowing_date': formatDay(sowingDate),
        'sop_version_id': ?sop?.id,
        'gps': gpsPayload(loc),
      },
    );
  }

  /// Mở vụ cho nhiều thửa trên máy (quản lý HTX, khi ngoại tuyến): mỗi thửa
  /// một bản ghi `season_open`; thửa đang có vụ bị bỏ qua kèm lý do.
  Future<(int, Map<String, String>)> openSeasonsOffline({
    required List<PlotItem> plots,
    required String seasonCode,
    required int seasonYear,
    required String varietyCode,
    required DateTime sowingDate,
  }) async {
    final skipped = <String, String>{};
    var opened = 0;
    for (final p in plots) {
      try {
        await openSeason(
          plot: p,
          seasonCode: seasonCode,
          seasonYear: seasonYear,
          varietyCode: varietyCode,
          sowingDate: sowingDate,
        );
        opened++;
      } on CropError catch (e) {
        if (e.message == msgSowingWindow) rethrow;
        skipped[p.code] = e.message;
      }
    }
    return (opened, skipped);
  }

  // ---- nhật ký (HTX US-LOG-01/02/04) ----

  Future<OutboxRecord> recordLog({
    required Season? season,
    required FarmActivity activity,
    int? sopStepSeq,
    List<String> machineCodes = const [],
    List<Map<String, Object?>> inputs = const [],
    String note = '',
    List<String> fileRefs = const [],
  }) async {
    if (season == null || !season.isOpen) throw CropError(msgSeasonNotOpen);
    if (note.length > 2000) throw CropError('Ghi chú tối đa 2000 ký tự.');
    // GPS + thời gian tự đính kèm; thiếu GPS không chặn (HTX US-GPS-01 AC-4).
    final loc = await _location.current();
    return _outbox.enqueue(
      entity: PushEntity.farmLog,
      payload: {
        'season_ref': season.seasonRef,
        'plot_ref': season.plotRef,
        'activity_code': activity.code,
        'sop_step_seq': sopStepSeq,
        'machine_codes': machineCodes,
        'inputs': inputs,
        'note': note.trim(),
        'file_refs': fileRefs,
        'gps': gpsPayload(loc),
      },
    );
  }

  /// Nội dung sửa được của nhật ký (không có GPS/thời gian — khóa, AC-3).
  static Map<String, Object?> _content({
    required String activityCode,
    int? sopStepSeq,
    required List<String> machineCodes,
    required List<Map<String, Object?>> inputs,
    required String note,
    required List<String> fileRefs,
  }) => {
    'activity_code': activityCode,
    'sop_step_seq': sopStepSeq,
    'machine_codes': machineCodes,
    'inputs': inputs,
    'note': note.trim(),
    'file_refs': fileRefs,
  };

  /// Bổ sung / sửa nhật ký bằng bản ghi bù (`op: correct`). Nhật ký máy chủ
  /// đã có (vd bị yêu cầu bổ sung — HTX US-LOG-05 AC-3): `supersedes_id` = id
  /// phiên bản; `event_time` và GPS giữ theo bản gốc. [changedGpsOrTime] = UI
  /// phát hiện người dùng cố đổi GPS/thời gian → chặn (AC-3).
  Future<OutboxRecord> supplementLog({
    required FarmLogView log,
    required String reason,
    FarmActivity? activity,
    List<String>? machineCodes,
    List<Map<String, Object?>>? inputs,
    String? note,
    List<String> addFileRefs = const [],
    bool changedGpsOrTime = false,
  }) async {
    if (changedGpsOrTime) throw CropError(msgLockedGpsTime);
    final payload = _content(
      activityCode: activity?.code ?? log.activityCode,
      sopStepSeq: log.sopStepSeq,
      machineCodes: machineCodes ?? log.machineCodes,
      inputs: inputs ?? log.inputs,
      note: note ?? log.note,
      fileRefs: [...log.fileRefs, ...addFileRefs],
    );
    try {
      final local = await _outbox.byId(log.id);
      if (local != null && local.status == OutboxStatus.rejected) {
        // Máy chủ không lưu bản bị từ chối → bản ghi MỚI thay thế, GPS giữ nguyên.
        return await _outbox.replaceRejected(
          originalId: log.id,
          payload: {...local.payload, ...payload},
        );
      }
      if (local != null) {
        return await _outbox.correct(
          originalId: log.id,
          reason: reason,
          payload: {...payload, 'gps': local.payload['gps']},
          lockedPayloadKeys: const {'gps'},
        );
      }
      return await _outbox.correctRemote(
        entity: PushEntity.farmLog,
        supersedesId: log.id,
        reason: reason,
        payload: payload,
        eventTime: DateTime.tryParse(log.eventTime),
      );
    } on LockedFieldException {
      throw CropError(msgLockedGpsTime);
    } on OutboxException catch (e) {
      throw CropError(e.message);
    }
  }

  /// Nhật ký của vụ: bản máy chủ (trạng thái duyệt) + bản trên máy chưa về.
  Future<List<LogEntry>> logs({String? seasonRef}) async {
    final out = <LogEntry>[];
    final seen = <String>{};
    final localById = {
      for (final r in await _outbox.list(entity: PushEntity.farmLog)) r.id: r,
    };
    for (final r in await _refs.list(RefEntity.farmLog)) {
      final v = FarmLogView.fromJson({'id': r.id, ...r.data});
      if (seasonRef != null && v.seasonRef != seasonRef) continue;
      seen.add(v.id);
      out.add(LogEntry(view: v, local: localById[v.id]));
    }
    for (final r in localById.values) {
      if (seen.contains(r.id) || r.status == OutboxStatus.superseded) continue;
      final p = r.payload;
      if (seasonRef != null && p['season_ref'] != seasonRef) continue;
      if (r.replacedBy != null) continue;
      out.add(
        LogEntry(
          view: FarmLogView.fromJson({
            ...p,
            'id': r.id,
            'event_time': r.eventTime,
            'gps_source': (p['gps'] as Map?)?['source'] ?? 'denied',
            'status': 'pending',
          }),
          local: r,
        ),
      );
    }
    out.sort((a, b) => b.view.eventTime.compareTo(a.view.eventTime));
    return out;
  }

  /// Nhật ký bị yêu cầu bổ sung (nông dân cần sửa — HTX US-LOG-05 AC-3).
  Future<List<LogEntry>> needingSupplement() async => [
    for (final l in await logs())
      if (l.view.status == 'needs_supplement' && l.local == null) l,
  ];

  // ---- sản lượng (HTX US-YIELD-01/03) ----

  /// Lý do chưa khai được (null = được): cổng thu hoạch theo SOP đã ghim.
  Future<String?> harvestBlock(Season s) async =>
      harvestBlockReason(s, await sopFor(s), today());

  Future<bool> hasLocalHarvest(String seasonRef) async {
    for (final r in await _outbox.list(entity: PushEntity.harvest)) {
      if (r.payload['season_ref'] == seasonRef &&
          r.status != OutboxStatus.rejected &&
          r.status != OutboxStatus.superseded) {
        return true;
      }
    }
    return false;
  }

  Future<OutboxRecord> declareHarvest({
    required Season season,
    required String massKgInput,
    required DateTime harvestDate,
    bool confirmAnomaly = false,
  }) async {
    final block = await harvestBlock(season);
    if (block != null) throw CropError(block);
    if (harvestDate.isAfter(today())) {
      throw CropError('Ngày thu hoạch không được ở tương lai.');
    }
    final MassKg m;
    try {
      m = MassKg.parse(massKgInput);
    } on YieldError catch (e) {
      throw CropError(e.message);
    }
    final thr = await anomalyThreshold(season.varietyCode);
    final a = yieldAnomaly(m, season.areaHa, thr);
    if (a != YieldAnomaly.none && !confirmAnomaly) {
      throw YieldNeedsConfirm(anomalyMessage(a, m, season.areaHa, thr), a);
    }
    final loc = await _location.current();
    return _outbox.enqueue(
      entity: PushEntity.harvest,
      payload: {
        'season_ref': season.seasonRef,
        'plot_ref': season.plotRef,
        'harvest_date': formatDay(harvestDate),
        'mass_kg': m.wire,
        'confirm_anomaly': confirmAnomaly,
        'gps': gpsPayload(loc),
      },
    );
  }

  /// Máy chủ trả `rejected cropcycle.yield_anomaly` (ngưỡng máy chủ khác máy):
  /// người dùng xác nhận → bản ghi **mới** `confirm_anomaly: true`
  /// (sync-contract: không dùng correct cho bản bị từ chối).
  Future<OutboxRecord> confirmRejectedHarvest(OutboxRecord rejected) {
    if (rejected.entity != PushEntity.harvest ||
        rejected.errorCode != 'cropcycle.yield_anomaly') {
      throw CropError('Bản ghi không cần xác nhận sản lượng.');
    }
    return _outbox.replaceRejected(
      originalId: rejected.id,
      payload: {...rejected.payload, 'confirm_anomaly': true},
    );
  }

  /// Lời khuyên nông vụ theo giai đoạn (HTX US-WEATHER-02): bước SOP hiện tại
  /// của vụ đang mở. null → ẩn thẻ (AC-2).
  Future<(Season, SopStep)?> advice() async {
    for (final s in await seasons()) {
      if (!s.isOpen || s.sowingDate == null) continue;
      final sop = await sopFor(s);
      final step = sop?.currentStep(s.sowingDate!, today());
      if (step != null) return (s, step);
    }
    return null;
  }
}

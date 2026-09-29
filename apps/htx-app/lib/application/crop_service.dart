import 'package:agrigreen_mobile/core/clock.dart';
import 'package:agrigreen_mobile/core/ids.dart';
import 'package:agrigreen_mobile/data/catalog/catalog_cache.dart';
import 'package:agrigreen_mobile/data/files/evidence_repository.dart';
import 'package:agrigreen_mobile/data/outbox/outbox_repository.dart';
import 'package:agrigreen_mobile/data/photo/location_source.dart';
import 'package:agrigreen_mobile/data/reference/reference_repository.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
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

/// Cấu hình vụ vừa hoàn thành của thửa (HTX US-SEASON-03).
class PreviousConfig {
  const PreviousConfig({
    required this.seasonRef,
    required this.seasonCode,
    required this.seasonYear,
    required this.varietyCode,
    this.sopVersionId,
    this.offline = false,
  });
  final String seasonRef;
  final String seasonCode;
  final int seasonYear;
  final String varietyCode;
  final String? sopVersionId;

  /// Suy từ dữ liệu trên máy (mất mạng) — máy chủ lấy lại cấu hình thật khi
  /// nhận (`copy_previous: true`).
  final bool offline;
}

/// Kế hoạch ghi nhật ký hàng loạt (HTX US-LOG-03): thửa được ghi và thửa bị
/// loại kèm lý do (AC-2).
class BatchPlan {
  const BatchPlan(this.included, this.skipped);
  final List<(PlotItem, Season)> included;

  /// Mã thửa → lý do.
  final Map<String, String> skipped;
}

/// Một lượt ghi hàng loạt đã lưu trên máy + kết quả máy chủ (sau đồng bộ).
class BatchSummary {
  const BatchSummary({
    required this.record,
    required this.activityCode,
    required this.plotCodes,
    required this.accepted,
    required this.serverSkipped,
  });
  final OutboxRecord record;
  final String activityCode;
  final List<String> plotCodes;

  /// Số dòng nhật ký máy chủ đã tạo (entity `farm_log` có `batch_ref`).
  final int accepted;

  /// Thửa đã gửi mà máy chủ không tạo dòng (chưa mở vụ / ngoài phạm vi lúc
  /// máy chủ nhận). Chỉ có nghĩa khi bản ghi đã gửi và pull đã về.
  final List<String> serverSkipped;
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
    SyncApi? api,
    EvidenceRepository? evidence,
  }) : _api = api,
       _evidence = evidence,
       _outbox = outbox,
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

  /// API cho lời gọi chỉ đọc khi có mạng (cấu hình vụ trước); null = chỉ dữ
  /// liệu trên máy.
  final SyncApi? _api;

  /// Ảnh đã chụp (tọa độ đóng dấu lúc chụp — dự phòng GPS, HTX US-GPS-02).
  final EvidenceRepository? _evidence;

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

  /// Bản sửa/hủy vụ trên máy (`season_open` op correct) chưa bị từ chối, theo
  /// `season_ref`, cũ trước.
  Future<Map<String, List<OutboxRecord>>> _localCorrections() async {
    final m = <String, List<OutboxRecord>>{};
    for (final r in await _outbox.list(entity: PushEntity.seasonOpen)) {
      if (r.op != RecordOp.correct) continue;
      if (r.status != OutboxStatus.pending && r.status != OutboxStatus.sent) {
        continue;
      }
      m.putIfAbsent('${r.payload['season_ref']}', () => []).add(r);
    }
    for (final l in m.values) {
      l.sort((a, b) => a.seq.compareTo(b.seq));
    }
    return m;
  }

  /// Phủ bản sửa/hủy trên máy lên vụ. Bản đã phản ánh (máy chủ trả
  /// `current_event_id` = id bản ghi) và mọi bản cũ hơn được bỏ qua.
  static Season _overlay(Season s, List<OutboxRecord>? recs) {
    if (recs == null || recs.isEmpty) return s;
    final seen = recs.indexWhere((r) => r.id == s.currentEventId);
    var cur = s;
    for (final r in recs.skip(seen + 1)) {
      final p = r.payload;
      cur = cur.applyLocal(
        eventId: r.id,
        pending: r.status == OutboxStatus.pending,
        varietyCode: p['variety_code'] as String?,
        sowingDate: parseDay(p['sowing_date'] as String?),
        sopVersionId: p['sop_version_id'] as String?,
        cancel: p['cancel'] == true,
      );
    }
    return cur;
  }

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
          // id sự kiện mở vụ = id bản ghi (sync-contract) → supersedes_id khi
          // sửa/hủy vụ trước khi máy chủ gửi vụ xuống.
          'current_event_id': r.id,
        }, localPending: r.status == OutboxStatus.pending),
      );
    }
    return out;
  }

  Future<List<Season>> seasons() async {
    final pulled = await _pulledSeasons();
    final local = await _localSeasons({for (final s in pulled) s.seasonRef});
    final fixes = await _localCorrections();
    final all =
        [
          for (final s in [...local, ...pulled])
            _overlay(s, fixes[s.seasonRef]),
        ]..sort(
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
    PreviousConfig? copied,
  }) async {
    final err = checkSowingDate(sowingDate, today());
    if (err != null) throw CropError(err);
    if (seasonCode.isEmpty || varietyCode.isEmpty) {
      throw CropError('Chọn vụ và giống lúa.');
    }
    // HTX US-SEASON-04 AC-1: chặn mở vụ trùng ngay trên máy.
    if (await openSeasonFor(plot.ref) != null) {
      throw CropError(msgSeasonAlreadyOpen);
    }
    final sop = copied?.sopVersionId != null
        ? await sopById(copied!.sopVersionId)
        : await sopForVariety(varietyCode);
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
        'sop_version_id': ?(sop?.id ?? copied?.sopVersionId),
        // HTX US-SEASON-03: máy chủ lấy giống + SOP của vụ vừa hoàn thành.
        if (copied != null) 'copy_previous': true,
        'gps': gpsPayload(loc),
      },
    );
  }

  // ---- sao chép cấu hình vụ trước (HTX US-SEASON-03) ----

  /// Giống + SOP của vụ vừa hoàn thành trên thửa. Có mạng → `GET
  /// /v1/cropcycle/plots/{ref}/previous-config`; mất mạng → vụ gần nhất trên
  /// máy. Vụ gần nhất chưa hoàn thành → chặn (AC-2).
  Future<PreviousConfig> previousConfig(PlotItem plot) async {
    final api = _api;
    if (api != null) {
      try {
        final j = await api.getJson(
          '/v1/cropcycle/plots/${plot.ref}/previous-config',
        );
        return PreviousConfig(
          seasonRef: '${j['season_ref']}',
          seasonCode: '${j['season_code'] ?? ''}',
          seasonYear: (j['season_year'] as num?)?.toInt() ?? 0,
          varietyCode: '${j['variety_code'] ?? ''}',
          sopVersionId: j['sop_version_id'] as String?,
        );
      } on NetworkException {
        // dùng dữ liệu trên máy
      } on ApiException catch (e) {
        if (e is DeviceRevokedException || e is UnauthenticatedException) {
          rethrow;
        }
        if (e.code == 'cropcycle.previous_season_not_completed') {
          throw CropError(msgPreviousNotDone);
        }
        if (e.code == 'cropcycle.no_previous_season') {
          throw CropError(msgNoPreviousSeason);
        }
        // Lỗi khác (5xx, 404 thửa chưa về máy chủ…) → dữ liệu trên máy.
      }
    }
    final mine = [
      for (final s in await seasons())
        if (s.plotRef == plot.ref) s,
    ];
    if (mine.isEmpty) throw CropError(msgNoPreviousSeason);
    // seasons() đã xếp mới nhất trước theo ngày sạ.
    if (mine.first.isOpen) throw CropError(msgPreviousNotDone);
    final done = mine.where((s) => s.status == 'completed').firstOrNull;
    if (done == null) throw CropError(msgNoPreviousSeason);
    return PreviousConfig(
      seasonRef: done.seasonRef,
      seasonCode: done.seasonCode,
      seasonYear: done.seasonYear,
      varietyCode: done.varietyCode,
      sopVersionId: done.sopVersionId,
      offline: true,
    );
  }

  // ---- sửa / hủy vụ (bản ghi bù `season_open` op correct) ----

  Future<String> _correctableEventId(Season season) async {
    if (!season.isOpen) {
      throw CropError('Chỉ sửa hoặc hủy được vụ đang mở.');
    }
    if (season.harvestId != null || await hasLocalHarvest(season.seasonRef)) {
      throw CropError(
        'Vụ đã khai sản lượng — sửa khai báo sản lượng thay vì sửa vụ.',
      );
    }
    final ev = season.currentEventId;
    if (ev == null) {
      throw CropError('Chưa có dữ liệu vụ từ máy chủ — đồng bộ rồi thử lại.');
    }
    return ev;
  }

  /// Sửa vụ đang mở (giống / ngày sạ) — nông dân và quản lý HTX, làm được khi
  /// ngoại tuyến. `supersedes_id` = sự kiện hiện hành của vụ; lý do ≥ 10 ký tự.
  Future<OutboxRecord> correctSeason({
    required Season season,
    required String reason,
    String? varietyCode,
    DateTime? sowingDate,
  }) async {
    final ev = await _correctableEventId(season);
    final r = checkReason(reason);
    if (r != null) throw CropError(r);
    final newVariety = varietyCode != null && varietyCode != season.varietyCode
        ? varietyCode
        : null;
    final newSowing = sowingDate != null && sowingDate != season.sowingDate
        ? sowingDate
        : null;
    if (newVariety == null && newSowing == null) {
      throw CropError('Chưa thay đổi giống hoặc ngày gieo sạ.');
    }
    if (newSowing != null) {
      final err = checkSowingDate(newSowing, today());
      if (err != null) throw CropError(err);
    }
    final sop = newVariety == null ? null : await sopForVariety(newVariety);
    try {
      return await _outbox.correctRemote(
        entity: PushEntity.seasonOpen,
        supersedesId: ev,
        reason: reason,
        payload: {
          'season_ref': season.seasonRef,
          'variety_code': ?newVariety,
          if (newSowing != null) 'sowing_date': formatDay(newSowing),
          'sop_version_id': ?sop?.id,
        },
      );
    } on OutboxException catch (e) {
      throw CropError(e.message);
    }
  }

  /// Hủy vụ mở nhầm (chưa khai sản lượng) — lý do ≥ 10 ký tự. Máy chủ gửi
  /// tombstone vụ; thửa trở lại "Chưa mở vụ".
  Future<OutboxRecord> cancelSeason({
    required Season season,
    required String reason,
  }) async {
    final ev = await _correctableEventId(season);
    final r = checkReason(reason);
    if (r != null) throw CropError(r);
    try {
      return await _outbox.correctRemote(
        entity: PushEntity.seasonOpen,
        supersedesId: ev,
        reason: reason,
        payload: {'season_ref': season.seasonRef, 'cancel': true},
      );
    } on OutboxException catch (e) {
      throw CropError(e.message);
    }
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
        // Nhãn lý do của HTX US-SEASON-04 AC-2.
        skipped[p.code] = e.message == msgSeasonAlreadyOpen
            ? 'Lô đang canh tác vụ khác'
            : e.message;
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
    // GPS yếu / không có → tọa độ đóng dấu trên ảnh vừa chụp (US-GPS-02).
    final loc = await _location.current();
    final gps = gpsWithPhotoFallback(loc, await _photoGps(fileRefs));
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
        'gps': gps,
      },
    );
  }

  Future<List<Map<String, Object?>>> _photoGps(List<String> fileRefs) async {
    final ev = _evidence;
    if (ev == null) return const [];
    final out = <Map<String, Object?>>[];
    for (final id in fileRefs) {
      final g = (await ev.byId(id))?.meta['gps'];
      if (g is Map) out.add(g.cast<String, Object?>());
    }
    return out;
  }

  // ---- nhật ký hàng loạt (HTX US-LOG-03) ----

  /// Tối đa thửa mỗi lượt (máy chủ `MaxBulk`).
  static const maxBatchPlots = 200;

  /// Thửa có vụ đang mở trên máy được ghi; thửa khác bị loại kèm lý do.
  Future<BatchPlan> planBatch(List<PlotItem> plots) async {
    final included = <(PlotItem, Season)>[];
    final skipped = <String, String>{};
    for (final p in plots) {
      final se = await openSeasonFor(p.ref);
      if (se == null) {
        skipped[p.code] = skipNotOpen;
      } else {
        included.add((p, se));
      }
    }
    return BatchPlan(included, skipped);
  }

  /// Ghi hàng loạt: MỘT bản ghi `farm_log_batch` (một đầu lô, máy chủ tạo
  /// một dòng mỗi thửa — AC-1). Thửa chưa mở vụ bị loại trước khi gửi; máy chủ
  /// còn loại thêm thửa ngoài phạm vi / đã đóng vụ lúc nhận (AC-2). Vị trí:
  /// chọn lô từ danh sách (`manual_pick`, máy chủ tự gắn — US-GPS-02 AC-2).
  Future<(OutboxRecord, BatchPlan)> recordBatch({
    required List<PlotItem> plots,
    required FarmActivity activity,
    List<String> machineCodes = const [],
    List<Map<String, Object?>> inputs = const [],
    String note = '',
  }) async {
    if (plots.isEmpty) {
      throw CropError('Chọn ít nhất 1 lô ruộng để ghi nhật ký hàng loạt.');
    }
    if (plots.length > maxBatchPlots) {
      throw CropError('Tối đa $maxBatchPlots lô ruộng mỗi lượt.');
    }
    if (note.length > 2000) throw CropError('Ghi chú tối đa 2000 ký tự.');
    final plan = await planBatch(plots);
    if (plan.included.isEmpty) {
      final why = [
        for (final e in plan.skipped.entries) 'Lô ${e.key}: ${e.value}',
      ].join('; ');
      throw CropError('Không còn lô hợp lệ để ghi ($why).');
    }
    final rec = await _outbox.enqueue(
      entity: PushEntity.farmLogBatch,
      payload: {
        'plot_refs': [for (final (p, _) in plan.included) p.ref],
        'activity_code': activity.code,
        'machine_codes': machineCodes,
        'inputs': inputs,
        'note': note.trim(),
      },
    );
    return (rec, plan);
  }

  /// Các lượt ghi hàng loạt trên máy (mới trước) kèm kết quả máy chủ.
  Future<List<BatchSummary>> batches() async {
    final codes = {for (final p in await plots()) p.ref: p.code};
    final lines = <String, Set<String>>{};
    for (final r in await _refs.list(RefEntity.farmLog)) {
      final b = r.data['batch_ref'];
      if (b != null) {
        lines.putIfAbsent('$b', () => {}).add('${r.data['plot_ref']}');
      }
    }
    final out = <BatchSummary>[];
    for (final r in await _outbox.list(entity: PushEntity.farmLogBatch)) {
      final refs = [
        for (final p in (r.payload['plot_refs'] as List?) ?? const []) '$p',
      ];
      final got = lines[r.id] ?? const <String>{};
      out.add(
        BatchSummary(
          record: r,
          activityCode: '${r.payload['activity_code']}',
          plotCodes: [for (final p in refs) codes[p] ?? p],
          accepted: got.length,
          serverSkipped: r.status == OutboxStatus.sent && got.isNotEmpty
              ? [
                  for (final p in refs)
                    if (!got.contains(p)) codes[p] ?? p,
                ]
              : const [],
        ),
      );
    }
    return out;
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
    // Dòng của lượt ghi hàng loạt chưa về từ máy chủ (HTX US-LOG-03).
    // Máy chủ đã gửi dòng nào của lượt → dữ liệu máy chủ là chuẩn (kể cả thửa
    // máy chủ bỏ qua) — không hiện dòng trên máy nữa.
    final batched = {
      for (final r in await _refs.list(RefEntity.farmLog))
        if (r.data['batch_ref'] != null) '${r.data['batch_ref']}',
    };
    final plotOfSeason = seasonRef == null
        ? null
        : (await seasonByRef(seasonRef))?.plotRef;
    for (final r in await _outbox.list(entity: PushEntity.farmLogBatch)) {
      if (batched.contains(r.id) ||
          r.status == OutboxStatus.rejected ||
          r.status == OutboxStatus.superseded) {
        continue;
      }
      final p = r.payload;
      for (final plot in (p['plot_refs'] as List?) ?? const []) {
        if (seasonRef != null && plot != plotOfSeason) continue;
        out.add(
          LogEntry(
            view: FarmLogView.fromJson({
              ...p,
              'id': '${r.id}:$plot',
              'plot_ref': plot,
              'season_ref': seasonRef ?? '',
              'event_time': r.eventTime,
              'gps_source': GpsSourceCode.manualPick,
              'batch_ref': r.id,
              'status': 'pending',
            }),
            local: r,
          ),
        );
      }
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

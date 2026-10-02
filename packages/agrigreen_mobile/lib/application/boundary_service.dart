import 'dart:convert';

import '../core/ids.dart';
import '../data/db/local_db.dart';
import '../data/outbox/outbox_repository.dart';
import '../data/reference/reference_repository.dart';
import '../domain/geo/boundary.dart';
import '../domain/outbox_record.dart';

class OverlapHit {
  const OverlapHit(this.plotId, this.label, {required this.sameCooperative});
  final String plotId;
  final String label;
  final bool sameCooperative;
}

class BoundaryCheck {
  const BoundaryCheck(this.validation, this.overlaps);
  final BoundaryValidation validation;
  final List<OverlapHit> overlaps;

  /// Chồng lấn với thửa của HTX khác → chặn lưu (KN US-PLOT-03).
  bool get blocked => overlaps.any((o) => !o.sameCooperative);
  bool get needsAck => overlaps.isNotEmpty && !blocked;
}

class BoundaryException implements Exception {
  BoundaryException(this.message);
  final String message;
  @override
  String toString() => message;
}

enum BoundaryMethod { gps, map }

/// Nguồn ranh giới gửi lên pf/geo (`domain.Source*`).
abstract final class BoundarySource {
  static const knFieldApp = 'kn_field_app';
  static const htxApp = 'htx_app';
}

/// Ngưỡng lệch diện tích khai báo cần xác nhận mặc định (HTX US-PLOT-02 AC-3)
/// khi chưa tải được tham số `geo.area_deviation_max_pct`.
const areaDeviationConfirmPct = 5.0;

/// Thông báo khi quản lý HTX mở/sửa lô của HTX khác (HTX US-PLOT-01 AC-4).
const msgPlotNotOwned = 'Bạn không có quyền truy cập lô ruộng này.';

/// Mã `rejected` của pf/geo mà người vẽ xác nhận được để gửi lại với
/// `confirm_overlap: true` (application.yaml: geo.overlap_needs_confirm,
/// geo.area_deviation_needs_confirm).
const confirmableRejectCodes = {
  'geo.overlap_needs_confirm',
  'geo.area_deviation_needs_confirm',
};

/// Trạng thái ranh giới của một thửa theo hàng đợi trên máy.
enum PlotSyncState {
  none(''),
  pending('Chờ đồng bộ'),
  accepted('Đã đồng bộ'),
  needsConfirm('Chồng lấn — cần xác nhận'),
  conflict('Xung đột — chờ cán bộ xử lý'),
  rejected('Bị từ chối');

  const PlotSyncState(this.label);
  final String label;
}

class PlotBoundaryStatus {
  const PlotBoundaryStatus(this.state, {this.record, this.detail});
  final PlotSyncState state;
  final OutboxRecord? record;
  final String? detail;

  /// Có thể gửi lại kèm `confirm_overlap: true` sau khi người dùng xác nhận.
  bool get canConfirm => state == PlotSyncState.needsConfirm;
}

/// Bản nháp đang vẽ (chưa bấm lưu) — giữ trong CSDL SQLCipher để mất mạng,
/// tắt app hay hết pin giữa lúc đi ranh giới không mất điểm (HTX US-PLOT-05
/// AC-1).
class BoundaryDraft {
  const BoundaryDraft(this.points, {this.accuracies = const [], this.method});
  final List<GeoPoint> points;

  /// Sai số (m) từng điểm, cùng thứ tự [points]; null = điểm chọn trên bản đồ.
  final List<double?> accuracies;
  final BoundaryMethod? method;

  /// Sai số lớn nhất trong các điểm GPS; null = không có điểm GPS.
  double? get worstAccuracyM {
    double? w;
    for (final a in accuracies) {
      if (a != null && (w == null || a > w)) w = a;
    }
    return w;
  }

  Map<String, Object?> toJson() => {
    'points': [
      for (var i = 0; i < points.length; i++)
        [
          points[i].lat,
          points[i].lon,
          if (i < accuracies.length && accuracies[i] != null) accuracies[i],
        ],
    ],
    'method': ?method?.name,
  };

  static BoundaryDraft? fromJson(Object? j) {
    if (j is! Map) return null;
    final pts = <GeoPoint>[];
    final acc = <double?>[];
    for (final e in (j['points'] as List?) ?? const []) {
      if (e is List && e.length >= 2) {
        pts.add(GeoPoint((e[0]! as num).toDouble(), (e[1]! as num).toDouble()));
        acc.add(e.length > 2 ? (e[2] as num?)?.toDouble() : null);
      }
    }
    final m = j['method'];
    return BoundaryDraft(
      pts,
      accuracies: acc,
      method: BoundaryMethod.values.where((x) => x.name == m).firstOrNull,
    );
  }
}

/// Parse "1,25" / "1.25" → 1.25; rỗng → null; sai → ném [BoundaryException].
double? parseDeclaredHa(String? s) {
  final t = s?.trim().replaceAll(',', '.') ?? '';
  if (t.isEmpty) return null;
  final v = double.tryParse(t);
  if (v == null || v <= 0 || v > 100000) {
    throw BoundaryException('Diện tích khai báo không hợp lệ.');
  }
  return v;
}

/// Lệch (%) giữa diện tích vẽ và diện tích khai báo.
double areaDeviationPct(double drawnHa, double declaredHa) =>
    (drawnHa - declaredHa).abs() / declaredHa * 100;

String _plotCoop(Map<String, Object?> d) =>
    ((d['cooperative_ref'] ?? d['cooperative_id']) as String?) ?? '';

/// Vẽ ranh giới thửa (KN epic C, HTX epic D), kể cả ngoại tuyến. Payload theo
/// `BoundaryPayload` của pf/geo (sync-contract, "Payload plot_boundary").
/// Dùng chung cho app hiện trường (cán bộ KN, `source: kn_field_app`) và App
/// HTX (quản lý HTX, `source: htx_app`, chỉ lô của HTX mình + lô nháp).
class BoundaryService {
  BoundaryService({
    required OutboxRepository outbox,
    required ReferenceRepository refs,
    required IdGenerator ids,
    this.source = BoundarySource.knFieldApp,
    LocalDb? db,
    Future<double> Function()? deviationLimitPct,
    Future<String?> Function()? ownCooperative,
  }) : _outbox = outbox,
       _refs = refs,
       _ids = ids,
       _db = db,
       _limit = deviationLimitPct,
       _own = ownCooperative;

  final OutboxRepository _outbox;
  final ReferenceRepository _refs;
  final IdGenerator _ids;
  final LocalDb? _db;
  final Future<double> Function()? _limit;

  /// HTX của người vẽ (quản lý HTX). null → không giới hạn theo HTX (cán bộ KN).
  final Future<String?> Function()? _own;

  /// `source` của payload (pf/geo `domain.Source*`).
  final String source;

  static String? _coopOf(Map<String, Object?> d) =>
      (d['cooperative_ref'] ?? d['cooperative_id']) as String?;

  /// Ngưỡng lệch diện tích (%) đang áp dụng (`geo.area_deviation_max_pct`).
  Future<double> deviationLimit() async =>
      await _limit?.call() ?? areaDeviationConfirmPct;

  /// HTX của người vẽ, nếu app giới hạn theo HTX.
  Future<String?> ownCooperative() async => _own?.call();

  /// Người vẽ có được xem/sửa lô [plot] không (HTX US-PLOT-01 AC-3/AC-4):
  /// quản lý HTX chỉ lô của HTX mình và lô nháp chưa gán HTX.
  Future<bool> canEdit(RefRow plot) async {
    if (_own == null) return true;
    final own = await _own();
    final coop = _plotCoop(plot.data);
    return coop.isEmpty || (own != null && coop == own);
  }

  Future<BoundaryCheck> check(
    List<GeoPoint> points, {
    String? plotId,
    String? cooperativeId,
  }) async {
    final v = validateBoundary(points);
    if (!v.ok) return BoundaryCheck(v, const []);
    final hits = <OverlapHit>[];
    for (final p in await _refs.list(RefEntity.plot)) {
      if (p.id == plotId) continue;
      final ring = ringFromGeoJson(p.data['boundary'] ?? p.data['geometry']);
      if (ring == null || ring.length < 3) continue;
      if (polygonsOverlap(v.ring, ring)) {
        final coop = _coopOf(p.data);
        hits.add(
          OverlapHit(
            p.id,
            (p.data['label'] ?? p.data['name'] ?? p.data['code'] ?? p.id)
                .toString(),
            sameCooperative:
                cooperativeId != null && coop != null && coop == cooperativeId,
          ),
        );
      }
    }
    return BoundaryCheck(v, hits);
  }

  /// Vòng ranh giới của các thửa khác trên máy (vẽ nền khi đang vẽ).
  Future<List<List<GeoPoint>>> otherRings({String? exceptPlotId}) async {
    final out = <List<GeoPoint>>[];
    for (final p in await _refs.list(RefEntity.plot)) {
      if (p.id == exceptPlotId) continue;
      final r = ringFromGeoJson(p.data['boundary'] ?? p.data['geometry']);
      if (r != null && r.length >= 3) out.add(r);
    }
    return out;
  }

  /// Xã (đơn vị hành chính cấp xã) đã đồng bộ về máy — chọn xã cho thửa mới.
  Future<List<RefRow>> communes() async => [
    for (final u in await _refs.list(RefEntity.adminUnit))
      if (u.data['level'] == null ||
          u.data['level'] == 'commune' ||
          u.data['level'] == 'xa')
        u,
  ];

  // ---- bản nháp đang vẽ (HTX US-PLOT-05 AC-1) ----

  static String _draftKey(String? plotId) =>
      'boundary.draft.${plotId ?? 'new'}';

  Future<void> saveDraft(String? plotId, BoundaryDraft d) async {
    final db = _db;
    if (db == null) return;
    await db.setMeta(
      _draftKey(plotId),
      d.points.isEmpty ? null : jsonEncode(d.toJson()),
    );
  }

  Future<BoundaryDraft?> loadDraft(String? plotId) async {
    final v = await _db?.getMeta(_draftKey(plotId));
    if (v == null) return null;
    try {
      final d = BoundaryDraft.fromJson(jsonDecode(v));
      return d == null || d.points.isEmpty ? null : d;
    } on FormatException {
      return null;
    }
  }

  Future<void> clearDraft(String? plotId) async =>
      _db?.setMeta(_draftKey(plotId), null);

  /// Lưu ranh giới vào hàng đợi.
  ///
  /// - [plotId] null = thửa mới: `op: create`, `plot_ref` UUIDv7 máy sinh,
  ///   bắt buộc [communeCode]. Quản lý HTX: thửa mới gán HTX của mình (mỗi lô
  ///   một HTX — HTX US-PLOT-01 AC-3).
  /// - Thửa đã có phiên bản ranh giới trên máy chủ (`boundary_id`): `op:
  ///   correct` với `supersedes_id = boundary_id`, bắt buộc [correctionReason]
  ///   ≥ 10 ký tự. Dùng chung một bản ghi thửa với phần KN vẽ trước (AC-2).
  /// - [acknowledgeOverlap] (người vẽ đã xác nhận chồng lấn cùng HTX hoặc lệch
  ///   diện tích > ngưỡng) → `confirm_overlap: true`.
  ///
  /// Lưu xong thì xóa bản nháp của thửa.
  Future<OutboxRecord> save({
    required List<GeoPoint> points,
    required BoundaryMethod method,
    String? plotId,
    String? cooperativeId,
    String? communeCode,
    String? name,
    String? householdRef,
    double? declaredAreaHa,
    String? correctionReason,
    bool acknowledgeOverlap = false,
    double? worstAccuracyM,
    String? lotName,
    String? note,
    String? farmerName,
    String? shape,
  }) async {
    final existing = plotId == null
        ? null
        : await _refs.get(RefEntity.plot, plotId);
    if (existing != null && !await canEdit(existing)) {
      throw BoundaryException(msgPlotNotOwned);
    }
    final own = await ownCooperative();
    final coop =
        cooperativeId ??
        (existing == null ? null : _coopOf(existing.data)) ??
        (existing == null ? own : null);
    final c = await check(points, plotId: plotId, cooperativeId: coop);
    if (!c.validation.ok) {
      throw BoundaryException(c.validation.errors.first.message);
    }
    if (c.blocked) {
      throw BoundaryException(
        'Ranh giới bị chồng lấn với lô đã đăng ký của HTX/nông hộ khác, vui lòng kiểm tra lại.',
      );
    }
    if (c.needsAck && !acknowledgeOverlap) {
      throw BoundaryException(
        'Ranh giới bị chồng lấn với lô đã đăng ký, vui lòng kiểm tra lại.',
      );
    }
    final deviation = declaredAreaHa == null
        ? null
        : areaDeviationPct(c.validation.areaHa, declaredAreaHa);
    final limit = await deviationLimit();
    if (deviation != null && deviation > limit && !acknowledgeOverlap) {
      throw BoundaryException(
        'Diện tích vẽ lệch ${deviation.toStringAsFixed(1)} % so với diện tích '
        'khai báo (ngưỡng ${limit.toStringAsFixed(0)} %). Hãy kiểm tra lại '
        'hoặc xác nhận giữ nguyên.',
      );
    }
    final boundaryId = existing?.data['boundary_id'] as String?;
    final commune =
        communeCode ??
        (existing?.data['commune_code'] ?? existing?.data['admin_unit_code'])
            as String?;
    final payload = <String, Object?>{
      'plot_ref': plotId ?? _ids.newId(),
      'name': ?name,
      'commune_code': ?commune,
      'cooperative_ref': ?coop,
      'household_ref': ?householdRef,
      'geometry': toGeoJsonPolygon(c.validation.ring),
      'gps_accuracy_m': ?worstAccuracyM?.toStringAsFixed(1),
      'declared_area_ha': ?declaredAreaHa?.toStringAsFixed(4),
      // KN v7.7 P7 (FN-XA-03.05): thuộc tính thửa; tên nông dân máy chủ đưa
      // vào kho định danh (vault), không lưu tên.
      if (lotName != null && lotName.trim().isNotEmpty)
        'lot_name': lotName.trim(),
      if (note != null && note.trim().isNotEmpty) 'note': note.trim(),
      if (farmerName != null && farmerName.trim().isNotEmpty)
        'farmer_name': farmerName.trim(),
      'shape': ?shape,
      'source': source,
      'confirm_overlap': acknowledgeOverlap,
      // Thông tin phụ cho màn hình trên máy (máy chủ bỏ qua trường lạ).
      'area_ha_client': formatHa(c.validation.areaHa),
      'point_count': c.validation.ring.length,
      'method': method.name,
      'overlap_plot_ids': [for (final o in c.overlaps) o.plotId],
    };
    final OutboxRecord rec;
    if (boundaryId != null) {
      final reason = correctionReason?.trim() ?? '';
      if (reason.length < 10) {
        throw BoundaryException(
          'Cần ghi lý do sửa ranh giới (ít nhất 10 ký tự).',
        );
      }
      rec = await _outbox.correctRemote(
        entity: PushEntity.plotBoundary,
        supersedesId: boundaryId,
        reason: reason,
        payload: payload,
      );
    } else {
      if (plotId == null && (commune == null || commune.isEmpty)) {
        throw BoundaryException('Chọn xã của thửa trước khi lưu.');
      }
      rec = await _outbox.enqueue(
        entity: PushEntity.plotBoundary,
        payload: payload,
      );
    }
    await clearDraft(plotId);
    return rec;
  }

  /// Trạng thái ranh giới của [plotId] theo bản ghi mới nhất trên máy.
  Future<PlotBoundaryStatus> statusOf(String plotId) async {
    for (final r in await _outbox.list(entity: PushEntity.plotBoundary)) {
      if (r.payload['plot_ref'] != plotId && r.payload['plot_id'] != plotId) {
        continue;
      }
      return statusOfRecord(r);
    }
    return const PlotBoundaryStatus(PlotSyncState.none);
  }

  /// Ranh giới vẽ trên máy chưa có thửa trên máy chủ (thửa mới chờ đồng bộ,
  /// xung đột hoặc bị từ chối) — hiện trên bản đồ để không vẽ trùng.
  Future<List<OutboxRecord>> localNewPlots() async {
    final known = {for (final p in await _refs.list(RefEntity.plot)) p.id};
    final seen = <String>{};
    final out = <OutboxRecord>[];
    for (final r in await _outbox.list(entity: PushEntity.plotBoundary)) {
      final ref = '${r.payload['plot_ref']}';
      if (r.op != RecordOp.create || known.contains(ref) || !seen.add(ref)) {
        continue;
      }
      if (r.status == OutboxStatus.superseded || r.replacedBy != null) {
        continue;
      }
      out.add(r);
    }
    return out;
  }

  /// Bản ghi `plot_boundary` cần người dùng xem lại (xung đột/bị từ chối).
  Future<List<PlotBoundaryStatus>> needingAttention() async => [
    for (final r in await _outbox.list(
      entity: PushEntity.plotBoundary,
      statuses: {OutboxStatus.conflict, OutboxStatus.rejected},
    ))
      if (r.replacedBy == null) statusOfRecord(r),
  ];

  PlotBoundaryStatus statusOfRecord(OutboxRecord r) {
    final confirmed = r.payload['confirm_overlap'] == true;
    return switch (r.status) {
      OutboxStatus.pending => PlotBoundaryStatus(
        PlotSyncState.pending,
        record: r,
      ),
      OutboxStatus.sent || OutboxStatus.superseded => PlotBoundaryStatus(
        PlotSyncState.accepted,
        record: r,
      ),
      OutboxStatus.conflict =>
        // Máy chủ giữ cả hai phiên bản. Chồng lấn chưa xác nhận → người vẽ
        // xác nhận được; đã xác nhận mà vẫn xung đột (HTX khác, sửa đồng
        // thời) → chờ cán bộ xử lý.
        PlotBoundaryStatus(
          confirmed ? PlotSyncState.conflict : PlotSyncState.needsConfirm,
          record: r,
          detail: r.conflictId == null ? null : 'Mã xung đột ${r.conflictId}',
        ),
      OutboxStatus.rejected => PlotBoundaryStatus(
        confirmableRejectCodes.contains(r.errorCode) && !confirmed
            ? PlotSyncState.needsConfirm
            : PlotSyncState.rejected,
        record: r,
        detail: r.errorSummary,
      ),
    };
  }

  /// Người vẽ đã kiểm tra thực địa và xác nhận giữ nguyên chồng lấn / lệch
  /// diện tích sau phản hồi của máy chủ:
  /// - `rejected` (geo.*_needs_confirm): máy chủ không lưu bản gốc → bản ghi
  ///   **mới** cùng nội dung, `confirm_overlap: true` (sync-contract, Làm rõ 4);
  /// - `conflict`: máy chủ đã giữ bản ghi → bản ghi bù `correct` trỏ tới nó.
  Future<OutboxRecord> confirmAfterServer(OutboxRecord r) async {
    final st = statusOfRecord(r);
    if (!st.canConfirm) {
      throw BoundaryException('Bản ghi này không cần xác nhận.');
    }
    final payload = Map<String, Object?>.from(r.payload)
      ..['confirm_overlap'] = true;
    if (r.status == OutboxStatus.rejected) {
      return _outbox.replaceRejected(originalId: r.id, payload: payload);
    }
    final rec = await _outbox.correctRemote(
      entity: PushEntity.plotBoundary,
      supersedesId: r.id,
      reason: 'Xác nhận giữ nguyên chồng lấn sau khi kiểm tra thực địa',
      payload: payload,
    );
    await _outbox.linkReplacement(r.id, rec.id);
    return rec;
  }
}

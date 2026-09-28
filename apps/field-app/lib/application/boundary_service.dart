import '../core/ids.dart';
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

/// Ngưỡng lệch diện tích khai báo cần xác nhận (HTX US-PLOT-02 AC-3).
const areaDeviationConfirmPct = 5.0;

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

/// Vẽ ranh giới thửa (KN epic C, HTX epic D), kể cả ngoại tuyến. Payload theo
/// `BoundaryPayload` của pf/geo (sync-contract, "Payload plot_boundary").
class BoundaryService {
  BoundaryService({
    required OutboxRepository outbox,
    required ReferenceRepository refs,
    required IdGenerator ids,
  }) : _outbox = outbox,
       _refs = refs,
       _ids = ids;

  final OutboxRepository _outbox;
  final ReferenceRepository _refs;
  final IdGenerator _ids;

  static String? _coopOf(Map<String, Object?> d) =>
      (d['cooperative_ref'] ?? d['cooperative_id']) as String?;

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

  /// Lưu ranh giới vào hàng đợi.
  ///
  /// - [plotId] null = thửa mới: `op: create`, `plot_ref` UUIDv7 máy sinh,
  ///   bắt buộc [communeCode].
  /// - Thửa đã có phiên bản ranh giới trên máy chủ (`boundary_id`): `op:
  ///   correct` với `supersedes_id = boundary_id`, bắt buộc [correctionReason]
  ///   ≥ 10 ký tự.
  /// - [acknowledgeOverlap] (người vẽ đã xác nhận chồng lấn cùng HTX hoặc lệch
  ///   diện tích > 5 %) → `confirm_overlap: true`.
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
  }) async {
    final c = await check(points, plotId: plotId, cooperativeId: cooperativeId);
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
    if (deviation != null &&
        deviation > areaDeviationConfirmPct &&
        !acknowledgeOverlap) {
      throw BoundaryException(
        'Diện tích vẽ lệch ${deviation.toStringAsFixed(1)} % so với diện tích '
        'khai báo. Hãy kiểm tra lại hoặc xác nhận giữ nguyên.',
      );
    }
    final existing = plotId == null
        ? null
        : await _refs.get(RefEntity.plot, plotId);
    final boundaryId = existing?.data['boundary_id'] as String?;
    final commune =
        communeCode ??
        (existing?.data['commune_code'] ?? existing?.data['admin_unit_code'])
            as String?;
    final payload = <String, Object?>{
      'plot_ref': plotId ?? _ids.newId(),
      'name': ?name,
      'commune_code': ?commune,
      'cooperative_ref': ?cooperativeId,
      'household_ref': ?householdRef,
      'geometry': toGeoJsonPolygon(c.validation.ring),
      'gps_accuracy_m': ?worstAccuracyM?.toStringAsFixed(1),
      'declared_area_ha': ?declaredAreaHa?.toStringAsFixed(4),
      'source': 'kn_field_app',
      'confirm_overlap': acknowledgeOverlap,
      // Thông tin phụ cho màn hình trên máy (máy chủ bỏ qua trường lạ).
      'area_ha_client': formatHa(c.validation.areaHa),
      'point_count': c.validation.ring.length,
      'method': method.name,
      'overlap_plot_ids': [for (final o in c.overlaps) o.plotId],
    };
    if (boundaryId != null) {
      final reason = correctionReason?.trim() ?? '';
      if (reason.length < 10) {
        throw BoundaryException(
          'Cần ghi lý do sửa ranh giới (ít nhất 10 ký tự).',
        );
      }
      return _outbox.correctRemote(
        entity: PushEntity.plotBoundary,
        supersedesId: boundaryId,
        reason: reason,
        payload: payload,
      );
    }
    if (plotId == null && (commune == null || commune.isEmpty)) {
      throw BoundaryException('Chọn xã của thửa trước khi lưu.');
    }
    return _outbox.enqueue(entity: PushEntity.plotBoundary, payload: payload);
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

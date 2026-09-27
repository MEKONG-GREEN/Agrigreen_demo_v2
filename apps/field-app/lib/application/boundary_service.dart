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

/// Vẽ ranh giới thửa (KN epic C, HTX epic D), kể cả ngoại tuyến.
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
        final coop = p.data['cooperative_id'] as String?;
        hits.add(
          OverlapHit(
            p.id,
            (p.data['label'] ?? p.data['code'] ?? p.id).toString(),
            sameCooperative:
                cooperativeId != null && coop != null && coop == cooperativeId,
          ),
        );
      }
    }
    return BoundaryCheck(v, hits);
  }

  /// Lưu ranh giới vào hàng đợi. [plotId] null = thửa mới (id sinh trên máy).
  Future<OutboxRecord> save({
    required List<GeoPoint> points,
    required BoundaryMethod method,
    String? plotId,
    String? cooperativeId,
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
    return _outbox.enqueue(
      entity: PushEntity.plotBoundary,
      payload: {
        'plot_id': plotId ?? _ids.newId(),
        'is_new_plot': plotId == null,
        'cooperative_id': cooperativeId,
        'geometry': toGeoJsonPolygon(c.validation.ring),
        'area_ha_client': formatHa(c.validation.areaHa),
        'point_count': c.validation.ring.length,
        'method': method.name,
        'worst_accuracy_m': worstAccuracyM,
        'overlap_acknowledged': c.overlaps.isNotEmpty,
        'overlap_plot_ids': [for (final o in c.overlaps) o.plotId],
      },
    );
  }
}

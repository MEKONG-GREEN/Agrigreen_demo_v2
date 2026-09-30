import 'package:agrigreen_mobile/data/outbox/outbox_repository.dart';
import 'package:agrigreen_mobile/data/reference/reference_repository.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';

import '../domain/collection.dart';

class CollectionError implements Exception {
  CollectionError(this.message);
  final String message;
  @override
  String toString() => message;
}

/// Lịch gom MKG (entity `htx_collection_schedule`).
class CollectionSchedule {
  const CollectionSchedule({
    required this.ref,
    required this.communeCode,
    required this.windowFrom,
    required this.windowTo,
    required this.status,
    required this.revision,
  });
  final String ref;
  final String communeCode;
  final String windowFrom;
  final String windowTo;
  final String status;
  final int revision;
  String get statusLabel => scheduleStatusLabels[status] ?? status;
}

/// Bảng kê số cuộn một lô (entity `htx_collection_statement`) + phản hồi
/// hiện hành từ máy chủ và phản hồi đang chờ gửi trên máy (nếu có).
class CollectionStatement {
  const CollectionStatement({
    required this.ref,
    required this.plotRef,
    required this.plotCode,
    required this.collectedOn,
    required this.rolls,
    required this.revision,
    this.responseOutcome,
    this.responseRevision,
    this.responseHtxRolls,
    this.pendingOutcome,
    this.rejected,
  });
  final String ref;
  final String plotRef;
  final String plotCode;
  final String collectedOn;
  final int rolls;
  final int revision;
  final String? responseOutcome;
  final int? responseRevision;
  final int? responseHtxRolls;

  /// Phản hồi đang chờ gửi (ngoại tuyến) cho revision này.
  final String? pendingOutcome;

  /// Mã lỗi khi máy chủ từ chối phản hồi gần nhất (vd bảng kê đã có bản sửa).
  final String? rejected;

  bool get answeredCurrent => responseRevision == revision;

  String get stateLabel {
    if (pendingOutcome != null) return 'Đang chờ gửi';
    if (responseOutcome == null) return 'Chờ HTX xác nhận';
    if (!answeredCurrent) return 'MKG đã sửa — cần xác nhận lại';
    return responseOutcome == 'confirmed'
        ? 'Đã xác nhận'
        : 'Đang tranh chấp (HTX đếm ${responseHtxRolls ?? '?'} cuộn)';
  }
}

/// Thu gom rơm MKG cho quản lý HTX (G3-2): đọc lịch gom và bảng kê đã đồng
/// bộ về máy; phản hồi luôn qua hàng đợi đồng bộ (entity
/// `htx_collection_response`) nên gửi được khi mất mạng. Máy chủ từ chối
/// nếu MKG đã gửi bản sửa (`cropcycle.collection_statement_stale`).
class CollectionService {
  CollectionService({
    required OutboxRepository outbox,
    required ReferenceRepository refs,
  }) : _outbox = outbox,
       _refs = refs;

  final OutboxRepository _outbox;
  final ReferenceRepository _refs;

  Future<List<CollectionSchedule>> schedules() async {
    final out = [
      for (final r in await _refs.list(RefEntity.htxCollectionSchedule))
        CollectionSchedule(
          ref: r.id,
          communeCode: '${r.data['commune_code'] ?? ''}',
          windowFrom: '${r.data['window_from'] ?? ''}',
          windowTo: '${r.data['window_to'] ?? ''}',
          status: '${r.data['status'] ?? ''}',
          revision: (r.data['revision'] as num?)?.toInt() ?? 0,
        ),
    ];
    out.sort((a, b) => b.windowFrom.compareTo(a.windowFrom));
    return out;
  }

  Future<List<CollectionStatement>> statements() async {
    final plots = {
      for (final p in await _refs.list(RefEntity.plot))
        p.id: '${p.data['code'] ?? ''}',
    };
    final queued = await _outbox.list(entity: PushEntity.htxCollectionResponse);
    final out = <CollectionStatement>[];
    for (final r in await _refs.list(RefEntity.htxCollectionStatement)) {
      final d = r.data;
      final rev = (d['revision'] as num?)?.toInt() ?? 0;
      final resp = d['response'] as Map<String, Object?>?;
      String? pending;
      String? rejected;
      for (final q in queued) {
        if (q.payload['statement_ref'] != r.id) continue;
        if (q.status == OutboxStatus.pending &&
            (q.payload['revision'] as num?)?.toInt() == rev) {
          pending = '${q.payload['outcome']}';
        }
        if (q.status == OutboxStatus.rejected) rejected = q.errorCode;
      }
      final plotRef = '${d['plot_ref'] ?? ''}';
      out.add(
        CollectionStatement(
          ref: r.id,
          plotRef: plotRef,
          plotCode: plots[plotRef] ?? (d['plot_code'] as String? ?? ''),
          collectedOn: '${d['collected_on'] ?? ''}',
          rolls: (d['rolls'] as num?)?.toInt() ?? 0,
          revision: rev,
          responseOutcome: resp?['outcome'] as String?,
          responseRevision: (resp?['revision'] as num?)?.toInt(),
          responseHtxRolls: (resp?['htx_rolls'] as num?)?.toInt(),
          pendingOutcome: pending,
          rejected: rejected,
        ),
      );
    }
    out.sort((a, b) => b.collectedOn.compareTo(a.collectedOn));
    return out;
  }

  /// Xếp phản hồi vào hàng đợi (gửi ngay nếu có mạng, tự gửi khi có mạng lại).
  Future<OutboxRecord> respond(
    CollectionStatement s, {
    required String outcome,
    int? htxRolls,
    String reason = '',
  }) async {
    final err = validateCollectionResponse(
      outcome: outcome,
      mkgRolls: s.rolls,
      htxRolls: htxRolls,
      reason: reason,
      superseding: s.answeredCurrent,
    );
    if (err != null) throw CollectionError(err);
    final r = reason.trim();
    return _outbox.enqueue(
      entity: PushEntity.htxCollectionResponse,
      payload: {
        'statement_ref': s.ref,
        'revision': s.revision,
        'outcome': outcome,
        if (outcome == 'disputed') 'htx_rolls': htxRolls,
        if (r.isNotEmpty) 'reason': r,
      },
    );
  }
}

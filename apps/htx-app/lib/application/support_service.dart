import 'package:agrigreen_mobile/data/outbox/outbox_repository.dart';
import 'package:agrigreen_mobile/data/photo/location_source.dart';
import 'package:agrigreen_mobile/data/reference/reference_repository.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';

import '../domain/support.dart';

class SupportError implements Exception {
  SupportError(this.message);
  final String message;
  @override
  String toString() => message;
}

/// Một yêu cầu hỗ trợ trên máy: bản máy chủ gửi xuống (entity
/// `support_request`, kèm trạng thái nhiệm vụ) hoặc bản đang chờ gửi.
class SupportItem {
  const SupportItem({
    required this.id,
    required this.category,
    required this.description,
    required this.statusLabel,
    required this.eventTime,
    this.pending = false,
    this.gpsMissing = false,
    this.photos = 0,
    this.lastNote,
  });
  final String id;
  final String category;
  final String description;
  final String statusLabel;
  final String eventTime;

  /// "Đang chờ gửi" (HTX US-SUPPORT-01 AC-2).
  final bool pending;
  final bool gpsMissing;
  final int photos;
  final String? lastNote;
}

/// Gửi yêu cầu hỗ trợ (HTX US-SUPPORT-01): luôn qua hàng đợi đồng bộ (entity
/// `support_request`) nên gửi được khi mất mạng; máy chủ tạo nhiệm vụ cho cán
/// bộ khuyến nông khi nhận (KN US-TASK-01).
class SupportService {
  SupportService({
    required OutboxRepository outbox,
    required ReferenceRepository refs,
    required LocationSource location,
  }) : _outbox = outbox,
       _refs = refs,
       _location = location;

  final OutboxRepository _outbox;
  final ReferenceRepository _refs;
  final LocationSource _location;

  /// Lấy vị trí (tùy chọn — thiếu vẫn gửi, nhãn "Thiếu vị trí GPS", AC-1).
  Future<LocationResult> locate() => _location.current();

  Future<OutboxRecord> submit({
    required String? category,
    required String description,
    String urgency = 'binh_thuong',
    String? householdRef,
    String? plotRef,
    List<String> photoRefs = const [],
    LocationResult? location,
  }) async {
    final err = validateSupport(
      category: category,
      description: description,
      photos: photoRefs.length,
    );
    if (err != null) throw SupportError(err);
    final fix = location?.fix;
    return _outbox.enqueue(
      entity: PushEntity.supportRequest,
      payload: {
        'household_ref': ?householdRef,
        'plot_ref': ?plotRef,
        'category': category,
        'urgency': urgency,
        'description': description.trim(),
        'photo_refs': photoRefs,
        if (fix != null && location!.status != GpsStatus.denied)
          'location': {
            'lat': fix.lat.toStringAsFixed(7),
            'lon': fix.lon.toStringAsFixed(7),
          },
        if (fix?.accuracyM != null)
          'gps_accuracy_m': fix!.accuracyM!.toStringAsFixed(1),
      },
    );
  }

  Future<List<SupportItem>> list() async {
    final out = <SupportItem>[];
    final seen = <String>{};
    for (final r in await _refs.list(RefEntity.supportRequest)) {
      final d = r.data;
      seen.add(r.id);
      out.add(
        SupportItem(
          id: r.id,
          category: '${d['category'] ?? ''}',
          description: '${d['description'] ?? ''}',
          statusLabel: supportStatusLabel(d['task_status'] as String?),
          eventTime: '${d['event_time'] ?? ''}',
          gpsMissing: d['gps_missing'] == true,
          photos: ((d['photo_refs'] as List?) ?? const []).length,
          lastNote: (d['last_note'] as String?)?.trim().isEmpty ?? true
              ? null
              : d['last_note'] as String?,
        ),
      );
    }
    for (final r in await _outbox.list(entity: PushEntity.supportRequest)) {
      if (seen.contains(r.id) || r.status == OutboxStatus.superseded) continue;
      final p = r.payload;
      out.add(
        SupportItem(
          id: r.id,
          category: '${p['category']}',
          description: '${p['description']}',
          statusLabel: switch (r.status) {
            OutboxStatus.pending => 'Đang chờ gửi',
            OutboxStatus.rejected => 'Bị từ chối: ${r.errorCode ?? ''}',
            _ => 'Đã gửi',
          },
          eventTime: r.eventTime,
          pending: r.status == OutboxStatus.pending,
          gpsMissing: p['location'] == null,
          photos: ((p['photo_refs'] as List?) ?? const []).length,
        ),
      );
    }
    out.sort((a, b) => b.eventTime.compareTo(a.eventTime));
    return out;
  }
}

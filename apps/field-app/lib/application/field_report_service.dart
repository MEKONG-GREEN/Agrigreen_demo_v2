import '../core/clock.dart';
import '../data/outbox/outbox_repository.dart';
import '../data/photo/location_source.dart';
import '../domain/outbox_record.dart';

/// Hoạt động canh tác chọn bằng biểu tượng (HTX US-LOG-01).
enum FarmActivity {
  lamDat('lam_dat', 'Làm đất'),
  gieoSa('gieo_sa', 'Gieo sạ'),
  tuoiNuoc('tuoi_nuoc', 'Tưới nước'),
  bonPhan('bon_phan', 'Bón phân'),
  phunThuoc('phun_thuoc', 'Phun thuốc'),
  kiemTra('kiem_tra', 'Kiểm tra'),
  thuHoach('thu_hoach', 'Thu hoạch');

  const FarmActivity(this.code, this.label);
  final String code;
  final String label;

  static FarmActivity? byCode(String? c) {
    for (final a in values) {
      if (a.code == c) return a;
    }
    return null;
  }
}

/// Khóa không được đổi khi sửa sai (US-LOG-01 AC-3).
const fieldReportLockedKeys = {'gps', 'gps_status', 'captured_at'};

/// Báo cáo hiện trường / nhật ký canh tác. Tọa độ GPS và thời gian gắn tự động
/// lúc lưu và **bị khóa**: sửa sai tạo bản ghi bù giữ nguyên các trường đó.
class FieldReportService {
  FieldReportService({
    required OutboxRepository outbox,
    required LocationSource location,
    required Clock clock,
  }) : _outbox = outbox,
       _location = location,
       _clock = clock;

  final OutboxRepository _outbox;
  final LocationSource _location;
  final Clock _clock;

  Future<OutboxRecord> create({
    required String plotId,
    required FarmActivity activity,
    String? householdRef,
    String note = '',
    List<String> machineryIds = const [],
    LocationResult? location,
  }) async {
    final loc = location ?? await _location.current();
    final now = _clock.now();
    return _outbox.enqueue(
      entity: PushEntity.fieldReport,
      eventTime: now,
      payload: {
        'plot_id': plotId,
        'household_ref': householdRef,
        'activity': activity.code,
        'note': note.trim(),
        'machinery_ids': machineryIds,
        'captured_at': formatRfc3339(now),
        'gps': loc.fix?.toJson(),
        'gps_status': loc.status.name,
      },
    );
  }

  /// Sửa sai: chỉ đổi hoạt động/ghi chú/máy móc; GPS và thời gian lấy từ bản gốc.
  Future<OutboxRecord> correct({
    required OutboxRecord original,
    required String reason,
    FarmActivity? activity,
    String? note,
    List<String>? machineryIds,
  }) {
    final p = Map<String, Object?>.from(original.payload);
    if (activity != null) p['activity'] = activity.code;
    if (note != null) p['note'] = note.trim();
    if (machineryIds != null) p['machinery_ids'] = machineryIds;
    return _outbox.correct(
      originalId: original.id,
      reason: reason,
      payload: p,
      lockedPayloadKeys: fieldReportLockedKeys,
    );
  }

  Future<List<OutboxRecord>> recent({int limit = 50}) =>
      _outbox.list(entity: PushEntity.fieldReport, limit: limit);
}

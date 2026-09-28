import '../core/clock.dart';
import '../data/db/local_db.dart';
import '../data/outbox/outbox_repository.dart';
import '../domain/consent.dart';
import '../domain/outbox_record.dart';

export '../domain/consent.dart';

/// Phiên bản văn bản đồng ý đang hiển thị trên máy. Đổi nội dung văn bản thì
/// tăng phiên bản (máy chủ lưu `text_version` cùng bản ghi đồng ý).
const mrvConsentTextVersion = 'mrv.evidence/vi/2026-09';

/// Văn bản đồng ý đọc cho nông hộ (tiếng Việt, ngắn gọn).
const mrvConsentText =
    'Cán bộ sẽ chụp ảnh thửa ruộng và hoạt động canh tác của hộ, kèm thời gian '
    'và tọa độ GPS, để làm minh chứng cho chương trình giảm phát thải (MRV). '
    'Ảnh được mã hóa ngay trên máy; chỉ người có thẩm quyền mới xem được, và '
    'chỉ cho mục đích này. Hộ có thể rút lại đồng ý bất cứ lúc nào; ảnh chụp '
    'sau khi rút lại sẽ không được thu.';

enum ConsentStatus {
  granted('granted', 'Đã đồng ý'),
  withdrawn('withdrawn', 'Đã rút lại');

  const ConsentStatus(this.code, this.label);
  final String code;
  final String label;
}

class ConsentState {
  const ConsentState(this.status, this.textVersion, this.eventTime);
  final ConsentStatus status;
  final String textVersion;
  final String eventTime;
  bool get granted => status == ConsentStatus.granted;
}

/// Ghi đồng ý theo mục đích tại hiện trường: bản ghi `consent` vào hàng đợi
/// (Sink pf/party, chỉ `op: create`; rút lại = bản ghi mới `withdrawn`) **và**
/// sổ đồng ý cục bộ `consent_local` trong cùng một giao dịch — để chặn chụp ảnh
/// ngay cả khi ngoại tuyến và sau khi bản ghi hàng đợi đã bị purge.
class ConsentService implements ConsentGate {
  ConsentService({
    required LocalDb db,
    required OutboxRepository outbox,
    required Clock clock,
  }) : _db = db,
       _outbox = outbox,
       _clock = clock;

  final LocalDb _db;
  final OutboxRepository _outbox;
  final Clock _clock;

  Future<OutboxRecord> record({
    required String subjectRef,
    required ConsentStatus status,
    String purpose = mrvEvidencePurpose,
    String textVersion = mrvConsentTextVersion,
  }) {
    final now = _clock.now();
    final eventTime = formatRfc3339(now);
    return _db.db.transaction((tx) async {
      final rec = await _outbox.enqueue(
        entity: PushEntity.consent,
        eventTimeRfc3339: eventTime,
        payload: {
          'subject_ref': subjectRef,
          'purpose': purpose,
          'text_version': textVersion,
          'status': status.code,
          'channel': 'field_app',
        },
        tx: tx,
      );
      await tx.insert('consent_local', {
        'record_id': rec.id,
        'subject_ref': subjectRef,
        'purpose': purpose,
        'text_version': textVersion,
        'status': status.code,
        'event_time': eventTime,
        'recorded_at': now.millisecondsSinceEpoch,
      });
      return rec;
    });
  }

  /// Trạng thái mới nhất trên máy (null = chưa ghi nhận trên máy này).
  Future<ConsentState?> current(
    String subjectRef, {
    String purpose = mrvEvidencePurpose,
  }) async {
    final rows = await _db.db.query(
      'consent_local',
      where: 'subject_ref = ? AND purpose = ?',
      whereArgs: [subjectRef, purpose],
      orderBy: 'recorded_at DESC, rowid DESC',
      limit: 1,
    );
    if (rows.isEmpty) return null;
    final r = rows.first;
    return ConsentState(
      r['status'] == 'granted'
          ? ConsentStatus.granted
          : ConsentStatus.withdrawn,
      r['text_version']! as String,
      r['event_time']! as String,
    );
  }

  @override
  Future<bool> isGranted(String subjectRef, String purpose) async =>
      (await current(subjectRef, purpose: purpose))?.granted ?? false;
}

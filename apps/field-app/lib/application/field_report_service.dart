import '../core/clock.dart';
import '../data/outbox/outbox_repository.dart';
import '../domain/field_report.dart';
import '../domain/outbox_record.dart';

export '../domain/field_report.dart';

/// Báo cáo hiện trường KN (FR-01 tiến độ gieo sạ, FR-02 sinh trưởng – sâu
/// bệnh) → bản ghi `field_report` theo `ReportPayload` của pf/extension.
///
/// Sửa sai = bản ghi bù `correct` (lý do ≥ 10 ký tự); loại, xã, HTX, kỳ (và
/// vụ, giống của báo cáo gieo sạ) giữ nguyên như bản bị thay — máy chủ cũng lấy
/// các trường này từ bản cũ.
class FieldReportService {
  FieldReportService({required OutboxRepository outbox, required Clock clock})
    : _outbox = outbox,
      _clock = clock;

  final OutboxRepository _outbox;
  final Clock _clock;

  Future<OutboxRecord> create(FieldReportDraft draft) async {
    draft.validate(_clock.now());
    return _outbox.enqueue(
      entity: PushEntity.fieldReport,
      eventTime: _clock.now(),
      payload: draft.toPayload(),
    );
  }

  /// Bản ghi bù: đổi số liệu/ghi chú, giữ khóa phiên bản của bản gốc.
  Future<OutboxRecord> correct({
    required OutboxRecord original,
    required String reason,
    FieldReportDraft? changes,
    String? note,
  }) async {
    if (reason.trim().runes.length < 10) {
      throw const FieldReportError('correction_reason', 'bắt buộc, ≥ 10 ký tự');
    }
    final p = Map<String, Object?>.from(original.payload);
    final kind = FieldReportKind.parse(p['kind'] as String?);
    if (changes != null) {
      // Chỉ số liệu của đúng loại báo cáo gốc được sửa.
      final editable = kind == FieldReportKind.sowing
          ? const {'sown_area_ha', 'sown_on', 'urgent', 'note'}
          : const {
              'growth_stage',
              'pest_code',
              'severity',
              'affected_area_ha',
              'urgent',
              'note',
            };
      final c = FieldReportDraft(
        kind: kind ?? changes.kind,
        communeCode: changes.communeCode,
        period: changes.period,
        cooperativeRef: changes.cooperativeRef,
        sownAreaHa: changes.sownAreaHa,
        sownOn: changes.sownOn,
        growthStage: changes.growthStage,
        pestCode: changes.pestCode,
        severity: changes.severity,
        affectedAreaHa: changes.affectedAreaHa,
        urgent: changes.urgent,
        note: changes.note,
      ).toPayload();
      for (final k in editable) {
        p[k] = c[k];
      }
    }
    if (note != null) p['note'] = note.trim();
    if (containsPii('${p['note'] ?? ''}')) {
      throw const FieldReportError(
        'note',
        'không ghi số điện thoại hoặc số CCCD vào ghi chú',
      );
    }
    return _outbox.correct(
      originalId: original.id,
      reason: reason,
      payload: p,
      lockedPayloadKeys: {
        ...fieldReportLockedKeys,
        if (kind == FieldReportKind.sowing) ...sowingLockedKeys,
      },
    );
  }

  Future<List<OutboxRecord>> recent({int limit = 50}) =>
      _outbox.list(entity: PushEntity.fieldReport, limit: limit);
}

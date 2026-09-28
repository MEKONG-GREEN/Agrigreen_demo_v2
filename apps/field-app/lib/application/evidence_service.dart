import '../core/clock.dart';
import '../core/ids.dart';
import '../data/catalog/catalog_cache.dart';
import '../data/files/evidence_repository.dart';
import '../data/photo/location_source.dart';
import '../data/photo/photo_pipeline.dart';
import '../domain/consent.dart';
import '../domain/mrv.dart';

class EvidenceFormException implements Exception {
  EvidenceFormException(this.message, [this.errors = const []]);
  final String message;
  final List<MrvFieldError> errors;
  @override
  String toString() => message;
}

/// Thu minh chứng MRV theo **phiên bản danh mục đã duyệt** (đệm từ
/// `GET /v1/mrv/catalog`), payload `CapturePayload` của pf/mrv.
///
/// Luồng: mở biểu mẫu (id UUIDv7) → chụp ảnh (mã hóa ngay, `evidence_file`
/// gắn biểu mẫu) → nhập giá trị → [submit] kiểm như máy chủ → lưu biểu mẫu →
/// SyncService tải ảnh → khi mọi ảnh đã tải, tạo bản ghi `evidence` với
/// `file_refs` = file_ref + sha256 ciphertext.
class EvidenceService {
  EvidenceService({
    required CatalogCache catalogs,
    required EvidenceRepository evidence,
    required PhotoPipeline photos,
    required LocationSource location,
    required ConsentGate consent,
    required Clock clock,
    required IdGenerator ids,
  }) : _catalogs = catalogs,
       _evidence = evidence,
       _photos = photos,
       _location = location,
       _consent = consent,
       _clock = clock,
       _ids = ids;

  final CatalogCache _catalogs;
  final EvidenceRepository _evidence;
  final PhotoPipeline _photos;
  final LocationSource _location;
  final ConsentGate _consent;
  final Clock _clock;
  final IdGenerator _ids;

  static const formParent = 'evidence_form';

  Future<List<MrvCatalog>> catalogs() => _catalogs.mrvCatalogs();

  /// Id biểu mẫu mới (ảnh chụp gắn vào id này).
  String newFormId() => _ids.newId();

  /// Lý do không thu được cho hộ (null = được): đồng ý + khóa công khai.
  Future<String?> blockReason(String householdRef) =>
      _photos.blockReason(householdRef);

  Future<CaptureOutcome?> capturePhoto({
    required String formId,
    required String householdRef,
  }) => _photos.capture(
    subjectRef: householdRef,
    parentEntity: formParent,
    parentId: formId,
  );

  Future<List<EvidenceFile>> photosOf(String formId) =>
      _evidence.forParent(formId);

  /// GPS hiện tại dạng chuỗi decimal (null nếu không có fix).
  Future<MrvGps?> currentGps() async {
    final l = await _location.current();
    final f = l.fix;
    return f == null ? null : MrvGps.fromDegrees(f.lat, f.lon, f.accuracyM);
  }

  /// Kiểm theo danh mục đã ghim + phong bì; lưu biểu mẫu chờ ảnh tải lên.
  /// `file_refs` của [draft] được thay bằng ảnh đã chụp cho [formId].
  Future<EvidenceForm> submit({
    required String formId,
    required MrvEvidenceDraft draft,
  }) async {
    final cat = await _catalogs.mrvCatalog(draft.evidenceType);
    if (cat == null || cat.version != draft.catalogVersion) {
      throw EvidenceFormException(
        'Danh mục trường "${draft.evidenceType}" phiên bản '
        '${draft.catalogVersion} chưa có trên máy. Hãy đồng bộ khi có mạng.',
      );
    }
    final hh = draft.householdRef;
    if (hh != null && !await _consent.isGranted(hh, mrvEvidencePurpose)) {
      throw EvidenceFormException(ConsentRequiredException(hh).message);
    }
    final photos = await photosOf(formId);
    final withFiles = draft.copyWith(
      fileRefs: [
        for (final p in photos)
          MrvFileRef(p.id, '${p.meta['sha256_ciphertext']}'.toLowerCase()),
      ],
    );
    final errs = [...withFiles.validateEnvelope(), ...cat.validate(withFiles)];
    if (errs.isNotEmpty) {
      throw EvidenceFormException(
        'Biểu mẫu chưa hợp lệ: ${errs.first.reason}',
        errs,
      );
    }
    await _evidence.addForm(
      id: formId,
      draft: draft,
      eventTime: formatRfc3339(_clock.now()),
    );
    return (await _evidence.forms()).firstWhere((f) => f.id == formId);
  }

  Future<List<EvidenceForm>> forms() => _evidence.forms();
}

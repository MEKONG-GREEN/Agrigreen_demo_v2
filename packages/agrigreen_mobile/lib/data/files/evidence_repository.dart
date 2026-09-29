import 'dart:convert';

import '../../core/clock.dart';
import '../../domain/mrv.dart';
import '../../domain/outbox_record.dart';
import '../crypto/envelope.dart';
import '../db/local_db.dart';
import '../outbox/outbox_repository.dart';
import 'blob_store.dart';

class EvidenceFile {
  const EvidenceFile({
    required this.id,
    required this.subjectRef,
    required this.meta,
    required this.blobPath,
    required this.sizeBytes,
    required this.status,
    this.parentEntity,
    this.parentId,
    this.fileRef,
    this.evidenceRecordId,
    this.attempts = 0,
    this.lastError,
  });

  final String id;
  final String subjectRef;
  final Map<String, Object?> meta;
  final String blobPath;
  final int sizeBytes;

  /// pending | uploaded | failed
  final String status;
  final String? parentEntity;
  final String? parentId;
  final String? fileRef;
  final String? evidenceRecordId;
  final int attempts;
  final String? lastError;

  factory EvidenceFile.fromRow(Map<String, Object?> r) => EvidenceFile(
    id: r['id']! as String,
    subjectRef: r['subject_ref']! as String,
    meta: (jsonDecode(r['meta']! as String) as Map).cast<String, Object?>(),
    blobPath: r['blob_path']! as String,
    sizeBytes: r['size_bytes']! as int,
    status: r['status']! as String,
    parentEntity: r['parent_entity'] as String?,
    parentId: r['parent_id'] as String?,
    fileRef: r['file_ref'] as String?,
    evidenceRecordId: r['evidence_record'] as String?,
    attempts: r['attempts']! as int,
    lastError: r['last_error'] as String?,
  );
}

/// Tệp minh chứng đã mã hóa phong bì, chờ `POST /v1/files`.
class EvidenceRepository {
  EvidenceRepository({
    required LocalDb db,
    required BlobStore blobs,
    required Clock clock,
  }) : _db = db,
       _blobs = blobs,
       _clock = clock;

  final LocalDb _db;
  final BlobStore _blobs;
  final Clock _clock;

  BlobStore get blobs => _blobs;

  Future<EvidenceFile> addSealed({
    required String id,
    required String subjectRef,
    required SealedEnvelope envelope,
    String? parentEntity,
    String? parentId,
  }) async {
    final path = await _blobs.write(id, envelope.ciphertext);
    await _db.db.insert('evidence_file', {
      'id': id,
      'subject_ref': subjectRef,
      'parent_entity': parentEntity,
      'parent_id': parentId,
      'meta': jsonEncode(envelope.meta),
      'blob_path': path,
      'size_bytes': envelope.ciphertext.length,
      'status': 'pending',
      'created_at': _clock.now().millisecondsSinceEpoch,
    });
    return (await byId(id))!;
  }

  Future<EvidenceFile?> byId(String id) async {
    final rows = await _db.db.query(
      'evidence_file',
      where: 'id = ?',
      whereArgs: [id],
    );
    return rows.isEmpty ? null : EvidenceFile.fromRow(rows.first);
  }

  Future<List<EvidenceFile>> pending() async {
    final rows = await _db.db.query(
      'evidence_file',
      where: "status = 'pending'",
      orderBy: 'created_at',
    );
    return rows.map(EvidenceFile.fromRow).toList();
  }

  Future<List<EvidenceFile>> forParent(String parentId) async {
    final rows = await _db.db.query(
      'evidence_file',
      where: 'parent_id = ?',
      whereArgs: [parentId],
      orderBy: 'created_at',
    );
    return rows.map(EvidenceFile.fromRow).toList();
  }

  Future<void> recordFailure(String id, String error, {bool fatal = false}) =>
      _db.db.rawUpdate(
        'UPDATE evidence_file SET attempts = attempts + 1, last_error = ?, '
        'status = ? WHERE id = ?',
        [error, fatal ? 'failed' : 'pending', id],
      );

  /// Sau khi máy chủ trả `file_ref`: đánh dấu đã tải rồi bỏ ciphertext cục bộ.
  /// Bản ghi `evidence` tạo ở [flushForms] khi **mọi** ảnh của biểu mẫu đã tải.
  Future<void> markUploaded(EvidenceFile f, String fileRef) async {
    await _db.db.update(
      'evidence_file',
      {'status': 'uploaded', 'file_ref': fileRef, 'last_error': null},
      where: 'id = ?',
      whereArgs: [f.id],
    );
    // Ciphertext đã nằm ở kho đối tượng; bỏ bản cục bộ để tiết kiệm bộ nhớ.
    await _blobs.delete(f.blobPath);
  }

  /// Lưu biểu mẫu minh chứng MRV (payload `CapturePayload` chưa có
  /// `file_refs`). Ảnh của biểu mẫu là `evidence_file` có
  /// `parent_entity = evidence_form`, `parent_id = id`.
  Future<void> addForm({
    required String id,
    required MrvEvidenceDraft draft,
    required String eventTime,
  }) => _db.db.insert('evidence_form', {
    'id': id,
    'payload': jsonEncode(draft.toPayload()),
    'event_time': eventTime,
    'status': 'waiting',
    'created_at': _clock.now().millisecondsSinceEpoch,
  });

  Future<List<EvidenceForm>> forms({String? status}) async {
    final rows = await _db.db.query(
      'evidence_form',
      where: status == null ? null : 'status = ?',
      whereArgs: status == null ? null : [status],
      orderBy: 'created_at DESC',
    );
    return rows.map(EvidenceForm.fromRow).toList();
  }

  /// Biểu mẫu có mọi ảnh đã tải → tạo bản ghi `evidence` (file_refs = file_ref
  /// + sha256 ciphertext do máy chủ xác nhận) **trong cùng giao dịch** với việc
  /// đánh dấu biểu mẫu đã xếp hàng — không mất, không trùng. Ảnh hỏng vĩnh viễn
  /// → biểu mẫu `blocked` (hiển thị, không gửi thiếu ảnh).
  Future<List<OutboxRecord>> flushForms(OutboxRepository outbox) async {
    final out = <OutboxRecord>[];
    for (final form in await forms(status: 'waiting')) {
      final files = await forParent(form.id);
      if (files.any((f) => f.status == 'failed')) {
        await _db.db.update(
          'evidence_form',
          {
            'status': 'blocked',
            'last_error': files
                .where((f) => f.status == 'failed')
                .map((f) => f.lastError)
                .join(', '),
          },
          where: 'id = ?',
          whereArgs: [form.id],
        );
        continue;
      }
      if (files.any((f) => f.status != 'uploaded')) continue;
      final byLocal = {for (final f in files) f.id: f.fileRef ?? f.id};
      final draft = form.draft;
      final values = <String, Object>{
        for (final e in draft.values.entries)
          e.key: e.value is List
              ? [for (final r in e.value as List) byLocal['$r'] ?? '$r']
              : e.value,
      };
      final payload = MrvEvidenceDraft(
        evidenceType: draft.evidenceType,
        catalogVersion: draft.catalogVersion,
        adminUnitCode: draft.adminUnitCode,
        plotRef: draft.plotRef,
        householdRef: draft.householdRef,
        seasonCode: draft.seasonCode,
        values: values,
        gps: draft.gps,
        fileRefs: [
          for (final f in files)
            MrvFileRef(
              f.fileRef ?? f.id,
              '${f.meta['sha256_ciphertext']}'.toLowerCase(),
            ),
        ],
      ).toPayload();
      final rec = await _db.db.transaction((tx) async {
        final cur = await tx.query(
          'evidence_form',
          columns: ['status', 'record_id'],
          where: 'id = ?',
          whereArgs: [form.id],
        );
        if (cur.isEmpty || cur.first['status'] != 'waiting') return null;
        final r = await outbox.enqueue(
          entity: PushEntity.evidence,
          eventTimeRfc3339: form.eventTime,
          payload: payload,
          tx: tx,
        );
        await tx.update(
          'evidence_form',
          {'status': 'queued', 'record_id': r.id, 'last_error': null},
          where: 'id = ?',
          whereArgs: [form.id],
        );
        return r;
      });
      if (rec != null) out.add(rec);
    }
    return out;
  }

  Future<int> purgeUploaded(Duration retention) {
    final cutoff = _clock.now().subtract(retention).millisecondsSinceEpoch;
    return _db.db.delete(
      'evidence_file',
      where: "status = 'uploaded' AND created_at <= ?",
      whereArgs: [cutoff],
    );
  }
}

/// Biểu mẫu minh chứng MRV chờ ảnh tải lên.
class EvidenceForm {
  const EvidenceForm({
    required this.id,
    required this.draft,
    required this.eventTime,
    required this.status,
    this.recordId,
    this.lastError,
  });

  final String id;
  final MrvEvidenceDraft draft;
  final String eventTime;

  /// waiting | queued | blocked
  final String status;
  final String? recordId;
  final String? lastError;

  factory EvidenceForm.fromRow(Map<String, Object?> r) => EvidenceForm(
    id: r['id']! as String,
    draft: MrvEvidenceDraft.fromPayload(
      (jsonDecode(r['payload']! as String) as Map).cast<String, Object?>(),
    ),
    eventTime: r['event_time']! as String,
    status: r['status']! as String,
    recordId: r['record_id'] as String?,
    lastError: r['last_error'] as String?,
  );
}

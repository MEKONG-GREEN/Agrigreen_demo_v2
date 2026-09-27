import 'dart:convert';

import '../../core/clock.dart';
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

  /// Sau khi máy chủ trả `file_ref`: đánh dấu đã tải **và** tạo bản ghi
  /// `evidence` trong hàng đợi trong cùng một giao dịch — không mất, không trùng.
  Future<OutboxRecord> markUploaded(
    EvidenceFile f,
    String fileRef,
    OutboxRepository outbox,
  ) async {
    final rec = await _db.db.transaction((tx) async {
      final cur = await tx.query(
        'evidence_file',
        columns: ['status', 'evidence_record'],
        where: 'id = ?',
        whereArgs: [f.id],
      );
      if (cur.isNotEmpty && cur.first['status'] == 'uploaded') {
        final existingId = cur.first['evidence_record'] as String?;
        final existing = existingId == null
            ? null
            : await outbox.byId(existingId, tx: tx);
        if (existing != null) return existing;
      }
      final r = await outbox.enqueue(
        entity: PushEntity.evidence,
        eventTimeRfc3339: f.meta['captured_at'] as String?,
        payload: {
          'file_ref': fileRef,
          'subject_ref': f.subjectRef,
          'parent_entity': f.parentEntity,
          'parent_id': f.parentId,
          'content_type': f.meta['content_type'],
          'captured_at': f.meta['captured_at'],
          'sha256_ciphertext': f.meta['sha256_ciphertext'],
          'gps_status': f.meta['gps'] == null ? 'missing' : 'ok',
        },
        tx: tx,
      );
      await tx.update(
        'evidence_file',
        {
          'status': 'uploaded',
          'file_ref': fileRef,
          'evidence_record': r.id,
          'last_error': null,
        },
        where: 'id = ?',
        whereArgs: [f.id],
      );
      return r;
    });
    // Ciphertext đã nằm ở kho đối tượng; bỏ bản cục bộ để tiết kiệm bộ nhớ.
    await _blobs.delete(f.blobPath);
    return rec;
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

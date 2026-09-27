import 'package:sqflite_common/sqlite_api.dart';

/// Lược đồ CSDL cục bộ (mã hóa SQLCipher trên máy).
///
/// - `outbox`: hàng đợi chiều lên, **chỉ ghi thêm** về nội dung; chỉ các cột
///   trạng thái gửi được đổi. Không có API sửa `payload`.
/// - `ref_entity`: dữ liệu tham chiếu chiều xuống (upsert/tombstone theo version).
/// - `household_pubkey`: khóa công khai X25519 của hộ thuộc địa bàn.
/// - `evidence_file`: ciphertext ảnh đã mã hóa phong bì, chờ tải lên.
/// - `meta`: bộ đếm seq, cursor pull, trạng thái lùi thử lại.
abstract final class Schema {
  static const version = 1;

  static Future<void> create(Database db) async {
    final b = db.batch();
    b.execute('''
CREATE TABLE meta (
  key   TEXT PRIMARY KEY,
  value TEXT
)''');
    b.execute('''
CREATE TABLE outbox (
  id                TEXT PRIMARY KEY,
  seq               INTEGER NOT NULL UNIQUE,
  idem_key          TEXT NOT NULL UNIQUE,
  entity            TEXT NOT NULL,
  op                TEXT NOT NULL CHECK (op IN ('create','correct')),
  event_time        TEXT NOT NULL,
  recorded_at       INTEGER NOT NULL,
  supersedes_id     TEXT,
  correction_reason TEXT,
  payload           TEXT NOT NULL,
  status            TEXT NOT NULL DEFAULT 'pending'
                    CHECK (status IN ('pending','sent','conflict','rejected','superseded')),
  batch_key         TEXT,
  attempts          INTEGER NOT NULL DEFAULT 0,
  acked_at          INTEGER,
  conflict_id       TEXT,
  error_code        TEXT,
  errors            TEXT,
  CHECK (op = 'create' OR (supersedes_id IS NOT NULL AND correction_reason IS NOT NULL))
)''');
    b.execute('CREATE INDEX outbox_status_seq ON outbox(status, seq)');
    b.execute('CREATE INDEX outbox_batch ON outbox(batch_key)');
    b.execute('CREATE INDEX outbox_supersedes ON outbox(supersedes_id)');
    // Nội dung bản ghi không được sửa sau khi lưu (append-only, US-LOG-01 AC-3).
    b.execute('''
CREATE TRIGGER outbox_immutable BEFORE UPDATE OF
  id, seq, idem_key, entity, op, event_time, recorded_at, supersedes_id,
  correction_reason, payload ON outbox
BEGIN
  SELECT RAISE(ABORT, 'outbox_append_only');
END''');
    b.execute('''
CREATE TABLE ref_entity (
  entity     TEXT NOT NULL,
  id         TEXT NOT NULL,
  version    INTEGER NOT NULL,
  deleted    INTEGER NOT NULL DEFAULT 0,
  data       TEXT,
  updated_at INTEGER NOT NULL,
  PRIMARY KEY (entity, id)
)''');
    b.execute('CREATE INDEX ref_entity_live ON ref_entity(entity, deleted)');
    b.execute('''
CREATE TABLE household_pubkey (
  subject_ref TEXT PRIMARY KEY,
  public_key  BLOB NOT NULL,
  key_id      TEXT,
  version     INTEGER NOT NULL,
  deleted     INTEGER NOT NULL DEFAULT 0
)''');
    b.execute('''
CREATE TABLE evidence_file (
  id              TEXT PRIMARY KEY,
  subject_ref     TEXT NOT NULL,
  parent_entity   TEXT,
  parent_id       TEXT,
  meta            TEXT NOT NULL,
  blob_path       TEXT NOT NULL,
  size_bytes      INTEGER NOT NULL,
  status          TEXT NOT NULL DEFAULT 'pending'
                  CHECK (status IN ('pending','uploaded','failed')),
  file_ref        TEXT,
  evidence_record TEXT,
  attempts        INTEGER NOT NULL DEFAULT 0,
  last_error      TEXT,
  created_at      INTEGER NOT NULL
)''');
    await b.commit(noResult: true);
  }

  static Future<void> upgrade(Database db, int from, int to) async {
    // Chưa có phiên bản nào sau v1. Migration cục bộ chỉ tiến.
  }
}

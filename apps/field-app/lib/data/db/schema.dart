import 'package:sqflite_common/sqlite_api.dart';

/// Lược đồ CSDL cục bộ (mã hóa SQLCipher trên máy).
///
/// - `outbox`: hàng đợi chiều lên, **chỉ ghi thêm** về nội dung; chỉ các cột
///   trạng thái gửi được đổi. Không có API sửa `payload`.
/// - `ref_entity`: dữ liệu tham chiếu chiều xuống (upsert/tombstone theo version).
/// - `household_pubkey`: khóa công khai X25519 của hộ thuộc địa bàn.
/// - `evidence_file`: ciphertext ảnh đã mã hóa phong bì, chờ tải lên.
/// - `meta`: bộ đếm seq, cursor pull, trạng thái lùi thử lại.
/// - `api_action` (v2): lệnh API idempotent xếp hàng khi ngoại tuyến (đánh dấu
///   đã đọc, xác nhận chỉ đạo, lưu/bỏ lưu tài liệu), phát lại khi có mạng.
/// - `consent_local` (v2): sổ đồng ý ghi trên máy (chỉ mã tham chiếu hộ).
/// - `tile_pack`, `map_tile` (v2): gói bản đồ ngoại tuyến (nằm trong SQLCipher).
abstract final class Schema {
  static const version = 2;

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
  replaced_by       TEXT,
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
    _createV2Tables(b);
    await b.commit(noResult: true);
  }

  static void _createV2Tables(Batch b) {
    b.execute('''
CREATE TABLE api_action (
  id          TEXT PRIMARY KEY,
  seq         INTEGER NOT NULL UNIQUE,
  kind        TEXT NOT NULL,
  target      TEXT,
  body        TEXT,
  status      TEXT NOT NULL DEFAULT 'pending'
              CHECK (status IN ('pending','done','failed')),
  attempts    INTEGER NOT NULL DEFAULT 0,
  last_error  TEXT,
  created_at  INTEGER NOT NULL,
  done_at     INTEGER
)''');
    b.execute('CREATE INDEX api_action_status ON api_action(status, seq)');
    b.execute('CREATE INDEX api_action_target ON api_action(kind, target)');
    b.execute('''
CREATE TABLE consent_local (
  record_id    TEXT PRIMARY KEY,
  subject_ref  TEXT NOT NULL,
  purpose      TEXT NOT NULL,
  text_version TEXT NOT NULL,
  status       TEXT NOT NULL CHECK (status IN ('granted','withdrawn')),
  event_time   TEXT NOT NULL,
  recorded_at  INTEGER NOT NULL
)''');
    b.execute(
      'CREATE INDEX consent_local_subject ON consent_local(subject_ref, purpose, recorded_at)',
    );
    b.execute('''
CREATE TABLE tile_pack (
  id          TEXT PRIMARY KEY,
  name        TEXT NOT NULL,
  min_lat     REAL NOT NULL,
  min_lon     REAL NOT NULL,
  max_lat     REAL NOT NULL,
  max_lon     REAL NOT NULL,
  min_zoom    INTEGER NOT NULL,
  max_zoom    INTEGER NOT NULL,
  tile_count  INTEGER NOT NULL DEFAULT 0,
  bytes       INTEGER NOT NULL DEFAULT 0,
  status      TEXT NOT NULL DEFAULT 'downloading'
              CHECK (status IN ('downloading','ready','failed')),
  created_at  INTEGER NOT NULL
)''');
    b.execute('''
CREATE TABLE map_tile (
  pack_id TEXT NOT NULL,
  z       INTEGER NOT NULL,
  x       INTEGER NOT NULL,
  y       INTEGER NOT NULL,
  data    BLOB NOT NULL,
  PRIMARY KEY (pack_id, z, x, y)
)''');
    b.execute('CREATE INDEX map_tile_zxy ON map_tile(z, x, y)');
  }

  static Future<void> upgrade(Database db, int from, int to) async {
    // Migration cục bộ chỉ tiến.
    if (from < 2) {
      final b = db.batch();
      b.execute('ALTER TABLE outbox ADD COLUMN replaced_by TEXT');
      _createV2Tables(b);
      await b.commit(noResult: true);
    }
  }
}

import 'dart:convert';
import 'dart:typed_data';

import 'package:sqflite_common/sqlite_api.dart';

import '../../core/clock.dart';
import '../db/local_db.dart';
import '../sync/sync_api.dart';

/// Thực thể chiều xuống G1 và G2 (App HTX).
abstract final class RefEntity {
  /// G2 pf/cropcycle: phiên bản SOP đã phát hành (bất biến).
  static const sopVersion = 'sop_version';

  /// G2 pf/cropcycle: vụ trong phạm vi (`id` = season_ref).
  static const season = 'season';

  /// G2 pf/cropcycle: phiên bản nhật ký kèm trạng thái duyệt.
  static const farmLog = 'farm_log';

  /// G2 pf/extension: yêu cầu hỗ trợ của nông hộ (vai farmer).
  static const supportRequest = 'support_request';

  static const catalogItem = 'catalog_item';
  static const adminUnit = 'admin_unit';
  static const cooperative = 'cooperative';
  static const household = 'household';
  static const plot = 'plot';
  static const task = 'task';
  static const directive = 'directive';
  static const document = 'document';
  static const pubkey = 'pubkey';
  static const notification = 'notification';
  static const libraryItem = 'library_item';
}

/// Bản ghi đánh dấu kết thúc một "lượt" khóa công khai (sync-contract, pubkey):
/// máy xóa mọi khóa có version nhỏ hơn version của bản ghi này.
const pubkeyRoundMarkerId = '00000000-0000-0000-0000-000000000000';

/// Máy tự bỏ thông báo cũ hơn 30 ngày (sync-contract, notification).
const notificationRetention = Duration(days: 30);

/// Trường nông hộ được phép lưu trên máy (mục 10.4: chỉ nhãn hiển thị tối
/// thiểu — không họ tên đầy đủ, SĐT, CCCD, địa chỉ).
const householdAllowedFields = {
  'label',
  'display_label',
  'code',
  'cooperative_id',
  'admin_unit_code',
  'status',
};

class RefRow {
  const RefRow(this.entity, this.id, this.version, this.data);
  final String entity;
  final String id;
  final int version;
  final Map<String, Object?> data;
}

/// Giải mã khóa công khai X25519 32 byte từ hex hoặc base64 (hợp đồng chưa
/// chốt mã hóa chuỗi — xem README "Điểm chưa rõ trong hợp đồng").
Uint8List? decodePublicKey(String s) {
  final t = s.trim();
  if (RegExp(r'^[0-9a-fA-F]{64}$').hasMatch(t)) {
    return Uint8List.fromList([
      for (var i = 0; i < 64; i += 2)
        int.parse(t.substring(i, i + 2), radix: 16),
    ]);
  }
  try {
    final b = base64.decode(
      base64.normalize(t.replaceAll('-', '+').replaceAll('_', '/')),
    );
    return b.length == 32 ? Uint8List.fromList(b) : null;
  } catch (_) {
    return null;
  }
}

class ReferenceRepository {
  ReferenceRepository(this._db, this._clock);
  final LocalDb _db;
  final Clock _clock;

  static const cursorKey = 'pull.cursor';
  static const lastPullKey = 'pull.last_at';

  Future<String?> cursor() => _db.getMeta(cursorKey);

  /// Áp một trang pull **và** con trỏ mới trong cùng một giao dịch: app chết
  /// giữa chừng thì lần sau pull lại từ con trỏ cũ (áp lặp là vô hại vì so
  /// version).
  Future<void> applyPage(PullPage page) {
    return _db.db.transaction((tx) async {
      if (page.reset) {
        // Phạm vi đã đổi (điều chuyển, sáp nhập xã): thay toàn bộ dữ liệu tham
        // chiếu bằng kết quả lượt mới.
        await tx.delete('ref_entity');
        await tx.delete('household_pubkey');
      }
      for (final c in page.changes) {
        await _apply(tx, c);
      }
      if (page.nextCursor != null) {
        await _db.setMeta(cursorKey, page.nextCursor, tx: tx);
      }
      await _db.setMeta(
        lastPullKey,
        '${_clock.now().millisecondsSinceEpoch}',
        tx: tx,
      );
    });
  }

  Future<void> _apply(Transaction tx, PullChange c) async {
    if (c.entity == RefEntity.pubkey) {
      await _applyPubkey(tx, c);
      return;
    }
    final existing = await tx.query(
      'ref_entity',
      columns: ['version'],
      where: 'entity = ? AND id = ?',
      whereArgs: [c.entity, c.id],
    );
    if (existing.isNotEmpty &&
        (existing.first['version']! as int) >= c.version) {
      return; // bản cũ hơn hoặc đã áp
    }
    final now = _clock.now().millisecondsSinceEpoch;
    if (c.op == 'tombstone') {
      await tx.insert('ref_entity', {
        'entity': c.entity,
        'id': c.id,
        'version': c.version,
        'deleted': 1,
        'data': null, // hết phạm vi → bỏ dữ liệu khỏi máy
        'updated_at': now,
      }, conflictAlgorithm: ConflictAlgorithm.replace);
      return;
    }
    var data = c.data ?? const <String, Object?>{};
    if (c.entity == RefEntity.household) {
      data = {
        for (final e in data.entries)
          if (householdAllowedFields.contains(e.key)) e.key: e.value,
      };
    }
    await tx.insert('ref_entity', {
      'entity': c.entity,
      'id': c.id,
      'version': c.version,
      'deleted': 0,
      'data': jsonEncode(data),
      'updated_at': now,
    }, conflictAlgorithm: ConflictAlgorithm.replace);
  }

  Future<void> _applyPubkey(Transaction tx, PullChange c) async {
    if (c.id == pubkeyRoundMarkerId && c.op == 'tombstone') {
      // Kết thúc lượt: khóa không có trong lượt (version cũ hơn) bị bỏ.
      await tx.delete(
        'household_pubkey',
        where: 'version < ?',
        whereArgs: [c.version],
      );
      return;
    }
    final subject =
        (c.data?['ref'] as String?) ??
        (c.data?['subject_ref'] as String?) ??
        c.id;
    final existing = await tx.query(
      'household_pubkey',
      columns: ['version'],
      where: 'subject_ref = ?',
      whereArgs: [subject],
    );
    if (existing.isNotEmpty &&
        (existing.first['version']! as int) >= c.version) {
      return;
    }
    if (c.op == 'tombstone') {
      await tx.insert('household_pubkey', {
        'subject_ref': subject,
        'public_key': Uint8List(0),
        'key_id': null,
        'version': c.version,
        'deleted': 1,
      }, conflictAlgorithm: ConflictAlgorithm.replace);
      return;
    }
    final raw = c.data?['public_key'];
    final key = raw is String ? decodePublicKey(raw) : null;
    if (key == null) return; // khóa hỏng — bỏ qua, không làm hỏng cả trang
    await tx.insert('household_pubkey', {
      'subject_ref': subject,
      'public_key': key,
      'key_id': c.data?['key_id'] as String?,
      'version': c.version,
      'deleted': 0,
    }, conflictAlgorithm: ConflictAlgorithm.replace);
  }

  Future<List<RefRow>> list(String entity) async {
    final rows = await _db.db.query(
      'ref_entity',
      where: 'entity = ? AND deleted = 0',
      whereArgs: [entity],
      orderBy: 'id',
    );
    return rows
        .map(
          (r) => RefRow(
            entity,
            r['id']! as String,
            r['version']! as int,
            (jsonDecode(r['data']! as String) as Map).cast<String, Object?>(),
          ),
        )
        .toList();
  }

  Future<RefRow?> get(String entity, String id) async {
    final rows = await _db.db.query(
      'ref_entity',
      where: 'entity = ? AND id = ? AND deleted = 0',
      whereArgs: [entity, id],
    );
    if (rows.isEmpty) return null;
    final r = rows.first;
    return RefRow(
      entity,
      id,
      r['version']! as int,
      (jsonDecode(r['data']! as String) as Map).cast<String, Object?>(),
    );
  }

  Future<int> count(String entity) async {
    final r = await _db.db.rawQuery(
      'SELECT COUNT(*) AS n FROM ref_entity WHERE entity = ? AND deleted = 0',
      [entity],
    );
    return r.first['n']! as int;
  }

  /// Khóa công khai của hộ (null nếu chưa đồng bộ hoặc đã hết phạm vi).
  Future<Uint8List?> publicKeyFor(String subjectRef) async {
    final rows = await _db.db.query(
      'household_pubkey',
      where: 'subject_ref = ? AND deleted = 0',
      whereArgs: [subjectRef],
    );
    if (rows.isEmpty) return null;
    return rows.first['public_key']! as Uint8List;
  }

  /// Bỏ thông báo cũ hơn [notificationRetention] khỏi máy.
  Future<int> purgeOldNotifications() async {
    final cutoff = _clock.now().toUtc().subtract(notificationRetention);
    var n = 0;
    for (final r in await list(RefEntity.notification)) {
      final t = DateTime.tryParse('${r.data['created_at']}');
      if (t != null && t.isBefore(cutoff)) {
        n += await _db.db.delete(
          'ref_entity',
          where: 'entity = ? AND id = ?',
          whereArgs: [RefEntity.notification, r.id],
        );
      }
    }
    return n;
  }

  Future<DateTime?> lastPullAt() async {
    final v = int.tryParse(await _db.getMeta(lastPullKey) ?? '');
    return v == null ? null : DateTime.fromMillisecondsSinceEpoch(v);
  }
}

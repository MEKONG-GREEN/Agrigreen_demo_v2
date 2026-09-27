import 'dart:convert';
import 'dart:math';

import 'package:sqflite_common/sqlite_api.dart';

import '../secure/secure_store.dart';
import 'schema.dart';

/// Trừu tượng hóa việc mở CSDL để trên máy dùng SQLCipher
/// (`sqflite_sqlcipher`), còn kiểm thử Dart VM dùng `sqflite_common_ffi`.
abstract class DbOpener {
  Future<Database> open(
    String path, {
    required String key,
    required int version,
    required OnDatabaseCreateFn onCreate,
    OnDatabaseVersionChangeFn? onUpgrade,
  });

  /// Xóa tệp CSDL (dùng khi xóa dữ liệu từ xa).
  Future<void> deleteDatabase(String path);
}

/// Khóa SQLCipher 256 bit sinh ngẫu nhiên lần đầu, lưu trong kho bí mật.
class DbKeyProvider {
  DbKeyProvider(this._store, {Random? random})
    : _random = random ?? Random.secure();
  final SecureStore _store;
  final Random _random;

  Future<String> obtainKey() async {
    final existing = await _store.read(SecureKeys.dbKey);
    if (existing != null && existing.length == 64) return existing;
    final bytes = List<int>.generate(32, (_) => _random.nextInt(256));
    final hex = bytes.map((b) => b.toRadixString(16).padLeft(2, '0')).join();
    await _store.write(SecureKeys.dbKey, hex);
    return hex;
  }

  Future<bool> hasKey() async => (await _store.read(SecureKeys.dbKey)) != null;
}

/// CSDL cục bộ đã mở. Mọi repository nhận đối tượng này.
class LocalDb {
  LocalDb._(this.db, this.path, this._opener);

  final Database db;
  final String path;
  final DbOpener _opener;

  static Future<LocalDb> open({
    required DbOpener opener,
    required String path,
    required DbKeyProvider keys,
  }) async {
    final key = await keys.obtainKey();
    // SQLCipher nhận passphrase dạng raw key: x'<hex>' để không phải chạy KDF.
    final db = await opener.open(
      path,
      key: "x'$key'",
      version: Schema.version,
      onCreate: (db, v) => Schema.create(db),
      onUpgrade: (db, from, to) => Schema.upgrade(db, from, to),
    );
    return LocalDb._(db, path, opener);
  }

  Future<void> close() => db.close();

  /// Đóng và xóa tệp CSDL. Không thể khôi phục.
  Future<void> destroy() async {
    try {
      await db.close();
    } catch (_) {
      // đã đóng
    }
    await _opener.deleteDatabase(path);
  }

  // ---- bảng meta: bộ đếm và trạng thái đồng bộ ----

  Future<String?> getMeta(String key, {DatabaseExecutor? tx}) async {
    final rows = await (tx ?? db).query(
      'meta',
      columns: ['value'],
      where: 'key = ?',
      whereArgs: [key],
    );
    if (rows.isEmpty) return null;
    return rows.first['value'] as String?;
  }

  Future<void> setMeta(
    String key,
    String? value, {
    DatabaseExecutor? tx,
  }) async {
    await (tx ?? db).insert('meta', {
      'key': key,
      'value': value,
    }, conflictAlgorithm: ConflictAlgorithm.replace);
  }

  Future<Map<String, Object?>?> getMetaJson(String key) async {
    final v = await getMeta(key);
    if (v == null) return null;
    return (jsonDecode(v) as Map).cast<String, Object?>();
  }
}

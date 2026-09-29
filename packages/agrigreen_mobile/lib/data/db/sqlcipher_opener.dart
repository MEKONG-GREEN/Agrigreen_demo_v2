import 'package:sqflite_sqlcipher/sqflite.dart' as cipher;

import 'local_db.dart';

/// Mở CSDL bằng SQLCipher trên Android. Cần thiết bị/emulator để chạy
/// (plugin native) — kiểm thử Dart VM dùng opener FFI trong `test/support`.
class SqlCipherOpener implements DbOpener {
  const SqlCipherOpener();

  @override
  Future<cipher.Database> open(
    String path, {
    required String key,
    required int version,
    required cipher.OnDatabaseCreateFn onCreate,
    cipher.OnDatabaseVersionChangeFn? onUpgrade,
  }) {
    return cipher.openDatabase(
      path,
      password: key,
      version: version,
      onConfigure: (db) async {
        // Ghi đè trang đã xóa bằng 0 để dữ liệu bị purge không còn trên đĩa.
        await db.rawQuery('PRAGMA secure_delete = ON');
      },
      onCreate: onCreate,
      onUpgrade: onUpgrade,
    );
  }

  @override
  Future<void> deleteDatabase(String path) => cipher.deleteDatabase(path);
}

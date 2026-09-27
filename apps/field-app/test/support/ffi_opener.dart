import 'dart:io';

import 'package:agrigreen_field/data/db/local_db.dart';
import 'package:sqflite_common_ffi/sqflite_ffi.dart';

/// Mở CSDL bằng sqlite3 FFI (không mã hóa) cho kiểm thử Dart VM. Ghi lại khóa
/// được truyền để kiểm tra khóa đến từ kho bí mật.
class FfiOpener implements DbOpener {
  FfiOpener() {
    sqfliteFfiInit();
  }

  final List<String> keysSeen = [];

  @override
  Future<Database> open(
    String path, {
    required String key,
    required int version,
    required OnDatabaseCreateFn onCreate,
    OnDatabaseVersionChangeFn? onUpgrade,
  }) {
    keysSeen.add(key);
    return databaseFactoryFfi.openDatabase(
      path,
      options: OpenDatabaseOptions(
        version: version,
        onCreate: onCreate,
        onUpgrade: onUpgrade,
        singleInstance: false,
      ),
    );
  }

  @override
  Future<void> deleteDatabase(String path) async {
    await databaseFactoryFfi.deleteDatabase(path);
    for (final suffix in ['', '-wal', '-shm', '-journal']) {
      final f = File('$path$suffix');
      if (await f.exists()) await f.delete();
    }
  }
}

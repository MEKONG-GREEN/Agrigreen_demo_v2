import 'dart:io';
import 'dart:typed_data';

import 'package:path/path.dart' as p;

/// Thư mục chứa **ciphertext** ảnh minh chứng (không bao giờ bản rõ — R-40).
class BlobStore {
  BlobStore(this.dir);
  final Directory dir;

  String pathFor(String id) => p.join(dir.path, '$id.bin');

  Future<String> write(String id, Uint8List ciphertext) async {
    await dir.create(recursive: true);
    final f = File(pathFor(id));
    final tmp = File('${f.path}.part');
    await tmp.writeAsBytes(ciphertext, flush: true);
    await tmp.rename(f.path); // ghi nguyên tử
    return f.path;
  }

  Future<Uint8List> read(String path) => File(path).readAsBytes();

  Future<void> delete(String path) async {
    final f = File(path);
    if (await f.exists()) await f.delete();
  }

  Future<void> deleteAll() async {
    if (await dir.exists()) await dir.delete(recursive: true);
  }
}

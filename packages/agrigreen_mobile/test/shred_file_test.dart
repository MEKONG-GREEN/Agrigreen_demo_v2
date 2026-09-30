import 'dart:io';
import 'dart:typed_data';

import 'package:agrigreen_mobile/data/photo/photo_pipeline.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  late Directory dir;

  setUp(() async {
    dir = await Directory.systemTemp.createTemp('shred_');
  });

  tearDown(() async {
    if (await dir.exists()) await dir.delete(recursive: true);
  });

  test(
    'R-40: ghi đè số 0 lên CHÍNH nội dung cũ (không nối vào cuối tệp)',
    () async {
      final f = File('${dir.path}/plain.jpg');
      // Lớn hơn một khối 64 KiB để phủ cả vòng lặp nhiều khối.
      final content = Uint8List.fromList(
        List<int>.generate(150 * 1024 + 7, (i) => (i * 31 + 7) % 251 + 1),
      );
      await f.writeAsBytes(content, flush: true);

      await overwriteWithZeros(f);

      final after = await f.readAsBytes();
      expect(after.length, content.length, reason: 'độ dài phải giữ nguyên');
      expect(
        after.every((b) => b == 0),
        isTrue,
        reason: 'mọi byte của nội dung cũ phải thành 0',
      );
    },
  );

  test('shredFile ghi đè rồi xóa tệp; tệp không có thì bỏ qua', () async {
    final f = File('${dir.path}/plain2.jpg');
    await f.writeAsBytes([1, 2, 3, 4], flush: true);
    await shredFile(f);
    expect(await f.exists(), isFalse);
    await shredFile(File('${dir.path}/khong-co.jpg'));
  });
}

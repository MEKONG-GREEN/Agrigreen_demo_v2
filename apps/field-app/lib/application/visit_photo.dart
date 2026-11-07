import 'dart:isolate';
import 'dart:typed_data';

import 'package:image/image.dart' as img;

/// Trần ảnh check-in/check-out (BRD CNTS v4.0 Mục VIII "ảnh nén ≤ 500 KB").
const maxVisitPhotoBytes = 500 * 1024;

/// Nén ảnh chụp tại chỗ về JPEG ≤ [maxVisitPhotoBytes], chỉ trong bộ nhớ (không
/// ghi tệp), ở isolate riêng. null khi không giải mã được hoặc không nén nổi.
Future<Uint8List?> compressVisitPhoto(Uint8List bytes) =>
    Isolate.run(() => compressVisitPhotoSync(bytes));

/// Bản đồng bộ của [compressVisitPhoto]: giữ nguyên JPEG đã đủ nhỏ; nếu không,
/// thu cạnh dài dần (1600 → 640 px) và giảm chất lượng (80 → 50).
Uint8List? compressVisitPhotoSync(Uint8List bytes) {
  final isJpeg =
      bytes.length > 3 &&
      bytes[0] == 0xFF &&
      bytes[1] == 0xD8 &&
      bytes[2] == 0xFF;
  if (isJpeg && bytes.length <= maxVisitPhotoBytes) return bytes;
  final im = img.decodeImage(bytes);
  if (im == null) return null;
  for (final side in const [1600, 1280, 1024, 800, 640]) {
    final landscape = im.width >= im.height;
    final fit = (landscape ? im.width : im.height) <= side
        ? im
        : img.copyResize(
            im,
            width: landscape ? side : null,
            height: landscape ? null : side,
          );
    for (final q in const [80, 65, 50]) {
      final out = img.encodeJpg(fit, quality: q);
      if (out.length <= maxVisitPhotoBytes) return out;
    }
  }
  return null;
}

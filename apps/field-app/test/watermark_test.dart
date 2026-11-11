import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:agrigreen_mobile/data/crypto/envelope.dart';
import 'package:agrigreen_mobile/data/photo/location_source.dart';
import 'package:agrigreen_mobile/data/photo/photo_pipeline.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:image/image.dart' as img;
import 'package:pinenacl/x25519.dart' as nacl;

import 'support/harness.dart';

/// Ảnh JPEG thật (nền xanh lá đồng nhất) để đo watermark.
Uint8List greenJpeg({int w = 640, int h = 480}) {
  final im = img.Image(width: w, height: h);
  img.fill(im, color: img.ColorRgb8(20, 160, 60));
  return img.encodeJpg(im, quality: 95);
}

class _Camera implements CameraSource {
  _Camera(this.bytes);
  final Uint8List bytes;
  int calls = 0;
  @override
  Future<CapturedImage?> capture() async {
    calls++;
    return CapturedImage(Uint8List.fromList(bytes), 'image/jpeg');
  }
}

void main() {
  group('watermark ảnh trước khi mã hóa', () {
    late Harness h;
    final sk = nacl.PrivateKey.generate();
    setUp(() async {
      h = await Harness.create();
      await h.refs.applyPage(
        PullPage(
          changes: [
            PullChange(
              entity: 'pubkey',
              id: 'hh1',
              version: 1,
              op: 'upsert',
              data: {
                'ref': 'hh1',
                'area_code': '31117',
                'public_key': base64.encode(sk.publicKey.asTypedList),
              },
            ),
          ],
          nextCursor: 'c',
          hasMore: false,
        ),
      );
    });
    tearDown(() => h.dispose());

    PhotoPipeline pipeline(_Camera cam, {Watermarker? wm}) => PhotoPipeline(
      camera: cam,
      location: const FixedLocationSource(
        LocationResult(
          GpsStatus.ok,
          GpsFix(lat: 10.031234, lon: 105.781234, accuracyM: 4),
        ),
      ),
      clock: h.clock,
      ids: h.ids,
      cipher: EnvelopeCipher(),
      refs: h.refs,
      evidence: h.evidence,
      watermarker: wm ?? const NoopWatermarker(),
    );

    test('watermark GPS + thời gian được đóng vào ảnh TRƯỚC khi mã hóa; không bản rõ trên đĩa', () async {
      final jpeg = greenJpeg();
      final out = await pipeline(
        _Camera(jpeg),
        wm: const ImageWatermarker(),
      ).capture(subjectRef: 'hh1');
      final f = out!.file;
      final plain = await EnvelopeCipher.openForTest(
        SealedEnvelope(
          meta: f.meta,
          ciphertext: await h.blobs.read(f.blobPath),
        ),
        Uint8List.fromList(sk.asTypedList),
      );
      expect(plain, isNot(jpeg));
      final im = img.decodeJpg(plain)!;
      expect(im.width, 640);
      // Dải dưới tối (nền watermark), phần trên vẫn xanh lá.
      final bottom = im.getPixel(600, 478);
      final top = im.getPixel(320, 100);
      expect(bottom.g, lessThan(120));
      expect(top.g, greaterThan(130));
      // Có chữ trắng trong dải.
      var white = 0;
      for (var x = 0; x < 400; x++) {
        for (var y = im.height - 60; y < im.height; y++) {
          final p = im.getPixel(x, y);
          if (p.r > 200 && p.g > 200 && p.b > 200) white++;
        }
      }
      expect(white, greaterThan(50));
      // Không tệp nào trong thư mục dữ liệu chứa JPEG bản rõ (SOI + JFIF).
      for (final e in h.dir.listSync(recursive: true).whereType<File>()) {
        final b = e.readAsBytesSync();
        expect(
          b.length > 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF,
          isFalse,
          reason: e.path,
        );
      }
    });
  });

  test('watermarkText chỉ ASCII, có thời gian và tọa độ', () {
    final t = watermarkText(
      '2026-12-02T08:15:00+07:00',
      const GpsFix(lat: 10.031234, lon: 105.781234, accuracyM: 4.4),
    );
    expect(t, contains('2026-12-02T08:15:00+07:00'));
    expect(t, contains('10.031234, 105.781234'));
    expect(t, contains('+/-4m'));
    expect(t.codeUnits.every((c) => c < 128), isTrue);
    expect(watermarkText('x', null), contains('GPS: N/A'));
  });

  test('ảnh không giải mã được → giữ nguyên (không mất ảnh)', () async {
    final junk = Uint8List.fromList(List.filled(100, 7));
    expect(burnWatermark(junk, 'x'), isNull);
    expect(await const ImageWatermarker().apply(junk, 'x', null), junk);
  });

  test('shredFile ghi đè 0 rồi xóa tệp tạm của camera', () async {
    final dir = await Directory.systemTemp.createTemp('ag_shred_');
    final f = File('${dir.path}/cam.jpg')
      ..writeAsBytesSync(List.filled(200000, 0xAB));
    await shredFile(f);
    expect(f.existsSync(), isFalse);
    await shredFile(f); // không lỗi khi đã xóa
    await dir.delete(recursive: true);
  });
}

import 'dart:io';
import 'dart:isolate';
import 'dart:typed_data';

import 'package:image/image.dart' as img;
import 'package:image_picker/image_picker.dart';

import '../../core/clock.dart';
import '../../core/ids.dart';
import '../../domain/consent.dart';
import '../crypto/envelope.dart';
import '../files/evidence_repository.dart';
import '../reference/reference_repository.dart';
import 'location_source.dart';

class CapturedImage {
  const CapturedImage(this.bytes, this.contentType);
  final Uint8List bytes;
  final String contentType;
}

/// Nguồn ảnh (camera). Hiện thực phải trả bytes và **xóa mọi tệp tạm bản rõ**.
abstract class CameraSource {
  Future<CapturedImage?> capture();
}

/// Dùng `image_picker` (ứng dụng camera hệ thống). Plugin buộc ghi JPEG vào
/// thư mục cache riêng của app, nên ngay sau khi đọc vào bộ nhớ, app **ghi đè
/// 0 toàn bộ tệp rồi xóa** (R-40).
///
/// Đã cân nhắc package `camera`: `takePicture()` cũng ghi tệp tạm; chỉ luồng
/// khung hình (`startImageStream`, YUV độ phân giải xem trước) là thuần bộ nhớ,
/// chất lượng ảnh minh chứng không đạt. Rủi ro còn lại ghi trong README
/// ("Hạn chế R-40").
class ImagePickerCameraSource implements CameraSource {
  ImagePickerCameraSource([ImagePicker? picker])
    : _picker = picker ?? ImagePicker();
  final ImagePicker _picker;

  @override
  Future<CapturedImage?> capture() async {
    final x = await _picker.pickImage(
      source: ImageSource.camera,
      maxWidth: 1920,
      maxHeight: 1920,
      imageQuality: 80,
      requestFullMetadata: false,
    );
    if (x == null) return null;
    try {
      final bytes = await x.readAsBytes();
      return CapturedImage(bytes, 'image/jpeg');
    } finally {
      await shredFile(File(x.path));
    }
  }
}

/// Ghi đè 0 nội dung tệp (cùng độ dài, flush) rồi xóa. Không bảo đảm tuyệt đối
/// trên bộ nhớ flash (wear-levelling, journal) — xem README.
Future<void> shredFile(File f) async {
  try {
    if (!await f.exists()) return;
    final len = await f.length();
    final raf = await f.open(mode: FileMode.writeOnlyAppend);
    try {
      await raf.setPosition(0);
      const chunk = 64 * 1024;
      final zeros = Uint8List(chunk);
      var left = len;
      while (left > 0) {
        final n = left < chunk ? left : chunk;
        await raf.writeFrom(zeros, 0, n);
        left -= n;
      }
      await raf.flush();
    } finally {
      await raf.close();
    }
    await f.delete();
  } catch (_) {
    try {
      await f.delete();
    } catch (_) {
      // đã bị xóa
    }
  }
}

/// Đóng dấu GPS/thời gian lên ảnh (HTX US-LOG-01 AC-4).
abstract class Watermarker {
  Future<Uint8List> apply(Uint8List jpeg, String capturedAt, GpsFix? gps);
}

/// Không đóng dấu (kiểm thử pipeline với dữ liệu không phải JPEG thật).
class NoopWatermarker implements Watermarker {
  const NoopWatermarker();
  @override
  Future<Uint8List> apply(
    Uint8List jpeg,
    String capturedAt,
    GpsFix? gps,
  ) async => jpeg;
}

/// Dòng chữ đóng dấu (chỉ ký tự ASCII — phông bitmap của `image`).
String watermarkText(String capturedAt, GpsFix? gps) {
  final where = gps == null
      ? 'GPS: N/A'
      : 'GPS: ${gps.lat.toStringAsFixed(6)}, ${gps.lon.toStringAsFixed(6)}'
            '${gps.accuracyM == null ? '' : ' +/-${gps.accuracyM!.round()}m'}';
  return 'AgriGreen  $capturedAt\n$where';
}

/// Vẽ dải nền tối + chữ trắng ở cạnh dưới ảnh rồi mã hóa lại JPEG. **Chỉ trong
/// bộ nhớ**: không ghi tệp nào. null nếu không giải mã được ảnh.
Uint8List? burnWatermark(Uint8List bytes, String text, {int quality = 85}) {
  final im = img.decodeImage(bytes);
  if (im == null) return null;
  final font = im.width >= 1400
      ? img.arial48
      : im.width >= 500
      ? img.arial24
      : img.arial14;
  final lines = text.split('\n');
  final pad = (font.lineHeight / 3).round();
  final boxH = lines.length * font.lineHeight + pad * 2;
  img.fillRect(
    im,
    x1: 0,
    y1: im.height - boxH,
    x2: im.width - 1,
    y2: im.height - 1,
    color: img.ColorRgba8(0, 0, 0, 150),
  );
  for (var i = 0; i < lines.length; i++) {
    img.drawString(
      im,
      lines[i],
      font: font,
      x: pad,
      y: im.height - boxH + pad + i * font.lineHeight,
      color: img.ColorRgb8(255, 255, 255),
    );
  }
  return img.encodeJpg(im, quality: quality);
}

/// Đóng dấu bằng package `image` (MIT), chạy trong isolate riêng để không giật
/// giao diện. Ảnh đã đóng dấu là ảnh được mã hóa (bản chưa đóng dấu không lưu).
/// Ảnh không giải mã được → giữ nguyên (metadata phong bì vẫn có GPS/thời gian).
class ImageWatermarker implements Watermarker {
  const ImageWatermarker();

  @override
  Future<Uint8List> apply(
    Uint8List jpeg,
    String capturedAt,
    GpsFix? gps,
  ) async {
    final text = watermarkText(capturedAt, gps);
    final out = await Isolate.run(() => burnWatermark(jpeg, text));
    return out ?? jpeg;
  }
}

class PhotoPipelineException implements Exception {
  PhotoPipelineException(this.message);
  final String message;
  @override
  String toString() => message;
}

class CaptureOutcome {
  const CaptureOutcome(this.file, this.gpsStatus);
  final EvidenceFile file;
  final GpsStatus gpsStatus;
}

/// Chụp → đóng dấu GPS/thời gian → mã hóa phong bì bằng khóa công khai của hộ
/// **ngay lập tức** → chỉ lưu ciphertext → chờ tải lên `POST /v1/files`
/// (SyncService) → bản ghi `evidence` tham chiếu `file_ref`.
///
/// Có [consent]: chỉ mở camera khi hộ đã đồng ý `mrv.evidence`.
class PhotoPipeline {
  PhotoPipeline({
    required CameraSource camera,
    required LocationSource location,
    required Clock clock,
    required IdGenerator ids,
    required EnvelopeCipher cipher,
    required ReferenceRepository refs,
    required EvidenceRepository evidence,
    this.watermarker = const NoopWatermarker(),
    this.maxBytes = 15 * 1024 * 1024,
    this.consent,
  }) : _camera = camera,
       _location = location,
       _clock = clock,
       _ids = ids,
       _cipher = cipher,
       _refs = refs,
       _evidence = evidence;

  final CameraSource _camera;
  final LocationSource _location;
  final Clock _clock;
  final IdGenerator _ids;
  final EnvelopeCipher _cipher;
  final ReferenceRepository _refs;
  final EvidenceRepository _evidence;
  final Watermarker watermarker;
  final int maxBytes;
  final ConsentGate? consent;

  static const _noKeyMessage =
      'Chưa có khóa công khai của hộ trên máy. Hãy đồng bộ khi có mạng.';

  /// Lý do không được chụp (null = được chụp) — hiển thị trước khi mở camera.
  Future<String?> blockReason(String subjectRef) async {
    final c = consent;
    if (c != null && !await c.isGranted(subjectRef, mrvEvidencePurpose)) {
      return ConsentRequiredException(subjectRef).message;
    }
    if (await _refs.publicKeyFor(subjectRef) == null) return _noKeyMessage;
    return null;
  }

  Future<CaptureOutcome?> capture({
    required String subjectRef,
    String? parentEntity,
    String? parentId,
  }) async {
    final c = consent;
    if (c != null && !await c.isGranted(subjectRef, mrvEvidencePurpose)) {
      throw ConsentRequiredException(subjectRef);
    }
    final pk = await _refs.publicKeyFor(subjectRef);
    if (pk == null) throw PhotoPipelineException(_noKeyMessage);
    final shot = await _camera.capture();
    if (shot == null) return null;
    Uint8List? plain = shot.bytes;
    try {
      final loc = await _location.current();
      final capturedAt = formatRfc3339(_clock.now());
      final marked = await watermarker.apply(plain, capturedAt, loc.fix);
      if (!identical(marked, plain)) plain.fillRange(0, plain.length, 0);
      plain = marked;
      final env = await _cipher.seal(
        plaintext: plain,
        householdPublicKey: pk,
        subjectRef: subjectRef,
        contentType: shot.contentType,
        capturedAt: capturedAt,
        gps: loc.fix,
      );
      if (env.ciphertext.length > maxBytes) {
        throw PhotoPipelineException('Ảnh vượt quá 15 MB.');
      }
      final f = await _evidence.addSealed(
        id: _ids.newId(),
        subjectRef: subjectRef,
        envelope: env,
        parentEntity: parentEntity,
        parentId: parentId,
      );
      return CaptureOutcome(f, loc.status);
    } finally {
      // Ghi đè bản rõ trong bộ nhớ ngay khi xong.
      plain?.fillRange(0, plain.length, 0);
      shot.bytes.fillRange(0, shot.bytes.length, 0);
    }
  }
}

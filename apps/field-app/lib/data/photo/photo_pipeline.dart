import 'dart:io';
import 'dart:typed_data';

import 'package:image_picker/image_picker.dart';

import '../../core/clock.dart';
import '../../core/ids.dart';
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

/// Dùng `image_picker`: plugin ghi JPEG vào thư mục cache, nên đọc xong xóa
/// ngay. TODO(r40): chuyển sang package `camera` chụp thẳng vào bộ nhớ để
/// bản rõ không chạm đĩa (R-40).
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
      try {
        await File(x.path).delete();
      } catch (_) {
        // đã bị xóa
      }
    }
  }
}

/// Đóng dấu GPS/thời gian lên ảnh (HTX US-LOG-01 AC-4).
abstract class Watermarker {
  Future<Uint8List> apply(Uint8List jpeg, String capturedAt, GpsFix? gps);
}

/// TODO(watermark): vẽ chữ mốc thời gian + tọa độ lên ảnh (dart:ui Canvas trong
/// isolate). Hiện chỉ ghi vào metadata phong bì — metadata là bắt buộc.
class NoopWatermarker implements Watermarker {
  const NoopWatermarker();
  @override
  Future<Uint8List> apply(
    Uint8List jpeg,
    String capturedAt,
    GpsFix? gps,
  ) async => jpeg;
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

  Future<CaptureOutcome?> capture({
    required String subjectRef,
    String? parentEntity,
    String? parentId,
  }) async {
    final pk = await _refs.publicKeyFor(subjectRef);
    if (pk == null) {
      throw PhotoPipelineException(
        'Chưa có khóa công khai của hộ trên máy. Hãy đồng bộ khi có mạng.',
      );
    }
    final img = await _camera.capture();
    if (img == null) return null;
    Uint8List? plain = img.bytes;
    try {
      final loc = await _location.current();
      final capturedAt = formatRfc3339(_clock.now());
      plain = await watermarker.apply(plain, capturedAt, loc.fix);
      final env = await _cipher.seal(
        plaintext: plain,
        householdPublicKey: pk,
        subjectRef: subjectRef,
        contentType: img.contentType,
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
      img.bytes.fillRange(0, img.bytes.length, 0);
    }
  }
}

import 'dart:convert';
import 'dart:math';
import 'dart:typed_data';

import 'package:crypto/crypto.dart' as hash;
import 'package:cryptography/cryptography.dart';
import 'package:pinenacl/x25519.dart' as nacl;

/// Phong bì mã hóa phiên bản 1 (docs/dev/envelope-format.md, ADR-0004):
/// - DEK 32 byte ngẫu nhiên mỗi tệp;
/// - nội dung: AES-256-GCM, nonce 12 byte, không AAD, đầu ra ciphertext ‖ tag;
/// - DEK được bọc bằng `crypto_box_seal` (X25519 + XSalsa20-Poly1305) với khóa
///   công khai của hộ → 80 byte. Tương thích Go `nacl/box.SealAnonymous`.
///
/// Thiết bị **không bao giờ** giữ khóa bí mật của hộ; [openForTest] chỉ dùng
/// trong kiểm thử và mô phỏng vault.
abstract final class Envelope {
  static const version = 1;
  static const alg = 'A256GCM+X25519-SEALEDBOX';
  static const dekLength = 32;
  static const nonceLength = 12;
  static const tagLength = 16;
  static const wrappedDekLength = 80;

  static final AesGcm _aes = AesGcm.with256bits();
}

class GpsFix {
  const GpsFix({required this.lat, required this.lon, this.accuracyM});
  final double lat;
  final double lon;
  final double? accuracyM;

  Map<String, Object?> toJson() => {
    'lat': lat,
    'lon': lon,
    if (accuracyM != null) 'accuracy_m': accuracyM,
  };
}

class SealedEnvelope {
  const SealedEnvelope({required this.meta, required this.ciphertext});

  /// JSON metadata theo envelope-format.md.
  final Map<String, Object?> meta;

  /// ciphertext ‖ tag (16 byte).
  final Uint8List ciphertext;
}

class EnvelopeException implements Exception {
  EnvelopeException(this.message);
  final String message;
  @override
  String toString() => 'EnvelopeException: $message';
}

class EnvelopeCipher {
  EnvelopeCipher({Random? random}) : _random = random ?? Random.secure();
  final Random _random;

  Uint8List _randomBytes(int n) =>
      Uint8List.fromList(List<int>.generate(n, (_) => _random.nextInt(256)));

  /// Mã hóa bằng DEK và nonce cho trước (dùng cho vector kiểm thử).
  static Future<Uint8List> aesGcmEncrypt(
    Uint8List dek,
    Uint8List nonce,
    Uint8List plaintext,
  ) async {
    if (dek.length != Envelope.dekLength) {
      throw EnvelopeException('DEK phải 32 byte');
    }
    if (nonce.length != Envelope.nonceLength) {
      throw EnvelopeException('nonce phải 12 byte');
    }
    final box = await Envelope._aes.encrypt(
      plaintext,
      secretKey: SecretKey(dek),
      nonce: nonce,
    );
    final out = Uint8List(box.cipherText.length + Envelope.tagLength)
      ..setAll(0, box.cipherText)
      ..setAll(box.cipherText.length, box.mac.bytes);
    return out;
  }

  static Future<Uint8List> aesGcmDecrypt(
    Uint8List dek,
    Uint8List nonce,
    Uint8List ciphertextAndTag,
  ) async {
    if (ciphertextAndTag.length < Envelope.tagLength) {
      throw EnvelopeException('ciphertext quá ngắn');
    }
    final n = ciphertextAndTag.length - Envelope.tagLength;
    final box = SecretBox(
      ciphertextAndTag.sublist(0, n),
      nonce: nonce,
      mac: Mac(ciphertextAndTag.sublist(n)),
    );
    try {
      final pt = await Envelope._aes.decrypt(box, secretKey: SecretKey(dek));
      return Uint8List.fromList(pt);
    } on SecretBoxAuthenticationError {
      throw EnvelopeException('tag GCM không khớp');
    }
  }

  /// `crypto_box_seal(dek, recipientPublicKey)` → 80 byte.
  static Uint8List sealDek(Uint8List dek, Uint8List recipientPublicKey) {
    if (recipientPublicKey.length != 32) {
      throw EnvelopeException('khóa công khai X25519 phải 32 byte');
    }
    final box = nacl.SealedBox(nacl.PublicKey(recipientPublicKey));
    return Uint8List.fromList(box.encrypt(dek));
  }

  /// `crypto_box_seal_open` — CHỈ vault làm việc này. Dùng trong kiểm thử.
  static Uint8List openDekForTest(
    Uint8List wrapped,
    Uint8List recipientSecretKey,
  ) {
    final box = nacl.SealedBox(nacl.PrivateKey(recipientSecretKey));
    return Uint8List.fromList(box.decrypt(wrapped));
  }

  static String sha256Hex(Uint8List data) =>
      hash.sha256.convert(data).toString();

  /// Mã hóa ngay khi chụp. Trả về metadata + ciphertext; DEK bị xóa khỏi bộ
  /// nhớ (ghi đè 0) trước khi trả.
  Future<SealedEnvelope> seal({
    required Uint8List plaintext,
    required Uint8List householdPublicKey,
    required String subjectRef,
    required String contentType,
    required String capturedAt,
    GpsFix? gps,
  }) async {
    final dek = _randomBytes(Envelope.dekLength);
    final nonce = _randomBytes(Envelope.nonceLength);
    try {
      final ct = await aesGcmEncrypt(dek, nonce, plaintext);
      final wrapped = sealDek(dek, householdPublicKey);
      if (wrapped.length != Envelope.wrappedDekLength) {
        throw EnvelopeException('wrapped_dek sai độ dài ${wrapped.length}');
      }
      return SealedEnvelope(
        meta: {
          'v': Envelope.version,
          'alg': Envelope.alg,
          'subject_ref': subjectRef,
          'nonce': base64.encode(nonce),
          'wrapped_dek': base64.encode(wrapped),
          'sha256_ciphertext': sha256Hex(ct),
          'content_type': contentType,
          'captured_at': capturedAt,
          'gps': gps?.toJson(),
        },
        ciphertext: ct,
      );
    } finally {
      dek.fillRange(0, dek.length, 0);
    }
  }

  /// Mở phong bì bằng khóa bí mật — mô phỏng vault, chỉ cho kiểm thử.
  static Future<Uint8List> openForTest(
    SealedEnvelope env,
    Uint8List recipientSecretKey,
  ) async {
    final meta = env.meta;
    if (meta['v'] != Envelope.version || meta['alg'] != Envelope.alg) {
      throw EnvelopeException('phiên bản/thuật toán không hỗ trợ');
    }
    if (sha256Hex(env.ciphertext) != meta['sha256_ciphertext']) {
      throw EnvelopeException('sha256_ciphertext không khớp');
    }
    final dek = openDekForTest(
      base64.decode(meta['wrapped_dek']! as String),
      recipientSecretKey,
    );
    return aesGcmDecrypt(
      dek,
      base64.decode(meta['nonce']! as String),
      env.ciphertext,
    );
  }
}

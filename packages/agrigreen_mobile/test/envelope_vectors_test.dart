import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:agrigreen_mobile/data/crypto/envelope.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:pinenacl/x25519.dart' as nacl;

/// Bộ vector dùng chung Go ↔ Dart (docs/dev/envelope-format.md).
const sharedVectorsPath =
    '../../internal/shared/envelope/testdata/vectors.json';

/// Vector do `golang.org/x/crypto/nacl/box.SealAnonymous` sinh, lưu trong repo
/// để kiểm tương thích libsodium kể cả khi bộ chung chưa có.
const localVectorsPath = 'test/fixtures/go_nacl_vectors.json';

Uint8List _bytes(Object? v) {
  if (v is! String) throw FormatException('không phải chuỗi: $v');
  final s = v.trim();
  if (s.isEmpty) return Uint8List(0);
  if (s.length.isEven && RegExp(r'^[0-9a-fA-F]+$').hasMatch(s)) {
    return Uint8List.fromList([
      for (var i = 0; i < s.length; i += 2)
        int.parse(s.substring(i, i + 2), radix: 16),
    ]);
  }
  return Uint8List.fromList(base64.decode(base64.normalize(s)));
}

/// Lấy trường theo nhiều tên có thể (tên chưa chốt giữa hai phía).
Uint8List? _field(Map<String, Object?> v, List<String> names) {
  for (final n in names) {
    for (final k in [n, '${n}_hex', '${n}_b64', '${n}_base64']) {
      if (v.containsKey(k) && v[k] != null) return _bytes(v[k]);
    }
  }
  return null;
}

List<Map<String, Object?>> _load(String path) {
  final j = jsonDecode(File(path).readAsStringSync());
  final list = j is List ? j : (j as Map)['vectors'] as List;
  return list.map((e) => (e as Map).cast<String, Object?>()).toList();
}

Future<void> _check(Map<String, Object?> v) async {
  final name = v['name'] ?? v['id'] ?? '?';
  Uint8List need(List<String> names) {
    final b = _field(v, names);
    if (b == null) {
      fail('vector $name thiếu trường ${names.first} (thử: $names)');
    }
    return b;
  }

  // Tên chuẩn theo bộ vector Go: recipient_secret_key_hex, recipient_public_key_hex.
  final sk = need([
    'recipient_secret_key',
    'recipient_sk',
    'secret_key',
    'sk',
    'private_key',
    'x25519_sk',
  ]);
  final pk = _field(v, [
    'recipient_public_key',
    'recipient_pk',
    'public_key',
    'pk',
    'x25519_pk',
  ]);
  final plaintext = need(['plaintext', 'pt']);
  final dek = need(['dek']);
  final nonce = need(['nonce']);
  var ct = need(['ciphertext', 'ct']);
  final tag = _field(v, ['tag']);
  if (tag != null) ct = Uint8List.fromList([...ct, ...tag]);
  final wrapped = need(['wrapped_dek', 'sealed_dek']);

  expect(wrapped, hasLength(Envelope.wrappedDekLength), reason: '$name');
  if (pk != null) {
    expect(
      nacl.PrivateKey(sk).publicKey.asTypedList,
      pk,
      reason: '$name: khóa công khai suy từ khóa bí mật',
    );
  }
  // 1. Mở sealed box do Go/libsodium tạo.
  expect(EnvelopeCipher.openDekForTest(wrapped, sk), dek, reason: '$name');
  // 2. AES-256-GCM tất định với DEK + nonce cho trước → đúng từng byte.
  expect(
    await EnvelopeCipher.aesGcmEncrypt(dek, nonce, plaintext),
    ct,
    reason: '$name: ciphertext‖tag',
  );
  expect(await EnvelopeCipher.aesGcmDecrypt(dek, nonce, ct), plaintext);
  final sha = v['sha256_ciphertext_hex'] ?? v['sha256_ciphertext'];
  if (sha is String) expect(EnvelopeCipher.sha256Hex(ct), sha.toLowerCase());
  // 3. Chiều ngược: DEK do Dart seal → mở lại được bằng khóa bí mật.
  final resealed = EnvelopeCipher.sealDek(
    dek,
    nacl.PrivateKey(sk).publicKey.asTypedList,
  );
  expect(EnvelopeCipher.openDekForTest(resealed, sk), dek);
}

void main() {
  group('vector Go nacl/box.SealAnonymous (lưu trong repo)', () {
    for (final v in _load(localVectorsPath)) {
      test('${v['name']}', () => _check(v));
    }
  });

  final shared = File(sharedVectorsPath);
  group('bộ vector chung internal/shared/envelope/testdata/vectors.json', () {
    if (!shared.existsSync()) {
      test(
        'chưa có tệp vector chung',
        () {},
        skip: 'Chưa có $sharedVectorsPath (phía Go đang làm).',
      );
      return;
    }
    final vectors = _load(shared.path);
    test('có ít nhất một vector', () => expect(vectors, isNotEmpty));
    for (var i = 0; i < vectors.length; i++) {
      test('${vectors[i]['name'] ?? '#$i'}', () => _check(vectors[i]));
    }
  });
}

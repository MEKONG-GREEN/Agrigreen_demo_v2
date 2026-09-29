import 'dart:convert';
import 'dart:math';
import 'dart:typed_data';

import 'package:agrigreen_mobile/data/crypto/envelope.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:pinenacl/x25519.dart' as nacl;

void main() {
  final sk = nacl.PrivateKey.generate();
  final pk = Uint8List.fromList(sk.publicKey.asTypedList);
  final skBytes = Uint8List.fromList(sk.asTypedList);

  test(
    'round-trip: mã hóa bằng khóa công khai, vault mở bằng khóa bí mật',
    () async {
      final plain = Uint8List.fromList(
        List<int>.generate(50000, (i) => (i * 31) & 0xff),
      );
      final env = await EnvelopeCipher(random: Random(1)).seal(
        plaintext: Uint8List.fromList(plain),
        householdPublicKey: pk,
        subjectRef: '0191aaaa-0000-7000-8000-000000000001',
        contentType: 'image/jpeg',
        capturedAt: '2026-12-03T07:14:22+07:00',
        gps: const GpsFix(lat: 10.03, lon: 105.78, accuracyM: 6.5),
      );
      final m = env.meta;
      expect(m['v'], 1);
      expect(m['alg'], 'A256GCM+X25519-SEALEDBOX');
      expect(m['subject_ref'], '0191aaaa-0000-7000-8000-000000000001');
      expect(base64.decode(m['nonce']! as String), hasLength(12));
      expect(base64.decode(m['wrapped_dek']! as String), hasLength(80));
      expect(m['sha256_ciphertext'], EnvelopeCipher.sha256Hex(env.ciphertext));
      expect(m['content_type'], 'image/jpeg');
      expect(m['captured_at'], '2026-12-03T07:14:22+07:00');
      expect(m['gps'], {'lat': 10.03, 'lon': 105.78, 'accuracy_m': 6.5});
      expect(env.ciphertext.length, plain.length + 16);
      // JSON metadata tuần tự hóa được như đặc tả.
      expect(jsonDecode(jsonEncode(m)), m);

      final opened = await EnvelopeCipher.openForTest(env, skBytes);
      expect(opened, plain);
    },
  );

  test('mỗi tệp một DEK và nonce khác nhau', () async {
    final c = EnvelopeCipher();
    Future<SealedEnvelope> s() => c.seal(
      plaintext: Uint8List.fromList([1, 2, 3]),
      householdPublicKey: pk,
      subjectRef: 'h',
      contentType: 'image/jpeg',
      capturedAt: 't',
    );
    final a = await s(), b = await s();
    expect(a.meta['nonce'], isNot(b.meta['nonce']));
    expect(a.meta['wrapped_dek'], isNot(b.meta['wrapped_dek']));
    expect(a.ciphertext, isNot(b.ciphertext));
  });

  test('sửa ciphertext hoặc tag → không mở được', () async {
    final env = await EnvelopeCipher().seal(
      plaintext: Uint8List.fromList(utf8.encode('ảnh hiện trường')),
      householdPublicKey: pk,
      subjectRef: 'h',
      contentType: 'image/jpeg',
      capturedAt: 't',
    );
    final ct = Uint8List.fromList(env.ciphertext);
    ct[ct.length - 1] ^= 1;
    final dek = EnvelopeCipher.openDekForTest(
      base64.decode(env.meta['wrapped_dek']! as String),
      skBytes,
    );
    await expectLater(
      EnvelopeCipher.aesGcmDecrypt(
        dek,
        base64.decode(env.meta['nonce']! as String),
        ct,
      ),
      throwsA(isA<EnvelopeException>()),
    );
    await expectLater(
      EnvelopeCipher.openForTest(
        SealedEnvelope(meta: env.meta, ciphertext: ct),
        skBytes,
      ),
      throwsA(isA<EnvelopeException>()),
    ); // sha256 không khớp
  });

  test('khóa bí mật khác không mở được DEK', () {
    final wrapped = EnvelopeCipher.sealDek(Uint8List(32), pk);
    final other = Uint8List.fromList(nacl.PrivateKey.generate().asTypedList);
    expect(
      () => EnvelopeCipher.openDekForTest(wrapped, other),
      throwsA(anything),
    );
  });

  test('khóa công khai sai độ dài bị từ chối', () {
    expect(
      () => EnvelopeCipher.sealDek(Uint8List(32), Uint8List(31)),
      throwsA(isA<EnvelopeException>()),
    );
  });
}

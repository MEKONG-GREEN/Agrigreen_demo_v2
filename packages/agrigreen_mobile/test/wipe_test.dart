import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:agrigreen_mobile/data/crypto/envelope.dart';
import 'package:agrigreen_mobile/data/secure/secure_store.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:agrigreen_mobile/data/sync/sync_service.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'support/harness.dart';

void main() {
  test(
    'HttpSyncApi: 403 code=sync.device_revoked → DeviceRevokedException',
    () async {
      final api = HttpSyncApi(
        baseUrl: 'https://api.test',
        accessToken: () async => 'tok',
        client: MockClient(
          (req) async => http.Response.bytes(
            utf8.encode(
              jsonEncode({
                'status': 403,
                'code': 'sync.device_revoked',
                'title': 'Thiết bị đã bị thu hồi',
              }),
            ),
            403,
            headers: {'content-type': 'application/problem+json'},
          ),
        ),
      );
      await expectLater(
        api.pull(limit: 500),
        throwsA(isA<DeviceRevokedException>()),
      );
    },
  );

  test('HttpSyncApi: 403 mã khác không bị coi là thu hồi', () async {
    final api = HttpSyncApi(
      baseUrl: 'https://api.test',
      accessToken: () async => 'tok',
      client: MockClient(
        (req) async => http.Response(
          jsonEncode({'status': 403, 'code': 'forbidden'}),
          403,
        ),
      ),
    );
    await expectLater(
      api.pull(limit: 500),
      throwsA(
        isA<ApiException>()
            .having((e) => e is DeviceRevokedException, 'revoked', isFalse)
            .having((e) => e.code, 'code', 'forbidden'),
      ),
    );
  });

  group('thu hồi thiết bị → xóa dữ liệu cục bộ và khóa token', () {
    late Harness h;
    setUp(() async => h = await Harness.create());
    tearDown(() => h.dispose());

    test('sync gặp 403 sync.device_revoked → xóa CSDL, ciphertext, token, khóa CSDL', () async {
      await h.outbox.enqueue(
        entity: PushEntity.fieldReport,
        payload: {'note': 'chưa gửi'},
      );
      await h.blobs.write('e1', Uint8List.fromList([1, 2, 3]));
      await h.store.write(SecureKeys.accessToken, 'at');
      await h.store.write(SecureKeys.refreshToken, 'rt');
      await h.store.write(SecureKeys.pinHash, 'x');
      expect(File(h.dbPath).existsSync(), isTrue);
      expect(h.blobs.dir.existsSync(), isTrue);

      h.fake.revoked = true;
      final rep = await h.sync.syncOnce();
      expect(rep, isNull);
      expect(h.wipe.wiped, isTrue);
      expect(h.wipedCallbacks, 1);
      expect(File(h.dbPath).existsSync(), isFalse);
      expect(h.blobs.dir.existsSync(), isFalse);
      expect(
        h.store.values,
        isEmpty,
      ); // token, refresh, PIN, khóa CSDL, device id
      expect((await h.sync.status()).state, SyncState.revoked);

      // Không còn gọi máy chủ sau khi xóa.
      h.fake.revoked = false;
      expect(await h.sync.syncOnce(force: true), isNull);
      expect(h.fake.pushCalls, 0);
    });

    test('thu hồi phát hiện khi đang tải ảnh cũng xóa', () async {
      await h.evidence.addSealed(
        id: 'f1',
        subjectRef: 'h1',
        envelope: SealedEnvelope(
          meta: const {'captured_at': '2026-12-02T08:00:00+07:00'},
          ciphertext: Uint8List(10),
        ),
      );
      h.fake.revoked = true;
      await h.sync.syncOnce();
      expect(h.wipe.wiped, isTrue);
      expect(File(h.dbPath).existsSync(), isFalse);
    });

    test('401 không xóa dữ liệu — chỉ báo cần đăng nhập lại', () async {
      await h.outbox.enqueue(entity: PushEntity.fieldReport, payload: {'n': 1});
      h.fake.unauthenticated = true;
      await h.sync.syncOnce();
      expect(h.wipe.wiped, isFalse);
      expect((await h.sync.status()).state, SyncState.needsLogin);
      expect((await h.outbox.counts()).pending, 1);
    });
  });
}

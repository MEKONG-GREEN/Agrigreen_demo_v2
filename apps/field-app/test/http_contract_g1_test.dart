import 'dart:convert';
import 'dart:typed_data';

import 'package:agrigreen_field/data/sync/sync_api.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

/// Hình dạng request của các endpoint G1 mới (application.yaml: pf/notify,
/// pf/content, pf/extension, pf/sync).
void main() {
  const dev = '0192aaaa-0000-7000-8000-00000000d001';
  late List<http.BaseRequest> seen;
  late List<String> bodies;

  HttpSyncApi api({int status = 200, String body = '{}'}) {
    seen = [];
    bodies = [];
    return HttpSyncApi(
      baseUrl: 'https://api.test',
      accessToken: () async => 'tok',
      deviceId: dev,
      client: MockClient((req) async {
        seen.add(req);
        bodies.add(req.body);
        return http.Response(
          body,
          status,
          headers: {'content-type': 'application/json'},
        );
      }),
    );
  }

  test(
    'mọi lời gọi mang X-Device-Id (sync-contract: pull/push/files)',
    () async {
      final a = api(body: jsonEncode({'changes': [], 'has_more': false}));
      await a.pull(limit: 10);
      expect(seen.single.headers['X-Device-Id'], dev);
      expect(seen.single.headers['Authorization'], 'Bearer tok');
    },
  );

  test(
    'push-token: POST /v1/devices/{id}/push-token {provider: fcm, token}',
    () async {
      final a = api();
      await a.registerPushToken(
        deviceId: dev,
        token: 'fcm_TOKEN-abc:123.xyz_00000000',
        idempotencyKey: 'ptok:abcdef:k1',
      );
      final r = seen.single;
      expect(r.method, 'POST');
      expect(r.url.path, '/v1/devices/$dev/push-token');
      expect(r.headers['Idempotency-Key'], 'ptok:abcdef:k1');
      expect(jsonDecode(bodies.single), {
        'provider': 'fcm',
        'token': 'fcm_TOKEN-abc:123.xyz_00000000',
      });
    },
  );

  test('lệnh xếp hàng: POST path + Idempotency-Key + thân tùy chọn', () async {
    final a = api(body: jsonEncode({'marked': 3}));
    final out = await a.postAction(
      path: '/v1/notifications/read-all',
      idempotencyKey: 'act:0192-1',
    );
    expect(out['marked'], 3);
    expect(seen.single.url.path, '/v1/notifications/read-all');
    expect(seen.single.headers['Idempotency-Key'], 'act:0192-1');
    expect(bodies.single, isEmpty);

    await a.postAction(
      path: '/v1/documents/d1/save',
      idempotencyKey: 'act:0192-2',
      body: {'note': 'đọc lại'},
    );
    expect(jsonDecode(bodies.last), {'note': 'đọc lại'});
  });

  test('404 của lệnh → ApiException không thử lại', () async {
    final a = api(
      status: 404,
      body: jsonEncode({'code': 'notify.notification_not_found'}),
    );
    await expectLater(
      a.postAction(path: '/v1/notifications/x/read', idempotencyKey: 'act:1'),
      throwsA(
        isA<ApiException>()
            .having((e) => e.code, 'code', 'notify.notification_not_found')
            .having((e) => e.retriable, 'retriable', isFalse),
      ),
    );
  });

  test('tải tệp gửi file_ref (UUIDv7 máy sinh) như trường văn bản', () async {
    final a = api(status: 201, body: jsonEncode({'file_ref': 'f1'}));
    await a.uploadFile(
      idempotencyKey: 'file:f1',
      meta: const {'v': 1},
      ciphertext: Uint8List.fromList([1, 2, 3]),
      fileRef: 'f1',
    );
    final r = seen.single;
    expect(r.headers['content-type'], startsWith('multipart/form-data'));
    expect(r.headers['X-Device-Id'], dev);
    final b = bodies.single;
    expect(b, contains('name="file_ref"\r\n\r\nf1'));
    expect(b, contains('name="meta"\r\n\r\n{"v":1}'));
  });

  test('pull đọc reset', () {
    final p = PullPage.fromJson({
      'changes': [],
      'next_cursor': 'c',
      'has_more': false,
      'reset': true,
    });
    expect(p.reset, isTrue);
  });
}

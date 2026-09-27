import 'dart:convert';
import 'dart:typed_data';

import 'package:agrigreen_field/data/sync/sync_api.dart';
import 'package:agrigreen_field/domain/outbox_record.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

void main() {
  final rec = OutboxRecord(
    id: '0192aaaa-0000-7000-8000-000000000001',
    seq: 7,
    idemKey: 'dev:dev1:7',
    entity: PushEntity.fieldReport,
    op: RecordOp.create,
    eventTime: '2026-12-02T08:15:00+07:00',
    recordedAt: DateTime.utc(2026, 12, 2),
    payload: const {'note': 'bón phân'},
    status: OutboxStatus.pending,
  );

  test('push: Idempotency-Key = mã lô, Bearer token, thân đúng hợp đồng; đọc kết quả', () async {
    late http.Request seen;
    final api = HttpSyncApi(
      baseUrl: 'https://api.test',
      accessToken: () async => 'tok',
      client: MockClient((req) async {
        seen = req;
        return http.Response(
          jsonEncode({
            'results': [
              {'id': rec.id, 'status': 'conflict', 'conflict_id': 'c1'},
            ],
          }),
          200,
        );
      }),
    );
    final res = await api.push(
      batchKey: 'batch-0001',
      deviceId: 'dev1',
      records: [rec],
    );
    expect(seen.method, 'POST');
    expect(seen.url.toString(), 'https://api.test/v1/sync/push');
    expect(seen.headers['Idempotency-Key'], 'batch-0001');
    expect(seen.headers['Authorization'], 'Bearer tok');
    final body = jsonDecode(seen.body) as Map<String, Object?>;
    expect(body['device_id'], 'dev1');
    expect(body['records'], [
      {
        'id': rec.id,
        'idem_key': 'dev:dev1:7',
        'entity': 'field_report',
        'op': 'create',
        'event_time': '2026-12-02T08:15:00+07:00',
        'supersedes_id': null,
        'correction_reason': null,
        'payload': {'note': 'bón phân'},
      },
    ]);
    expect(res.single.status, 'conflict');
    expect(res.single.conflictId, 'c1');
  });

  test('thân lô ổn định từng byte giữa các lần mã hóa', () {
    expect(
      HttpSyncApi.encodePushBody('d', [rec]),
      HttpSyncApi.encodePushBody('d', [rec]),
    );
  });

  test('pull: gửi cursor và limit, đọc changes/next_cursor/has_more', () async {
    late http.BaseRequest seen;
    final api = HttpSyncApi(
      baseUrl: 'https://api.test',
      accessToken: () async => 'tok',
      client: MockClient((req) async {
        seen = req;
        return http.Response(
          jsonEncode({
            'changes': [
              {
                'entity': 'plot',
                'id': 'p',
                'version': 3,
                'op': 'upsert',
                'data': {},
              },
              {'entity': 'task', 'id': 't', 'version': 1, 'op': 'tombstone'},
            ],
            'next_cursor': 'opaque==',
            'has_more': true,
            'server_time': '2026-12-03T02:10:00Z',
          }),
          200,
        );
      }),
    );
    final page = await api.pull(cursor: 'abc+/=', limit: 500);
    expect(seen.url.path, '/v1/sync/pull');
    expect(seen.url.queryParameters, {'cursor': 'abc+/=', 'limit': '500'});
    expect(page.changes, hasLength(2));
    expect(page.changes[1].op, 'tombstone');
    expect(page.nextCursor, 'opaque==');
    expect(page.hasMore, isTrue);

    await api.pull(limit: 500);
    expect(seen.url.queryParameters.containsKey('cursor'), isFalse);
  });

  test(
    'tải tệp: multipart có meta (JSON) và blob (ciphertext), Idempotency-Key',
    () async {
      late http.Request seen;
      final api = HttpSyncApi(
        baseUrl: 'https://api.test',
        accessToken: () async => 'tok',
        client: MockClient((req) async {
          seen = req;
          return http.Response(jsonEncode({'file_ref': 'fr1'}), 201);
        }),
      );
      final ref = await api.uploadFile(
        idempotencyKey: 'file:abc',
        meta: const {'v': 1, 'alg': 'A256GCM+X25519-SEALEDBOX'},
        ciphertext: Uint8List.fromList([9, 8, 7]),
      );
      expect(ref, 'fr1');
      expect(seen.url.path, '/v1/files');
      expect(seen.headers['Idempotency-Key'], 'file:abc');
      expect(seen.headers['content-type'], startsWith('multipart/form-data'));
      final body = latin1.decode(seen.bodyBytes);
      expect(body, contains('name="meta"'));
      expect(body, contains('"alg":"A256GCM+X25519-SEALEDBOX"'));
      expect(body, contains('name="blob"'));
    },
  );

  test('401 → UnauthenticatedException; 5xx và 409 in_progress là lỗi thử lại được', () async {
    Future<Object?> call(int status, String code) async {
      final api = HttpSyncApi(
        baseUrl: 'https://api.test',
        accessToken: () async => null,
        client: MockClient(
          (_) async => http.Response(jsonEncode({'code': code}), status),
        ),
      );
      try {
        await api.pull(limit: 1);
      } catch (e) {
        return e;
      }
      return null;
    }

    expect(await call(401, 'unauthenticated'), isA<UnauthenticatedException>());
    expect((await call(503, 'x') as ApiException).retriable, isTrue);
    expect(
      (await call(409, 'idempotency_in_progress') as ApiException).retriable,
      isTrue,
    );
    expect(
      (await call(422, 'validation_failed') as ApiException).retriable,
      isFalse,
    );
  });

  test('lỗi mạng → NetworkException', () async {
    final api = HttpSyncApi(
      baseUrl: 'https://api.test',
      accessToken: () async => null,
      client: MockClient((_) async => throw http.ClientException('no route')),
    );
    await expectLater(api.pull(limit: 1), throwsA(isA<NetworkException>()));
  });
}

import 'dart:convert';

import 'package:agrigreen_mobile/data/actions/action_queue.dart';
import 'package:agrigreen_mobile/data/outbox/outbox_repository.dart';
import 'package:agrigreen_mobile/data/reference/reference_repository.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';
import 'package:agrigreen_mobile/testing.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'support/harness.dart';

/// Máy chủ giả trả `fieldops.cursor_invalid` cho con trỏ lạ (đồng bộ vùng mkg).
class _MkgCursorServer extends FakeServer {
  @override
  Future<PullPage> pull({
    String? cursor,
    required int limit,
    List<String>? entities,
  }) async {
    if (cursor == 'tampered') {
      pullCursors.add(cursor);
      throw ApiException(400, 'fieldops.cursor_invalid');
    }
    return super.pull(cursor: cursor, limit: limit, entities: entities);
  }
}

void main() {
  test(
    'G3 mkg: HttpSyncApi mặc định giữ đường dẫn nền tảng /v1/sync/push|pull',
    () async {
      final urls = <String>[];
      final api = HttpSyncApi(
        baseUrl: 'https://api.test',
        accessToken: () async => 't',
        deviceId: 'd1',
        client: MockClient((req) async {
          urls.add(req.url.path);
          return http.Response(
            jsonEncode(
              req.method == 'POST'
                  ? {'results': <Object?>[]}
                  : {'changes': <Object?>[], 'has_more': false},
            ),
            200,
          );
        }),
      );
      await api.push(batchKey: 'b', deviceId: 'd1', records: const []);
      await api.pull(limit: 10);
      expect(urls, ['/v1/sync/push', '/v1/sync/pull']);
    },
  );

  test('G3 mkg: pushPath/pullPath trỏ /v1/fieldops/sync/* của api-internal, vẫn gửi X-Device-Id', () async {
    final seen = <http.BaseRequest>[];
    final api = HttpSyncApi(
      baseUrl: 'https://mkg-internal.test',
      accessToken: () async => 't',
      deviceId: 'd1',
      pushPath: '/v1/fieldops/sync/push',
      pullPath: '/v1/fieldops/sync/pull',
      client: MockClient((req) async {
        seen.add(req);
        return http.Response(
          jsonEncode(
            req.method == 'POST'
                ? {
                    'results': [
                      {'id': 'r1', 'status': 'accepted'},
                    ],
                  }
                : {
                    'changes': [
                      {
                        'entity': 'collection_order',
                        'id': 'o1',
                        'version': 3,
                        'op': 'upsert',
                        'data': {'order_id': 'o1', 'status': 'approved'},
                      },
                    ],
                    'next_cursor': 'c2',
                    'has_more': false,
                  },
          ),
          200,
        );
      }),
    );
    final res = await api.push(
      batchKey: 'b1',
      deviceId: 'd1',
      records: const [],
    );
    final page = await api.pull(cursor: 'c1', limit: 50);
    expect(
      seen[0].url.toString(),
      'https://mkg-internal.test/v1/fieldops/sync/push',
    );
    expect(seen[0].headers['X-Device-Id'], 'd1');
    expect(seen[0].headers['Idempotency-Key'], 'b1');
    expect(seen[1].url.path, '/v1/fieldops/sync/pull');
    expect(seen[1].url.queryParameters, {'cursor': 'c1', 'limit': '50'});
    expect(seen[1].headers['X-Device-Id'], 'd1');
    expect(res.single.status, 'accepted');
    expect(page.changes.single.entity, RefEntity.collectionOrder);
  });

  test('G3 mkg: OutboxRepository chỉ nhận thực thể được phép (mặc định nền tảng; MKG roll_count)', () async {
    final h = await Harness.create();
    addTearDown(h.dispose);
    await expectLater(
      h.outbox.enqueue(entity: PushEntity.rollCount, payload: const {}),
      throwsA(isA<OutboxException>()),
    );
    final mkg = OutboxRepository(
      db: h.db,
      deviceId: testDeviceId,
      clock: h.clock,
      ids: h.ids,
      entities: PushEntity.mkgFieldops,
    );
    final r = await mkg.enqueue(
      entity: PushEntity.rollCount,
      payload: const {'rolls': 3},
    );
    expect(r.idemKey, 'dev:$testDeviceId:${r.seq}');
    await expectLater(
      mkg.enqueue(entity: PushEntity.fieldReport, payload: const {}),
      throwsA(isA<OutboxException>()),
    );
  });

  test('G3 mkg: fieldops.sink_failed giữ pending (tự gửi lại), không thành rejected', () async {
    final server = FakeServer();
    final h = await Harness.create(server: server);
    addTearDown(h.dispose);
    final rec = await h.outbox.enqueue(
      entity: PushEntity.fieldReport,
      payload: const {'note': 'x'},
    );
    final batch = (await h.outbox.claimBatch(10))!;
    await h.outbox.applyResults(batch.batchKey, [
      PushResult(id: rec.id, status: 'rejected', code: 'fieldops.sink_failed'),
    ]);
    expect((await h.outbox.byId(rec.id))!.status, OutboxStatus.pending);
  });

  test('G3 mkg: fieldops.cursor_invalid → pull lại từ đầu một lần', () async {
    final server = _MkgCursorServer();
    server.pages[null] = const PullPage(
      changes: [],
      nextCursor: 'fresh',
      hasMore: false,
    );
    final h = await Harness.create(server: server);
    addTearDown(h.dispose);
    await h.db.setMeta(ReferenceRepository.cursorKey, 'tampered');
    final rep = await h.sync.syncOnce(force: true);
    expect(rep, isNotNull);
    expect(server.pullCursors, ['tampered', null]);
    expect(await h.refs.cursor(), 'fresh');
  });

  test('G3 mkg: lệnh mở/đóng ca tổ gom xếp hàng tới đúng đường dẫn', () {
    expect(
      ApiActionKind.fieldopsCrewShift.pathFor('o1'),
      '/v1/fieldops/orders/o1/crew-shifts',
    );
    expect(
      ApiActionKind.parse('fieldops_crew_shift'),
      ApiActionKind.fieldopsCrewShift,
    );
  });
}

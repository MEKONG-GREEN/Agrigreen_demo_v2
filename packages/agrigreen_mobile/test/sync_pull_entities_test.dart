import 'dart:math';

import 'package:agrigreen_mobile/core/backoff.dart';
import 'package:agrigreen_mobile/data/reference/reference_repository.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:agrigreen_mobile/data/sync/sync_service.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/harness.dart';

PullChange _up(String entity, String id, int v) => PullChange(
  entity: entity,
  id: id,
  version: v,
  op: 'upsert',
  data: {'label': '$entity $id'},
);

/// KN-STANDALONE-9 (#270): app khai `pullEntities` chỉ kéo các nguồn của mình
/// ở MỌI lượt pull; app không khai (App HTX, App MKG) giữ nguyên "pull tất cả".
void main() {
  late Harness h;
  late SyncService sync;
  setUp(() async {
    h = await Harness.create();
    sync = SyncService(
      db: h.db,
      api: h.server,
      outbox: h.outbox,
      refs: h.refs,
      evidence: h.evidence,
      wipe: h.wipe,
      clock: h.clock,
      deviceId: testDeviceId,
      backoff: const Backoff(jitter: 0),
      random: Random(1),
      pullEntities: const {'kn_plot', 'notification'},
    );
  });
  tearDown(() async {
    await sync.dispose();
    await h.dispose();
  });

  test('lượt đầy đủ gửi ?entities = danh sách của app (đã sắp xếp); entity '
      'ngoài danh sách không về máy', () async {
    h.fake.pages[null] = PullPage(
      changes: [_up('kn_plot', 'p1', 1), _up('plot', 'x1', 1)],
      nextCursor: 'c1',
      hasMore: false,
    );
    await sync.syncOnce(force: true);
    expect(h.fake.pullEntities.single, ['kn_plot', 'notification']);
    expect(await h.refs.count(RefEntity.knPlot), 1);
    expect(await h.refs.count(RefEntity.plot), 0);
  });

  test('sync_now có entities[] → giao với danh sách của app; không giao '
      'nhau → kéo danh sách của app', () async {
    h.fake.pages[null] = const PullPage(
      changes: [],
      nextCursor: 'c1',
      hasMore: false,
    );
    h.fake.pages['c1'] = const PullPage(
      changes: [],
      nextCursor: 'c1',
      hasMore: false,
    );
    await sync.syncOnce(force: true, entities: {'kn_plot', 'farm_log'});
    expect(h.fake.pullEntities.last, ['kn_plot']);
    await sync.syncOnce(force: true, entities: {'farm_log', 'season'});
    expect(h.fake.pullEntities.last, ['kn_plot', 'notification']);
  });

  test('trang reset của lượt đầy đủ (theo danh sách của app) được ÁP — không '
      'rơi về pull tất cả', () async {
    await h.refs.applyPage(
      PullPage(
        changes: [_up('kn_plot', 'p0', 1)],
        nextCursor: 'c0',
        hasMore: false,
      ),
    );
    h.fake.pages['c0'] = PullPage(
      changes: [_up('kn_plot', 'p1', 1)],
      nextCursor: 'c1',
      hasMore: false,
      reset: true,
    );
    await sync.syncOnce(force: true);
    expect(h.fake.pullEntities, [
      ['kn_plot', 'notification'],
    ]);
    expect((await h.refs.list(RefEntity.knPlot)).single.id, 'p1');
    expect(await h.refs.cursor(), 'c1');
  });

  test('trang reset khi pull một phần → bỏ trang, pull lại theo danh sách '
      'của app (không bao giờ pull tất cả)', () async {
    h.fake.pages[null] = PullPage(
      changes: [_up('kn_plot', 'p1', 1), _up('notification', 'n1', 1)],
      nextCursor: 'c1',
      hasMore: false,
      reset: true,
    );
    await sync.syncOnce(force: true, entities: {'kn_plot'});
    expect(h.fake.pullEntities, [
      ['kn_plot'],
      ['kn_plot', 'notification'],
    ]);
    expect(await h.refs.count(RefEntity.notification), 1);
  });

  test('không khai pullEntities (App HTX/MKG) → pull tất cả như cũ', () async {
    h.fake.pages[null] = const PullPage(
      changes: [],
      nextCursor: 'c1',
      hasMore: false,
    );
    await h.sync.syncOnce(force: true);
    expect(h.fake.pullEntities.single, isNull);
  });
}

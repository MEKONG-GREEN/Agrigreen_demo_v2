import 'package:agrigreen_htx/application/news_service.dart';
import 'package:agrigreen_htx/domain/cropcycle.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/test_env.dart';

Map<String, Object?> _doc(String id, String title, {String? severity}) => {
  'entity': 'document',
  'id': id,
  'op': 'upsert',
  'data': {
    'title': title,
    'summary': 'Tóm tắt $title',
    'body': 'Nội dung $title',
    'published_at': '2026-09-2${id.length % 9}T00:00:00Z',
    'severity': ?severity,
  },
};

void main() {
  testWidgets(
    'HTX US-NEWS-02: tin đồng bộ về máy, lưu đọc ngoại tuyến (lệnh xếp hàng), '
    'bài đã lưu bị gỡ vẫn đọc được kèm nhãn; cảnh báo khẩn cấp lên đầu',
    (tester) async {
      final env = TestEnv()
        ..seed(
          extra: [
            _doc('doc-1', 'Kỹ thuật sạ hàng'),
            _doc('doc-22', 'Cảnh báo rầy nâu', severity: 'urgent'),
            _doc('doc-333', 'Quy trình cũ'),
            {
              'entity': 'library_item',
              'id': 'doc-333',
              'op': 'upsert',
              'data': {
                'document_ref': 'doc-333',
                'title': 'Quy trình cũ',
                'available': false,
              },
            },
          ],
        );
      final s = await loginToHome(tester, env);
      final list = await s.news.articles();
      expect(list.first.title, 'Cảnh báo rầy nâu');
      expect(list.firstWhere((a) => a.ref == 'doc-333').available, isFalse);

      env.server.failNextActions = 100; // mất mạng
      await openTile(tester, 'Tin tức');
      await tapText(tester, 'Kỹ thuật sạ hàng');
      await tapOn(tester, find.byTooltip('Lưu đọc ngoại tuyến'));
      expect(find.byTooltip('Bỏ lưu'), findsOneWidget);
      await tester.pageBack();
      await settle(tester);
      await tapText(tester, 'Đã lưu');
      expect(find.text('Kỹ thuật sạ hàng'), findsOneWidget);
      await tapText(tester, 'Quy trình cũ');
      expect(find.text(msgArticleWithdrawn), findsOneWidget);
      expect(find.text('Nội dung Quy trình cũ'), findsOneWidget);

      await s.sync.syncOnce(force: true);
      expect(await s.actions.pendingCount(), 1);
      env.server.failNextActions = 0;
      await s.sync.syncOnce(force: true);
      expect(env.server.actionEffects['/v1/documents/doc-1/save'], 1);
      await finish(tester);
    },
  );

  testWidgets(
    'HTX US-LOG-04: hàng đợi — chờ gửi khi lỗi máy chủ nhiều lần (giữ bản ghi, '
    'lùi thử lại), rồi accepted; bị từ chối hiện lý do dễ hiểu',
    (tester) async {
      final server = CodedRejectServer()..code = 'cropcycle.season_not_open';
      final env = TestEnv(server: server)..seed(season: true);
      final s = await loginToHome(tester, env);
      final season = (await s.crop.openSeasonFor(plotRef))!;
      final ok = await s.crop.recordLog(
        season: season,
        activity: FarmActivity.tuoiNuoc,
      );
      final bad = await s.crop.recordLog(
        season: season,
        activity: FarmActivity.kiemTra,
        note: 'từ chối',
      );
      server.rejectIf = (w) => (w['payload']! as Map)['note'] == 'từ chối';
      server.failNextPushes = 3;
      for (var i = 0; i < 3; i++) {
        await s.sync.syncOnce(force: true);
        expect((await s.outbox.byId(ok.id))!.status, OutboxStatus.pending);
      }
      final st = await s.sync.status();
      expect(st.failures, 3);
      expect(st.nextAttemptAt, isNotNull);
      await s.sync.syncOnce(force: true);
      expect((await s.outbox.byId(ok.id))!.status, OutboxStatus.sent);
      expect((await s.outbox.byId(bad.id))!.status, OutboxStatus.rejected);
      // Một bản ghi đúng một lần trên máy chủ dù gửi lại nhiều lần.
      expect(server.ledger.values.where((w) => w['id'] == ok.id), hasLength(1));

      await openTile(tester, 'Hàng đợi đồng bộ');
      expect(find.text(msgSeasonNotOpen), findsOneWidget);
      expect(find.text('Đã đồng bộ'), findsOneWidget);
      await finish(tester);
    },
  );

  for (final size in const [Size(320, 568), Size(360, 640)]) {
    for (final roles in const [
      ['farmer'],
      ['coop_manager'],
    ]) {
      testWidgets(
        'màn hình nhỏ ${size.width.toInt()}×${size.height.toInt()} (${roles.first}): '
        'trang chủ, ghi nhật ký, mở vụ không tràn',
        (tester) async {
          tester.view.physicalSize = size;
          tester.view.devicePixelRatio = 1;
          addTearDown(tester.view.reset);
          final env = TestEnv(roles: roles)..seed(season: true);
          await loginToHome(tester, env);
          expect(tester.takeException(), isNull);
          await openTile(
            tester,
            roles.first == 'farmer' ? 'Ghi nhật ký' : 'Ghi nhật ký hộ',
          );
          expect(tester.takeException(), isNull);
          await tester.pageBack();
          await settle(tester);
          if (roles.first == 'farmer') {
            await openTile(tester, 'Ruộng của tôi');
            await tapText(tester, 'Lô T2');
            await tapText(tester, 'Mở vụ mới');
            expect(tester.takeException(), isNull);
          }
          await finish(tester);
        },
      );
    }
  }
}

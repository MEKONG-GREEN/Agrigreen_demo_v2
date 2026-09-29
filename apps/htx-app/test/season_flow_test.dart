import 'package:agrigreen_htx/domain/cropcycle.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/test_env.dart';

void main() {
  testWidgets(
    'HTX US-SEASON-01: mở vụ 4 bước ngoại tuyến — SOP tự gán theo giống, '
    'ngày sạ ngoài [−15, +30] bị chặn, lưu "Chờ đồng bộ" rồi push accepted',
    (tester) async {
      final env = TestEnv()..seed();
      final s = await loginToHome(tester, env);
      env.server.failNextPushes = 100; // mất mạng khi đồng bộ

      await openTile(tester, 'Ruộng của tôi');
      await tapText(tester, 'T1 · Ruộng bờ kênh');
      await tapText(tester, 'Mở vụ mới');
      expect(find.text('Mở vụ mới · Bước 1/4'), findsOneWidget);

      // Bước 1: lô đã chọn sẵn; chọn vụ.
      await tapText(tester, 'Thu Đông');
      await tapText(tester, 'Tiếp');
      // Bước 2: giống → SOP tự gán (HTX US-SEASON-01 AC-1).
      await tapText(tester, 'OM18');
      expect(find.text('Quy trình lúa OM18'), findsOneWidget);
      await tapText(tester, 'Tiếp');
      // Bước 3: ngày sạ 21 ngày trước → chặn (AC-2).
      await tester.enterText(
        find.byKey(const ValueKey('sowing-date')),
        '10/09/2026',
      );
      await tapText(tester, 'Tiếp');
      expect(find.text(msgSowingWindow), findsOneWidget);
      expect(find.text('Mở vụ mới · Bước 3/4'), findsOneWidget);
      await tester.enterText(
        find.byKey(const ValueKey('sowing-date')),
        '16/09/2026',
      );
      await tapText(tester, 'Tiếp');
      // Bước 4: xác nhận.
      expect(find.text('Mở vụ mới · Bước 4/4'), findsOneWidget);
      await tapText(tester, 'Lưu mở vụ');
      expect(find.textContaining('Chờ đồng bộ'), findsWidgets);

      final rec = (await s.outbox.list(entity: PushEntity.seasonOpen)).single;
      expect(rec.status, OutboxStatus.pending);
      expect(rec.payload['plot_ref'], plotRef);
      expect(rec.payload['season_code'], 'TD');
      expect(rec.payload['season_year'], 2026);
      expect(rec.payload['variety_code'], 'OM18');
      expect(rec.payload['sowing_date'], '2026-09-16');
      expect(rec.payload['sop_version_id'], sopId);
      expect(rec.payload['gps'], {
        'lat': '10.0451234',
        'lon': '105.7701234',
        'accuracy_m': '6.0',
        'source': 'device',
      });
      // Vụ mở trên máy dùng được ngay (ghi nhật ký ngoại tuyến).
      final open = await s.crop.openSeasonFor(plotRef);
      expect(open?.localPending, isTrue);
      expect(open?.seasonRef, rec.payload['season_ref']);

      await s.sync.syncOnce(force: true);
      expect((await s.outbox.byId(rec.id))!.status, OutboxStatus.pending);

      // Có mạng lại → gửi đúng một lần.
      env.server.failNextPushes = 0;
      await s.sync.syncOnce(force: true);
      expect((await s.outbox.byId(rec.id))!.status, OutboxStatus.sent);
      expect(
        env.server.ledger.values.where((w) => w['entity'] == 'season_open'),
        hasLength(1),
      );
      await finish(tester);
    },
  );

  test('HTX US-SEASON-01 AC-2: cửa sổ ngày sạ theo ngày Việt Nam', () {
    final today = vnToday(DateTime.utc(2026, 9, 30, 18)); // 01:00 ngày 01/10 VN
    expect(formatDay(today), '2026-10-01');
    expect(checkSowingDate(DateTime.utc(2026, 9, 16), today), isNull);
    expect(checkSowingDate(DateTime.utc(2026, 9, 15), today), msgSowingWindow);
    expect(checkSowingDate(DateTime.utc(2026, 10, 31), today), isNull);
    expect(checkSowingDate(DateTime.utc(2026, 11, 1), today), msgSowingWindow);
  });

  testWidgets(
    'HTX US-SEASON-02: quản lý mở vụ hàng loạt — thửa lỗi bỏ qua kèm lý do',
    (tester) async {
      final env = TestEnv(roles: const ['coop_manager'])..seed();
      await loginToHome(tester, env);
      Map<String, Object?>? body;
      env.server.onPost = (path, b) async {
        if (path != '/v1/cropcycle/seasons/bulk-open') return null;
        body = b;
        return {
          'opened': 1,
          'skipped': 1,
          'items': [
            {'plot_ref': plotRef, 'status': 'opened'},
            {
              'plot_ref': plot2Ref,
              'status': 'skipped',
              'reason': 'Lô đang canh tác vụ khác',
            },
          ],
        };
      };
      await openTile(tester, 'Mở vụ hàng loạt');
      await tapOn(tester, find.byType(CheckboxListTile).at(0));
      await tapOn(tester, find.byType(CheckboxListTile).at(1));
      await tapText(tester, 'Hè Thu');
      await tapText(tester, 'ST25');
      await tapText(tester, 'Mở vụ cho các lô đã chọn');
      expect(find.text('Đã mở vụ cho 1 lô'), findsOneWidget);
      expect(
        find.textContaining('Lô T2: Lô đang canh tác vụ khác'),
        findsOneWidget,
      );
      expect(body!['plot_refs'], [plotRef, plot2Ref]);
      expect(body!['season_code'], 'HT');
      expect(body!['sowing_date'], '2026-10-01');
      await tapText(tester, 'Đóng');
      await finish(tester);
    },
  );
}

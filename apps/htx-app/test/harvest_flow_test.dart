import 'package:agrigreen_htx/domain/cropcycle.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/test_env.dart';

void main() {
  testWidgets(
    'HTX US-YIELD-01 AC-1: trước cổng thu hoạch của SOP → chặn khai, đúng thông báo',
    (tester) async {
      final env = TestEnv()..seed(season: true, harvestOpen: '2027-01-10');
      final s = await loginToHome(tester, env);
      await openTile(tester, 'Khai sản lượng');
      await tapText(tester, 'Lô T1');
      expect(find.byKey(const ValueKey('harvest-blocked')), findsOneWidget);
      expect(find.text(msgHarvestTooEarly), findsOneWidget);
      expect(find.byKey(const ValueKey('harvest-kg')), findsNothing);
      expect(await s.outbox.list(entity: PushEntity.harvest), isEmpty);
      await finish(tester);
    },
  );

  testWidgets(
    'HTX US-YIELD-01/03: kg → t/ha; vượt ngưỡng của giống → hỏi xác nhận, '
    '"Sửa lại" không lưu, "Vẫn lưu" → confirm_anomaly',
    (tester) async {
      final env = TestEnv()..seed(season: true, harvestOpen: '2026-09-30');
      final s = await loginToHome(tester, env);
      await openTile(tester, 'Khai sản lượng');
      await tapText(tester, 'Lô T1');
      await tester.enterText(find.byKey(const ValueKey('harvest-kg')), '20000');
      await settle(tester, 3);
      expect(find.text('Năng suất: 10,00 t/ha'), findsOneWidget);
      await tapText(tester, 'Lưu sản lượng');
      expect(find.text('Sản lượng bất thường'), findsOneWidget);
      expect(
        find.textContaining('vượt ngưỡng bất thường 9 t/ha'),
        findsOneWidget,
      );
      await tapText(tester, 'Sửa lại');
      expect(await s.outbox.list(entity: PushEntity.harvest), isEmpty);

      await tapText(tester, 'Lưu sản lượng');
      await tapText(tester, 'Vẫn lưu');
      final rec = (await s.outbox.list(entity: PushEntity.harvest)).single;
      expect(rec.payload['mass_kg'], '20000');
      expect(rec.payload['confirm_anomaly'], isTrue);
      expect(rec.payload['harvest_date'], '2026-10-01');
      expect(rec.payload['season_ref'], '01920000-0000-7000-8000-00000000e001');
      await finish(tester);
    },
  );

  testWidgets(
    'HTX US-YIELD-03: máy chủ từ chối cropcycle.yield_anomaly → xác nhận trong '
    'hàng đợi → bản ghi MỚI confirm_anomaly → accepted, bản cũ superseded',
    (tester) async {
      final server = CodedRejectServer()..code = 'cropcycle.yield_anomaly';
      final env = TestEnv(server: server)
        ..seed(season: true, harvestOpen: '2026-09-30');
      server.rejectIf = (w) =>
          w['entity'] == 'harvest' &&
          (w['payload']! as Map)['confirm_anomaly'] != true;
      final s = await loginToHome(tester, env);
      final season = (await s.crop.openSeasonFor(plotRef))!;
      final orig = await s.crop.declareHarvest(
        season: season,
        massKgInput: '15000',
        harvestDate: DateTime.utc(2026, 10, 1),
      );
      expect(orig.payload['confirm_anomaly'], isFalse);
      await s.sync.syncOnce(force: true);
      expect((await s.outbox.byId(orig.id))!.status, OutboxStatus.rejected);

      await openTile(tester, 'Hàng đợi đồng bộ');
      expect(
        find.text('Sản lượng bất thường — cần xác nhận lại trước khi lưu.'),
        findsOneWidget,
      );
      await tapText(tester, 'Xác nhận sản lượng');
      await tapText(tester, 'Xác nhận đúng');
      await s.sync.syncOnce(force: true);
      final all = await s.outbox.list(entity: PushEntity.harvest);
      final fresh = all.firstWhere((r) => r.id != orig.id);
      expect(fresh.op, RecordOp.create);
      expect(fresh.payload['confirm_anomaly'], isTrue);
      expect(fresh.status, OutboxStatus.sent);
      expect((await s.outbox.byId(orig.id))!.status, OutboxStatus.superseded);
      await finish(tester);
    },
  );
}

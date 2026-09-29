import 'package:agrigreen_htx/application/crop_service.dart';
import 'package:agrigreen_htx/domain/cropcycle.dart';
import 'package:agrigreen_htx/ui/screens/batch_log_screen.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/test_env.dart';

const seasonRef = '01920000-0000-7000-8000-00000000e001';
const season2Ref = '01920000-0000-7000-8000-00000000e002';

Map<String, Object?> _season2() => {
  'entity': 'season',
  'id': 's-2',
  'op': 'upsert',
  'data': {
    'season_ref': season2Ref,
    'plot_ref': plot2Ref,
    'plot_code': 'T2',
    'household_ref': householdRef,
    'cooperative_ref': coopRef,
    'season_code': 'TD',
    'season_year': 2026,
    'variety_code': 'OM18',
    'sop_version_id': sopId,
    'sowing_date': '2026-09-25',
    'harvest_open_date': '2027-01-10',
    'area_ha': '1.5000',
    'status': 'open',
    'current_event_id': '01920000-0000-7000-8000-00000000e0e2',
  },
};

PullChange _line(String id, String batch, String plot, String season) =>
    PullChange.fromJson({
      'entity': 'farm_log',
      'id': id,
      'version': 90,
      'op': 'upsert',
      'data': {
        'id': id,
        'log_ref': id,
        'season_ref': season,
        'plot_ref': plot,
        'activity_code': 'bon_phan',
        'event_time': '2026-10-01T10:00:00+07:00',
        'gps_source': 'manual_pick',
        'location_status': 'missing',
        'machine_codes': [],
        'inputs': [],
        'note': 'Bón thúc đợt 1',
        'file_refs': [],
        'batch_ref': batch,
        'on_behalf': true,
        'status': 'pending',
      },
    });

/// Cuộn danh sách dựng lười tới khi [f] được dựng.
Future<void> _reveal(WidgetTester tester, Finder f) async {
  await tester.scrollUntilVisible(
    f,
    250,
    scrollable: find.byType(Scrollable).first,
  );
  await tester.pump();
}

void main() {
  testWidgets(
    'HTX US-LOG-03: quản lý chọn nhiều lô → MỘT bản ghi farm_log_batch (một '
    'đầu lô, AC-1); lô chưa mở vụ bị loại và liệt kê kèm lý do (AC-2); sau '
    'đồng bộ hiện số lô máy chủ đã ghi',
    (tester) async {
      final env = TestEnv(roles: const ['coop_manager'])..seed(season: true);
      final s = await loginToHome(tester, env);
      await openTile(tester, 'Nhật ký hàng loạt');
      expect(find.byType(BatchLogScreen), findsOneWidget);
      await tapOn(tester, find.byKey(const ValueKey('batch-plot-T1')));
      await tapOn(tester, find.byKey(const ValueKey('batch-plot-T2')));
      await tapOn(tester, find.byKey(const ValueKey('act-bon_phan')));
      await _reveal(tester, find.byKey(const ValueKey('batch-note')));
      await tester.enterText(
        find.byKey(const ValueKey('batch-note')),
        'Bón thúc đợt 1',
      );
      await tapText(tester, 'Ghi cho các lô đã chọn');
      // Xác nhận liệt kê lô bị bỏ qua (AC-2).
      expect(find.text('Ghi nhật ký cho 1 lô'), findsOneWidget);
      expect(find.textContaining('• Lô T2: $skipNotOpen'), findsOneWidget);
      await tapText(tester, 'Ghi hàng loạt');

      final recs = await s.outbox.list(entity: PushEntity.farmLogBatch);
      expect(recs, hasLength(1));
      final rec = recs.single;
      expect(rec.payload, {
        'plot_refs': [plotRef],
        'activity_code': 'bon_phan',
        'machine_codes': [],
        'inputs': [],
        'note': 'Bón thúc đợt 1',
      });
      expect(
        rec.payload.containsKey('gps'),
        isFalse,
        reason: 'chọn lô từ danh sách → máy chủ gắn manual_pick (US-GPS-02)',
      );
      // Dòng trên máy hiện ngay ở chi tiết lô T1 (chờ đồng bộ).
      final t1 = await s.crop.logs(seasonRef: seasonRef);
      expect(t1.single.view.batchRef, rec.id);
      expect(t1.single.statusLabel, 'Chờ đồng bộ');
      expect(find.textContaining('Chờ đồng bộ'), findsWidgets);

      await s.sync.syncOnce(force: true);
      expect((await s.outbox.byId(rec.id))!.status, OutboxStatus.sent);
      env.server.pages['c1'] = PullPage(
        changes: [
          _line(
            '01920000-0000-7000-8000-00000000f1a1',
            rec.id,
            plotRef,
            seasonRef,
          ),
        ],
        nextCursor: 'c2',
        hasMore: false,
      );
      await s.sync.syncOnce(force: true);
      final b = (await s.crop.batches()).single;
      expect(b.accepted, 1);
      expect(b.serverSkipped, isEmpty);
      // Dòng máy chủ thay dòng trên máy (không trùng).
      final after = await s.crop.logs(seasonRef: seasonRef);
      expect(after, hasLength(1));
      expect(after.single.local, isNull);
      await finish(tester);
    },
  );

  testWidgets(
    'HTX US-LOG-03 AC-2: máy chủ loại thêm lô (ngoài phạm vi / vụ đã đóng lúc '
    'nhận) → "Máy chủ bỏ qua"; không còn lô hợp lệ → chặn trên máy',
    (tester) async {
      final env = TestEnv(roles: const ['coop_manager'])
        ..seed(season: true, extra: [_season2()]);
      final s = await loginToHome(tester, env);
      final plots = await s.crop.plots();
      final (rec, plan) = await s.crop.recordBatch(
        plots: plots,
        activity: FarmActivity.phunThuoc,
      );
      expect(plan.skipped, isEmpty);
      expect(rec.payload['plot_refs'], [plotRef, plot2Ref]);
      await s.sync.syncOnce(force: true);
      env.server.pages['c1'] = PullPage(
        changes: [
          _line(
            '01920000-0000-7000-8000-00000000f1b1',
            rec.id,
            plotRef,
            seasonRef,
          ),
        ],
        nextCursor: 'c2',
        hasMore: false,
      );
      await s.sync.syncOnce(force: true);
      final b = (await s.crop.batches()).single;
      expect(b.accepted, 1);
      expect(b.serverSkipped, ['T2']);
      // T2: không còn dòng "chờ đồng bộ" giả (máy chủ là chuẩn).
      expect(await s.crop.logs(seasonRef: season2Ref), isEmpty);

      await openTile(tester, 'Nhật ký hàng loạt');
      await _reveal(tester, find.textContaining('Đã ghi 1/2 lô'));
      expect(find.textContaining('Đã ghi 1/2 lô'), findsOneWidget);
      expect(find.textContaining('Máy chủ bỏ qua: T2'), findsOneWidget);
      await tester.pageBack();
      await settle(tester);

      // Không lô nào mở vụ → chặn (như cropcycle.batch_empty).
      final env2Plots = [
        for (final p in plots)
          if (p.ref == plot2Ref) p,
      ];
      await s.crop.cancelSeason(
        season: (await s.crop.seasonByRef(season2Ref))!,
        reason: 'Mở nhầm vụ lô T2 hôm qua',
      );
      await expectLater(
        s.crop.recordBatch(plots: env2Plots, activity: FarmActivity.kiemTra),
        throwsA(
          isA<CropError>().having(
            (e) => e.message,
            'm',
            contains('Lô T2: $skipNotOpen'),
          ),
        ),
      );
      await finish(tester);
    },
  );

  testWidgets('HTX US-LOG-03 (nông dân): ghi hàng loạt từ màn Ghi nhật ký', (
    tester,
  ) async {
    final env = TestEnv()..seed(season: true, extra: [_season2()]);
    final s = await loginToHome(tester, env);
    await openTile(tester, 'Ghi nhật ký');
    await tapOn(tester, find.byTooltip('Ghi hàng loạt nhiều lô'));
    await tapText(tester, 'Chọn lô đang có vụ');
    await tapOn(tester, find.byKey(const ValueKey('act-tuoi_nuoc')));
    await tapText(tester, 'Ghi cho các lô đã chọn');
    expect(find.text('Ghi nhật ký cho 2 lô'), findsOneWidget);
    await tapText(tester, 'Ghi hàng loạt');
    final rec = (await s.outbox.list(entity: PushEntity.farmLogBatch)).single;
    expect(rec.payload['plot_refs'], [plotRef, plot2Ref]);
    expect(rec.payload['activity_code'], 'tuoi_nuoc');
    await finish(tester);
  });
}

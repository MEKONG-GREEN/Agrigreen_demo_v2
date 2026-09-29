import 'package:agrigreen_htx/application/crop_service.dart';
import 'package:agrigreen_htx/domain/cropcycle.dart';
import 'package:agrigreen_mobile/data/photo/location_source.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/test_env.dart';

const seasonRef = '01920000-0000-7000-8000-00000000e001';
const currentEvent = '01920000-0000-7000-8000-00000000e0e1';

Future<void> _openPlot(WidgetTester tester, {bool manager = false}) async {
  await openTile(tester, manager ? 'Ghi nhật ký hộ' : 'Ruộng của tôi');
  await tapText(tester, 'T1 · Ruộng bờ kênh');
}

void main() {
  testWidgets(
    'HTX US-SEASON-01 (sửa vụ): nông dân sửa ngày sạ ngoại tuyến — lý do < 10 '
    'ký tự bị chặn; bản ghi bù season_open correct supersedes current_event_id; '
    'màn hình hiện ngày mới "Chờ đồng bộ"',
    (tester) async {
      final env = TestEnv()..seed(season: true);
      final s = await loginToHome(tester, env);
      env.server.failNextPushes = 100;
      await _openPlot(tester);
      await tapText(tester, 'Sửa vụ');
      await tester.enterText(
        find.byKey(const ValueKey('edit-sowing-date')),
        '20/09/2026',
      );
      await tester.enterText(
        find.byKey(const ValueKey('edit-season-reason')),
        'nhầm',
      );
      await tapText(tester, 'Lưu sửa vụ');
      expect(find.text(msgReasonTooShort), findsOneWidget);
      await tester.enterText(
        find.byKey(const ValueKey('edit-season-reason')),
        'Ghi nhầm ngày sạ, sạ thực tế 20/09',
      );
      await tapText(tester, 'Lưu sửa vụ');

      final rec = (await s.outbox.list(entity: PushEntity.seasonOpen)).single;
      expect(rec.op, RecordOp.correct);
      expect(rec.supersedesId, currentEvent);
      expect(rec.correctionReason, 'Ghi nhầm ngày sạ, sạ thực tế 20/09');
      expect(rec.payload, {
        'season_ref': seasonRef,
        'sowing_date': '2026-09-20',
      });
      expect(find.text('Ngày sạ: 20/09/2026'), findsOneWidget);
      expect(find.text('Chờ đồng bộ'), findsOneWidget);

      // Sửa lần hai khi vẫn ngoại tuyến → nối chuỗi vào bản sửa trước.
      final se = (await s.crop.seasonByRef(seasonRef))!;
      final rec2 = await s.crop.correctSeason(
        season: se,
        reason: 'Đổi sang giống ST25 theo HTX',
        varietyCode: 'ST25',
      );
      expect(rec2.supersedesId, rec.id);
      expect((await s.crop.seasonByRef(seasonRef))!.varietyCode, 'ST25');

      env.server.failNextPushes = 0;
      await s.sync.syncOnce(force: true);
      final wires = env.server.ledger.values
          .where((w) => w['entity'] == 'season_open')
          .toList();
      expect(wires, hasLength(2));
      expect(wires.first['op'], 'correct');
      await finish(tester);
    },
  );

  testWidgets(
    'HTX US-SEASON-02 (hủy vụ): quản lý hủy vụ mở nhầm với lý do ≥ 10 ký tự → '
    'cancel: true; lô trở lại "Chưa mở vụ" và mở vụ lại được',
    (tester) async {
      final env = TestEnv(roles: const ['coop_manager'])..seed(season: true);
      final s = await loginToHome(tester, env);
      await _openPlot(tester, manager: true);
      await tapText(tester, 'Hủy vụ');
      await tester.enterText(
        find.byKey(const ValueKey('cancel-season-reason')),
        'ngắn',
      );
      await tapOn(tester, find.widgetWithText(FilledButton, 'Hủy vụ'));
      expect(find.text(msgReasonTooShort), findsOneWidget);
      await tester.enterText(
        find.byKey(const ValueKey('cancel-season-reason')),
        'Mở nhầm lô, nông dân chưa sạ',
      );
      await tapOn(tester, find.widgetWithText(FilledButton, 'Hủy vụ'));

      final rec = (await s.outbox.list(entity: PushEntity.seasonOpen)).single;
      expect(rec.op, RecordOp.correct);
      expect(rec.supersedesId, currentEvent);
      expect(rec.payload, {'season_ref': seasonRef, 'cancel': true});
      expect(find.text('Chưa mở vụ.'), findsOneWidget);
      expect(find.text('Mở vụ mới'), findsOneWidget);
      expect(await s.crop.openSeasonFor(plotRef), isNull);
      await finish(tester);
    },
  );

  testWidgets(
    'sửa/hủy vụ: vụ mở trên máy (chưa gửi) → supersedes = id bản ghi mở vụ; '
    'đã khai sản lượng → chặn; không đổi gì → chặn; ngày sạ ngoài cửa sổ → chặn',
    (tester) async {
      final env = TestEnv()..seed(season: true);
      final s = await loginToHome(tester, env);
      final plot2 = (await s.crop.plot(plot2Ref))!;
      final open = await s.crop.openSeason(
        plot: plot2,
        seasonCode: 'TD',
        seasonYear: 2026,
        varietyCode: 'OM18',
        sowingDate: DateTime.utc(2026, 9, 28),
      );
      final local = (await s.crop.openSeasonFor(plot2Ref))!;
      expect(local.currentEventId, open.id);
      final cancel = await s.crop.cancelSeason(
        season: local,
        reason: 'Mở nhầm lô T2 trên máy',
      );
      expect(cancel.supersedesId, open.id);
      expect(await s.crop.openSeasonFor(plot2Ref), isNull);

      final se = (await s.crop.seasonByRef(seasonRef))!;
      await expectLater(
        s.crop.correctSeason(season: se, reason: 'Không đổi gì cả nhé'),
        throwsA(isA<CropError>()),
      );
      await expectLater(
        s.crop.correctSeason(
          season: se,
          reason: 'Ngày sạ quá xa hiện tại',
          sowingDate: DateTime.utc(2026, 8, 1),
        ),
        throwsA(
          isA<CropError>().having((e) => e.message, 'm', msgSowingWindow),
        ),
      );
      await s.outbox.enqueue(
        entity: PushEntity.harvest,
        payload: {'season_ref': seasonRef, 'mass_kg': '5000'},
      );
      await expectLater(
        s.crop.cancelSeason(season: se, reason: 'Hủy sau khi khai sản lượng'),
        throwsA(
          isA<CropError>().having(
            (e) => e.message,
            'm',
            contains('đã khai sản lượng'),
          ),
        ),
      );
      await finish(tester);
    },
  );

  testWidgets(
    'HTX US-SEASON-04 AC-1: mở vụ trùng trên lô đang có vụ → "Lô ruộng đang '
    'canh tác vụ khác, không thể mở vụ mới."',
    (tester) async {
      final env = TestEnv()..seed(season: true);
      final s = await loginToHome(tester, env);
      await expectLater(
        s.crop.openSeason(
          plot: (await s.crop.plot(plotRef))!,
          seasonCode: 'TD',
          seasonYear: 2026,
          varietyCode: 'OM18',
          sowingDate: DateTime.utc(2026, 9, 28),
        ),
        throwsA(
          isA<CropError>().having((e) => e.message, 'm', msgSeasonAlreadyOpen),
        ),
      );
      await finish(tester);
    },
  );

  testWidgets('HTX US-SEASON-03: sao chép cấu hình vụ trước — có mạng dùng '
      'previous-config (copy_previous: true); vụ gần nhất chưa xong → chặn (AC-2); '
      'mất mạng → vụ đã hoàn thành trên máy', (tester) async {
    final env = TestEnv()
      ..seed(
        extra: [
          {
            'entity': 'season',
            'id': 's-old',
            'op': 'upsert',
            'data': {
              'season_ref': '01920000-0000-7000-8000-00000000e0a0',
              'plot_ref': plotRef,
              'plot_code': 'T1',
              'household_ref': householdRef,
              'season_code': 'HT',
              'season_year': 2026,
              'variety_code': 'ST25',
              'sop_version_id': sopId,
              'sowing_date': '2026-05-01',
              'status': 'completed',
            },
          },
        ],
      );
    final base = env.server.onGet!;
    var mode = 'ok';
    env.server.onGet = (path, q) async {
      if (path.endsWith('/previous-config')) {
        if (mode == 'offline') throw NetworkException('offline');
        if (mode == 'busy') {
          throw ApiException(409, 'cropcycle.previous_season_not_completed');
        }
        return {
          'season_ref': '01920000-0000-7000-8000-00000000e0a0',
          'season_code': 'HT',
          'season_year': 2026,
          'variety_code': 'OM18',
          'sop_template_ref': templateRef,
          'sop_version_id': sopId,
          'sop_version_no': 1,
        };
      }
      return base(path, q);
    };
    final s = await loginToHome(tester, env);
    await openTile(tester, 'Ruộng của tôi');
    await tapText(tester, 'T1 · Ruộng bờ kênh');
    await tapText(tester, 'Mở vụ mới');
    await tapText(tester, 'Thu Đông');
    await tapText(tester, 'Tiếp');
    await tapOn(tester, find.byKey(const ValueKey('copy-previous')));
    expect(find.text('Theo vụ HT 2026'), findsOneWidget);
    expect(find.text('Quy trình lúa OM18'), findsOneWidget);
    await tapText(tester, 'Tiếp');
    await tester.enterText(
      find.byKey(const ValueKey('sowing-date')),
      '28/09/2026',
    );
    await tapText(tester, 'Tiếp');
    await tapText(tester, 'Lưu mở vụ');
    final rec = (await s.outbox.list(entity: PushEntity.seasonOpen)).single;
    expect(rec.payload['copy_previous'], isTrue);
    expect(rec.payload['variety_code'], 'OM18');
    expect(rec.payload['sop_version_id'], sopId);

    final plot2 = (await s.crop.plot(plot2Ref))!;
    mode = 'busy';
    await expectLater(
      s.crop.previousConfig(plot2),
      throwsA(
        isA<CropError>().having((e) => e.message, 'm', msgPreviousNotDone),
      ),
    );
    mode = 'offline';
    // T2 chưa có vụ nào trên máy.
    await expectLater(
      s.crop.previousConfig(plot2),
      throwsA(
        isA<CropError>().having((e) => e.message, 'm', msgNoPreviousSeason),
      ),
    );
    // T1: vụ mới vừa mở (trên máy) là vụ gần nhất → chưa hoàn thành (AC-2).
    await expectLater(
      s.crop.previousConfig((await s.crop.plot(plotRef))!),
      throwsA(
        isA<CropError>().having((e) => e.message, 'm', msgPreviousNotDone),
      ),
    );
    await finish(tester);
  });

  testWidgets(
    'HTX US-SEASON-03 ngoại tuyến: lấy giống + SOP của vụ đã hoàn thành trên máy',
    (tester) async {
      final env = TestEnv()
        ..seed(
          extra: [
            {
              'entity': 'season',
              'id': 's-old',
              'op': 'upsert',
              'data': {
                'season_ref': '01920000-0000-7000-8000-00000000e0a0',
                'plot_ref': plotRef,
                'plot_code': 'T1',
                'household_ref': householdRef,
                'season_code': 'HT',
                'season_year': 2026,
                'variety_code': 'ST25',
                'sop_version_id': sopId,
                'sowing_date': '2026-05-01',
                'status': 'completed',
              },
            },
          ],
        );
      final base = env.server.onGet!;
      env.server.onGet = (path, q) async => path.endsWith('/previous-config')
          ? throw NetworkException('offline')
          : base(path, q);
      final s = await loginToHome(tester, env);
      final c = await s.crop.previousConfig((await s.crop.plot(plotRef))!);
      expect(c.offline, isTrue);
      expect(c.varietyCode, 'ST25');
      expect(c.sopVersionId, sopId);
      await finish(tester);
    },
  );

  testWidgets(
    'HTX US-GPS-02: GPS không có lúc lưu nhưng ảnh vừa chụp có tọa độ → '
    'gps.source = photo_exif; nhãn nguồn luôn có',
    (tester) async {
      final env = TestEnv()..seed(season: true);
      final s = await loginToHome(tester, env);
      final shot = await s.photos.capture(
        subjectRef: householdRef,
        parentEntity: 'farm_log',
      );
      env.location.result = const LocationResult(GpsStatus.unavailable);
      final rec = await s.crop.recordLog(
        season: await s.crop.seasonByRef(seasonRef),
        activity: FarmActivity.kiemTra,
        fileRefs: [shot!.file.id],
      );
      expect(rec.payload['gps'], {
        'lat': '10.0451234',
        'lon': '105.7701234',
        'accuracy_m': '6.0',
        'source': 'photo_exif',
      });
      // Không ảnh → không có GPS thiết bị (không chặn lưu).
      final rec2 = await s.crop.recordLog(
        season: await s.crop.seasonByRef(seasonRef),
        activity: FarmActivity.kiemTra,
      );
      expect(rec2.payload['gps'], {'source': 'no_device'});
      await finish(tester);
    },
  );
}

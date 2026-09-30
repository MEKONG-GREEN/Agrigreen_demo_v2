import 'package:agrigreen_mkg_field/application/app_services.dart';
import 'package:agrigreen_mkg_field/ui/providers.dart';
import 'package:agrigreen_mkg_field/ui/screens/home_screen.dart';
import 'package:agrigreen_mkg_field/ui/screens/sync_queue_screen.dart';
import 'package:agrigreen_mkg_field/ui/screens/trip_detail_screen.dart';
import 'package:agrigreen_mkg_field/ui/screens/trip_list_screen.dart';
import 'package:agrigreen_mobile/data/photo/location_source.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/fake_logistics.dart';
import 'support/test_env.dart';

TestEnv _env({List<String> roles = const ['driver']}) {
  final env = TestEnv(roles: roles)..seed(orders: const []);
  env.server.logistics
    ..add(tripData(driver: meSub))
    ..add(
      tripData(
        id: tripB,
        code: 'CH000002',
        driver: otherSub,
        plannedAt: '2026-10-01T02:00:00Z',
      ),
    );
  return env;
}

/// Đăng nhập tài khoản chỉ có vai tài xế → trang chủ "Chuyến của tôi".
Future<MkgServices> _loginDriver(WidgetTester tester, TestEnv env) async {
  await tester.pumpWidget(appUnderTest(env));
  await settle(tester);
  await tester.tap(find.text('Đăng nhập'));
  await settle(tester);
  for (var round = 0; round < 2; round++) {
    for (final d in ['1', '3', '5', '7', '9', '2']) {
      await tester.tap(find.text(d));
      await tester.pump();
    }
    await settle(tester, 10);
  }
  await settle(tester, 30);
  expect(find.byType(TripListScreen), findsOneWidget);
  final c = ProviderScope.containerOf(
    tester.element(find.byType(TripListScreen)),
  );
  final s = c.read(appControllerProvider).services!;
  await s.sync.syncOnce(force: true);
  c.invalidate(tripListProvider);
  await settle(tester);
  return s;
}

Future<void> _confirmMilestone(WidgetTester tester, String code) async {
  await tapOn(tester, find.byKey(ValueKey('record-$code')));
  await tapOn(tester, find.byKey(const ValueKey('milestone-confirm')));
  await settle(tester, 40);
}

String _chip(WidgetTester tester) {
  final chip = find.byKey(const ValueKey('trip-status'));
  return tester
      .widget<Text>(find.descendant(of: chip, matching: find.byType(Text)))
      .data!;
}

void main() {
  testWidgets(
    'W-02 tài khoản chỉ vai driver: trang chủ là "Chuyến của tôi", chỉ chuyến giao cho tôi; chi tiết: chặng, điểm đến, tấn ước',
    (tester) async {
      final env = _env();
      await _loginDriver(tester, env);
      expect(find.text('Chuyến của tôi'), findsOneWidget);
      expect(find.byKey(const ValueKey('trip-$tripA')), findsOneWidget);
      expect(find.byKey(const ValueKey('trip-$tripB')), findsNothing);
      expect(find.text('Chờ xuất phát'), findsOneWidget);
      expect(find.textContaining('Đường thủy · ước 12,5 tấn'), findsOneWidget);
      // Không có chức năng tổ gom / cảnh báo "chưa có vai".
      expect(find.textContaining('chưa có vai'), findsNothing);
      await tapOn(tester, find.byKey(const ValueKey('trip-$tripA')));
      expect(find.byType(TripDetailScreen), findsOneWidget);
      expect(find.text('Chuyến CH000001'), findsOneWidget);
      expect(find.text('Tấn ước: 12,5 tấn'), findsOneWidget);
      expect(
        find.text('Điểm đến: Kho / nhà máy (10.2001000, 105.9002000)'),
        findsOneWidget,
      );
      expect(find.byKey(const ValueKey('leg-1')), findsOneWidget);
      expect(find.textContaining('42,3 km'), findsOneWidget);
      // Chỉ mốc kế tiếp có nút.
      expect(find.byKey(const ValueKey('record-departed')), findsOneWidget);
      expect(find.byKey(const ValueKey('record-arrived')), findsNothing);
      await finish(tester);
    },
  );

  testWidgets(
    'W-02 ghi xuất phát ngoại tuyến → Đang chạy + Chờ đồng bộ; chỉ còn nút "Ghi đã đến"; có mạng → gửi',
    (tester) async {
      final env = _env();
      final s = await _loginDriver(tester, env);
      await tapOn(tester, find.byKey(const ValueKey('trip-$tripA')));
      env.server.offline = true;
      await _confirmMilestone(tester, 'departed');
      expect(_chip(tester), 'Đang chạy');
      expect(find.textContaining('Chờ đồng bộ'), findsWidgets);
      expect(find.textContaining('(±6 m)'), findsWidgets);
      expect(find.byKey(const ValueKey('record-departed')), findsNothing);
      expect(find.byKey(const ValueKey('record-arrived')), findsOneWidget);
      expect(env.server.logistics.milestones, isEmpty);

      env.server.offline = false;
      await s.sync.syncOnce(force: true);
      await settle(tester);
      expect(env.server.logistics.milestones.single['kind'], 'departed');
      await _confirmMilestone(tester, 'arrived');
      expect(env.server.logistics.milestones.last['kind'], 'arrived');
      expect(_chip(tester), 'Đã đến nơi');
      expect(find.byKey(const ValueKey('no-next-milestone')), findsOneWidget);
      expect(find.textContaining('chờ cân nhà máy'), findsOneWidget);
      await finish(tester);
    },
  );

  testWidgets(
    'W-02 xung đột 409 hiển thị rõ ở chi tiết chuyến, danh sách và hàng đợi đồng bộ',
    (tester) async {
      final env = _env();
      final s = await _loginDriver(tester, env);
      await tapOn(tester, find.byKey(const ValueKey('trip-$tripA')));
      env.server.offline = true;
      await _confirmMilestone(tester, 'departed');
      // Điều phối đã ghi xuất phát + đến trên web trong lúc mất sóng.
      env.server.logistics.trips[tripA]!
        ..['status'] = 'arrived'
        ..['departed_at'] = '2026-10-01T02:40:00Z'
        ..['arrived_at'] = '2026-10-01T02:58:00Z';
      env.server.offline = false;
      await s.sync.syncOnce(force: true);
      await settle(tester);
      expect(find.textContaining('bị máy chủ từ chối'), findsOneWidget);
      expect(find.textContaining('chỉ ghi tiến từng bậc'), findsOneWidget);
      expect(_chip(tester), 'Đã đến nơi');
      await tester.pageBack();
      await settle(tester);
      expect(
        find.byKey(const ValueKey('trip-conflict-$tripA')),
        findsOneWidget,
      );
      await tapOn(tester, find.byKey(const ValueKey('trip-sync-card')));
      expect(find.byType(SyncQueueScreen), findsOneWidget);
      expect(
        find.textContaining('Xuất phát · chuyến CH000001'),
        findsOneWidget,
      );
      await finish(tester);
    },
  );

  testWidgets('W-02 không có GPS → báo lỗi, không ghi mốc', (tester) async {
    final env = _env();
    await _loginDriver(tester, env);
    env.location.result = const LocationResult(GpsStatus.unavailable);
    await tapOn(tester, find.byKey(const ValueKey('trip-$tripA')));
    await _confirmMilestone(tester, 'departed');
    expect(find.textContaining('Chưa lấy được vị trí GPS'), findsOneWidget);
    expect(_chip(tester), 'Chờ xuất phát');
    expect(env.server.actionCalls, isEmpty);
    await finish(tester);
  });

  testWidgets(
    'W-02 tổ gom kiêm tài xế: trang chủ có nút "Chuyến của tôi" → danh sách chuyến',
    (tester) async {
      final env = _env(roles: const ['field_collector', 'driver'])..seed();
      await loginToHome(tester, env);
      expect(find.byType(HomeScreen), findsOneWidget);
      expect(find.byKey(const ValueKey('order-$orderId')), findsOneWidget);
      await tapOn(tester, find.byKey(const ValueKey('my-trips')));
      expect(find.byType(TripListScreen), findsOneWidget);
      expect(find.byKey(const ValueKey('trip-$tripA')), findsOneWidget);
      await finish(tester);
    },
  );

  testWidgets('W-02 tổ gom không có vai driver: không thấy "Chuyến của tôi"', (
    tester,
  ) async {
    final env = _env(roles: const ['field_collector'])..seed();
    await loginToHome(tester, env);
    expect(find.byKey(const ValueKey('my-trips')), findsNothing);
    expect(env.server.logistics.listQueries, isEmpty);
    await finish(tester);
  });

  testWidgets(
    'W-02 màn hình nhỏ 320×568: danh sách và chi tiết chuyến không tràn',
    (tester) async {
      tester.view.physicalSize = const Size(320, 568);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.reset);
      final env = _env();
      await _loginDriver(tester, env);
      expect(tester.takeException(), isNull);
      await tapOn(tester, find.byKey(const ValueKey('trip-$tripA')));
      expect(tester.takeException(), isNull);
      await _confirmMilestone(tester, 'departed');
      expect(tester.takeException(), isNull);
      await finish(tester);
    },
  );
}

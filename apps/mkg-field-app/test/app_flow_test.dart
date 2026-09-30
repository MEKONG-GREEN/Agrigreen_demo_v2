import 'package:agrigreen_mkg_field/application/app_services.dart';
import 'package:agrigreen_mkg_field/domain/fieldops.dart';
import 'package:agrigreen_mkg_field/domain/roles.dart';
import 'package:agrigreen_mkg_field/ui/providers.dart';
import 'package:agrigreen_mkg_field/ui/screens/count_form_screen.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/test_env.dart';

ProviderContainer _c(WidgetTester t) =>
    ProviderScope.containerOf(t.element(find.byType(MaterialApp)));

Future<void> _enterCount(
  WidgetTester tester, {
  required String rolls,
  String? reason,
}) async {
  await tester.enterText(find.byKey(const ValueKey('rolls')), rolls);
  if (reason != null) {
    await tester.enterText(find.byKey(const ValueKey('discrepancy')), reason);
  }
  await tapOn(tester, find.text('Lưu'));
}

void main() {
  testWidgets(
    'đăng nhập OIDC (client agrigreen-mkg-field-app) → PIN → lệnh gom của tôi (pull collection_order)',
    (tester) async {
      final env = TestEnv()..seed();
      final s = await loginToHome(tester, env);
      expect(_c(tester).read(appControllerProvider).mode, FieldMode.collector);
      expect(env.platform.config.oidcClientId, 'agrigreen-mkg-field-app');
      expect(
        env.platform.config.oidcRedirectUrl,
        'vn.agrigreen.mkgfield:/oauth2redirect',
      );
      expect(find.byKey(const ValueKey('order-$orderId')), findsOneWidget);
      expect(find.textContaining('2 thửa'), findsOneWidget);
      // Tổ gom không thấy chức năng giám sát.
      expect(find.text('Duyệt lệnh gom'), findsNothing);
      // Không đăng ký thiết bị ở nền tảng: api-internal không có /v1/devices.
      expect(env.server.registeredDevices, isEmpty);
      expect(s.deviceId, isNotEmpty);
      await finish(tester);
    },
  );

  testWidgets('FM-03/FM-04 trên giao diện: gather chưa ghi được khi bale trống; nhiều hơn bị chặn; '
      'ít hơn kèm lý do → Chờ đồng bộ → gửi', (tester) async {
    final env = TestEnv()..seed();
    final s = await loginToHome(tester, env);
    await tapOn(tester, find.byKey(const ValueKey('order-$orderId')));
    await settle(tester);
    // FM-03: nút ghi gather bị khóa khi bale chưa có số.
    final gatherBtn = tester.widget<TextButton>(
      find.byKey(const ValueKey('record-$plotA-gather')),
    );
    expect(gatherBtn.onPressed, isNull);

    await tapOn(tester, find.byKey(const ValueKey('record-$plotA-bale')));
    expect(find.byType(CountFormScreen), findsOneWidget);
    // FM-09: bắt buộc số cuộn.
    await _enterCount(tester, rolls: '');
    expect(find.textContaining('FM-09'), findsOneWidget);
    await _enterCount(tester, rolls: '20');
    await settle(tester);
    expect(find.byType(CountFormScreen), findsNothing);
    expect(find.text('Cuộn tại thửa: 20 cuộn'), findsOneWidget);

    await tapOn(tester, find.byKey(const ValueKey('record-$plotA-gather')));
    expect(find.textContaining('Cuộn tại thửa: 20 cuộn'), findsOneWidget);
    await _enterCount(tester, rolls: '22');
    expect(find.textContaining('FM-04:'), findsOneWidget);
    expect(find.textContaining('nhiều hơn'), findsOneWidget);
    await _enterCount(tester, rolls: '18');
    expect(find.textContaining('lý do chênh'), findsWidgets);
    await _enterCount(tester, rolls: '18', reason: 'Hai cuộn bị ướt, bỏ lại');
    await settle(tester, 40);
    expect(find.text('Tập kết: 18 cuộn'), findsOneWidget);
    expect(find.textContaining('Hai cuộn bị ướt'), findsOneWidget);
    await s.sync.syncOnce(force: true);
    expect(env.server.rollCounts, hasLength(2));
    await finish(tester);
  });

  testWidgets(
    'hàng đợi đồng bộ hiển thị lý do từ chối fieldops.* bằng lời dễ hiểu',
    (tester) async {
      final env = TestEnv()..seed();
      env.server.rejectWith = (_) => 'fieldops.stage_exceeds_previous';
      final s = await loginToHome(tester, env);
      final o = (await s.field.order(orderId))!;
      final r = await s.field.recordCount(
        order: o,
        plotRef: plotB,
        stage: Stage.bale,
        rollsText: '7',
      );
      await s.sync.syncOnce(force: true);
      expect((await s.outbox.byId(r.record.id))!.status, OutboxStatus.rejected);
      _c(tester).invalidate(homeProvider);
      await settle(tester);
      expect(find.textContaining('1 bị từ chối'), findsOneWidget);
      await tapOn(tester, find.byKey(const ValueKey('sync-card')));
      expect(find.text('Hàng đợi đồng bộ'), findsOneWidget);
      expect(find.byKey(ValueKey('reject-${r.record.id}')), findsOneWidget);
      expect(
        find.textContaining('nhiều hơn công đoạn trước (FM-04)'),
        findsOneWidget,
      );
      expect(find.text('Bị từ chối'), findsOneWidget);
      await finish(tester);
    },
  );

  testWidgets(
    'R-56 giám sát: nút Duyệt khóa với lệnh mình lập; duyệt lệnh người khác',
    (tester) async {
      final env = TestEnv(roles: const ['field_supervisor'])..seed();
      env.server.planned = [
        orderData(id: orderId, status: 'planned', createdBy: meSub),
        orderData(id: order2Id, status: 'planned', createdBy: otherSub),
      ];
      env.server.recon = [
        {
          'plot_ref': plotA,
          'order_id': orderId,
          'mkg_rolls': 18,
          'htx_rolls': 17,
          'status': 'mismatch',
        },
      ];
      await loginToHome(tester, env);
      await tapOn(tester, find.text('Duyệt lệnh gom'));
      await settle(tester);
      expect(find.byKey(const ValueKey('own-order')), findsOneWidget);
      final own = tester.widget<FilledButton>(
        find.byKey(const ValueKey('approve-$orderId')),
      );
      expect(own.onPressed, isNull);
      await tapOn(tester, find.byKey(const ValueKey('approve-$order2Id')));
      await settle(tester);
      expect(
        env.server.actionCalls.map((c) => c.$1),
        contains('/v1/fieldops/orders/$order2Id/decision'),
      );
      expect(
        env.server.actionCalls.map((c) => c.$1),
        isNot(contains('/v1/fieldops/orders/$orderId/decision')),
      );
      await tester.pageBack();
      await settle(tester);
      await tapOn(tester, find.text('Đối soát chiều mua'));
      await settle(tester);
      expect(find.text('Khớp 0 · Lệch 1 · Chờ 0'), findsOneWidget);
      expect(find.textContaining('MKG 18 cuộn · HTX 17 cuộn'), findsOneWidget);
      await finish(tester);
    },
  );

  testWidgets('ca tổ gom: mở ca trên giao diện → Chờ đồng bộ', (tester) async {
    final env = TestEnv()..seed();
    await loginToHome(tester, env);
    await tapOn(tester, find.byKey(const ValueKey('order-$orderId')));
    expect(find.text('Chưa mở ca'), findsOneWidget);
    await tapOn(tester, find.text('Mở ca'));
    await tester.enterText(find.byKey(const ValueKey('headcount')), '5');
    await tapOn(tester, find.byKey(const ValueKey('prompt-ok')));
    await settle(tester, 40);
    expect(find.text('Ca đang mở'), findsOneWidget);
    expect(
      env.server.actionCalls.map((c) => c.$1),
      contains('/v1/fieldops/orders/$orderId/crew-shifts'),
    );
    await finish(tester);
  });

  testWidgets(
    'màn hình nhỏ 320×568: trang chủ, chi tiết lệnh và form đếm không tràn',
    (tester) async {
      tester.view.physicalSize = const Size(320, 568);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.reset);
      final env = TestEnv(roles: const ['field_supervisor'])..seed();
      await loginToHome(tester, env);
      expect(tester.takeException(), isNull);
      await tapOn(tester, find.byKey(const ValueKey('order-$orderId')));
      expect(tester.takeException(), isNull);
      await tapOn(tester, find.byKey(const ValueKey('record-$plotA-bale')));
      await _enterCount(tester, rolls: '5', reason: 'x');
      expect(find.byKey(const ValueKey('count-errors')), findsOneWidget);
      expect(tester.takeException(), isNull);
      await finish(tester);
    },
  );

  test('hằng số client OIDC và đường dẫn đồng bộ vùng mkg', () {
    expect(mkgOidcClientId, 'agrigreen-mkg-field-app');
    expect(mkgPushPath, '/v1/fieldops/sync/push');
    expect(mkgPullPath, '/v1/fieldops/sync/pull');
  });
}

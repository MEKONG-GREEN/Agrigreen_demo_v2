import 'package:agrigreen_htx/domain/roles.dart';
import 'package:agrigreen_htx/ui/providers.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/test_env.dart';

void main() {
  testWidgets(
    'HTX US-LOGIN-01: nông dân đăng nhập → PIN → trang chủ nông dân; thời tiết '
    'chờ nguồn, thẻ lời khuyên ẩn khi chưa có vụ (US-WEATHER-02 AC-2)',
    (tester) async {
      final env = TestEnv()..seed();
      final s = await loginToHome(tester, env);
      expect(
        containerOf(tester).read(appControllerProvider).mode,
        HtxMode.farmer,
      );
      expect(find.text('Ruộng của tôi'), findsOneWidget);
      expect(find.text('Duyệt nhật ký'), findsNothing);
      expect(find.textContaining('Chưa có nguồn thời tiết'), findsOneWidget);
      expect(find.byKey(const ValueKey('advice-card')), findsNothing);
      // Đăng ký thiết bị + pull đã chạy; client OIDC riêng của App HTX.
      expect(env.server.registeredDevices, [s.deviceId]);
      expect(env.platform.config.oidcClientId, 'agrigreen-htx-app');
      expect(
        env.platform.config.oidcRedirectUrl,
        'vn.agrigreen.htx:/oauth2redirect',
      );
      await finish(tester);
    },
  );

  testWidgets('HTX US-WEATHER-02 AC-1: có vụ → thẻ lời khuyên theo bước SOP', (
    tester,
  ) async {
    // Sạ 25/09, hôm nay 01/10 → ngày 6: bước "Bón phân đợt 1" (7 − 2 ≤ 6).
    final env = TestEnv()..seed(season: true);
    await loginToHome(tester, env);
    expect(find.byKey(const ValueKey('advice-card')), findsOneWidget);
    expect(find.textContaining('Bón phân đợt 1'), findsOneWidget);
    expect(find.textContaining('Bón thúc 7–10 ngày'), findsOneWidget);
    await finish(tester);
  });

  testWidgets('quản lý HTX: trang chủ bộ chức năng quản lý', (tester) async {
    final env = TestEnv(roles: const ['coop_manager'])..seed();
    await loginToHome(tester, env);
    expect(find.text('Duyệt nhật ký'), findsOneWidget);
    expect(find.text('Mở vụ hàng loạt'), findsOneWidget);
    expect(find.text('Ruộng của tôi'), findsNothing);
    await finish(tester);
  });

  testWidgets(
    'Kích hoạt tài khoản: chỉ mở liên kết https đúng máy chủ đăng nhập',
    (tester) async {
      final env = TestEnv();
      await tester.pumpWidget(appUnderTest(env));
      await settle(tester);
      await tester.tap(find.text('Kích hoạt tài khoản'));
      await settle(tester);
      await tester.enterText(
        find.byKey(const ValueKey('activation-link')),
        'https://evil.example.com/realms/agrigreen/login-actions/action-token?key=x',
      );
      await tester.tap(find.text('Mở trang kích hoạt'));
      await settle(tester);
      expect(find.textContaining('tránh lừa đảo'), findsOneWidget);
      expect(env.dialer.opened, isEmpty);

      const link =
          'https://id.agrigreen.test/realms/agrigreen/login-actions/action-token?key=abc';
      await tester.enterText(
        find.byKey(const ValueKey('activation-link')),
        link,
      );
      await tester.tap(find.text('Mở trang kích hoạt'));
      await settle(tester);
      expect(env.dialer.opened.single.toString(), link);
    },
  );

  test('vai từ access token (ngoại tuyến, chưa có /v1/me)', () {
    expect(rolesFromJwt(fakeJwt(['farmer', 'offline_access'])), [
      'farmer',
      'offline_access',
    ]);
    expect(rolesFromJwt('không-phải-jwt'), isEmpty);
    expect(modeFor(['farmer', 'coop_manager']), HtxMode.manager);
    expect(modeFor(['kn_officer']), HtxMode.none);
  });
}

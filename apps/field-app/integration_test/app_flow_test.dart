import 'dart:io';

import 'package:agrigreen_field/application/app_services.dart';
import 'package:agrigreen_field/core/clock.dart';
import 'package:agrigreen_field/core/config.dart';
import 'package:agrigreen_field/core/ids.dart';
import 'package:agrigreen_field/data/crypto/envelope.dart';
import 'package:agrigreen_field/data/db/sqlcipher_opener.dart';
import 'package:agrigreen_field/data/photo/location_source.dart';
import 'package:agrigreen_field/data/push/push_messaging.dart';
import 'package:agrigreen_field/data/secure/secure_store.dart';
import 'package:agrigreen_field/data/sync/sync_api.dart';
import 'package:agrigreen_field/ui/app.dart';
import 'package:agrigreen_field/ui/providers.dart';
import 'package:agrigreen_field/ui/screens/home_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';
import 'package:path/path.dart' as p;
import 'package:path_provider/path_provider.dart';

import 'support/mock_api_server.dart';
import 'support/scenario.dart';

/// Kiểm thử tích hợp trên emulator Android (CI job `field-app-emulator`):
/// SQLCipher thật, Keystore thật (flutter_secure_storage), giao diện thật;
/// máy chủ API giả chạy ngay trong tiến trình kiểm thử (dart:io, 127.0.0.1).
/// OIDC ở chế độ kiểm thử (không có Keycloak); FCM/thông báo cục bộ là bản giả
/// sau lớp trừu tượng.
void main() {
  IntegrationTestWidgetsFlutterBinding.ensureInitialized();

  Future<void> pumpUntil(
    WidgetTester tester,
    Finder f, {
    Duration timeout = const Duration(seconds: 30),
  }) async {
    final end = DateTime.now().add(timeout);
    while (f.evaluate().isEmpty) {
      if (DateTime.now().isAfter(end)) fail('Không thấy $f');
      await tester.pump(const Duration(milliseconds: 100));
    }
  }

  testWidgets(
    'đăng nhập (chế độ kiểm thử) → PIN → đồng bộ ngoại tuyến đầu–cuối',
    (tester) async {
      final server = await MockApiServer.start();
      final sk = seedServer(server);
      final store = FlutterSecureStore();
      await store.deleteAll();
      final support = await getApplicationSupportDirectory();
      final dataDir = Directory(p.join(support.path, 'agrigreen_it'));
      if (await dataDir.exists()) await dataDir.delete(recursive: true);
      await dataDir.create(recursive: true);
      final fcm = FakePushMessaging();
      final notes = RecordingLocalNotifier();

      final pf = AppPlatform(
        config: AppConfig(
          apiBaseUrl: server.baseUrl,
          oidcIssuer: 'http://127.0.0.1/oidc-test-mode',
          tileUrlTemplate: '${server.baseUrl}/tiles/{z}/{x}/{y}.png',
        ),
        store: store,
        clock: const SystemClock(),
        ids: UuidV7Generator(),
        opener: const SqlCipherOpener(),
        dataDir: dataDir,
        apiFactory: (token, deviceId) => HttpSyncApi(
          baseUrl: server.baseUrl,
          accessToken: token,
          deviceId: deviceId,
          timeout: const Duration(seconds: 5),
        ),
        camera: GeneratedJpegCamera(),
        location: const FixedLocationSource(
          LocationResult(
            GpsStatus.ok,
            GpsFix(lat: 10.045, lon: 105.77, accuracyM: 5),
          ),
        ),
        push: fcm,
        notifier: notes,
      );

      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            platformProvider.overrideWithValue(pf),
            oidcProvider.overrideWithValue(TestModeOidc()),
          ],
          child: const FieldApp(),
        ),
      );

      // Đăng nhập chế độ kiểm thử → tạo PIN → nhập lại.
      await pumpUntil(tester, find.text('Đăng nhập'));
      await tester.tap(find.text('Đăng nhập'));
      await pumpUntil(tester, find.text('Tạo mã PIN 6 số'));
      for (var round = 0; round < 2; round++) {
        for (final d in ['2', '4', '6', '8', '1', '3']) {
          await tester.tap(find.text(d));
          await tester.pump(const Duration(milliseconds: 50));
        }
        await tester.pump(const Duration(milliseconds: 300));
      }
      await pumpUntil(tester, find.byType(HomeScreen));

      final container = ProviderScope.containerOf(
        tester.element(find.byType(HomeScreen)),
      );
      final services = container.read(appControllerProvider).services!;
      // Khóa SQLCipher dạng raw key nằm trong Keystore, không phải PIN.
      expect(await store.read(SecureKeys.dbKey), hasLength(64));

      try {
        await runOfflineFieldScenario(
          s: services,
          server: server,
          householdKey: sk,
          fcm: fcm,
          notes: notes,
          tick: () => tester.pump(const Duration(milliseconds: 100)),
        );
        // Chuông hiện số chưa đọc; mở hộp thư bằng chạm.
        container.invalidate(homeSummaryProvider);
        await tester.pump(const Duration(milliseconds: 500));
        await tester.tap(find.byIcon(Icons.notifications).first);
        await pumpUntil(tester, find.text('Tài liệu mới: phòng trừ rầy nâu'));
      } finally {
        await server.close();
      }
    },
  );
}

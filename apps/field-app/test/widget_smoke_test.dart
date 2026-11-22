import 'dart:io';

import 'package:agrigreen_field/application/app_services.dart';
import 'package:agrigreen_field/ui/app.dart';
import 'package:agrigreen_field/ui/providers.dart';
import 'package:agrigreen_mobile/application/auth_flow.dart';
import 'package:agrigreen_mobile/core/clock.dart';
import 'package:agrigreen_mobile/core/config.dart';
import 'package:agrigreen_mobile/core/ids.dart';
import 'package:agrigreen_mobile/data/photo/location_source.dart';
import 'package:agrigreen_mobile/data/photo/photo_pipeline.dart';
import 'package:agrigreen_mobile/data/secure/secure_store.dart';
import 'package:agrigreen_mobile/testing.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/ffi_opener.dart';

class _NoCamera implements CameraSource {
  @override
  Future<CapturedImage?> capture() async => null;
}

void main() {
  testWidgets(
    'lần đầu mở app: màn đăng nhập → đăng nhập → vào thẳng, không có PIN',
    (tester) async {
      final pf = AppPlatform(
        config: const AppConfig(
          apiBaseUrl: 'https://api.test',
          oidcIssuer: 'https://id.test',
        ),
        store: MemorySecureStore(),
        clock: const SystemClock(),
        ids: UuidV7Generator(),
        opener: FfiOpener(),
        dataDir: Directory.systemTemp,
        apiFactory: (_, _) => FakeServer(),
        camera: _NoCamera(),
        location: const FixedLocationSource(LocationResult(GpsStatus.denied)),
      );
      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            platformProvider.overrideWithValue(pf),
            oidcProvider.overrideWithValue(FakeOidc()),
          ],
          child: const FieldApp(),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('AgriGreen Hiện trường'), findsOneWidget);
      expect(find.text('Đăng nhập'), findsOneWidget);

      // BRD 2.0 FN-CH-00.01, FN-05 (ADR-0057): Mobile không có đăng nhập bằng PIN — luồng của app vào thẳng sau đăng
      // nhập trực tuyến (không mở CSDL ở kiểm thử widget), đăng xuất buộc đăng nhập lại.
      final container = ProviderScope.containerOf(
        tester.element(find.byType(FieldApp)),
      );
      final flow = container.read(authFlowProvider);
      expect(flow.pins, isNull);
      final v = await tester.runAsync(flow.loginOnline);
      expect(v!.stage, AuthStage.unlocked);
      expect(
        await tester.runAsync(() => pf.store.read(SecureKeys.pinHash)),
        isNull,
      );
      final out = await tester.runAsync(flow.signOut);
      expect(out!.stage, AuthStage.needsOnlineLogin);
      expect(find.textContaining('PIN'), findsNothing);
    },
  );
}

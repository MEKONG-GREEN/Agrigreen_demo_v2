import 'dart:io';

import 'package:agrigreen_field/application/app_services.dart';
import 'package:agrigreen_field/core/clock.dart';
import 'package:agrigreen_field/core/config.dart';
import 'package:agrigreen_field/core/ids.dart';
import 'package:agrigreen_field/data/photo/location_source.dart';
import 'package:agrigreen_field/data/photo/photo_pipeline.dart';
import 'package:agrigreen_field/data/secure/secure_store.dart';
import 'package:agrigreen_field/ui/app.dart';
import 'package:agrigreen_field/ui/providers.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/fake_oidc.dart';
import 'support/fake_server.dart';
import 'support/ffi_opener.dart';

class _NoCamera implements CameraSource {
  @override
  Future<CapturedImage?> capture() async => null;
}

void main() {
  testWidgets('lần đầu mở app: màn đăng nhập → đăng nhập → tạo PIN', (
    tester,
  ) async {
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

    await tester.tap(find.text('Đăng nhập'));
    await tester.pumpAndSettle();
    expect(find.text('Tạo mã PIN 6 số'), findsOneWidget);
    for (final d in ['1', '2', '3', '4', '5', '6']) {
      await tester.tap(find.text(d));
      await tester.pump();
    }
    await tester.pumpAndSettle();
    expect(find.text('Nhập lại mã PIN'), findsOneWidget);
  });
}

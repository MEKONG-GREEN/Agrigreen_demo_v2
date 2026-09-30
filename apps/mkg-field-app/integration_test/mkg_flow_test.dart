import 'dart:convert';
import 'dart:io';

import 'package:agrigreen_mkg_field/application/app_services.dart';
import 'package:agrigreen_mkg_field/domain/fieldops.dart';
import 'package:agrigreen_mkg_field/ui/app.dart';
import 'package:agrigreen_mkg_field/ui/providers.dart';
import 'package:agrigreen_mkg_field/ui/screens/home_screen.dart';
import 'package:agrigreen_mobile/core/clock.dart';
import 'package:agrigreen_mobile/core/config.dart';
import 'package:agrigreen_mobile/core/ids.dart';
import 'package:agrigreen_mobile/data/auth/oidc_client.dart';
import 'package:agrigreen_mobile/data/crypto/envelope.dart';
import 'package:agrigreen_mobile/data/db/sqlcipher_opener.dart';
import 'package:agrigreen_mobile/data/photo/location_source.dart';
import 'package:agrigreen_mobile/data/secure/secure_store.dart';
import 'package:agrigreen_mobile/data/sync/sync_service.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';
import 'package:path/path.dart' as p;
import 'package:path_provider/path_provider.dart';

import 'support/mock_mkg_server.dart';

const orderId = '01920000-0000-7000-8000-0000000001a1';
const plotRef = '01920000-0000-7000-8000-0000000000b1';
const crewRef = '01920000-0000-7000-8000-0000000000e1';

String _jwt(List<String> roles) {
  String b(Object o) =>
      base64Url.encode(utf8.encode(jsonEncode(o))).replaceAll('=', '');
  return '${b({'alg': 'none'})}.${b({
    'sub': '01920000-0000-7000-8000-00000000aa01',
    'realm_access': {'roles': roles},
  })}.sig';
}

/// Đăng nhập chế độ kiểm thử (không có Keycloak) — chỉ trong kiểm thử.
class TestModeOidc implements OidcClient {
  @override
  Future<OidcTokens> signIn() async => OidcTokens(
    accessToken: _jwt(['field_collector']),
    refreshToken: 'it-refresh-token',
    accessTokenExpiry: DateTime.now().add(const Duration(hours: 8)),
  );
  @override
  Future<OidcTokens> refresh(String refreshToken) => signIn();
}

/// Kiểm thử tích hợp trên emulator Android (CI job `mkg-field-app-emulator`):
/// SQLCipher thật, Keystore thật, giao diện thật; api-internal giả chạy trong
/// tiến trình kiểm thử (127.0.0.1).
void main() {
  IntegrationTestWidgetsFlutterBinding.ensureInitialized();

  Future<void> pumpUntil(WidgetTester tester, Finder f) async {
    final end = DateTime.now().add(const Duration(seconds: 30));
    while (f.evaluate().isEmpty) {
      if (DateTime.now().isAfter(end)) fail('Không thấy $f');
      await tester.pump(const Duration(milliseconds: 100));
    }
  }

  testWidgets(
    'tổ gom: đăng nhập → PIN → pull lệnh gom → đếm bale/gather/boat_load và mở ca khi '
    'mất mạng → có mạng lại → push đúng một lần tới /v1/fieldops/sync/push',
    (tester) async {
      final server = await MockMkgServer.start();
      server.addOrder({
        'order_id': orderId,
        'status': 'assigned',
        'revision': 1,
        'htx_ref': '01920000-0000-7000-8000-0000000000c1',
        'commune_code': '31117',
        'window_from': '2026-10-01',
        'window_to': '2026-10-10',
        'plot_refs': [plotRef],
        'crew_ref': crewRef,
      });
      final store = FlutterSecureStore();
      await store.deleteAll();
      final support = await getApplicationSupportDirectory();
      final dataDir = Directory(p.join(support.path, 'agrigreen_mkg_field_it'));
      if (await dataDir.exists()) await dataDir.delete(recursive: true);
      await dataDir.create(recursive: true);

      final pf = MkgPlatform(
        config: AppConfig(
          apiBaseUrl: server.baseUrl,
          oidcIssuer: 'http://127.0.0.1/oidc-test-mode',
          oidcClientId: mkgOidcClientId,
          oidcRedirectUrl: mkgOidcRedirect,
        ),
        store: store,
        clock: const SystemClock(),
        ids: UuidV7Generator(),
        opener: const SqlCipherOpener(),
        dataDir: dataDir,
        apiFactory: (token, deviceId) => mkgHttpApi(
          baseUrl: server.baseUrl,
          token: token,
          deviceId: deviceId,
          timeout: const Duration(seconds: 5),
        ),
        location: const FixedLocationSource(
          LocationResult(
            GpsStatus.ok,
            GpsFix(lat: 10.045, lon: 105.77, accuracyM: 5),
          ),
        ),
        autoSync: false,
      );

      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            platformProvider.overrideWithValue(pf),
            oidcProvider.overrideWithValue(TestModeOidc()),
          ],
          child: const MkgFieldApp(),
        ),
      );
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
      final s = container.read(appControllerProvider).services!;
      expect(await store.read(SecureKeys.dbKey), hasLength(64));

      try {
        await s.sync.syncOnce(force: true);
        final o = (await s.field.order(orderId))!;
        expect(o.plotRefs, [plotRef]);

        server.offline = true;
        final f = s.field;
        await f.recordCount(
          order: o,
          plotRef: plotRef,
          stage: Stage.bale,
          rollsText: '30',
        );
        await f.recordCount(
          order: o,
          plotRef: plotRef,
          stage: Stage.gather,
          rollsText: '30',
        );
        final last = await f.recordCount(
          order: o,
          plotRef: plotRef,
          stage: Stage.boatLoad,
          rollsText: '29',
          discrepancyReason: 'Một cuộn rơi xuống kênh',
        );
        await f.openShift(o, '6');
        await s.sync.syncOnce(force: true);
        expect((await s.sync.status()).state, SyncState.offline);

        server.offline = false;
        await s.sync.syncOnce(force: true);
        await s.sync.syncOnce(force: true);
        expect(
          (await s.outbox.byId(last.record.id))!.status,
          OutboxStatus.sent,
        );
        expect(server.rollCounts, hasLength(3));
        expect(server.shifts[orderId], hasLength(1));
        expect(
          server.requests.where((r) => r == 'POST /v1/fieldops/sync/push'),
          isNotEmpty,
        );
        final b = await f.board(o);
        expect(b.count(plotRef, Stage.boatLoad)!.rolls, 29);
      } finally {
        await server.close();
      }
    },
  );
}

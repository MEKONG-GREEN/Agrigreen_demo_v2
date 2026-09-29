import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:agrigreen_htx/application/app_services.dart';
import 'package:agrigreen_htx/domain/cropcycle.dart';
import 'package:agrigreen_htx/domain/roles.dart';
import 'package:agrigreen_htx/ui/app.dart';
import 'package:agrigreen_htx/ui/providers.dart';
import 'package:agrigreen_htx/ui/screens/home_screen.dart';
import 'package:agrigreen_mobile/core/clock.dart';
import 'package:agrigreen_mobile/core/config.dart';
import 'package:agrigreen_mobile/core/ids.dart';
import 'package:agrigreen_mobile/data/auth/oidc_client.dart';
import 'package:agrigreen_mobile/data/crypto/envelope.dart';
import 'package:agrigreen_mobile/data/db/sqlcipher_opener.dart';
import 'package:agrigreen_mobile/data/photo/location_source.dart';
import 'package:agrigreen_mobile/data/photo/photo_pipeline.dart';
import 'package:agrigreen_mobile/data/secure/secure_store.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:agrigreen_mobile/data/sync/sync_service.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:image/image.dart' as img;
import 'package:integration_test/integration_test.dart';
import 'package:path/path.dart' as p;
import 'package:path_provider/path_provider.dart';
import 'package:pinenacl/x25519.dart' as nacl;

import 'support/mock_api_server.dart';

const householdRef = '01920000-0000-7000-8000-0000000000a1';
const plotRef = '01920000-0000-7000-8000-0000000000b1';
const sopId = '01920000-0000-7000-8000-0000000005a1';
const templateRef = '01920000-0000-7000-8000-0000000005f1';

/// Access token giả mang `realm_access.roles` (máy chủ giả không kiểm chữ ký).
String _jwt(List<String> roles) {
  String b(Object o) =>
      base64Url.encode(utf8.encode(jsonEncode(o))).replaceAll('=', '');
  return '${b({'alg': 'none'})}.${b({
    'realm_access': {'roles': roles},
  })}.sig';
}

/// Đăng nhập chế độ kiểm thử (không có Keycloak) — chỉ trong kiểm thử.
class TestModeOidc implements OidcClient {
  @override
  Future<OidcTokens> signIn() async => OidcTokens(
    accessToken: _jwt(['farmer']),
    refreshToken: 'it-refresh-token',
    accessTokenExpiry: DateTime.now().add(const Duration(hours: 8)),
  );
  @override
  Future<OidcTokens> refresh(String refreshToken) => signIn();
}

/// Camera giả: JPEG thật tạo trong bộ nhớ (không chạm đĩa).
class GeneratedJpegCamera implements CameraSource {
  @override
  Future<CapturedImage?> capture() async {
    final im = img.Image(width: 800, height: 600);
    img.fill(im, color: img.ColorRgb8(30, 150, 70));
    return CapturedImage(img.encodeJpg(im, quality: 90), 'image/jpeg');
  }
}

nacl.PrivateKey _seed(MockApiServer s) {
  final sk = nacl.PrivateKey.generate();
  s.masterdata['season'] = [
    {'catalog': 'season', 'code': 'TD', 'name': 'Thu Đông', 'attrs': {}},
  ];
  s.masterdata['rice_variety'] = [
    {'catalog': 'rice_variety', 'code': 'OM18', 'name': 'OM18', 'attrs': {}},
  ];
  s.masterdata['machine_type'] = [
    {
      'catalog': 'machine_type',
      'code': 'may_cay',
      'name': 'Máy cày',
      'attrs': {},
    },
  ];
  s.getJson['/v1/me'] = {
    'roles': ['farmer'],
    'org': null,
  };
  s.getJson['/v1/farmer/households'] = {
    'items': [
      {'household_ref': householdRef, 'commune_code': '31117'},
    ],
  };
  s.getJson['/v1/cropcycle/variety-bindings'] = {
    'items': [
      {'variety_code': 'OM18', 'template_ref': templateRef},
    ],
  };
  s
    ..addChange('plot', plotRef, {
      'ref': plotRef,
      'code': 'T1',
      'name': 'Ruộng bờ kênh',
      'commune_code': '31117',
      'household_ref': householdRef,
      'area_ha': '2.0000',
      'status': 'active',
    })
    ..addChange('sop_version', sopId, {
      'id': sopId,
      'template_ref': templateRef,
      'name': 'Quy trình lúa OM18',
      'version_no': 1,
      'steps': [
        {
          'seq': 1,
          'name': 'Gieo sạ',
          'activity_code': 'gieo_sa',
          'day_offset': 0,
          'window_days': 1,
        },
        {
          'seq': 2,
          'name': 'Thu hoạch',
          'activity_code': 'thu_hoach',
          'day_offset': 100,
          'window_days': 5,
        },
      ],
    })
    ..addChange('pubkey', householdRef, {
      'ref': householdRef,
      'area_code': '31117',
      'public_key': base64.encode(sk.publicKey.asTypedList),
    });
  return sk;
}

Future<void> _waitUntil(
  Future<bool> Function() cond,
  Future<void> Function() tick, {
  String reason = '',
}) async {
  final end = DateTime.now().add(const Duration(seconds: 30));
  while (!await cond()) {
    if (DateTime.now().isAfter(end)) fail('Hết thời gian chờ: $reason');
    await tick();
  }
}

/// Kiểm thử tích hợp trên emulator Android (CI job `htx-app-emulator`):
/// SQLCipher thật, Keystore thật, giao diện thật; máy chủ API giả chạy trong
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
    'nông dân: đăng nhập → PIN → mở vụ, nhật ký có ảnh, yêu cầu hỗ trợ khi mất '
    'mạng → có mạng lại → push accepted, ảnh chỉ ở dạng bản mã',
    (tester) async {
      final server = await MockApiServer.start();
      final sk = _seed(server);
      final store = FlutterSecureStore();
      await store.deleteAll();
      final support = await getApplicationSupportDirectory();
      final dataDir = Directory(p.join(support.path, 'agrigreen_htx_it'));
      if (await dataDir.exists()) await dataDir.delete(recursive: true);
      await dataDir.create(recursive: true);
      final copied = <String>[];

      final pf = HtxPlatform(
        config: AppConfig(
          apiBaseUrl: server.baseUrl,
          oidcIssuer: 'http://127.0.0.1/oidc-test-mode',
          oidcClientId: htxOidcClientId,
          oidcRedirectUrl: htxOidcRedirect,
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
        copyText: (t) async => copied.add(t),
        autoSync: false,
      );

      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            platformProvider.overrideWithValue(pf),
            oidcProvider.overrideWithValue(TestModeOidc()),
          ],
          child: const HtxApp(),
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
      Future<void> tick() => tester.pump(const Duration(milliseconds: 100));
      expect(await store.read(SecureKeys.dbKey), hasLength(64));

      try {
        await s.sync.syncOnce(force: true);
        await _waitUntil(
          () async => await s.refs.publicKeyFor(householdRef) != null,
          tick,
          reason: 'pull ban đầu',
        );
        expect(await s.profile.mode(), HtxMode.farmer);
        expect(server.registeredDevices, contains(s.deviceId));

        // Mất mạng: mở vụ, ghi nhật ký có ảnh, gửi yêu cầu hỗ trợ.
        server.offline = true;
        final plot = (await s.crop.plots()).single;
        final today = s.crop.today();
        final open = await s.crop.openSeason(
          plot: plot,
          seasonCode: 'TD',
          seasonYear: today.year,
          varietyCode: 'OM18',
          sowingDate: addDays(today, -1),
        );
        expect(open.payload['sop_version_id'], sopId);
        final season = (await s.crop.openSeasonFor(plotRef))!;
        final shot = await s.photos.capture(subjectRef: householdRef);
        final log = await s.crop.recordLog(
          season: season,
          activity: FarmActivity.gieoSa,
          machineCodes: const ['may_cay'],
          fileRefs: [shot!.file.id],
        );
        final req = await s.support.submit(
          category: 'dich_hai',
          description: 'Rầy nâu xuất hiện ở góc ruộng',
          location: await s.support.locate(),
        );
        expect(await s.crop.harvestBlock(season), msgHarvestTooEarly);
        await s.sync.syncOnce(force: true);
        expect((await s.sync.status()).state, SyncState.offline);
        expect((await s.outbox.byId(log.id))!.status, OutboxStatus.pending);

        // Có mạng lại.
        server.offline = false;
        await s.sync.syncOnce(force: true);
        await _waitUntil(
          () async =>
              (await s.outbox.byId(req.id))?.status == OutboxStatus.sent,
          tick,
          reason: 'push sau khi có mạng',
        );
        for (final r in [open, log, req]) {
          expect((await s.outbox.byId(r.id))!.status, OutboxStatus.sent);
        }
        expect(server.recordsOf('season_open'), hasLength(1));
        expect(server.recordsOf('farm_log'), hasLength(1));
        expect(server.recordsOf('support_request'), hasLength(1));
        final f = server.files[shot.file.id]!;
        expect(f.meta['subject_ref'], householdRef);
        final plain = await EnvelopeCipher.openForTest(
          SealedEnvelope(meta: f.meta, ciphertext: f.blob),
          Uint8List.fromList(sk.asTypedList),
        );
        expect(img.decodeJpg(plain), isNotNull);
      } finally {
        await server.close();
      }
    },
  );
}

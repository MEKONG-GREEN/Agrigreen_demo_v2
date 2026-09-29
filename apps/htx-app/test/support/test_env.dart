import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:agrigreen_htx/application/app_services.dart';
import 'package:agrigreen_htx/ui/app.dart';
import 'package:agrigreen_htx/ui/providers.dart';
import 'package:agrigreen_htx/ui/screens/home_screen.dart';
import 'package:agrigreen_mobile/core/clock.dart';
import 'package:agrigreen_mobile/core/config.dart';
import 'package:agrigreen_mobile/core/ids.dart';
import 'package:agrigreen_mobile/data/auth/oidc_client.dart';
import 'package:agrigreen_mobile/data/auth/pin_service.dart';
import 'package:agrigreen_mobile/data/crypto/envelope.dart';
import 'package:agrigreen_mobile/data/db/local_db.dart';
import 'package:agrigreen_mobile/data/dial/dialer.dart';
import 'package:agrigreen_mobile/data/files/blob_store.dart';
import 'package:agrigreen_mobile/data/photo/location_source.dart';
import 'package:agrigreen_mobile/data/photo/photo_pipeline.dart';
import 'package:agrigreen_mobile/data/push/push_messaging.dart';
import 'package:agrigreen_mobile/data/secure/secure_store.dart';
import 'package:agrigreen_mobile/data/outbox/outbox_repository.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';
import 'package:agrigreen_mobile/testing.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:image/image.dart' as img;
import 'package:pinenacl/x25519.dart' as nacl;
import 'package:sqflite_common_ffi/sqflite_ffi.dart';

/// CSDL SQLite trong bộ nhớ, chạy ngay trong isolate kiểm thử (không isolate
/// phụ) nên dùng được trong `testWidgets` (fake async).
class MemoryDbOpener implements DbOpener {
  MemoryDbOpener() {
    sqfliteFfiInit();
  }

  @override
  Future<Database> open(
    String path, {
    required String key,
    required int version,
    required OnDatabaseCreateFn onCreate,
    OnDatabaseVersionChangeFn? onUpgrade,
  }) => databaseFactoryFfiNoIsolate.openDatabase(
    inMemoryDatabasePath,
    options: OpenDatabaseOptions(
      version: version,
      onCreate: onCreate,
      onUpgrade: onUpgrade,
      singleInstance: false,
    ),
  );

  @override
  Future<void> deleteDatabase(String path) async {}
}

/// Kho ciphertext trong bộ nhớ (không chạm đĩa trong fake async).
class MemoryBlobStore extends BlobStore {
  MemoryBlobStore() : super(Directory('mem'));
  final Map<String, Uint8List> blobs = {};

  @override
  Future<String> write(String id, Uint8List ciphertext) async {
    blobs['mem/$id'] = Uint8List.fromList(ciphertext);
    return 'mem/$id';
  }

  @override
  Future<Uint8List> read(String path) async => blobs[path]!;

  @override
  Future<void> delete(String path) async => blobs.remove(path);

  @override
  Future<void> deleteAll() async => blobs.clear();
}

/// Access token JWT giả mang `realm_access.roles` (không ký — máy chủ giả
/// không kiểm).
String fakeJwt(List<String> roles) {
  String b(Object o) =>
      base64Url.encode(utf8.encode(jsonEncode(o))).replaceAll('=', '');
  return '${b({'alg': 'none'})}.${b({
    'sub': 'u-1',
    'realm_access': {'roles': roles},
  })}.sig';
}

class JwtOidc implements OidcClient {
  JwtOidc(this.roles);
  final List<String> roles;
  int signIns = 0;

  @override
  Future<OidcTokens> signIn() async {
    signIns++;
    return OidcTokens(
      accessToken: fakeJwt(roles),
      refreshToken: 'rt',
      accessTokenExpiry: DateTime.utc(2100),
    );
  }

  @override
  Future<OidcTokens> refresh(String refreshToken) => signIn();
}

class MutableLocation implements LocationSource {
  LocationResult result = const LocationResult(
    GpsStatus.ok,
    GpsFix(lat: 10.0451234, lon: 105.7701234, accuracyM: 6),
  );
  @override
  Future<LocationResult> current() async => result;
}

class FakeCamera implements CameraSource {
  int shots = 0;
  @override
  Future<CapturedImage?> capture() async {
    shots++;
    return CapturedImage(
      Uint8List.fromList(List.filled(64, shots)),
      'image/jpeg',
    );
  }
}

class RecordingDialer implements Dialer {
  final List<Uri> opened = [];
  bool ok = true;
  @override
  Future<bool> open(Uri uri) async {
    opened.add(uri);
    return ok;
  }
}

const householdRef = '01920000-0000-7000-8000-0000000000a1';
const plotRef = '01920000-0000-7000-8000-0000000000b1';
const plot2Ref = '01920000-0000-7000-8000-0000000000b2';
const coopRef = '01920000-0000-7000-8000-0000000000c1';
const sopId = '01920000-0000-7000-8000-0000000005a1';
const templateRef = '01920000-0000-7000-8000-0000000005f1';

/// Hôm nay (giờ VN) của kiểm thử: 01/10/2026 10:00.
final testNow = DateTime.utc(2026, 10, 1, 3);

/// Tile PNG 1×1 trong suốt thay máy chủ tile (kiểm thử không gọi mạng).
final Uint8List blankTilePng = img.encodePng(img.Image(width: 1, height: 1));

/// Môi trường kiểm thử: máy chủ giả theo hợp đồng + mọi phụ thuộc nền tảng.
class TestEnv {
  TestEnv({List<String> roles = const ['farmer'], FakeServer? server})
    : oidc = JwtOidc(roles),
      roles = roles,
      server = server ?? FakeServer();

  final List<String> roles;
  final FakeServer server;
  final MemorySecureStore store = MemorySecureStore();
  final FakeClock clock = FakeClock(testNow);
  final MutableLocation location = MutableLocation();
  final FakeCamera camera = FakeCamera();
  final RecordingDialer dialer = RecordingDialer();
  final MemoryBlobStore blobs = MemoryBlobStore();
  final List<String> copied = [];
  final JwtOidc oidc;

  /// Kênh đẩy giả (FCM) và thông báo cục bộ ghi lại — `notifier.tap(link)`
  /// mô phỏng người dùng chạm thông báo.
  final FakePushMessaging push = FakePushMessaging();
  final RecordingLocalNotifier notifier = RecordingLocalNotifier();
  final householdKey = nacl.PrivateKey.fromSeed(
    Uint8List(32)..fillRange(0, 32, 7),
  );

  late final HtxPlatform platform = HtxPlatform(
    config: const AppConfig(
      apiBaseUrl: 'https://api.test',
      oidcIssuer: 'https://id.agrigreen.test/realms/agrigreen',
      oidcClientId: htxOidcClientId,
      oidcRedirectUrl: htxOidcRedirect,
    ),
    store: store,
    clock: clock,
    ids: UuidV7Generator(),
    opener: MemoryDbOpener(),
    dataDir: Directory('mem'),
    apiFactory: (_, _) => server,
    camera: camera,
    location: location,
    copyText: (t) async => copied.add(t),
    watermarker: const NoopWatermarker(),
    dialer: dialer,
    blobStore: blobs,
    autoSync: false,
    push: push,
    notifier: notifier,
    tileFetcher: (_) async => blankTilePng,
  );

  /// Dữ liệu chiều xuống mặc định: thửa của hộ, SOP, khóa công khai hộ.
  void seed({
    List<Map<String, Object?>> extra = const [],
    bool season = false,
    String sowing = '2026-09-25',
    String harvestOpen = '2027-01-10',
  }) {
    var v = 0;
    Map<String, Object?> ch(String e, String id, Map<String, Object?> d) => {
      'entity': e,
      'id': id,
      'version': ++v,
      'op': 'upsert',
      'data': d,
    };
    final changes = [
      ch('plot', plotRef, {
        'ref': plotRef,
        'code': 'T1',
        'name': 'Ruộng bờ kênh',
        'commune_code': '31117',
        'cooperative_ref': coopRef,
        'household_ref': householdRef,
        'area_ha': '2.0000',
        'status': 'active',
      }),
      ch('plot', plot2Ref, {
        'ref': plot2Ref,
        'code': 'T2',
        'commune_code': '31117',
        'cooperative_ref': coopRef,
        'household_ref': householdRef,
        'area_ha': '1.5000',
        'status': 'active',
      }),
      ch('sop_version', sopId, {
        'id': sopId,
        'template_ref': templateRef,
        'code': 'SOP-OM18',
        'name': 'Quy trình lúa OM18',
        'template_status': 'active',
        'version_no': 1,
        'steps': [
          {
            'seq': 1,
            'name': 'Làm đất',
            'activity_code': 'lam_dat',
            'day_offset': -7,
            'window_days': 3,
          },
          {
            'seq': 2,
            'name': 'Gieo sạ',
            'activity_code': 'gieo_sa',
            'day_offset': 0,
            'window_days': 1,
          },
          {
            'seq': 3,
            'name': 'Bón phân đợt 1',
            'activity_code': 'bon_phan',
            'day_offset': 7,
            'window_days': 2,
            'instruction': 'Bón thúc 7–10 ngày sau sạ, giữ nước 3–5 cm.',
          },
          {
            'seq': 4,
            'name': 'Thu hoạch',
            'activity_code': 'thu_hoach',
            'day_offset': 100,
            'window_days': 5,
          },
        ],
        'harvest_day_offset': 100,
        'harvest_window_days': 5,
      }),
      ch('pubkey', householdRef, {
        'ref': householdRef,
        'area_code': '31117',
        'public_key': base64.encode(householdKey.publicKey.asTypedList),
      }),
      if (season)
        ch('season', 's-1', {
          'season_ref': '01920000-0000-7000-8000-00000000e001',
          'plot_ref': plotRef,
          'plot_code': 'T1',
          'household_ref': householdRef,
          'cooperative_ref': coopRef,
          'commune_code': '31117',
          'season_code': 'TD',
          'season_year': 2026,
          'variety_code': 'OM18',
          'sop_version_id': sopId,
          'sowing_date': sowing,
          'expected_harvest_date': '2027-01-03',
          'harvest_open_date': harvestOpen,
          'area_ha': '2.0000',
          'status': 'open',
          'current_event_id': '01920000-0000-7000-8000-00000000e0e1',
        }),
      for (final e in extra) {...e, 'version': ++v},
    ];
    server.pages[null] = PullPage(
      changes: [for (final c in changes) PullChange.fromJson(c)],
      nextCursor: 'c1',
      hasMore: false,
    );
    server.onGet = (path, query) async => switch (path) {
      '/v1/me' => {
        'roles': roles,
        'org': roles.contains('coop_manager')
            ? {'ref': coopRef, 'name': 'HTX Tân Phú', 'kind': 'cooperative'}
            : null,
      },
      '/v1/farmer/households' => {
        'items': [
          {
            'household_ref': householdRef,
            'cooperative_ref': coopRef,
            'commune_code': '31117',
          },
        ],
      },
      '/v1/masterdata/catalogs/season' => {
        'items': [
          {'code': 'DX', 'name': 'Đông Xuân'},
          {'code': 'HT', 'name': 'Hè Thu'},
          {'code': 'TD', 'name': 'Thu Đông'},
        ],
        'next_cursor': null,
      },
      '/v1/masterdata/catalogs/rice_variety' => {
        'items': [
          {
            'code': 'OM18',
            'name': 'OM18',
            'attrs': {'yield_anomaly_max_t_per_ha': '9'},
          },
          {'code': 'ST25', 'name': 'ST25', 'attrs': {}},
        ],
        'next_cursor': null,
      },
      '/v1/masterdata/catalogs/machine_type' => {
        'items': [
          {'code': 'may_cay', 'name': 'Máy cày'},
          {'code': 'drone', 'name': 'Drone phun thuốc'},
        ],
        'next_cursor': null,
      },
      '/v1/cropcycle/variety-bindings' => {
        'items': [
          {'variety_code': 'OM18', 'template_ref': templateRef},
        ],
      },
      _ => null,
    };
  }
}

/// Bơm khung hình một lúc (không dùng pumpAndSettle vì vòng quay tải).
Future<void> settle(WidgetTester tester, [int frames = 20]) async {
  for (var i = 0; i < frames; i++) {
    await tester.pump(const Duration(milliseconds: 50));
  }
}

Widget appUnderTest(TestEnv env) => ProviderScope(
  overrides: [
    platformProvider.overrideWithValue(env.platform),
    oidcProvider.overrideWithValue(env.oidc),
    // PBKDF2 nhẹ cho kiểm thử (bản thật 60 000 vòng).
    pinServiceProvider.overrideWith(
      (ref) => PinService(store: env.store, clock: env.clock, iterations: 1000),
    ),
  ],
  child: const HtxApp(),
);

/// Đăng nhập (OIDC giả) → tạo PIN → trang chủ; đợi lượt đồng bộ đầu.
Future<HtxServices> loginToHome(WidgetTester tester, TestEnv env) async {
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
  expect(find.byType(HomeScreen), findsOneWidget);
  final c = ProviderScope.containerOf(tester.element(find.byType(HomeScreen)));
  final s = c.read(appControllerProvider).services!;
  await s.sync.syncOnce(force: true);
  await c.read(appControllerProvider.notifier).refreshMode();
  c.invalidate(homeSummaryProvider);
  await settle(tester);
  return s;
}

ProviderContainer containerOf(WidgetTester tester) =>
    ProviderScope.containerOf(tester.element(find.byType(MaterialApp)));

/// Mở màn hình con bằng chạm ô lối tắt trên trang chủ.
Future<void> openTile(WidgetTester tester, String label) =>
    tapOn(tester, find.text(label));

/// Mở bằng khóa công khai của hộ (kiểm tra ảnh đã mã hóa đúng).
Future<Uint8List> openEnvelope(
  TestEnv env,
  Map<String, Object?> meta,
  Uint8List blob,
) => EnvelopeCipher.openForTest(
  SealedEnvelope(meta: meta, ciphertext: blob),
  Uint8List.fromList(env.householdKey.asTypedList),
);

/// Chạm widget (cuộn tới nếu cần).
Future<void> tapOn(WidgetTester tester, Finder f) async {
  if (f.evaluate().isEmpty) {
    // Danh sách dựng lười: kéo tới khi phần tử được dựng.
    final scrollable = find.descendant(
      of: find.byType(ListView).first,
      matching: find.byType(Scrollable),
    );
    // Cuộn bằng vị trí (kéo có thể bị ô nhập nhiều dòng "nuốt").
    final pos = tester.state<ScrollableState>(scrollable.first).position;
    pos.jumpTo(0);
    await tester.pump();
    for (var i = 0; i < 40 && f.evaluate().isEmpty; i++) {
      if (pos.pixels >= pos.maxScrollExtent) break;
      pos.jumpTo((pos.pixels + 250).clamp(0, pos.maxScrollExtent));
      await tester.pump();
    }
  }
  // SnackBar có thể che nút ở đáy màn hình: bỏ trước khi chạm.
  ScaffoldMessenger.maybeOf(tester.element(f.first))?.clearSnackBars();
  await tester.pump(const Duration(milliseconds: 400));
  await tester.ensureVisible(f.first);
  await tester.pump();
  await tester.tap(f.first);
  await settle(tester);
}

Future<void> tapText(WidgetTester tester, String text) =>
    tapOn(tester, find.text(text));

/// Kết thúc: tháo cây widget, chạy hết bộ đếm giờ (SnackBar…). CSDL trong bộ
/// nhớ được bỏ cùng tiến trình kiểm thử.
Future<void> finish(WidgetTester tester) async {
  await tester.pumpWidget(const SizedBox());
  await tester.pump(const Duration(seconds: 10));
}

/// Máy chủ giả trả mã lỗi nghiệp vụ cụ thể cho bản ghi bị từ chối
/// (vd `cropcycle.yield_anomaly`).
class CodedRejectServer extends FakeServer {
  String code = 'validation_failed';

  @override
  Future<List<PushResult>> push({
    required String batchKey,
    required String deviceId,
    required List<OutboxRecord> records,
  }) async => [
    for (final r in await super.push(
      batchKey: batchKey,
      deviceId: deviceId,
      records: records,
    ))
      r.status == 'rejected'
          ? PushResult(id: r.id, status: 'rejected', code: code)
          : r,
  ];
}

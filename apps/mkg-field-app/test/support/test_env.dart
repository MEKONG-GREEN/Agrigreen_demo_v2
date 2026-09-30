import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:agrigreen_mkg_field/application/app_services.dart';
import 'package:agrigreen_mkg_field/ui/app.dart';
import 'package:agrigreen_mkg_field/ui/providers.dart';
import 'package:agrigreen_mkg_field/ui/screens/home_screen.dart';
import 'package:agrigreen_mobile/core/clock.dart';
import 'package:agrigreen_mobile/core/config.dart';
import 'package:agrigreen_mobile/core/ids.dart';
import 'package:agrigreen_mobile/data/auth/oidc_client.dart';
import 'package:agrigreen_mobile/data/auth/pin_service.dart';
import 'package:agrigreen_mobile/data/crypto/envelope.dart';
import 'package:agrigreen_mobile/data/db/local_db.dart';
import 'package:agrigreen_mobile/data/files/blob_store.dart';
import 'package:agrigreen_mobile/data/outbox/outbox_repository.dart';
import 'package:agrigreen_mobile/data/photo/location_source.dart';
import 'package:agrigreen_mobile/data/secure/secure_store.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';
import 'package:agrigreen_mobile/testing.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sqflite_common_ffi/sqflite_ffi.dart';

/// SQLite trong bộ nhớ, cùng isolate (dùng được trong `testWidgets`).
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

class MemoryBlobStore extends BlobStore {
  MemoryBlobStore() : super(Directory('mem'));
  @override
  Future<String> write(String id, Uint8List ciphertext) async => 'mem/$id';
  @override
  Future<Uint8List> read(String path) async => Uint8List(0);
  @override
  Future<void> delete(String path) async {}
  @override
  Future<void> deleteAll() async {}
}

const meSub = '01920000-0000-7000-8000-00000000aa01';
const otherSub = '01920000-0000-7000-8000-00000000aa02';
const orderId = '01920000-0000-7000-8000-0000000001a1';
const order2Id = '01920000-0000-7000-8000-0000000001a2';
const htxRef = '01920000-0000-7000-8000-0000000000c1';
const crewRef = '01920000-0000-7000-8000-0000000000e1';
const plotA = '01920000-0000-7000-8000-0000000000b1';
const plotB = '01920000-0000-7000-8000-0000000000b2';

/// JWT giả (không ký) mang `sub` và `realm_access.roles`.
String fakeJwt(List<String> roles, {String sub = meSub}) {
  String b(Object o) =>
      base64Url.encode(utf8.encode(jsonEncode(o))).replaceAll('=', '');
  return '${b({'alg': 'none'})}.${b({
    'sub': sub,
    'realm_access': {'roles': roles},
  })}.sig';
}

class JwtOidc implements OidcClient {
  JwtOidc(this.roles);
  final List<String> roles;
  @override
  Future<OidcTokens> signIn() async => OidcTokens(
    accessToken: fakeJwt(roles),
    refreshToken: 'rt',
    accessTokenExpiry: DateTime.utc(2100),
  );
  @override
  Future<OidcTokens> refresh(String refreshToken) => signIn();
}

class MutableLocation implements LocationSource {
  LocationResult result = const LocationResult(
    GpsStatus.ok,
    GpsFix(lat: 10.0451234, lon: 105.7701234, accuracyM: 6),
  );
  bool hang = false;
  @override
  Future<LocationResult> current() async {
    if (hang) await Future<void>.delayed(const Duration(hours: 1));
    return result;
  }
}

/// Máy chủ api-internal giả (mkg/fieldops): push/pull theo hợp đồng (lõi
/// FakeServer: chống trùng theo idem_key, Idempotency-Key theo lô) + GET/POST
/// của fieldops. [rejectWith] trả mã `fieldops.*` để từ chối một bản ghi.
class FakeMkgServer extends FakeServer {
  String? Function(Map<String, Object?> wire)? rejectWith;

  /// Số hiệu lực trả cho `GET …/roll-counts` (order → totals).
  final Map<String, List<Map<String, Object?>>> totals = {};
  final Map<String, List<Map<String, Object?>>> shiftItems = {};
  List<Map<String, Object?>> planned = [];
  List<Map<String, Object?>> recon = [];
  final List<(String, Map<String, Object?>?)> posts = [];
  bool offline = false;

  @override
  Future<List<PushResult>> push({
    required String batchKey,
    required String deviceId,
    required List<OutboxRecord> records,
  }) async {
    if (offline) throw NetworkException('offline');
    final codes = {
      for (final r in records)
        r.id: rejectWith?.call(
          jsonDecode(jsonEncode(r.toWire())) as Map<String, Object?>,
        ),
    };
    final res = await super.push(
      batchKey: batchKey,
      deviceId: deviceId,
      records: records,
    );
    final keys = {for (final r in records) r.id: r.idemKey};
    final out = <PushResult>[];
    for (final r in res) {
      final code = codes[r.id];
      if (code != null && r.status == 'accepted') {
        ledger.remove(keys[r.id]); // bản bị từ chối không được ghi sổ
        out.add(PushResult(id: r.id, status: 'rejected', code: code));
      } else {
        out.add(r);
      }
    }
    return out;
  }

  @override
  Future<PullPage> pull({
    String? cursor,
    required int limit,
    List<String>? entities,
  }) {
    if (offline) throw NetworkException('offline');
    return super.pull(cursor: cursor, limit: limit, entities: entities);
  }

  FakeMkgServer() {
    onGet = (path, query) async {
      if (offline) throw NetworkException('offline');
      final m = RegExp(
        r'^/v1/fieldops/orders/([^/]+)/(roll-counts|crew-shifts)$',
      ).firstMatch(path);
      if (m != null) {
        return m.group(2) == 'roll-counts'
            ? {
                'counts': <Object?>[],
                'totals': totals[m.group(1)] ?? derivedTotals(m.group(1)!),
              }
            : {'items': shiftItems[m.group(1)] ?? const []};
      }
      if (path == '/v1/fieldops/orders' && query?['status'] == 'planned') {
        return {'items': planned, 'next_cursor': null};
      }
      if (path == '/v1/fieldops/reconciliation') {
        return {'items': recon, 'next_cursor': null};
      }
      return null;
    };
    onPost = (path, body) async {
      if (offline) throw NetworkException('offline');
      posts.add((path, body));
      return const {};
    };
  }

  /// Số hiệu lực suy từ sổ (bản sau thay bản trước cùng thửa/công đoạn) —
  /// như máy chủ thật sau khi nhận push.
  List<Map<String, Object?>> derivedTotals(String order) {
    final eff = <String, Map<String, Object?>>{};
    for (final w in rollCounts) {
      final p = (w['payload']! as Map).cast<String, Object?>();
      if (p['order_id'] != order) continue;
      eff['${p['plot_ref']}|${p['stage']}'] = {
        'plot_ref': p['plot_ref'],
        'stage': p['stage'],
        'rolls': p['rolls'],
        'loose_kg': p['loose_kg'],
        'discrepancy_reason': p['discrepancy_reason'],
        'roll_count_id': w['id'],
      };
    }
    return eff.values.toList();
  }

  /// Rớt ledger thành danh sách bản ghi roll_count đã ghi sổ.
  List<Map<String, Object?>> get rollCounts => [
    for (final w in ledger.values)
      if (w['entity'] == 'roll_count') w,
  ];
}

Map<String, Object?> orderData({
  String id = orderId,
  String status = 'assigned',
  List<String> plots = const [plotA, plotB],
  String? crew = crewRef,
  String? createdBy,
}) => {
  'order_id': id,
  'status': status,
  'revision': 1,
  'htx_ref': htxRef,
  'commune_code': '31117',
  'window_from': '2026-10-01',
  'window_to': '2026-10-10',
  'plot_refs': plots,
  'crew_ref': crew,
  'created_by': ?createdBy,
};

/// Hôm nay của kiểm thử: 01/10/2026 10:00 giờ VN.
final testNow = DateTime.utc(2026, 10, 1, 3);

class TestEnv {
  TestEnv({List<String> roles = const ['field_collector']})
    : oidc = JwtOidc(roles),
      server = FakeMkgServer();

  final FakeMkgServer server;
  final MemorySecureStore store = MemorySecureStore();
  final FakeClock clock = FakeClock(testNow);
  final MutableLocation location = MutableLocation();
  final JwtOidc oidc;

  late final MkgPlatform platform = MkgPlatform(
    config: const AppConfig(
      apiBaseUrl: 'https://mkg-internal.test',
      oidcIssuer: 'https://id.agrigreen.test/realms/agrigreen',
      oidcClientId: mkgOidcClientId,
      oidcRedirectUrl: mkgOidcRedirect,
    ),
    store: store,
    clock: clock,
    ids: UuidV7Generator(),
    opener: MemoryDbOpener(),
    dataDir: Directory('mem'),
    apiFactory: (_, _) => server,
    location: location,
    blobStore: MemoryBlobStore(),
    autoSync: false,
    gpsTimeout: const Duration(milliseconds: 200),
  );

  /// Chiều xuống: các lệnh gom (collection_order).
  void seed({List<Map<String, Object?>>? orders}) {
    var v = 0;
    server.pages[null] = PullPage(
      changes: [
        for (final o in orders ?? [orderData()])
          PullChange.fromJson({
            'entity': 'collection_order',
            'id': o['order_id'],
            'version': ++v,
            'op': 'upsert',
            'data': o,
          }),
      ],
      nextCursor: 'c1',
      hasMore: false,
    );
  }

  /// Mở dịch vụ trực tiếp (kiểm thử miền, không giao diện).
  Future<MkgServices> open() async {
    await store.write(SecureKeys.accessToken, fakeJwt(oidc.roles));
    return MkgServices.open(platform, accessToken: () async => 'tok');
  }
}

Future<void> settle(WidgetTester tester, [int frames = 20]) async {
  for (var i = 0; i < frames; i++) {
    await tester.pump(const Duration(milliseconds: 50));
  }
}

Widget appUnderTest(TestEnv env) => ProviderScope(
  overrides: [
    platformProvider.overrideWithValue(env.platform),
    oidcProvider.overrideWithValue(env.oidc),
    pinServiceProvider.overrideWith(
      (ref) => PinService(store: env.store, clock: env.clock, iterations: 1000),
    ),
  ],
  child: const MkgFieldApp(),
);

/// Đăng nhập (OIDC giả) → tạo PIN → trang chủ; đợi lượt đồng bộ đầu.
Future<MkgServices> loginToHome(WidgetTester tester, TestEnv env) async {
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
  c.invalidate(homeProvider);
  await settle(tester);
  return s;
}

/// Chạm widget (cuộn tới nếu cần).
Future<void> tapOn(WidgetTester tester, Finder f) async {
  if (f.evaluate().isEmpty) {
    // Danh sách dựng lười (màn hình nhỏ): cuộn tới khi phần tử được dựng.
    await tester.scrollUntilVisible(
      f,
      200,
      scrollable: find.byType(Scrollable).last,
    );
  }
  await tester.ensureVisible(f.first);
  await tester.pump();
  await tester.tap(f.first);
  await settle(tester);
}

Future<void> finish(WidgetTester tester) async {
  await tester.pumpWidget(const SizedBox());
  await tester.pump(const Duration(seconds: 10));
}

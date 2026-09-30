import 'dart:convert';

import 'package:agrigreen_mkg_field/application/app_services.dart';
import 'package:agrigreen_mkg_field/domain/trips.dart';
import 'package:agrigreen_mobile/data/actions/action_queue.dart';
import 'package:agrigreen_mobile/data/secure/secure_store.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:agrigreen_mobile/data/sync/sync_service.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'support/fake_logistics.dart';
import 'support/test_env.dart';

/// api-internal giả sau HTTP thật của lõi (HttpSyncApi + MockClient): kiểm
/// header (Authorization, Idempotency-Key, X-Device-Id), thân JSON đúng hợp
/// đồng `recordLogisticsMilestone`, problem+json → mã `logistics.*`.
class _HttpLogistics {
  _HttpLogistics();
  final FakeLogistics logistics = FakeLogistics();
  final requests = <http.Request>[];

  /// Trả lỗi này cho POST mốc kế tiếp (status, code) rồi xóa.
  final failNextPosts = <(int, String)>[];
  bool offline = false;

  static final _idemRe = RegExp(r'^[A-Za-z0-9._:-]{8,200}$');
  static final _ms = RegExp(r'^/v1/logistics/trips/([^/]+)/milestones$');

  http.Response _problem(int status, String code) => http.Response.bytes(
    utf8.encode(jsonEncode({'status': status, 'code': code, 'title': code})),
    status,
    headers: {'content-type': 'application/problem+json'},
  );

  http.Response _ok(Object body, [int status = 200]) => http.Response.bytes(
    utf8.encode(jsonEncode(body)),
    status,
    headers: {'content-type': 'application/json'},
  );

  late final client = MockClient((req) async {
    if (offline) throw http.ClientException('offline');
    requests.add(req);
    if (req.headers['Authorization'] != 'Bearer tok') {
      return _problem(401, 'unauthenticated');
    }
    final path = req.url.path;
    if (req.method == 'GET' && path == mkgPullPath) {
      return _ok({'changes': [], 'next_cursor': 'c1', 'has_more': false});
    }
    try {
      if (req.method == 'GET') {
        final r = logistics.get(path, req.url.queryParameters);
        return r == null ? _problem(404, 'not_found') : _ok(r);
      }
      final m = _ms.firstMatch(path);
      if (req.method == 'POST' && m != null) {
        final key = req.headers['Idempotency-Key'];
        if (key == null || !_idemRe.hasMatch(key)) {
          return _problem(400, 'idempotency_key_required');
        }
        if (failNextPosts.isNotEmpty) {
          final (s, c) = failNextPosts.removeAt(0);
          return _problem(s, c);
        }
        final body = (jsonDecode(req.body) as Map).cast<String, Object?>();
        final replay = logistics.replays;
        final r = logistics.milestone(m.group(1)!, key, body);
        return _ok(r, logistics.replays > replay ? 200 : 201);
      }
    } on ApiException catch (e) {
      return _problem(e.status, e.code);
    }
    return _problem(404, 'not_found');
  });
}

void main() {
  test('W-02 HTTP: mốc qua hàng đợi — 503 thử lại, mất mạng giữ chờ, gửi lại cùng Idempotency-Key, 409 problem+json → xung đột', () async {
    final h = _HttpLogistics();
    h.logistics
      ..add(tripData(driver: meSub))
      ..add(
        tripData(
          id: tripB,
          code: 'CH000002',
          driver: meSub,
          plannedAt: '2026-10-01T00:30:00Z',
        ),
      );
    final env = TestEnv(roles: const ['driver']);
    await env.store.write(SecureKeys.accessToken, fakeJwt(const ['driver']));
    final pf = env.platform;
    final s = await MkgServices.open(
      MkgPlatformWithApi(
        pf,
        (deviceId) => HttpSyncApi(
          baseUrl: pf.config.apiBaseUrl,
          accessToken: () async => 'tok',
          deviceId: deviceId,
          client: h.client,
          pushPath: mkgPushPath,
          pullPath: mkgPullPath,
        ),
      ).platform,
      accessToken: () async => 'tok',
    );
    addTearDown(s.close);

    await s.sync.syncOnce(force: true);
    expect(
      [for (final v in await s.trips.trips()) v.trip.code],
      ['CH000001', 'CH000002'],
    );
    final gets = [
      for (final r in h.requests)
        if (r.url.path == '/v1/logistics/trips')
          r.url.queryParameters['status'],
    ];
    expect(gets, ['planned', 'departed', 'arrived']);

    // 1) 503 → lệnh vẫn chờ, lượt đồng bộ lùi thử lại.
    final l = await s.trips.record(tripA, Milestone.departed);
    h.failNextPosts.add((503, 'unavailable'));
    await s.sync.syncOnce(force: true);
    expect((await s.sync.status()).state, SyncState.error);
    expect(await s.trips.pendingCount(), 1);
    expect((await s.actions.pending()).single.lastError, 'unavailable');

    // 2) Mất mạng → vẫn chờ.
    h.offline = true;
    await s.sync.syncOnce(force: true);
    expect((await s.sync.status()).state, SyncState.offline);
    h.offline = false;

    // 3) Máy chủ ghi nhưng phản hồi mất → gửi lại cùng khóa, phát lại 200.
    h.logistics.dropResponses = 1;
    await s.sync.syncOnce(force: true);
    await s.sync.syncOnce(force: true);
    final posts = [
      for (final r in h.requests)
        if (r.method == 'POST') r,
    ];
    expect(posts.map((r) => r.headers['Idempotency-Key']).toSet(), {
      'act:${l.actionId}',
    });
    expect(posts.every((r) => r.headers['X-Device-Id'] == s.deviceId), isTrue);
    final body = jsonDecode(posts.last.body) as Map;
    expect(body.keys.toSet(), {'kind', 'event_time', 'lat', 'lng'});
    expect(body['lat'], '10.0451234');
    // Thân gửi lại giống từng byte (máy chủ băm thân theo Idempotency-Key).
    expect(posts.map((r) => r.body).toSet(), hasLength(1));
    expect(h.logistics.milestones, hasLength(1));
    expect(h.logistics.replays, 1);
    expect(await s.trips.pendingCount(), 0);
    expect((await s.trips.trip(tripA))!.trip.status, TripStatus.departed);

    // 4) 409 problem+json logistics.trip_transition → xung đột, không thử lại.
    await s.trips.record(tripB, Milestone.departed);
    h.logistics.trips[tripB]!['status'] = 'arrived';
    await s.sync.syncOnce(force: true);
    final failed = [
      for (final a in await s.actions.ofKind(ApiActionKind.logisticsMilestone))
        if (a.status == ApiActionStatus.failed) a,
    ];
    expect(failed.single.lastError, 'logistics.trip_transition');
    expect((await s.sync.status()).state, SyncState.idle);
    final v = (await s.trips.trip(tripB))!;
    expect(v.status, TripStatus.arrived);
    expect(v.next, isNull);
    expect(v.conflicts.single.errorCode, 'logistics.trip_transition');
  });
}

/// Bọc MkgPlatform để thay apiFactory bằng HTTP thật tới MockClient.
class MkgPlatformWithApi {
  MkgPlatformWithApi(MkgPlatform pf, SyncApi Function(String deviceId) api)
    : platform = MkgPlatform(
        config: pf.config,
        store: pf.store,
        clock: pf.clock,
        ids: pf.ids,
        opener: pf.opener,
        dataDir: pf.dataDir,
        apiFactory: (_, deviceId) => api(deviceId),
        location: pf.location,
        blobStore: pf.blobStore,
        autoSync: false,
        gpsTimeout: pf.gpsTimeout,
      );
  final MkgPlatform platform;
}

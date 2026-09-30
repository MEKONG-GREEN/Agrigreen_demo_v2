import 'package:agrigreen_mkg_field/application/trip_service.dart';
import 'package:agrigreen_mkg_field/domain/trips.dart';
import 'package:agrigreen_mobile/data/actions/action_queue.dart';
import 'package:agrigreen_mobile/data/crypto/envelope.dart';
import 'package:agrigreen_mobile/data/photo/location_source.dart';
import 'package:agrigreen_mobile/data/sync/sync_service.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/fake_logistics.dart';
import 'support/test_env.dart';

TestEnv _driverEnv({List<String> roles = const ['driver']}) {
  final env = TestEnv(roles: roles)..seed(orders: const []);
  env.server.logistics
    ..add(tripData(driver: meSub))
    ..add(
      tripData(
        id: tripB,
        code: 'CH000002',
        driver: otherSub,
        plannedAt: '2026-10-01T02:00:00Z',
      ),
    )
    ..add(
      tripData(
        id: tripC,
        code: 'CH000003',
        status: 'completed',
        driver: meSub,
        plannedAt: '2026-09-30T02:00:00Z',
      ),
    );
  return env;
}

/// G4 W-02: mốc chuyến của tài xế ngoại tuyến trước — kéo chuyến khi đồng bộ
/// (chỉ vai driver, con trỏ), xếp mốc vào hàng lệnh API với Idempotency-Key
/// cố định, gửi khi có mạng, phát lại an toàn, 409 logistics.* → xung đột.
void main() {
  test(
    'W-02 chỉ vai driver mới kéo chuyến; tổ gom không gọi /v1/logistics',
    () async {
      final env = _driverEnv(roles: const ['field_collector']);
      final s = await env.open();
      addTearDown(s.close);
      await s.sync.syncOnce(force: true);
      expect(env.server.logistics.listQueries, isEmpty);
      expect(await s.trips.trips(), isEmpty);
      await expectLater(
        s.trips.record(tripA, Milestone.departed),
        throwsA(
          isA<TripError>().having(
            (e) => e.message,
            'm',
            contains('vai tài xế'),
          ),
        ),
      );
    },
  );

  test('W-02 kéo chuyến theo con trỏ (trang 1 chuyến), lọc driver_ref = sub, bỏ completed; chi tiết kèm chặng; không lưu cước', () async {
    final env = _driverEnv();
    env.server.logistics.pageCap = 1;
    env.server.logistics.add(
      tripData(
        id: '01920000-0000-7000-8000-00000000d0a4',
        code: 'CH000004',
        status: 'departed',
        driver: meSub,
        plannedAt: '2026-09-30T23:00:00Z',
      ),
    );
    final s = await env.open();
    addTearDown(s.close);
    final r = await s.sync.syncOnce(force: true);
    expect(r, isNotNull);
    final q = env.server.logistics.listQueries;
    // Ba trạng thái hoạt động; planned có 2 chuyến → 2 trang + trang rỗng.
    expect(
      {for (final x in q) x?['status']},
      {'planned', 'departed', 'arrived'},
    );
    expect(q.where((x) => x?['cursor'] != null), isNotEmpty);
    expect(q.any((x) => x?['status'] == 'completed'), isFalse);
    final trips = await s.trips.trips();
    expect([for (final v in trips) v.trip.code], ['CH000004', 'CH000001']);
    expect(trips.last.trip.legs.single.to.ref, facilityRef);
    expect(trips.last.trip.destination?.kind, 'facility');
    final raw = await s.db.getMeta(TripService.tripsKey);
    expect(raw, isNot(contains('planned_cost')));
    expect(raw, isNot(contains('11250000')));
    expect(raw, isNot(contains('cargo')));
    // Lượt sau: plan_revision không đổi → không tải lại chi tiết.
    final before = env.server.logistics.detailGets.length;
    await s.sync.syncOnce(force: true);
    expect(env.server.logistics.detailGets.length, before);
    // Con trỏ hỏng giữa chừng (422) → đọc lại từ đầu một lần, không mất chuyến.
    env.server.logistics.corruptCursors = 1;
    q.clear();
    await s.sync.syncOnce(force: true);
    expect(q.where((x) => x?['cursor'] == 'hỏng'), hasLength(1));
    expect(
      [for (final v in await s.trips.trips()) v.trip.code],
      ['CH000004', 'CH000001'],
    );
  });

  test('W-02 ghi mốc ngoại tuyến: event_time theo đồng hồ máy, GPS + sai số, chỉ tiến; có mạng → gửi đúng một lần', () async {
    final env = _driverEnv();
    final s = await env.open();
    addTearDown(s.close);
    await s.sync.syncOnce(force: true);
    env.server.offline = true;
    env.location.result = const LocationResult(
      GpsStatus.weak,
      GpsFix(lat: 10.04512345678, lon: 105.7701, accuracyM: 35),
    );
    final l = await s.trips.record(tripA, Milestone.departed);
    expect(l.state, MilestoneState.pending);
    expect(l.lat, '10.0451235');
    expect(l.lng, '105.7701000');
    expect(l.accuracyM, 35);
    expect(l.gpsLabel, '10.0451235, 105.7701000 (±35 m)');
    expect(l.eventTime.toUtc(), testNow);
    var v = (await s.trips.trip(tripA))!;
    expect(v.status, TripStatus.departed);
    expect(v.next, Milestone.arrived);
    // Chỉ tiến: ghi lại xuất phát bị chặn trên máy.
    await expectLater(
      s.trips.record(tripA, Milestone.departed),
      throwsA(
        isA<TripError>().having(
          (e) => e.message,
          'm',
          contains('không ghi lùi'),
        ),
      ),
    );
    // Đồng bộ khi mất mạng: vẫn chờ.
    await s.sync.syncOnce(force: true);
    expect(await s.trips.pendingCount(), 1);
    expect(env.server.logistics.milestones, isEmpty);

    env.clock.advance(const Duration(hours: 2));
    await s.trips.record(tripA, Milestone.arrived);
    expect((await s.trips.trip(tripA))!.status, TripStatus.arrived);
    expect((await s.trips.trip(tripA))!.next, isNull);

    env.server.offline = false;
    await s.sync.syncOnce(force: true);
    final ms = env.server.logistics.milestones;
    expect([for (final m in ms) m['kind']], ['departed', 'arrived']);
    // RFC 3339 có offset của máy, đúng thời điểm bấm (đồng hồ máy).
    final t0 = '${ms.first['event_time']}';
    expect(
      t0,
      matches(RegExp(r'^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d[+-]\d\d:\d\d$')),
    );
    expect(DateTime.parse(t0).toUtc(), testNow);
    expect(
      DateTime.parse('${ms.last['event_time']}').toUtc(),
      testNow.add(const Duration(hours: 2)),
    );
    expect(ms.first['lat'], '10.0451235');
    expect(ms.first.keys, containsAll(['kind', 'event_time', 'lat', 'lng']));
    expect(ms.first['key'], startsWith('act:'));
    expect(await s.trips.pendingCount(), 0);
    v = (await s.trips.trip(tripA))!;
    expect(v.trip.status, TripStatus.arrived); // bản máy chủ sau pull
    expect(v.conflicts, isEmpty);
    expect(env.server.actionCalls.map((c) => c.$1).toSet(), {
      '/v1/logistics/trips/$tripA/milestones',
    });
  });

  test('W-02 phát lại idempotent: phản hồi mất sau khi máy chủ ghi → gửi lại cùng Idempotency-Key, không ghi hai lần', () async {
    final env = _driverEnv();
    final s = await env.open();
    addTearDown(s.close);
    await s.sync.syncOnce(force: true);
    env.server.logistics.dropResponses = 1;
    final l = await s.trips.record(tripA, Milestone.departed);
    await s.sync.syncOnce(force: true);
    // Máy chưa thấy phản hồi → vẫn chờ, lùi thử lại.
    expect((await s.sync.status()).state, SyncState.offline);
    expect(await s.trips.pendingCount(), 1);
    expect(env.server.logistics.milestones, hasLength(1));
    await s.sync.syncOnce(force: true);
    expect(env.server.logistics.milestones, hasLength(1));
    expect(env.server.logistics.replays, 1);
    final keys = [for (final c in env.server.actionCalls) c.$2];
    expect(keys, ['act:${l.actionId}', 'act:${l.actionId}']);
    expect(await s.trips.pendingCount(), 0);
    expect((await s.trips.trip(tripA))!.trip.status, TripStatus.departed);
  });

  test('W-02 409 logistics.trip_transition (điều phối đã ghi mốc) → xung đột hiển thị, không thử lại, trạng thái theo máy chủ', () async {
    final env = _driverEnv();
    final s = await env.open();
    addTearDown(s.close);
    await s.sync.syncOnce(force: true);
    env.server.offline = true;
    await s.trips.record(tripA, Milestone.departed);
    // Trong lúc mất sóng, điều phối ghi xuất phát trên web.
    env.server.logistics.trips[tripA]!['status'] = 'departed';
    env.server.logistics.trips[tripA]!['departed_at'] = '2026-10-01T02:55:00Z';
    env.server.offline = false;
    await s.sync.syncOnce(force: true);
    final c = await s.trips.conflicts();
    expect(c.single.errorCode, 'logistics.trip_transition');
    expect(logisticsMessage(c.single.errorCode), contains('ghi ở nơi khác'));
    final v = (await s.trips.trip(tripA))!;
    expect(v.status, TripStatus.departed);
    expect(v.conflicts, hasLength(1));
    expect(v.next, Milestone.arrived);
    expect(
      v.timeOf(Milestone.departed)!.toUtc(),
      DateTime.utc(2026, 10, 1, 2, 55),
    );
    // Không gửi lại lệnh đã bị từ chối.
    final calls = env.server.actionCalls.length;
    await s.sync.syncOnce(force: true);
    expect(env.server.actionCalls.length, calls);
    // Đi tiếp bình thường.
    await s.trips.record(tripA, Milestone.arrived);
    await s.sync.syncOnce(force: true);
    expect(env.server.logistics.trips[tripA]!['status'], 'arrived');
    expect(
      (await s.actions.ofKind(ApiActionKind.logisticsMilestone))
          .where((a) => a.status == ApiActionStatus.failed),
      hasLength(1),
    );
  });

  test('W-02 409 logistics.trip_closed (chuyến bị hủy khi đang ngoại tuyến) → xung đột, chuyến rời danh sách', () async {
    final env = _driverEnv();
    final s = await env.open();
    addTearDown(s.close);
    await s.sync.syncOnce(force: true);
    env.server.offline = true;
    await s.trips.record(tripA, Milestone.departed);
    env.server.logistics.trips[tripA]!['status'] = 'cancelled';
    env.server.offline = false;
    await s.sync.syncOnce(force: true);
    expect(
      (await s.trips.conflicts()).single.errorCode,
      'logistics.trip_closed',
    );
    expect(await s.trips.trip(tripA), isNull);
  });

  test(
    'W-02 không có GPS → không xếp hàng, báo rõ (hợp đồng bắt buộc lat/lng)',
    () async {
      final env = _driverEnv();
      final s = await env.open();
      addTearDown(s.close);
      await s.sync.syncOnce(force: true);
      env.location.result = const LocationResult(GpsStatus.denied);
      await expectLater(
        s.trips.record(tripA, Milestone.departed),
        throwsA(
          isA<TripError>().having(
            (e) => e.message,
            'm',
            contains('quyền vị trí'),
          ),
        ),
      );
      env.location.hang = true; // quá gpsTimeout
      await expectLater(
        s.trips.record(tripA, Milestone.departed),
        throwsA(
          isA<TripError>().having(
            (e) => e.message,
            'm',
            contains('Chưa lấy được vị trí'),
          ),
        ),
      );
      expect(await s.actions.pendingCount(), 0);
      // Chuyến của tài xế khác không có trên máy.
      await expectLater(
        s.trips.record(tripB, Milestone.departed),
        throwsA(isA<TripError>()),
      );
    },
  );
}

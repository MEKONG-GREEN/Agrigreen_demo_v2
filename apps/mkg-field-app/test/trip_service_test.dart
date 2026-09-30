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
/// (chỉ vai driver, máy chủ lọc assigned_to_me, delta updated_since), xếp mốc vào hàng lệnh API với Idempotency-Key
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

  test('W-02 lượt đầu đọc đầy đủ "chuyến của tôi" do máy chủ lọc (assigned_to_me=true), theo con trỏ (trang 1 chuyến), bỏ completed; chi tiết kèm chặng; không lưu cước; lưu con trỏ delta = recorded_at lớn nhất', () async {
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
    // Ba trạng thái hoạt động; planned có 1 chuyến của tôi → trang + trang rỗng.
    expect(
      {for (final x in q) x?['status']},
      {'planned', 'departed', 'arrived'},
    );
    expect(q.every((x) => x?['assigned_to_me'] == 'true'), isTrue);
    expect(q.any((x) => x?['updated_since'] != null), isFalse);
    expect(q.where((x) => x?['cursor'] != null), isNotEmpty);
    expect(q.any((x) => x?['status'] == 'completed'), isFalse);
    final trips = await s.trips.trips();
    expect([for (final v in trips) v.trip.code], ['CH000004', 'CH000001']);
    expect(trips.last.trip.legs.single.to.ref, facilityRef);
    expect(trips.last.trip.destination?.kind, 'facility');
    // Chuyến của tài xế khác không được tải chi tiết (máy chủ đã lọc).
    expect(env.server.logistics.detailGets, isNot(contains(tripB)));
    final raw = await s.db.getMeta(TripService.tripsKey);
    expect(raw, isNot(contains('planned_cost')));
    expect(raw, isNot(contains('11250000')));
    expect(raw, isNot(contains('cargo')));
    // Con trỏ delta = recorded_at lớn nhất của các chuyến đã thấy.
    expect(await s.trips.deltaCursor(), DateTime.utc(2026, 10, 1, 1));

    // Lượt sau: delta updated_since (lùi deltaOverlap), mọi trạng thái;
    // plan_revision không đổi → không tải lại chi tiết.
    final before = env.server.logistics.detailGets.length;
    q.clear();
    await s.sync.syncOnce(force: true);
    expect(env.server.logistics.detailGets.length, before);
    expect(q, isNotEmpty);
    for (final x in q) {
      expect(x?['assigned_to_me'], 'true');
      expect(x?['updated_since'], '2026-10-01T00:58:00.000Z');
      expect(x?.containsKey('status'), isFalse);
    }
    // Con trỏ trang hỏng giữa chừng (422) ở delta → đọc đầy đủ lại, không
    // mất chuyến.
    env.server.logistics.corruptCursors = 1;
    q.clear();
    await s.sync.syncOnce(force: true);
    expect(q.where((x) => x?['cursor'] == 'hỏng'), hasLength(1));
    expect(q.where((x) => x?['status'] != null), isNotEmpty);
    expect(
      [for (final v in await s.trips.trips()) v.trip.code],
      ['CH000004', 'CH000001'],
    );
  });

  test('W-02 delta updated_since: chuyến mới giao, đổi kế hoạch, bị hủy / hoàn tất → cập nhật máy; phân trang next_cursor giữ updated_since; con trỏ lưu = recorded_at lớn nhất', () async {
    final env = _driverEnv();
    final lg = env.server.logistics;
    final s = await env.open();
    addTearDown(s.close);
    await s.sync.syncOnce(force: true);
    expect([for (final v in await s.trips.trips()) v.trip.code], ['CH000001']);
    final c0 = (await s.trips.deltaCursor())!;

    // Trong khi đó trên web: giao chuyến mới cho tôi, đổi kế hoạch chuyến A,
    // chuyến của người khác đổi (không phải của tôi — máy chủ lọc).
    const tripD = '01920000-0000-7000-8000-00000000d0a4';
    lg.add(
      tripData(
        id: tripD,
        code: 'CH000004',
        driver: meSub,
        plannedAt: '2026-10-01T02:30:00Z',
      ),
    );
    lg.touch(tripD);
    lg.update(tripA, {'plan_revision': 2});
    lg.update(tripB, {'status': 'departed'});
    lg.pageCap = 1;
    lg.listQueries.clear();
    lg.detailGets.clear();
    await s.sync.syncOnce(force: true);
    final q = lg.listQueries;
    // 2 chuyến của tôi, trang 1 → 2 trang đầy + 1 trang rỗng, cùng
    // updated_since, con trỏ trang theo next_cursor.
    expect(q, hasLength(3));
    expect({for (final x in q) x?['updated_since']}, hasLength(1));
    expect(q.first?['cursor'], isNull);
    expect(q[1]?['cursor'], 'c:1');
    expect(
      [for (final v in await s.trips.trips()) v.trip.code],
      ['CH000004', 'CH000001'],
    );
    // Chi tiết chỉ tải cho chuyến mới và chuyến đổi kế hoạch.
    expect(lg.detailGets.toSet(), {tripD, tripA});
    final c1 = (await s.trips.deltaCursor())!;
    expect(c1.isAfter(c0), isTrue);
    expect(c1, DateTime.parse('${lg.trips[tripA]!['recorded_at']}'));

    // Hủy D, hoàn tất A → cả hai rời máy.
    lg.update(tripD, {'status': 'cancelled'});
    lg.update(tripA, {'status': 'completed'});
    await s.sync.syncOnce(force: true);
    expect(await s.trips.trips(), isEmpty);
    expect(
      await s.trips.deltaCursor(),
      DateTime.parse('${lg.trips[tripA]!['recorded_at']}'),
    );
    // Không có gì mới → con trỏ giữ nguyên.
    final c2 = await s.trips.deltaCursor();
    await s.sync.syncOnce(force: true);
    expect(await s.trips.deltaCursor(), c2);
  });

  test('W-02 máy chủ từ chối updated_since (422) → đọc đầy đủ lại an toàn; quá fullEvery → đọc đầy đủ (dọn chuyến bị giao sang người khác)', () async {
    final env = _driverEnv();
    final lg = env.server.logistics;
    final s = await env.open();
    addTearDown(s.close);
    await s.sync.syncOnce(force: true);
    lg.rejectUpdatedSince = 1;
    lg.listQueries.clear();
    await s.sync.syncOnce(force: true);
    final q = lg.listQueries;
    expect(q.first?['updated_since'], isNotNull);
    expect(
      [for (final x in q.skip(1)) x?['status']],
      ['planned', 'departed', 'arrived'],
    );
    expect([for (final v in await s.trips.trips()) v.trip.code], ['CH000001']);
    expect(await s.trips.deltaCursor(), DateTime.utc(2026, 10, 1, 1));

    // Điều phối giao chuyến A cho tài xế khác: delta "của tôi" không báo …
    lg.update(tripA, {'driver_ref': otherSub});
    await s.sync.syncOnce(force: true);
    expect([for (final v in await s.trips.trips()) v.trip.code], ['CH000001']);
    // … lượt đầy đủ định kỳ dọn khỏi máy.
    env.clock.advance(const Duration(hours: 7));
    lg.listQueries.clear();
    await s.sync.syncOnce(force: true);
    expect(lg.listQueries.first?['status'], 'planned');
    expect(await s.trips.trips(), isEmpty);
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
    expect(
      ms.first.keys,
      containsAll(['kind', 'event_time', 'lat', 'lng', 'accuracy_m']),
    );
    // Sai số GPS gửi kèm (hợp đồng: chuỗi thập phân, chỉ kèm tọa độ).
    expect(ms.first['accuracy_m'], '35');
    expect(ms.last['accuracy_m'], '35');
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
    env.server.logistics.update(tripA, {
      'status': 'departed',
      'departed_at': '2026-10-01T02:55:00Z',
    });
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
    env.server.logistics.update(tripA, {'status': 'cancelled'});
    env.server.offline = false;
    await s.sync.syncOnce(force: true);
    expect(
      (await s.trips.conflicts()).single.errorCode,
      'logistics.trip_closed',
    );
    expect(await s.trips.trip(tripA), isNull);
  });

  test('W-02 không có GPS → vẫn xếp hàng, lat/lng bỏ trống (không bao giờ 0,0), ghi chú "không có GPS"; máy chủ nhận', () async {
    final env = _driverEnv();
    final s = await env.open();
    addTearDown(s.close);
    await s.sync.syncOnce(force: true);
    env.server.offline = true;
    env.location.result = const LocationResult(GpsStatus.denied);
    final l = await s.trips.record(tripA, Milestone.departed);
    expect(l.state, MilestoneState.pending);
    expect(l.hasGps, isFalse);
    expect(l.lat, isNull);
    expect(l.lng, isNull);
    expect(l.gpsStatus, 'denied');
    expect(l.gpsLabel, 'không có GPS');
    final a = (await s.actions.pending()).single;
    expect(a.body!.keys.toSet(), {'kind', 'event_time'});
    expect((await s.trips.queued()).single.gpsLabel, 'không có GPS');
    expect((await s.trips.trip(tripA))!.status, TripStatus.departed);

    // GPS quá hạn chờ, hoặc máy báo 0,0 → cũng coi như không có GPS.
    env.clock.advance(const Duration(hours: 1));
    env.location.result = const LocationResult(
      GpsStatus.ok,
      GpsFix(lat: 0, lon: 0, accuracyM: 5),
    );
    final l2 = await s.trips.record(tripA, Milestone.arrived);
    expect(l2.hasGps, isFalse);
    expect((await s.actions.pending()).last.body!.keys.toSet(), {
      'kind',
      'event_time',
    });

    env.server.offline = false;
    await s.sync.syncOnce(force: true);
    final ms = env.server.logistics.milestones;
    expect([for (final m in ms) m['kind']], ['departed', 'arrived']);
    for (final m in ms) {
      expect(m.containsKey('lat'), isFalse);
      expect(m.containsKey('lng'), isFalse);
      expect(m.containsKey('accuracy_m'), isFalse);
    }
    expect(await s.trips.pendingCount(), 0);
    expect(
      [for (final m in await s.trips.queued()) m.state],
      [MilestoneState.sent, MilestoneState.sent],
    );
  });

  test('W-02 GPS quá hạn chờ → mốc vẫn ghi, không có GPS', () async {
    final env = _driverEnv();
    final s = await env.open();
    addTearDown(s.close);
    await s.sync.syncOnce(force: true);
    env.location.hang = true; // quá gpsTimeout
    final l = await s.trips.record(tripA, Milestone.departed);
    expect(l.hasGps, isFalse);
    expect(l.gpsStatus, 'unavailable');
    expect(await s.actions.pendingCount(), 1);
    // Chuyến của tài xế khác không có trên máy.
    await expectLater(
      s.trips.record(tripB, Milestone.departed),
      throwsA(isA<TripError>()),
    );
  });
}

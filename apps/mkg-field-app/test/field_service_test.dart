import 'package:agrigreen_mkg_field/application/app_services.dart';
import 'package:agrigreen_mkg_field/application/field_service.dart';
import 'package:agrigreen_mkg_field/domain/fieldops.dart';
import 'package:agrigreen_mobile/data/photo/location_source.dart';
import 'package:agrigreen_mobile/data/sync/sync_service.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/test_env.dart';

void main() {
  late TestEnv env;

  Future<(MkgServices, CollectionOrder)> ready({
    List<String> roles = const ['field_collector'],
  }) async {
    env = TestEnv(roles: roles)..seed();
    final s = await env.open();
    addTearDown(s.close);
    await s.sync.syncOnce(force: true);
    return (s, (await s.field.order(orderId))!);
  }

  test('pull collection_order → lệnh gom của tôi; push đi /v1/fieldops/sync (entity roll_count)', () async {
    final (s, o) = await ready();
    expect((await s.field.orders()).single.orderId, orderId);
    expect(o.plotRefs, [plotA, plotB]);
    expect(o.countable, isTrue);
    expect(env.server.pullCursors.first, isNull);
  });

  test('FM-03/FM-04/FM-09: bale → gather → boat_load ngoại tuyến, gửi đúng một lần khi có mạng', () async {
    final (s, o) = await ready();
    final f = s.field;
    env.server.offline = true;
    final bale = await f.recordCount(
      order: o,
      plotRef: plotA,
      stage: Stage.bale,
      rollsText: '20',
    );
    // FM-04: nhiều hơn công đoạn trước bị chặn trên máy.
    await expectLater(
      f.recordCount(
        order: o,
        plotRef: plotA,
        stage: Stage.gather,
        rollsText: '21',
      ),
      throwsA(
        isA<FieldError>().having((e) => e.errors.single.rule, 'rule', 'FM-04'),
      ),
    );
    // FM-03: boat_load khi gather chưa có số bị chặn.
    await expectLater(
      f.recordCount(
        order: o,
        plotRef: plotA,
        stage: Stage.boatLoad,
        rollsText: '20',
      ),
      throwsA(
        isA<FieldError>().having((e) => e.errors.single.rule, 'rule', 'FM-03'),
      ),
    );
    await f.recordCount(
      order: o,
      plotRef: plotA,
      stage: Stage.gather,
      rollsText: '18',
      discrepancyReason: '2 cuộn bị ướt, bỏ lại ruộng',
    );
    await f.recordCount(
      order: o,
      plotRef: plotA,
      stage: Stage.boatLoad,
      rollsText: '18',
    );
    // Mỗi (thửa, công đoạn) một số: ghi lại bị chặn (dùng bản bù).
    await expectLater(
      f.recordCount(
        order: o,
        plotRef: plotA,
        stage: Stage.bale,
        rollsText: '20',
      ),
      throwsA(isA<FieldError>()),
    );
    expect(bale.gps, GpsStatus.ok);
    expect(bale.record.payload['gps'], isNotNull);

    await s.sync.syncOnce(force: true);
    expect((await s.sync.status()).state, SyncState.offline);
    expect(await s.outbox.counts().then((c) => c.pending), 3);

    // Có mạng: lần đầu phản hồi bị mất → gửi lại cùng lô, máy chủ không trùng.
    env.server.offline = false;
    env.server.dropNextResponses = 1;
    await s.sync.syncOnce(force: true);
    await s.sync.syncOnce(force: true);
    expect(env.server.pushBatchKeys.toSet(), hasLength(1));
    expect(env.server.pushBatchKeys, hasLength(2));
    expect(env.server.rollCounts, hasLength(3));
    expect((await s.outbox.byId(bale.record.id))!.status, OutboxStatus.sent);
    final wire = env.server.rollCounts.first;
    expect(wire['idem_key'], 'dev:${s.deviceId}:1');
    expect(wire['payload'], containsPair('stage', 'bale'));
    expect(wire['payload'], containsPair('order_id', orderId));
    expect(wire['payload'], containsPair('rolls', 20));
    final b = await f.board(o);
    expect(b.totalRolls(Stage.bale), 20);
    expect(b.totalRolls(Stage.boatLoad), 18);
  });

  test('FM-09: rơm rời — 0 cuộn kèm kg, không số thực', () async {
    final (s, o) = await ready();
    final r = await s.field.recordCount(
      order: o,
      plotRef: plotB,
      stage: Stage.bale,
      rollsText: '0',
      looseKgText: '350,25',
    );
    expect(r.record.payload['loose_kg'], '350.25');
    expect(r.record.payload['rolls'], 0);
  });

  test('GPS tùy chọn: không có / treo GPS vẫn lưu (không chặn)', () async {
    final (s, o) = await ready();
    final f = s.field;
    env.location.result = const LocationResult(GpsStatus.denied);
    final a = await f.recordCount(
      order: o,
      plotRef: plotA,
      stage: Stage.bale,
      rollsText: '10',
    );
    expect(a.gps, GpsStatus.denied);
    expect(a.record.payload.containsKey('gps'), isFalse);
    env.location.hang = true;
    final b = await f.recordCount(
      order: o,
      plotRef: plotB,
      stage: Stage.bale,
      rollsText: '12',
    );
    expect(b.gps, GpsStatus.unavailable);
  });

  test('RB-1: sửa số máy chủ bằng bản ghi bù (supersedes_id = roll_count_id, lý do ≥ 10)', () async {
    final (s, o) = await ready();
    final f = s.field;
    env.server.totals[orderId] = [
      {
        'plot_ref': plotA,
        'stage': 'bale',
        'rolls': 20,
        'roll_count_id': '01920000-0000-7000-8000-00000000f001',
      },
      {
        'plot_ref': plotA,
        'stage': 'gather',
        'rolls': 20,
        'roll_count_id': '01920000-0000-7000-8000-00000000f002',
      },
    ];
    await f.refreshCounts(orderId);
    await expectLater(
      f.correctCount(
        order: o,
        plotRef: plotA,
        stage: Stage.bale,
        rollsText: '22',
        correctionReason: 'sai',
      ),
      throwsA(isA<FieldError>()),
    );
    // FM-04 với công đoạn sau: bale 15 < gather 20 → chặn.
    await expectLater(
      f.correctCount(
        order: o,
        plotRef: plotA,
        stage: Stage.bale,
        rollsText: '15',
        correctionReason: 'Đếm lại thiếu năm cuộn',
      ),
      throwsA(
        isA<FieldError>().having((e) => e.errors.single.rule, 'rule', 'FM-04'),
      ),
    );
    final r = await f.correctCount(
      order: o,
      plotRef: plotA,
      stage: Stage.gather,
      rollsText: '19',
      discrepancyReason: 'Một cuộn rơi xuống kênh',
      correctionReason: 'Đếm lại tại bờ kênh',
    );
    expect(r.record.op, RecordOp.correct);
    expect(r.record.supersedesId, '01920000-0000-7000-8000-00000000f002');
    expect(r.record.correctionReason, 'Đếm lại tại bờ kênh');
    expect((await f.board(o)).count(plotA, Stage.gather)!.rolls, 19);
    await s.sync.syncOnce(force: true);
    final w = env.server.rollCounts.single;
    expect(w['op'], 'correct');
    expect(w['supersedes_id'], '01920000-0000-7000-8000-00000000f002');
  });

  test('máy chủ từ chối (fieldops.*) → hiển thị mã, ghi lại tạo bản ghi mới thay thế', () async {
    final (s, o) = await ready();
    final f = s.field;
    env.server.rejectWith = (w) => 'fieldops.stage_already_counted';
    final a = await f.recordCount(
      order: o,
      plotRef: plotA,
      stage: Stage.bale,
      rollsText: '20',
    );
    await s.sync.syncOnce(force: true);
    final rej = (await s.outbox.byId(a.record.id))!;
    expect(rej.status, OutboxStatus.rejected);
    expect(rej.errorCode, 'fieldops.stage_already_counted');
    final b = await f.board(o);
    expect(b.count(plotA, Stage.bale), isNull);
    expect(b.rejectedOf(plotA, Stage.bale)!.id, a.record.id);

    env.server.rejectWith = null;
    final again = await f.recordCount(
      order: o,
      plotRef: plotA,
      stage: Stage.bale,
      rollsText: '21',
    );
    await s.sync.syncOnce(force: true);
    expect((await s.outbox.byId(again.record.id))!.status, OutboxStatus.sent);
    expect((await s.outbox.byId(a.record.id))!.status, OutboxStatus.superseded);
  });

  test('fieldops.sink_failed: giữ pending, tự gửi lại', () async {
    final (s, o) = await ready();
    final f = s.field;
    var n = 0;
    env.server.rejectWith = (w) => n++ == 0 ? 'fieldops.sink_failed' : null;
    final a = await f.recordCount(
      order: o,
      plotRef: plotA,
      stage: Stage.bale,
      rollsText: '20',
    );
    await s.sync.syncOnce(force: true);
    expect((await s.outbox.byId(a.record.id))!.status, OutboxStatus.pending);
  });

  test(
    'lệnh chưa duyệt / đã đóng: không ghi số (fieldops.order_not_open)',
    () async {
      env = TestEnv()..seed(orders: [orderData(status: 'planned')]);
      final s = await env.open();
      addTearDown(s.close);
      const o = CollectionOrder(
        orderId: orderId,
        status: 'closed',
        revision: 1,
        htxRef: htxRef,
        communeCode: '31117',
        windowFrom: '2026-10-01',
        windowTo: '2026-10-02',
        plotRefs: [plotA],
      );
      await expectLater(
        s.field.recordCount(
          order: o,
          plotRef: plotA,
          stage: Stage.bale,
          rollsText: '1',
        ),
        throwsA(isA<FieldError>()),
      );
    },
  );

  test(
    'ca tổ gom: mở (số người 1..200) / đóng — xếp hàng tới crew-shifts',
    () async {
      final (s, o) = await ready();
      final f = s.field;
      await expectLater(f.openShift(o, '0'), throwsA(isA<FieldError>()));
      await expectLater(f.closeShift(o), throwsA(isA<FieldError>()));
      env.server.offline = true;
      await f.openShift(o, '6');
      expect((await f.shifts(orderId)).open, isTrue);
      expect((await f.shifts(orderId)).last!.pending, isTrue);
      await expectLater(f.openShift(o, '6'), throwsA(isA<FieldError>()));
      env.clock.advance(const Duration(hours: 4));
      await f.closeShift(o);
      expect((await f.shifts(orderId)).open, isFalse);
      env.server.offline = false;
      await s.sync.syncOnce(force: true);
      final paths = [for (final c in env.server.actionCalls) c.$1];
      expect(paths, [
        '/v1/fieldops/orders/$orderId/crew-shifts',
        '/v1/fieldops/orders/$orderId/crew-shifts',
      ]);
      final body = env.server.posts.first.$2!;
      expect(body['kind'], 'opened');
      expect(body['headcount'], 6);
      expect(body['crew_ref'], crewRef);
      expect(body['device_id'], s.deviceId);
    },
  );

  test('ca: lệnh chưa giao tổ → không mở ca', () async {
    env = TestEnv()..seed(orders: [orderData(status: 'approved', crew: null)]);
    final s = await env.open();
    addTearDown(s.close);
    await s.sync.syncOnce(force: true);
    final o = (await s.field.order(orderId))!;
    await expectLater(s.field.openShift(o, '5'), throwsA(isA<FieldError>()));
  });

  test('R-56 giám sát: không tự duyệt lệnh mình lập; duyệt lệnh người khác (Idempotency-Key cố định)', () async {
    final (s, _) = await ready(roles: const ['field_supervisor']);
    final f = s.field;
    env.server.planned = [
      orderData(id: orderId, status: 'planned', createdBy: meSub),
      orderData(id: order2Id, status: 'planned', createdBy: otherSub),
    ];
    final list = (await f.plannedOrders()).value;
    expect(await f.isOwn(list[0]), isTrue);
    expect(await f.isOwn(list[1]), isFalse);
    await expectLater(
      f.decide(list[0], approve: true),
      throwsA(
        isA<FieldError>().having((e) => e.message, 'm', contains('R-56')),
      ),
    );
    expect(env.server.actionCalls, isEmpty);
    await f.decide(list[1], approve: true);
    await f.decide(list[1], approve: true);
    expect(env.server.actionCalls.map((c) => c.$2).toSet(), {
      'decide:$order2Id:1:approved',
    });
    expect(env.server.posts.single.$2, {'decision': 'approved'});
    await expectLater(
      f.decide(list[1], approve: false, reason: 'ngắn'),
      throwsA(isA<FieldError>()),
    );
    // Máy chủ chặn lần hai: 403 self_approval_forbidden → lời dễ hiểu.
    env.server.actionErrors['/v1/fieldops/orders/$order2Id/decision'] = (
      403,
      'self_approval_forbidden',
    );
    await expectLater(
      f.decide(list[1], approve: false, reason: 'Thửa chưa gặt xong'),
      throwsA(
        isA<FieldError>().having((e) => e.message, 'm', contains('R-56')),
      ),
    );
  });

  test(
    'RB-6 đối soát chiều mua: trực tuyến, xem bản lưu khi mất mạng',
    () async {
      final (s, _) = await ready(roles: const ['field_supervisor']);
      final f = s.field;
      env.server.recon = [
        {
          'plot_ref': plotA,
          'order_id': orderId,
          'mkg_rolls': 18,
          'htx_rolls': 18,
          'status': 'match',
        },
        {
          'plot_ref': plotB,
          'order_id': orderId,
          'mkg_rolls': 12,
          'htx_rolls': 10,
          'status': 'mismatch',
        },
      ];
      final r = await f.reconciliation();
      expect(r.stale, isFalse);
      expect(r.value.map((x) => x.status), ['match', 'mismatch']);
      env.server.offline = true;
      final cached = await f.reconciliation();
      expect(cached.stale, isTrue);
      expect(cached.value, hasLength(2));
    },
  );
}

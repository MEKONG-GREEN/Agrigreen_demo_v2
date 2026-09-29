import 'dart:math';

import 'package:agrigreen_field/application/boundary_service.dart';
import 'package:agrigreen_mobile/core/ids.dart';
import 'package:agrigreen_mobile/data/outbox/outbox_repository.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:agrigreen_mobile/domain/geo/boundary.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';
import 'package:agrigreen_mobile/testing.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/harness.dart';

/// Hình vuông cạnh [sideM] mét, góc tây nam tại (lat, lon).
List<GeoPoint> square(double lat, double lon, double sideM) {
  const mPerDegLat = 110574.0; // gần xích đạo ~10°N
  final mPerDegLon = 111320.0 * cos(lat * pi / 180);
  final dLat = sideM / mPerDegLat, dLon = sideM / mPerDegLon;
  return [
    GeoPoint(lat, lon),
    GeoPoint(lat, lon + dLon),
    GeoPoint(lat + dLat, lon + dLon),
    GeoPoint(lat + dLat, lon),
  ];
}

void main() {
  group('kiểm tra ranh giới (US-PLOT-01, HTX US-PLOT-02 AC-2)', () {
    test('ít hơn 4 điểm bị chặn', () {
      final v = validateBoundary(const [
        GeoPoint(10, 105),
        GeoPoint(10, 105.001),
        GeoPoint(10.001, 105.001),
      ]);
      expect(v.ok, isFalse);
      expect(v.errors, [BoundaryError.tooFewPoints]);
      expect(
        v.errors.first.message,
        'Cần tối thiểu 4 điểm ranh giới không thẳng hàng để khép vùng.',
      );
    });

    test('điểm khép (cuối = đầu) và điểm trùng không được tính', () {
      final s = square(10.03, 105.78, 100);
      final v = validateBoundary([...s.take(3), s[2], s.first]);
      expect(v.errors, [BoundaryError.tooFewPoints]);
      expect(validateBoundary([...s, s.first]).ok, isTrue);
      expect(validateBoundary([...s, s.first]).ring, hasLength(4));
    });

    test('4 điểm thẳng hàng bị chặn', () {
      final v = validateBoundary(const [
        GeoPoint(10.0, 105.0),
        GeoPoint(10.001, 105.001),
        GeoPoint(10.002, 105.002),
        GeoPoint(10.003, 105.003),
      ]);
      expect(v.errors, [BoundaryError.collinear]);
    });

    test('đa giác tự cắt (hình nơ) bị chặn', () {
      final s = square(10.03, 105.78, 100);
      final bowtie = [s[0], s[2], s[1], s[3]];
      final v = validateBoundary(bowtie);
      expect(v.errors, [BoundaryError.selfIntersecting]);
      expect(isSelfIntersecting(s), isFalse);
    });

    test('ngũ giác lõm hợp lệ không bị coi là tự cắt', () {
      const concave = [
        GeoPoint(10.000, 105.000),
        GeoPoint(10.000, 105.002),
        GeoPoint(10.001, 105.001), // lõm vào
        GeoPoint(10.002, 105.002),
        GeoPoint(10.002, 105.000),
      ];
      expect(validateBoundary(concave).ok, isTrue);
    });

    test('tọa độ ngoài phạm vi', () {
      final v = validateBoundary(const [
        GeoPoint(91, 105),
        GeoPoint(10, 105.1),
        GeoPoint(10.1, 105.1),
        GeoPoint(10.1, 105),
      ]);
      expect(v.errors, contains(BoundaryError.outOfRange));
    });
  });

  group('diện tích ha theo xấp xỉ trắc địa', () {
    test('hình vuông 100 m × 100 m ≈ 1 ha (sai lệch < 1 %)', () {
      final v = validateBoundary(square(10.03, 105.78, 100));
      expect(v.ok, isTrue);
      expect(v.areaHa, closeTo(1.0, 0.01));
      expect(formatHa(v.areaHa), matches(RegExp(r'^\d+\.\d{4}$')));
    });

    test('hình chữ nhật 250 m × 400 m ≈ 10 ha; không phụ thuộc chiều vẽ', () {
      const mPerDegLat = 110574.0;
      final mPerDegLon = 111320.0 * cos(9.5 * pi / 180);
      final dLat = 400 / mPerDegLat, dLon = 250 / mPerDegLon;
      final ring = [
        const GeoPoint(9.5, 106.0),
        GeoPoint(9.5, 106.0 + dLon),
        GeoPoint(9.5 + dLat, 106.0 + dLon),
        GeoPoint(9.5 + dLat, 106.0),
      ];
      final a = validateBoundary(ring).areaHa;
      final b = validateBoundary(ring.reversed.toList()).areaHa;
      expect(a, closeTo(10.0, 0.1));
      expect(a, closeTo(b, 1e-9));
    });

    test(
      '1° × 1° tại xích đạo khớp diện tích cầu tham chiếu (~12 364 km²)',
      () {
        // Tham chiếu: R² · Δλ · (sin φ2 − sin φ1), R = 6 378 137 m.
        const r = 6378137.0;
        final expected = r * r * (pi / 180) * sin(pi / 180) / 1e4; // ha
        final got =
            sphericalAreaM2(const [
              GeoPoint(0, 0),
              GeoPoint(0, 1),
              GeoPoint(1, 1),
              GeoPoint(1, 0),
            ]) /
            1e4;
        expect(got, closeTo(expected, expected * 0.001));
      },
    );
  });

  group('chồng lấn và GeoJSON', () {
    test('phát hiện chồng lấn cắt cạnh và lồng nhau; tách rời thì không', () {
      final a = square(10.03, 105.78, 100);
      final b = square(10.0304, 105.7804, 100); // lệch ~45 m → cắt nhau
      final inner = square(10.0302, 105.7802, 20); // nằm trong a
      final far = square(10.05, 105.80, 100);
      expect(polygonsOverlap(a, b), isTrue);
      expect(polygonsOverlap(a, inner), isTrue);
      expect(polygonsOverlap(inner, a), isTrue);
      expect(polygonsOverlap(a, far), isFalse);
    });

    test('GeoJSON Polygon khép kín, thứ tự [lon, lat], đọc lại được', () {
      final s = square(10.03, 105.78, 100);
      final g = toGeoJsonPolygon(s);
      final ring = (g['coordinates']! as List).first as List;
      expect(g['type'], 'Polygon');
      expect(ring, hasLength(5));
      expect(ring.first, ring.last);
      expect(ring.first, [s.first.lon, s.first.lat]);
      expect(ringFromGeoJson(g), s);
    });
  });

  group('BoundaryService — cảnh báo chồng lấn khi lưu (US-PLOT-03/04)', () {
    late Harness h;
    setUp(() async {
      h = await Harness.create();
      await h.refs.applyPage(
        PullPage(
          changes: [
            PullChange(
              entity: 'plot',
              id: 'own',
              version: 1,
              op: 'upsert',
              data: {
                'label': 'Thửa HTX A',
                'cooperative_id': 'coopA',
                'boundary': toGeoJsonPolygon(square(10.03, 105.78, 100)),
              },
            ),
            PullChange(
              entity: 'plot',
              id: 'other',
              version: 1,
              op: 'upsert',
              data: {
                'label': 'Thửa HTX B',
                'cooperative_id': 'coopB',
                'boundary': toGeoJsonPolygon(square(10.04, 105.79, 100)),
              },
            ),
          ],
          nextCursor: 'c',
          hasMore: false,
        ),
      );
    });
    tearDown(() => h.dispose());

    BoundaryService svc() =>
        BoundaryService(outbox: h.outbox, refs: h.refs, ids: h.ids);

    test(
      'chồng lấn thửa cùng HTX → cần xác nhận; xác nhận xong mới lưu',
      () async {
        final pts = square(10.0304, 105.7804, 100);
        final c = await svc().check(pts, cooperativeId: 'coopA');
        expect(c.needsAck, isTrue);
        expect(c.overlaps.single.plotId, 'own');
        await expectLater(
          svc().save(
            points: pts,
            method: BoundaryMethod.gps,
            cooperativeId: 'coopA',
          ),
          throwsA(isA<BoundaryException>()),
        );
        await expectLater(
          svc().save(
            points: pts,
            method: BoundaryMethod.gps,
            cooperativeId: 'coopA',
            acknowledgeOverlap: true,
          ),
          throwsA(
            isA<BoundaryException>().having(
              (e) => e.message,
              'message',
              contains('xã'),
            ),
          ),
        );
        final rec = await svc().save(
          points: pts,
          method: BoundaryMethod.gps,
          cooperativeId: 'coopA',
          communeCode: '31117',
          acknowledgeOverlap: true,
          worstAccuracyM: 3.46,
        );
        expect(rec.entity, PushEntity.plotBoundary);
        expect(rec.op, RecordOp.create);
        // Payload theo BoundaryPayload của pf/geo (sync-contract).
        expect(isUuidV7(rec.payload['plot_ref']! as String), isTrue);
        expect(rec.payload['commune_code'], '31117');
        expect(rec.payload['cooperative_ref'], 'coopA');
        expect(rec.payload['confirm_overlap'], isTrue);
        expect(rec.payload['source'], 'kn_field_app');
        expect(rec.payload['gps_accuracy_m'], '3.5');
        expect(rec.payload.containsKey('declared_area_ha'), isFalse);
        expect((rec.payload['geometry']! as Map)['type'], 'Polygon');
        expect(rec.payload['overlap_plot_ids'], ['own']);
        expect(rec.payload['area_ha_client'], matches(RegExp(r'^\d+\.\d{4}$')));
      },
    );

    test('chồng lấn thửa của HTX khác → chặn lưu kể cả khi xác nhận', () async {
      final pts = square(10.0404, 105.7904, 100);
      final c = await svc().check(pts, cooperativeId: 'coopA');
      expect(c.blocked, isTrue);
      await expectLater(
        svc().save(
          points: pts,
          method: BoundaryMethod.map,
          cooperativeId: 'coopA',
          acknowledgeOverlap: true,
        ),
        throwsA(isA<BoundaryException>()),
      );
      expect(await h.outbox.list(), isEmpty);
    });

    test('vẽ lại ranh giới chính thửa đó không tự báo chồng lấn; lưu được khi ngoại tuyến', () async {
      final pts = square(10.03, 105.78, 101);
      final rec = await svc().save(
        points: pts,
        method: BoundaryMethod.map,
        plotId: 'own',
        cooperativeId: 'coopA',
      );
      expect(rec.payload['plot_ref'], 'own');
      expect(rec.payload['confirm_overlap'], isFalse);
      expect(rec.status, OutboxStatus.pending);
      expect(h.fake.pushCalls, 0);
    });

    test('ranh giới không hợp lệ không vào hàng đợi', () async {
      await expectLater(
        svc().save(
          points: const [GeoPoint(10, 105)],
          method: BoundaryMethod.map,
        ),
        throwsA(isA<BoundaryException>()),
      );
      expect(await h.outbox.list(), isEmpty);
    });
  });

  group('BoundaryService — hợp đồng pf/geo: sửa, diện tích khai báo, xác nhận sau phản hồi', () {
    late Harness h;
    setUp(() async {
      h = await Harness.create();
      await h.refs.applyPage(
        PullPage(
          changes: [
            PullChange(
              entity: 'plot',
              id: 'p1',
              version: 2,
              op: 'upsert',
              data: {
                'name': 'Thửa bờ kênh',
                'commune_code': '31117',
                'cooperative_ref': 'coopA',
                'boundary_id': 'b-cur',
                'geometry': toGeoJsonPolygon(square(10.05, 105.60, 100)),
              },
            ),
          ],
          nextCursor: 'c',
          hasMore: false,
        ),
      );
    });
    tearDown(() => h.dispose());

    BoundaryService svc() =>
        BoundaryService(outbox: h.outbox, refs: h.refs, ids: h.ids);

    test('sửa thửa đã có boundary_id → op correct supersedes_id = boundary_id, bắt buộc lý do ≥ 10 ký tự', () async {
      final pts = square(10.05, 105.60, 110);
      await expectLater(
        svc().save(
          points: pts,
          method: BoundaryMethod.map,
          plotId: 'p1',
          cooperativeId: 'coopA',
          correctionReason: 'ngắn',
        ),
        throwsA(isA<BoundaryException>()),
      );
      final rec = await svc().save(
        points: pts,
        method: BoundaryMethod.map,
        plotId: 'p1',
        cooperativeId: 'coopA',
        correctionReason: 'Đo lại bờ phía kênh sau mùa lũ',
      );
      expect(rec.op, RecordOp.correct);
      expect(rec.supersedesId, 'b-cur');
      expect(rec.correctionReason, 'Đo lại bờ phía kênh sau mùa lũ');
      expect(rec.payload['commune_code'], '31117');
      expect(rec.toWire()['supersedes_id'], 'b-cur');
    });

    test('declared_area_ha dạng chuỗi; lệch > 5 % phải xác nhận', () async {
      // Hình vuông cạnh 100 m ≈ 1 ha.
      final pts = square(10.07, 105.62, 100);
      await expectLater(
        svc().save(
          points: pts,
          method: BoundaryMethod.map,
          communeCode: '31117',
          declaredAreaHa: 1.5,
        ),
        throwsA(isA<BoundaryException>()),
      );
      final ok = await svc().save(
        points: pts,
        method: BoundaryMethod.map,
        communeCode: '31117',
        declaredAreaHa: 1.0,
      );
      expect(ok.payload['declared_area_ha'], '1.0000');
      expect(ok.payload['confirm_overlap'], isFalse);
      final confirmed = await svc().save(
        points: pts,
        method: BoundaryMethod.map,
        communeCode: '31117',
        declaredAreaHa: 1.5,
        acknowledgeOverlap: true,
      );
      expect(confirmed.payload['confirm_overlap'], isTrue);
      expect(parseDeclaredHa('1,25'), 1.25);
      expect(parseDeclaredHa(' '), isNull);
      expect(() => parseDeclaredHa('abc'), throwsA(isA<BoundaryException>()));
    });

    test('rejected geo.overlap_needs_confirm → cần xác nhận → bản ghi MỚI confirm_overlap; bản gốc superseded khi được nhận', () async {
      final rec = await svc().save(
        points: square(10.08, 105.63, 100),
        method: BoundaryMethod.map,
        communeCode: '31117',
      );
      h.fake.rejectIf = (w) => (w['payload'] as Map)['confirm_overlap'] != true;
      await h.sync.syncOnce();
      // FakeServer trả validation_failed — mô phỏng mã geo bằng cách đổi kết quả.
      var st = await svc().statusOf(rec.payload['plot_ref']! as String);
      expect(st.state, PlotSyncState.rejected);
      expect(st.canConfirm, isFalse);

      // Máy chủ thật trả geo.overlap_needs_confirm.
      await h.db.db.update(
        'outbox',
        {'error_code': 'geo.overlap_needs_confirm'},
        where: 'id = ?',
        whereArgs: [rec.id],
      );
      st = await svc().statusOf(rec.payload['plot_ref']! as String);
      expect(st.state, PlotSyncState.needsConfirm);
      expect(await svc().needingAttention(), hasLength(1));

      final again = await svc().confirmAfterServer(st.record!);
      expect(again.op, RecordOp.create);
      expect(again.id, isNot(rec.id));
      expect(again.payload['plot_ref'], rec.payload['plot_ref']);
      expect(again.payload['confirm_overlap'], isTrue);
      expect(await svc().needingAttention(), isEmpty);
      // Gọi lại không tạo bản thứ hai.
      expect((await svc().confirmAfterServer(st.record!)).id, again.id);

      await h.sync.syncOnce(force: true);
      expect((await h.outbox.byId(rec.id))!.status, OutboxStatus.superseded);
      expect((await h.outbox.byId(again.id))!.status, OutboxStatus.sent);
      final latest = await svc().statusOf(rec.payload['plot_ref']! as String);
      expect(latest.state, PlotSyncState.accepted);
    });

    test('conflict chưa xác nhận → bản ghi bù correct trỏ tới bản ghi xung đột, confirm_overlap = true', () async {
      final rec = await svc().save(
        points: square(10.09, 105.64, 100),
        method: BoundaryMethod.map,
        communeCode: '31117',
      );
      h.fake.conflictIf = (w) =>
          (w['payload'] as Map)['confirm_overlap'] != true;
      await h.sync.syncOnce();
      final st = await svc().statusOf(rec.payload['plot_ref']! as String);
      expect(st.state, PlotSyncState.needsConfirm);
      expect(st.detail, contains('c-${rec.id}'));
      final fix = await svc().confirmAfterServer(st.record!);
      expect(fix.op, RecordOp.correct);
      expect(fix.supersedesId, rec.id);
      expect(fix.correctionReason!.length, greaterThanOrEqualTo(10));
      expect(fix.payload['confirm_overlap'], isTrue);
      expect(await svc().needingAttention(), isEmpty);
      await h.sync.syncOnce(force: true);
      expect((await h.outbox.byId(fix.id))!.status, OutboxStatus.sent);
    });

    test('rejected sync.sink_failed → giữ pending, tự gửi lại (không bắt tạo bản bù)', () async {
      await h.outbox.enqueue(
        entity: PushEntity.fieldReport,
        payload: {'note': 'x'},
      );
      final fake = _SinkFailingOnce(h.fake);
      h.server = fake;
      await h.reopen();
      await h.sync.syncOnce();
      final r = (await h.outbox.list()).single;
      expect(r.status, OutboxStatus.pending);
      expect(r.errorCode, 'sync.sink_failed');
      await h.sync.syncOnce(force: true);
      expect((await h.outbox.list()).single.status, OutboxStatus.sent);
    });
  });
}

/// Máy chủ giả trả `rejected sync.sink_failed` cho lô đầu tiên.
class _SinkFailingOnce extends FakeServer {
  _SinkFailingOnce(FakeServer base) {
    ledger.addAll(base.ledger);
  }
  bool _failed = false;

  @override
  Future<List<PushResult>> push({
    required String batchKey,
    required String deviceId,
    required List<OutboxRecord> records,
  }) async {
    if (!_failed) {
      _failed = true;
      return [
        for (final r in records)
          PushResult(id: r.id, status: 'rejected', code: 'sync.sink_failed'),
      ];
    }
    return super.push(batchKey: batchKey, deviceId: deviceId, records: records);
  }
}

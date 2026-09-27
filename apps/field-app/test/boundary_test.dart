import 'dart:math';

import 'package:agrigreen_field/application/boundary_service.dart';
import 'package:agrigreen_field/data/sync/sync_api.dart';
import 'package:agrigreen_field/domain/geo/boundary.dart';
import 'package:agrigreen_field/domain/outbox_record.dart';
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
        final rec = await svc().save(
          points: pts,
          method: BoundaryMethod.gps,
          cooperativeId: 'coopA',
          acknowledgeOverlap: true,
        );
        expect(rec.entity, PushEntity.plotBoundary);
        expect(rec.payload['overlap_plot_ids'], ['own']);
        expect(rec.payload['overlap_acknowledged'], isTrue);
        expect(rec.payload['is_new_plot'], isTrue);
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
      expect(rec.payload['plot_id'], 'own');
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
}

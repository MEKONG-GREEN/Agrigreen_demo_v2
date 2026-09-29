import 'dart:math';

import 'package:agrigreen_mobile/application/boundary_service.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:agrigreen_mobile/domain/geo/boundary.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/harness.dart';

const coopA = '01920000-0000-7000-8000-0000000000c1';
const coopB = '01920000-0000-7000-8000-0000000000c2';
const ownPlot = '01920000-0000-7000-8000-0000000000b1';
const foreignPlot = '01920000-0000-7000-8000-0000000000b2';
const draftPlot = '01920000-0000-7000-8000-0000000000b3';

List<GeoPoint> square(double lat, double lon, double m) {
  final dLat = m / 111320;
  final dLon = m / (111320 * cos(lat * pi / 180));
  return [
    GeoPoint(lat, lon),
    GeoPoint(lat, lon + dLon),
    GeoPoint(lat + dLat, lon + dLon),
    GeoPoint(lat + dLat, lon),
  ];
}

PullChange _plot(String ref, String? coop, List<GeoPoint> ring) =>
    PullChange.fromJson({
      'entity': 'plot',
      'id': ref,
      'version': 1,
      'op': 'upsert',
      'data': {
        'ref': ref,
        'code': ref.substring(34),
        'commune_code': '31117',
        'cooperative_ref': coop,
        'source': 'kn_field_app',
        'geometry': toGeoJsonPolygon(ring),
      },
    });

void main() {
  late Harness h;
  late BoundaryService svc;

  setUp(() async {
    h = await Harness.create();
    await h.refs.applyPage(
      PullPage(
        changes: [
          _plot(ownPlot, coopA, square(10.04, 105.77, 100)),
          _plot(foreignPlot, coopB, square(10.05, 105.77, 100)),
          _plot(draftPlot, null, square(10.06, 105.77, 100)),
        ],
        nextCursor: 'c1',
        hasMore: false,
      ),
    );
    svc = _htx(h);
  });
  tearDown(() => h.dispose());

  test('HTX US-PLOT-05 AC-1: điểm đang vẽ dở lưu trong CSDL, còn sau khi app '
      'bị tắt đột ngột; lưu xong thì xóa nháp', () async {
    final pts = square(10.07, 105.77, 80).take(3).toList();
    await svc.saveDraft(
      null,
      BoundaryDraft(
        pts,
        accuracies: const [4, 12, null],
        method: BoundaryMethod.gps,
      ),
    );
    await h.reopen();
    svc = _htx(h);
    final d = (await svc.loadDraft(null))!;
    expect(d.points, pts);
    expect(d.worstAccuracyM, 12);
    expect(d.method, BoundaryMethod.gps);
    expect(await svc.loadDraft(ownPlot), isNull);

    await svc.save(
      points: [...d.points, square(10.07, 105.77, 80)[3]],
      method: BoundaryMethod.gps,
      communeCode: '31117',
      worstAccuracyM: d.worstAccuracyM,
    );
    expect(await svc.loadDraft(null), isNull);
  });

  test(
    'HTX US-PLOT-01/02: quản lý HTX — lô mới gán HTX mình, nguồn htx_app; '
    'lô nháp sửa được; lô HTX khác bị chặn; ngưỡng lệch theo tham số',
    () async {
      expect(await svc.deviationLimit(), 3);
      final r = await svc.save(
        points: square(10.08, 105.77, 100),
        method: BoundaryMethod.gps,
        communeCode: '31117',
      );
      expect(r.entity, PushEntity.plotBoundary);
      expect(r.payload['source'], BoundarySource.htxApp);
      expect(r.payload['cooperative_ref'], coopA);

      expect(await svc.canEdit((await h.refs.get('plot', ownPlot))!), isTrue);
      expect(await svc.canEdit((await h.refs.get('plot', draftPlot))!), isTrue);
      expect(
        await svc.canEdit((await h.refs.get('plot', foreignPlot))!),
        isFalse,
      );
      await expectLater(
        svc.save(
          points: square(10.05, 105.77, 90),
          method: BoundaryMethod.map,
          plotId: foreignPlot,
        ),
        throwsA(
          isA<BoundaryException>().having(
            (e) => e.message,
            'message',
            msgPlotNotOwned,
          ),
        ),
      );

      // Lệch 4 % > ngưỡng 3 % của tham số → phải xác nhận.
      await expectLater(
        svc.save(
          points: square(10.09, 105.77, 100),
          method: BoundaryMethod.map,
          communeCode: '31117',
          declaredAreaHa: 1.04,
        ),
        throwsA(isA<BoundaryException>()),
      );
      final ok = await svc.save(
        points: square(10.09, 105.77, 100),
        method: BoundaryMethod.map,
        communeCode: '31117',
        declaredAreaHa: 1.04,
        acknowledgeOverlap: true,
      );
      expect(ok.payload['confirm_overlap'], isTrue);
      expect(await svc.localNewPlots(), hasLength(2));
    },
  );

  test(
    'app hiện trường: mặc định nguồn kn_field_app, không giới hạn HTX',
    () async {
      final kn = BoundaryService(outbox: h.outbox, refs: h.refs, ids: h.ids);
      expect(
        await kn.canEdit((await h.refs.get('plot', foreignPlot))!),
        isTrue,
      );
      expect(await kn.deviationLimit(), areaDeviationConfirmPct);
      final r = await kn.save(
        points: square(10.1, 105.77, 100),
        method: BoundaryMethod.map,
        communeCode: '31117',
      );
      expect(r.payload['source'], BoundarySource.knFieldApp);
      expect(r.payload.containsKey('cooperative_ref'), isFalse);
    },
  );
}

BoundaryService _htx(Harness h) => BoundaryService(
  outbox: h.outbox,
  refs: h.refs,
  ids: h.ids,
  db: h.db,
  source: BoundarySource.htxApp,
  deviationLimitPct: () async => 3,
  ownCooperative: () async => coopA,
);

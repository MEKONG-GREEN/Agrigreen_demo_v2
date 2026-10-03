import 'dart:math';
import 'dart:typed_data';

import 'package:agrigreen_htx/ui/screens/plot_map_screen.dart';
import 'package:agrigreen_mobile/application/boundary_service.dart';
import 'package:agrigreen_mobile/data/crypto/envelope.dart';
import 'package:agrigreen_mobile/data/photo/location_source.dart';
import 'package:agrigreen_mobile/domain/geo/boundary.dart';
import 'package:agrigreen_mobile/domain/geo/tiles.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';
import 'package:agrigreen_mobile/ui/map/offline_tile_provider.dart';
import 'package:flutter/material.dart';
import 'package:flutter_map/flutter_map.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/test_env.dart';

const knDraftRef = '01920000-0000-7000-8000-0000000000b3';
const otherCoopPlotRef = '01920000-0000-7000-8000-0000000000b4';
const unassignedRef = '01920000-0000-7000-8000-0000000000b5';
const otherCoop = '01920000-0000-7000-8000-0000000000c2';
const knBoundaryId = '01920000-0000-7000-8000-0000000000d3';

/// Góc ruộng vuông cạnh [m] mét, góc tây nam tại ([lat], [lon]).
List<GeoPoint> squareAt(double lat, double lon, double m) {
  final dLat = m / 111320;
  final dLon = m / (111320 * cos(lat * pi / 180));
  return [
    GeoPoint(lat, lon),
    GeoPoint(lat, lon + dLon),
    GeoPoint(lat + dLat, lon + dLon),
    GeoPoint(lat + dLat, lon),
  ];
}

Map<String, Object?> _plot(
  String ref,
  String name, {
  String? coop,
  String source = 'kn_field_app',
  String? boundaryId,
  List<GeoPoint>? ring,
}) => {
  'entity': 'plot',
  'id': ref,
  'op': 'upsert',
  'data': {
    'ref': ref,
    'code': name,
    'name': name,
    'commune_code': '31117',
    'cooperative_ref': coop,
    'status': 'active',
    'source': source,
    'area_ha': '1.0000',
    'boundary_id': boundaryId,
    'geometry': ring == null ? null : toGeoJsonPolygon(ring),
  },
};

const _commune = {
  'entity': 'admin_unit',
  'id': '31117',
  'op': 'upsert',
  'data': {'code': '31117', 'name': 'Xã Tân Phú', 'level': 'commune'},
};

/// Quản lý HTX có vai vẽ thửa (pf/geo `plot_editor`).
TestEnv managerEnv({
  String deviationPct = '4',
  List<Map<String, Object?>>? extra,
}) {
  final env = TestEnv(roles: const ['coop_manager', 'plot_editor'])
    ..seed(extra: extra ?? const [_commune]);
  final base = env.server.onGet!;
  env.server.onGet = (path, query) async {
    if (path == '/v1/masterdata/params/value' &&
        query?['key'] == 'geo.area_deviation_max_pct') {
      return {'key': 'geo.area_deviation_max_pct', 'value': deviationPct};
    }
    return base(path, query);
  };
  return env;
}

Future<void> _tall(WidgetTester tester) async {
  await tester.binding.setSurfaceSize(const Size(900, 1800));
  addTearDown(() => tester.binding.setSurfaceSize(null));
}

Future<void> _gpsPoint(
  WidgetTester tester,
  TestEnv env,
  GeoPoint p, {
  double acc = 5,
  GpsStatus status = GpsStatus.ok,
}) async {
  env.location.result = LocationResult(
    status,
    GpsFix(lat: p.lat, lon: p.lon, accuracyM: acc),
  );
  await tapText(tester, 'Điểm GPS');
}

/// Chạm bản đồ lệch [o] so với tâm. Tâm tính lại mỗi lần vì khung bản đồ co
/// giãn theo bảng dưới (tâm bản đồ luôn là cùng một tọa độ).
Future<void> _tapMap(WidgetTester tester, Offset o) =>
    tester.tapAt(tester.getRect(find.byType(FlutterMap)).center + o);

Future<void> _openEditor(WidgetTester tester) async {
  await openTile(tester, 'Bản đồ lô ruộng');
  await tapText(tester, 'Vẽ lô mới');
}

void main() {
  testWidgets(
    'HTX US-PLOT-01: bản đồ lô của HTX mình, lô KN vẽ trước mang nhãn '
    '"Nháp - KN vẽ trước" (không phải vẽ lại), lô nháp chưa gán HTX; lô HTX '
    'khác không hiện và không sửa được',
    (tester) async {
      await _tall(tester);
      final env = managerEnv(
        extra: [
          _commune,
          _plot(
            knDraftRef,
            'Lô KN vẽ',
            coop: coopRef,
            boundaryId: knBoundaryId,
            ring: squareAt(10.0451, 105.7701, 100),
          ),
          _plot(
            otherCoopPlotRef,
            'Lô HTX khác',
            coop: otherCoop,
            ring: squareAt(10.0461, 105.7721, 80),
          ),
          _plot(
            unassignedRef,
            'Lô chưa gán',
            source: 'kn_web',
            ring: squareAt(10.0471, 105.7741, 60),
          ),
        ],
      );
      final s = await loginToHome(tester, env);
      await openTile(tester, 'Bản đồ lô ruộng');
      expect(find.text('Lô ruộng & Ranh giới GPS'), findsOneWidget);
      expect(find.byType(FlutterMap), findsOneWidget);
      expect(find.text('Lô KN vẽ'), findsOneWidget);
      expect(find.text('Lô chưa gán'), findsOneWidget);
      expect(find.text(msgKnDraft), findsNWidgets(2));
      expect(find.text(msgUnassigned), findsOneWidget);
      // Mỗi lô một HTX (AC-3): lô của HTX khác không hiện cho quản lý HTX này.
      expect(find.text('Lô HTX khác'), findsNothing);
      // Lô seed T1/T2 của HTX (không phải KN vẽ) không mang nhãn nháp.
      expect(find.text('T1'), findsNothing); // có tên → hiện theo tên
      expect(find.text('Ruộng bờ kênh'), findsOneWidget);

      // AC-4: sửa lô HTX khác bị chặn ngay trên máy.
      final other = (await s.refs.get('plot', otherCoopPlotRef))!;
      expect(await s.boundaries.canEdit(other), isFalse);
      await expectLater(
        s.boundaries.save(
          points: squareAt(10.0461, 105.7721, 80),
          method: BoundaryMethod.map,
          plotId: otherCoopPlotRef,
          correctionReason: 'Sửa lại theo đo đạc thực địa',
        ),
        throwsA(
          isA<BoundaryException>().having(
            (e) => e.message,
            'message',
            msgPlotNotOwned,
          ),
        ),
      );

      // AC-2: sửa lô KN vẽ trước → bản ghi bù của CÙNG lô (supersedes phiên
      // bản KN), không tạo lô song song; nguồn htx_app.
      await tapOn(
        tester,
        find.descendant(
          of: find.ancestor(
            of: find.text('Lô KN vẽ'),
            matching: find.byType(Card),
          ),
          matching: find.text('Sửa ranh giới'),
        ),
      );
      expect(find.text('Ranh giới: Lô KN vẽ'), findsOneWidget);
      // Ranh giới KN hiện ngay: 4 điểm, có diện tích.
      expect(find.textContaining('4 điểm · Diện tích ≈'), findsOneWidget);
      await tapText(tester, 'Chọn trên bản đồ');
      await _tapMap(tester, const Offset(-150, 0));
      await settle(tester);
      await tapText(tester, 'Lưu ranh giới');
      await tester.enterText(
        find.byType(TextField).last,
        'Bổ sung góc ruộng theo nông hộ',
      );
      await tapText(tester, 'Lưu');
      await settle(tester);
      final recs = await s.outbox.list(entity: PushEntity.plotBoundary);
      expect(recs, hasLength(1));
      final r = recs.single;
      expect(r.op, RecordOp.correct);
      expect(r.supersedesId, knBoundaryId);
      expect(r.payload['plot_ref'], knDraftRef);
      expect(r.payload['source'], BoundarySource.htxApp);
      expect(r.payload['cooperative_ref'], coopRef);
      expect(r.payload['point_count'], 5);
      await finish(tester);
    },
  );

  testWidgets(
    'HTX US-PLOT-02: đi GPS — < 3 điểm hoặc thẳng hàng bị chặn; GPS yếu → '
    'cảnh báo, đánh dấu lại hoặc chuyển sang chọn trên bản đồ; lệch diện tích '
    '> geo.area_deviation_max_pct phải xác nhận; khép vùng → lô mới của HTX',
    (tester) async {
      await _tall(tester);
      final env = managerEnv(deviationPct: '4');
      final s = await loginToHome(tester, env);
      expect(await s.boundaries.deviationLimit(), 4);
      await _openEditor(tester);
      expect(find.text('Vẽ thửa mới'), findsOneWidget);
      final sq = squareAt(10.0451, 105.7701, 100);

      // AC-2 (A2 03/10/2026: tối thiểu 3 điểm): 2 điểm → chặn, thông báo đúng AC.
      for (final p in sq.take(2)) {
        await _gpsPoint(tester, env, p);
      }
      const msg4 =
          'Cần tối thiểu 3 điểm ranh giới không thẳng hàng để khép vùng.';
      expect(find.text(msg4), findsOneWidget);
      expect(
        tester
            .widget<FilledButton>(
              find.ancestor(
                of: find.text('Lưu ranh giới'),
                matching: find.byWidgetPredicate((w) => w is FilledButton),
              ),
            )
            .onPressed,
        isNull,
      );
      // 4 điểm thẳng hàng → vẫn chặn.
      await tapOn(tester, find.byTooltip('Xóa hết'));
      for (var i = 0; i < 4; i++) {
        await _gpsPoint(tester, env, GeoPoint(10.0451 + i * 0.0003, 105.7701));
      }
      expect(find.text(msg4), findsOneWidget);
      await tapOn(tester, find.byTooltip('Xóa hết'));

      // AC-4: GPS yếu → cảnh báo; "Đánh dấu lại" thay điểm yếu.
      await _gpsPoint(tester, env, sq[0]);
      await _gpsPoint(tester, env, sq[1], acc: 35, status: GpsStatus.weak);
      expect(
        find.textContaining('Độ chính xác GPS thấp (±35 m)'),
        findsOneWidget,
      );
      env.location.result = LocationResult(
        GpsStatus.ok,
        GpsFix(lat: sq[1].lat, lon: sq[1].lon, accuracyM: 4),
      );
      await tapText(tester, 'Đánh dấu lại');
      expect(find.textContaining('Độ chính xác GPS thấp'), findsNothing);
      expect(find.textContaining('2 điểm'), findsOneWidget);
      await _gpsPoint(tester, env, sq[2]);
      await _gpsPoint(tester, env, sq[3]);
      // AC-1: tự nối thành vùng khép kín, tính diện tích ≈ 1 ha.
      expect(find.textContaining('4 điểm · Diện tích ≈'), findsOneWidget);

      // AC-3: khai 1,5 ha → lệch > 4 % → phải xác nhận; chọn "Chỉnh lại".
      await tester.enterText(
        find.widgetWithText(TextField, 'Diện tích khai báo (ha)'),
        '1,5',
      );
      await tapText(tester, 'Lưu ranh giới');
      expect(find.text('Lệch diện tích khai báo'), findsOneWidget);
      expect(find.textContaining('ngưỡng 4 %'), findsOneWidget);
      await tapText(tester, 'Chỉnh lại');
      expect(await s.outbox.list(entity: PushEntity.plotBoundary), isEmpty);

      await tester.enterText(
        find.widgetWithText(TextField, 'Diện tích khai báo (ha)'),
        '1,02',
      );
      await tapText(tester, 'Lưu ranh giới');
      expect(find.text('Lệch diện tích khai báo'), findsNothing);
      final r = (await s.outbox.list(entity: PushEntity.plotBoundary)).single;
      expect(r.op, RecordOp.create);
      expect(r.status, OutboxStatus.pending);
      expect(r.payload['source'], 'htx_app');
      expect(r.payload['method'], 'gps');
      expect(r.payload['cooperative_ref'], coopRef);
      expect(r.payload['commune_code'], '31117');
      expect(r.payload['declared_area_ha'], '1.0200');
      expect(r.payload['gps_accuracy_m'], '5.0');
      expect(r.payload['point_count'], 4);
      expect(
        double.parse(r.payload['area_ha_client']! as String),
        closeTo(1.0, 0.01),
      );
      // Về bản đồ: lô mới hiện ngay, chờ đồng bộ.
      expect(find.text('Lô ruộng & Ranh giới GPS'), findsOneWidget);
      expect(find.text('Chờ đồng bộ'), findsOneWidget);
      await finish(tester);
    },
  );

  testWidgets(
    'HTX US-PLOT-03: chọn góc trên bản đồ (nền theo AG_TILE_URL, vệ tinh chờ '
    'RB-7) — cùng ràng buộc ≥ 3 điểm không thẳng hàng; chạm bản đồ ở chế độ '
    'đi GPS không thêm điểm',
    (tester) async {
      await _tall(tester);
      final env = managerEnv();
      final s = await loginToHome(tester, env);
      await _openEditor(tester);
      // Chế độ đi GPS: chạm bản đồ bị bỏ qua.
      await _tapMap(tester, Offset.zero);
      await settle(tester);
      expect(find.text('0 điểm'), findsOneWidget);

      await tapText(tester, 'Chọn trên bản đồ');
      expect(find.text('Điểm GPS'), findsNothing);
      // 3 điểm thẳng hàng + 1 → vẫn thẳng hàng → chặn.
      for (final dx in [-90.0, -30.0, 30.0, 90.0]) {
        await _tapMap(tester, Offset(dx, 0));
        await settle(tester);
      }
      expect(
        find.text(
          'Cần tối thiểu 3 điểm ranh giới không thẳng hàng để khép vùng.',
        ),
        findsOneWidget,
      );
      await tapOn(tester, find.byTooltip('Xóa hết'));
      for (final o in const [
        Offset(-60, -60),
        Offset(60, -60),
        Offset(60, 60),
        Offset(-60, 60),
      ]) {
        await _tapMap(tester, o);
        await settle(tester);
      }
      expect(find.textContaining('4 điểm · Diện tích ≈'), findsOneWidget);
      await tapText(tester, 'Lưu ranh giới');
      final r = (await s.outbox.list(entity: PushEntity.plotBoundary)).single;
      expect(r.payload['method'], 'map');
      expect(r.payload['gps_accuracy_m'], isNull);
      expect(r.payload['source'], 'htx_app');
      final ring = ringFromGeoJson(r.payload['geometry'])!;
      expect(validateBoundary(ring).ok, isTrue);
      await finish(tester);
    },
  );

  testWidgets(
    'HTX US-PLOT-05: mất sóng giữa lúc đi ranh giới — điểm lưu tạm trên máy '
    '(mở lại màn vẫn còn), lưu vào hàng đợi; có mạng → đồng bộ đúng một bản '
    'ghi kể cả khi mất phản hồi; chồng lấn máy chủ báo → xung đột trong hàng '
    'đợi, xác nhận được',
    (tester) async {
      await _tall(tester);
      final env = managerEnv();
      final s = await loginToHome(tester, env);
      env.server.failNextPushes = 100; // mất sóng
      await _openEditor(tester);
      final sq = squareAt(10.0451, 105.7701, 100);
      for (final p in sq.take(3)) {
        await _gpsPoint(tester, env, p);
      }
      // Rời màn giữa chừng (hết pin, tắt app…): điểm đã lưu tạm trên máy.
      await tester.pageBack();
      await settle(tester);
      await tapText(tester, 'Vẽ lô mới');
      expect(
        find.text('Đã khôi phục 3 điểm đang vẽ dở trên máy.'),
        findsOneWidget,
      );
      expect(find.textContaining('3 điểm'), findsWidgets);
      await _gpsPoint(tester, env, sq[3]);
      await tapText(tester, 'Lưu ranh giới');
      final rec = (await s.outbox.list(entity: PushEntity.plotBoundary)).single;
      expect(await s.boundaries.loadDraft(null), isNull);

      // Vẫn mất sóng: đồng bộ thất bại, bản ghi giữ nguyên chờ gửi.
      await s.sync.syncOnce(force: true);
      expect((await s.outbox.byId(rec.id))!.status, OutboxStatus.pending);
      expect(env.server.ledger, isEmpty);

      // Có mạng nhưng mất phản hồi lần đầu → gửi lại cùng idem_key → một bản.
      env.server.failNextPushes = 0;
      env.server.dropNextResponses = 1;
      await s.sync.syncOnce(force: true);
      await s.sync.syncOnce(force: true);
      expect((await s.outbox.byId(rec.id))!.status, OutboxStatus.sent);
      expect(
        env.server.ledger.values.where((w) => w['entity'] == 'plot_boundary'),
        hasLength(1),
      );
      await s.sync.syncOnce(force: true);
      expect(
        env.server.ledger.values.where((w) => w['entity'] == 'plot_boundary'),
        hasLength(1),
      );

      // Lô thứ hai chồng lấn lô cùng HTX trên máy chủ → pf/geo trả conflict
      // (sync: 409 geo.overlap_needs_confirm ngoại tuyến → giữ cả hai, mở xung
      // đột) → hàng đợi hiện "cần xác nhận", xác nhận → bản bù confirm_overlap.
      env.server.conflictIf = (w) => w['entity'] == 'plot_boundary';
      final second = await s.boundaries.save(
        points: squareAt(10.0461, 105.7721, 50),
        method: BoundaryMethod.map,
        communeCode: '31117',
      );
      await s.sync.syncOnce(force: true);
      expect((await s.outbox.byId(second.id))!.status, OutboxStatus.conflict);
      await tester.pageBack();
      await settle(tester);
      await openTile(tester, 'Hàng đợi đồng bộ');
      expect(find.textContaining('Chồng lấn — cần xác nhận'), findsOneWidget);
      await tapText(tester, 'Xác nhận giữ nguyên');
      await tapText(tester, 'Xác nhận');
      final fix = (await s.outbox.list(entity: PushEntity.plotBoundary))
          .firstWhere((r) => r.supersedesId == second.id);
      expect(fix.payload['confirm_overlap'], isTrue);
      expect(fix.op, RecordOp.correct);
      await finish(tester);
    },
  );

  testWidgets(
    'HTX US-PLOT-05: nền bản đồ đọc gói tile đã tải trong CSDL mã hóa, không '
    'cần mạng',
    (tester) async {
      final env = managerEnv();
      final s = await loginToHome(tester, env);
      const t = TileId(15, 25999, 15000);
      final png = Uint8List.fromList([0x89, 0x50, 0x4E, 0x47, 1, 2, 3, 4]);
      {
        await s.tiles.store.createPack(
          id: 'pack-1',
          name: 'Xã Tân Phú',
          bbox: const GeoBBox(
            minLat: 10.0,
            minLon: 105.7,
            maxLat: 10.1,
            maxLon: 105.8,
          ),
          minZoom: 15,
          maxZoom: 15,
          now: testNow,
        );
        await s.tiles.store.putTile('pack-1', t, png);
      }
      final provider = OfflineFirstTileProvider(
        store: s.tiles.store,
        // Máy chủ tile không tới được: chỉ đọc gói trên máy.
        urlTemplate: 'https://tile.invalid/{z}/{x}/{y}.png',
      );
      final img = provider.getImage(
        const TileCoordinates(25999, 15000, 15),
        TileLayer(urlTemplate: 'https://tile.invalid/{z}/{x}/{y}.png'),
      ) as StoredTileImage;
      final bytes = await img.load(t);
      expect(bytes, png);
      provider.dispose();
      await finish(tester);
    },
  );
}

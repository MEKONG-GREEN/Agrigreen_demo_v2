import 'dart:convert';
import 'dart:typed_data';

import 'package:agrigreen_mobile/application/tile_pack_service.dart';
import 'package:agrigreen_mobile/data/map/tile_store.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:agrigreen_mobile/domain/geo/tiles.dart';
import 'package:agrigreen_mobile/testing.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'support/harness.dart';

final _png = Uint8List.fromList([
  0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, //
  ...List.filled(200, 1),
]);

/// Khung ~1,1 km × 1,1 km quanh Ninh Kiều, Cần Thơ.
const _ninhKieu = GeoBBox(
  minLat: 10.030,
  minLon: 105.780,
  maxLat: 10.040,
  maxLon: 105.790,
);

void main() {
  group('toán tile XYZ', () {
    test('lon/lat → x/y khớp OSM', () {
      // Cần Thơ ở z=12: x=3251, y=1933 (tính theo công thức slippy map).
      expect(lonToTileX(105.7880, 12), 3251);
      expect(latToTileY(10.0341, 12), 1933);
      expect(lonToTileX(-180, 3), 0);
      expect(lonToTileX(180, 3), 7); // kẹp vào biên
      expect(latToTileY(89, 3), 0);
      expect(latToTileY(-89, 3), 7);
    });

    test('đếm tile = số phần tử liệt kê; zoom tăng → số tile tăng ~4 lần', () {
      final n = tileCount(_ninhKieu, 12, 17);
      expect(tilesFor(_ninhKieu, 12, 17).length, n);
      expect(tilesFor(_ninhKieu, 12, 17).toSet().length, n);
      final z16 = tileCount(_ninhKieu, 16, 16);
      final z17 = tileCount(_ninhKieu, 17, 17);
      expect(z17, inInclusiveRange(z16 * 2, z16 * 6));
      expect(_ninhKieu.areaKm2, closeTo(1.21, 0.1));
    });

    test('URL từ mẫu', () {
      expect(
        tileUrl('https://t.test/{z}/{x}/{y}.png', const TileId(3, 1, 2)),
        'https://t.test/3/1/2.png',
      );
    });
  });

  group('gói bản đồ ngoại tuyến (US-MAP-03)', () {
    late Harness h;
    late List<Uri> fetched;
    setUp(() async {
      h = await Harness.create();
      fetched = [];
    });
    tearDown(() => h.dispose());

    TilePackService svc({
      TilePackLimits limits = const TilePackLimits(),
      http.Response Function(Uri u)? respond,
    }) => TilePackService(
      store: TileStore(h.db),
      urlTemplate: 'https://tiles.test/{z}/{x}/{y}.png',
      clock: h.clock,
      ids: h.ids,
      limits: limits,
      client: MockClient((req) async {
        fetched.add(req.url);
        expect(req.headers['User-Agent'], contains('agrigreen'));
        return respond?.call(req.url) ?? http.Response.bytes(_png, 200);
      }),
    );

    test(
      'giới hạn: khung sai, zoom ngoài dải, vùng quá rộng, quá nhiều tile',
      () {
        final s = svc();
        expect(
          () => s.plan(
            const GeoBBox(minLat: 11, minLon: 105, maxLat: 10, maxLon: 106),
            12,
            14,
          ),
          throwsA(isA<TilePackException>()),
        );
        expect(
          () => s.plan(_ninhKieu, 12, 19),
          throwsA(isA<TilePackException>()),
        );
        expect(
          () => s.plan(_ninhKieu, 5, 12),
          throwsA(isA<TilePackException>()),
        );
        expect(
          () => s.plan(
            const GeoBBox(minLat: 9, minLon: 105, maxLat: 11, maxLon: 107),
            10,
            12,
          ),
          throwsA(
            isA<TilePackException>().having(
              (e) => e.message,
              'message',
              contains('quá rộng'),
            ),
          ),
        );
        expect(
          () =>
              svc(limits: const TilePackLimits(maxTilesPerPack: 10))
                  .plan(_ninhKieu, 12, 17),
          throwsA(
            isA<TilePackException>().having(
              (e) => e.message,
              'message',
              contains('vượt giới hạn'),
            ),
          ),
        );
        expect(s.plan(_ninhKieu, 12, 16).tiles, tileCount(_ninhKieu, 12, 16));
      },
    );

    test(
      'tải → lưu trong CSDL mã hóa; đọc lại tile; tải tiếp bỏ qua tile đã có',
      () async {
        final s = svc();
        final plan = s.plan(_ninhKieu, 13, 15);
        final progress = <int>[];
        final pack = await s.download(
          plan,
          name: 'Ninh Kiều',
          onProgress: (p) => progress.add(p.done),
        );
        expect(pack.status, 'ready');
        expect(pack.tileCount, plan.tiles);
        expect(pack.bytes, plan.tiles * _png.length);
        expect(progress.last, plan.tiles);
        expect(fetched, hasLength(plan.tiles));
        final any = tilesFor(_ninhKieu, 13, 15).first;
        expect(await s.store.tile(any), _png);
        expect(await s.store.tile(const TileId(1, 0, 0)), isNull);

        // Gọi lại cùng gói: không tải lại.
        fetched.clear();
        await s.download(plan, name: 'Ninh Kiều', packId: pack.id);
        expect(fetched, isEmpty);

        await s.store.deletePack(pack.id);
        expect(await s.store.tile(any), isNull);
        expect(await s.store.totalBytes(), 0);
      },
    );

    test(
      'tile lỗi/không phải ảnh/quá lớn → bỏ qua, gói "failed" để tải tiếp',
      () async {
        var i = 0;
        final s = svc(
          limits: const TilePackLimits(maxTileBytes: 1000),
          respond: (u) => switch (i++ % 3) {
            0 => http.Response('oops', 500),
            1 => http.Response('<html>', 200),
            _ => http.Response.bytes(_png, 200),
          },
        );
        final plan = s.plan(_ninhKieu, 14, 15);
        final pack = await s.download(plan, name: 'x');
        expect(pack.status, 'failed');
        expect(pack.tileCount, lessThan(plan.tiles));
        expect(pack.tileCount, greaterThan(0));
      },
    );

    test('vượt tổng dung lượng cho phép → dừng, báo lỗi', () async {
      final s = svc(limits: TilePackLimits(maxTotalBytes: _png.length * 3));
      final plan = s.plan(_ninhKieu, 14, 15);
      await expectLater(
        s.download(plan, name: 'x'),
        throwsA(isA<TilePackException>()),
      );
      expect(await s.store.totalBytes(), lessThanOrEqualTo(_png.length * 3));
      // Đầy → không cho tải gói mới.
      final s2 = svc(limits: TilePackLimits(maxTotalBytes: _png.length * 3));
      await expectLater(
        s2.download(plan, name: 'y'),
        throwsA(isA<TilePackException>()),
      );
    });

    test('dừng giữa chừng', () async {
      final s = svc();
      final plan = s.plan(_ninhKieu, 13, 16);
      var n = 0;
      final pack = await s.download(plan, name: 'x', cancelled: () => n++ > 3);
      expect(pack.status, 'failed');
      expect(pack.tileCount, 4);
    });
  });

  group('pull: pubkey theo lượt, reset phạm vi', () {
    late Harness h;
    setUp(() async => h = await Harness.create());
    tearDown(() => h.dispose());

    PullChange key(String ref, int v, int fill) => PullChange(
      entity: 'pubkey',
      id: ref,
      version: v,
      op: 'upsert',
      data: {
        'ref': ref,
        'area_code': '31117',
        'public_key': base64.encode(List.filled(32, fill)),
      },
    );

    test('lượt mới kết thúc bằng marker → xóa khóa version cũ hơn', () async {
      await h.refs.applyPage(
        PullPage(
          changes: [key('hh1', 5, 1), key('hh2', 5, 2)],
          nextCursor: 'c1',
          hasMore: false,
        ),
      );
      expect(await h.refs.publicKeyFor('hh2'), isNotNull);
      await h.refs.applyPage(
        PullPage(
          changes: [
            key('hh1', 6, 1),
            const PullChange(
              entity: 'pubkey',
              id: '00000000-0000-0000-0000-000000000000',
              version: 6,
              op: 'tombstone',
            ),
          ],
          nextCursor: 'c2',
          hasMore: false,
        ),
      );
      expect(
        await h.refs.publicKeyFor('hh1'),
        Uint8List.fromList(List.filled(32, 1)),
      );
      expect(await h.refs.publicKeyFor('hh2'), isNull);
    });

    test('reset = true → thay toàn bộ dữ liệu tham chiếu', () async {
      await h.refs.applyPage(
        PullPage(
          changes: [
            const PullChange(
              entity: 'plot',
              id: 'p-old',
              version: 1,
              op: 'upsert',
              data: {'name': 'Thửa xã cũ'},
            ),
            key('hh1', 1, 1),
          ],
          nextCursor: 'c1',
          hasMore: false,
        ),
      );
      await h.refs.applyPage(
        const PullPage(
          changes: [
            PullChange(
              entity: 'plot',
              id: 'p-new',
              version: 1,
              op: 'upsert',
              data: {'name': 'Thửa xã mới'},
            ),
          ],
          nextCursor: 'c2',
          hasMore: false,
          reset: true,
        ),
      );
      expect((await h.refs.list('plot')).map((r) => r.id), ['p-new']);
      expect(await h.refs.publicKeyFor('hh1'), isNull);
    });

    test('400 sync.cursor_invalid → pull lại từ đầu', () async {
      await h.db.setMeta('pull.cursor', 'tampered');
      final api = _CursorInvalidOnce();
      h.server = api;
      await h.reopen();
      await h.sync.syncOnce();
      expect(api.cursors, ['tampered', null]);
      expect(await h.refs.cursor(), 'fresh');
    });
  });
}

class _CursorInvalidOnce extends FakeServer {
  final cursors = <String?>[];
  @override
  Future<PullPage> pull({String? cursor, required int limit}) async {
    cursors.add(cursor);
    if (cursor == 'tampered') throw ApiException(400, 'sync.cursor_invalid');
    return const PullPage(changes: [], nextCursor: 'fresh', hasMore: false);
  }
}

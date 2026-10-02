import 'package:agrigreen_field/application/kn_collection_service.dart';
import 'package:agrigreen_mobile/data/actions/action_queue.dart';
import 'package:agrigreen_mobile/data/catalog/catalog_cache.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/harness.dart';

void main() {
  late Harness h;
  late KnCollectionService svc;
  final queries = <Map<String, String>?>[];

  setUp(() async {
    h = await Harness.create();
    queries.clear();
    svc = KnCollectionService(
      db: h.db,
      api: h.server,
      actions: h.actions,
      catalogs: CatalogCache(h.db, h.clock),
      clock: h.clock,
      pageSize: 2,
    );
  });
  tearDown(() => h.dispose());

  test('FN-XA-03.03 danh sách HTX: phân trang cursor, SĐT che, xem bản đệm khi ngoại tuyến', () async {
    var offline = false;
    h.fake.onGet = (path, q) async {
      if (offline) throw NetworkException('offline');
      queries.add(q);
      if (path != '/v1/kn/cooperatives') return null;
      return q?['cursor'] == null
          ? {
              'items': [
                {
                  'ref': 'c1',
                  'name': 'HTX Mỹ Hòa',
                  'representative': {
                    'label': 'Nguyễn V. A',
                    'masked_phone': '0912•••123',
                  },
                },
                {'ref': 'c2', 'name': 'HTX Tân Lập'},
              ],
              'next_cursor': 'p2',
            }
          : {
              'items': [
                {'ref': 'c3', 'name': 'HTX Phú Lộc'},
              ],
              'next_cursor': null,
            };
    };
    final p1 = await svc.cooperatives(commune: '30280', q: ' Mỹ ');
    expect(queries.last, {
      'commune': '30280',
      'status': 'active',
      'q': 'Mỹ',
      'limit': '2',
    });
    expect(p1.items, hasLength(2));
    expect(p1.hasMore, isTrue);
    expect(
      (p1.items.first['representative'] as Map)['masked_phone'],
      '0912•••123',
    );
    final p2 = await svc.cooperatives(
      commune: '30280',
      q: 'Mỹ',
      cursor: p1.nextCursor,
    );
    expect(p2.items.single['ref'], 'c3');
    expect(p2.hasMore, isFalse);
    offline = true;
    final cached = await svc.cooperatives(commune: '30280', q: 'Mỹ');
    expect(cached.fromCache, isTrue);
    expect(cached.items, hasLength(2));
    await expectLater(
      svc.cooperatives(commune: '30280', q: 'Mỹ', cursor: 'p2'),
      throwsA(isA<NetworkException>()),
    );
  });

  test('FN-XA-03.03 thêm HTX: Idempotency-Key cố định; trùng tên 409 → báo lỗi; mất mạng → xếp hàng rồi phát lại', () async {
    final ok = await svc.createCooperative(
      name: ' HTX Mỹ Hòa ',
      communeCode: '30280',
      areaHa: '12,5',
      repName: 'Nguyễn Văn A',
      repPhone: '0912345123',
    );
    expect(ok.queued, isFalse);
    final (path, key) = h.fake.actionCalls.single;
    expect(path, '/v1/kn/cooperatives');
    expect(key, startsWith('act:'));

    h.fake.actionErrors['/v1/kn/cooperatives'] = (
      409,
      'party.cooperative_name_taken',
    );
    await expectLater(
      svc.createCooperative(name: 'HTX Mỹ Hòa', communeCode: '30280'),
      throwsA(
        isA<KnCommandError>().having(
          (e) => e.message,
          'message',
          'Tên HTX đã có. Đặt tên khác.',
        ),
      ),
    );
    h.fake.actionErrors.clear();

    h.fake.failNextActions = 1;
    final q = await svc.createCooperative(
      name: 'HTX Tân Lập',
      communeCode: '30281',
    );
    expect(q.queued, isTrue);
    final qa = (await h.actions.pending()).single;
    await h.sync.syncOnce(force: true);
    expect(await h.actions.pendingCount(), 0);
    // Phát lại cùng Idempotency-Key (máy chủ chỉ thực hiện một lần).
    expect(
      h.fake.actionCalls.where((c) => c.$2 == qa.idempotencyKey),
      hasLength(2),
    );
  });

  test('FN-XA-03.03 sửa HTX / nông hộ = PUT; ngoại tuyến xếp hàng, SyncService phát lại bằng PUT', () async {
    var down = true;
    h.fake.onSend = (method, path, body) async {
      if (down) throw NetworkException('offline');
      return null;
    };
    final r = await svc.updateHousehold(
      'hh-1',
      areaHa: '1,25',
      reason: 'đo lại',
    );
    expect(r.queued, isTrue);
    final a = (await h.actions.pending()).single;
    expect(a.kind, ApiActionKind.knHouseholdUpdate);
    expect(a.method, 'PUT');
    expect(a.body, {'cultivated_area_ha': '1.25', 'reason': 'đo lại'});
    down = false;
    await h.sync.syncOnce(force: true);
    final last = h.fake.sendCalls.last;
    expect(last.$1, 'PUT');
    expect(last.$2, '/v1/kn/households/hh-1');
    expect(last.$3, a.idempotencyKey);
    expect(await h.actions.pendingCount(), 0);
  });

  test('FN-XA-03.03 vô hiệu hóa: lý do ≥ 20 ký tự (= CHECK sổ org_event), trả cảnh báo dữ liệu liên kết', () async {
    expect(
      () => svc.deactivateCooperative('c1', 'ngắn'),
      throwsA(isA<KnCommandError>()),
    );
    // 19 ký tự (có dấu): máy chủ từ chối → chặn ngay trên máy, không xếp hàng.
    expect(
      () => svc.deactivateCooperative(
        'c1',
        'HTX giải thể QĐ 12'.padRight(19, '.'),
      ),
      throwsA(isA<KnCommandError>()),
    );
    h.fake.onPost = (path, body) async => {
      'cooperative': {'ref': 'c1', 'active': false},
      'warnings': {'households': 3, 'plots': 2, 'machines': 0},
    };
    final r = await svc.deactivateCooperative('c1', 'HTX giải thể theo QĐ 12');
    expect((r.response!['warnings'] as Map)['plots'], 2);
    expect(h.fake.actionCalls.single.$1, '/v1/kn/cooperatives/c1/deactivate');
  });

  test('FN-XA-03.04 máy móc: số nguyên ≥ 0, trùng loại gợi ý cập nhật dòng có sẵn, PUT thay toàn bộ', () async {
    final errs = validateMachineLines(const [
      KnMachineLine('may_cay', '3'),
      KnMachineLine('may_gat', '-1'),
      KnMachineLine('may_cay', '2'),
      KnMachineLine('may_say', '1.5'),
    ]);
    expect(errs[1], 'Số lượng phải là số nguyên ≥ 0');
    expect(errs[2], 'Trùng loại máy ở dòng 1 — cập nhật dòng đã có');
    expect(errs[3], 'Số lượng phải là số nguyên ≥ 0');
    expect(errs.containsKey(0), isFalse);
    expect(
      () => svc.saveMachineCounts('c1', const [
        KnMachineLine('may_cay', '1'),
        KnMachineLine('may_cay', '1'),
      ]),
      throwsA(isA<KnCommandError>()),
    );
    await svc.saveMachineCounts('c1', const [
      KnMachineLine('may_cay', '3', note: 'mới mua'),
      KnMachineLine('may_gat', '0'),
    ]);
    final c = h.fake.sendCalls.single;
    expect(c.$1, 'PUT');
    expect(c.$2, '/v1/kn/machine-counts/c1');
    expect(c.$4, {
      'lines': [
        {'machine_type': 'may_cay', 'quantity': 3, 'note': 'mới mua'},
        {'machine_type': 'may_gat', 'quantity': 0},
      ],
    });
  });

  test('FN-XA-03.06 vật tư: ĐVT đếm được → số nguyên; ≤ 4 chữ số lẻ; không ở tương lai; sửa = bản ghi bù có lý do', () async {
    expect(
      validateMaterialQuantity('2.5', 'bao'),
      'ĐVT "bao" phải là số nguyên',
    );
    expect(validateMaterialQuantity('2.0', 'bao'), isNull);
    expect(
      validateMaterialQuantity('1,23456', 'kg'),
      'Tối đa 4 chữ số thập phân',
    );
    expect(validateMaterialQuantity('1,2345', 'kg'), isNull);
    expect(validateMaterialQuantity('-1', 'kg'), 'Số lượng không hợp lệ');
    expect(
      validateMaterialQuantity('2000000000', 'kg'),
      'Tối đa 1.000.000.000',
    );
    expect(
      () => svc.createMaterials(
        communeCode: '30280',
        recordedOn: '2026-12-05',
        lines: [(code: 'ure', quantity: '10', unit: 'kg', note: null)],
      ),
      throwsA(isA<KnCommandError>()),
    );
    await svc.createMaterials(
      communeCode: '30280',
      recordedOn: '2026-12-02',
      lines: [(code: 'ure', quantity: '10,5', unit: 'kg', note: 'vụ ĐX')],
    );
    expect(h.fake.actionCalls.single.$1, '/v1/kn/materials');
    expect(
      () => svc.correctMaterial('m1', quantity: '3', unit: 'bao', reason: 'x'),
      throwsA(isA<KnCommandError>()),
    );
    await svc.correctMaterial(
      'm1',
      quantity: '3',
      unit: 'bao',
      reason: 'Nhập nhầm số bao',
    );
    expect(h.fake.actionCalls.last.$1, '/v1/kn/materials/m1/correct');
  });

  test('FN-XA-03.05 danh sách thửa theo HTX (REST), danh mục máy/vật tư đệm ngoại tuyến', () async {
    var offline = false;
    h.fake.onGet = (path, q) async {
      if (offline) throw NetworkException('offline');
      queries.add(q);
      if (path == '/v1/kn/plots') {
        return {
          'items': [
            {'ref': 'p1', 'lot_name': 'Lô 1', 'area_ha': '1.2000'},
          ],
          'next_cursor': null,
        };
      }
      if (path == '/v1/masterdata/catalogs/material') {
        return {
          'items': [
            {
              'code': 'ure',
              'name': 'Phân urê',
              'attrs': {'unit': 'kg'},
            },
          ],
          'next_cursor': null,
        };
      }
      return null;
    };
    final p = await svc.plots(coopRef: 'c1');
    expect(queries.last, {'cooperative_ref': 'c1', 'limit': '2'});
    expect(p.items.single['lot_name'], 'Lô 1');
    expect((await svc.catalog('material')).single.attrs['unit'], 'kg');
    offline = true;
    expect((await svc.catalog('material')).single.code, 'ure');
  });
}

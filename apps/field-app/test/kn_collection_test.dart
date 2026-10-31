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

  test('FN-XA-03.03 / #161 danh sách hồ sơ KN: /v1/kn/producer-records theo loại, phân trang cursor, SĐT che, bản đệm khi ngoại tuyến', () async {
    var offline = false;
    final paths = <String>[];
    h.fake.onGet = (path, q) async {
      if (offline) throw NetworkException('offline');
      queries.add(q);
      paths.add(path);
      if (path != '/v1/kn/producer-records') return null;
      if (q?['kind'] == 'household') {
        return {
          'items': [
            {'ref': 'hh-1', 'kind': 'household', 'display_name': 'Trần T. B'},
          ],
          'next_cursor': null,
        };
      }
      return q?['cursor'] == null
          ? {
              'items': [
                {
                  'ref': 'c1',
                  'kind': 'cooperative',
                  'name': 'HTX Mỹ Hòa',
                  'representative': {
                    'label': 'Nguyễn V. A',
                    'masked_phone': '0912•••123',
                  },
                },
                {'ref': 'c2', 'kind': 'cooperative', 'name': 'HTX Tân Lập'},
              ],
              'next_cursor': 'p2',
            }
          : {
              'items': [
                {'ref': 'c3', 'kind': 'cooperative', 'name': 'HTX Phú Lộc'},
              ],
              'next_cursor': null,
            };
    };
    final p1 = await svc.records(
      kind: KnRecordKind.cooperative,
      commune: '30280',
      q: ' Mỹ ',
    );
    expect(queries.last, {
      'kind': 'cooperative',
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
    final p2 = await svc.records(
      kind: KnRecordKind.cooperative,
      commune: '30280',
      q: 'Mỹ',
      cursor: p1.nextCursor,
    );
    expect(p2.items.single['ref'], 'c3');
    expect(p2.hasMore, isFalse);
    // Nông hộ của một HTX: lọc parent_ref, không gửi q (tên chủ hộ ở kho định danh).
    final hh = await svc.households('c1');
    expect(queries.last, {
      'kind': 'household',
      'status': 'active',
      'parent_ref': 'c1',
      'limit': '2',
    });
    expect(hh.items.single['ref'], 'hh-1');
    expect(paths.toSet(), {'/v1/kn/producer-records'});
    offline = true;
    final cached = await svc.records(
      kind: KnRecordKind.cooperative,
      commune: '30280',
      q: 'Mỹ',
    );
    expect(cached.fromCache, isTrue);
    expect(cached.items, hasLength(2));
    await expectLater(
      svc.records(
        kind: KnRecordKind.cooperative,
        commune: '30280',
        q: 'Mỹ',
        cursor: 'p2',
      ),
      throwsA(isA<NetworkException>()),
    );
  });

  test('FN-XA-03.03 / #161 thêm HTX = POST /v1/kn/producer-records: Idempotency-Key cố định; trùng tên 409 → báo lỗi; mất mạng → xếp hàng rồi phát lại', () async {
    final bodies = <Map<String, Object?>?>[];
    h.fake.onPost = (path, body) async {
      bodies.add(body);
      return null;
    };
    final ok = await svc.createRecord(
      kind: KnRecordKind.cooperative,
      name: ' HTX Mỹ Hòa ',
      communeCode: '30280',
      areaHa: '12,5',
      taxCode: ' ',
      address: 'Ấp 3',
      personName: 'Nguyễn Văn A',
      personPhone: '0912345123',
      htxRef: 'htx-pub-1',
    );
    expect(ok.queued, isFalse);
    final (path, key) = h.fake.actionCalls.single;
    expect(path, '/v1/kn/producer-records');
    expect(key, startsWith('act:'));
    expect(bodies.single, {
      'kind': 'cooperative',
      'commune_code': '30280',
      'name': 'HTX Mỹ Hòa',
      'area_ha': '12.5',
      'address': 'Ấp 3',
      'representative': {'full_name': 'Nguyễn Văn A', 'phone': '0912345123'},
      'htx_ref': 'htx-pub-1',
    });

    h.fake.actionErrors['/v1/kn/producer-records'] = (
      409,
      'extension.kn_record_name_taken',
    );
    await expectLater(
      svc.createRecord(
        kind: KnRecordKind.cooperative,
        name: 'HTX Mỹ Hòa',
        communeCode: '30280',
        personName: 'Nguyễn Văn A',
        personPhone: '0912345123',
      ),
      throwsA(
        isA<KnCommandError>().having(
          (e) => e.message,
          'message',
          'Tên đã có trong xã (cùng loại, đang hoạt động). Đặt tên khác.',
        ),
      ),
    );
    h.fake.actionErrors.clear();

    h.fake.failNextActions = 1;
    final q = await svc.createRecord(
      kind: KnRecordKind.coopGroup,
      name: 'THT Tân Lập',
      communeCode: '30281',
      personName: 'Lê Văn C',
      personPhone: '0987654321',
      taxCode: '0123456789',
      htxRef: 'x',
    );
    expect(q.queued, isTrue);
    final qa = (await h.actions.pending()).single;
    expect(qa.kind, ApiActionKind.knRecordCreate);
    // THT: không MST, không liên kết HTX đã công bố.
    expect(qa.body!.containsKey('tax_code'), isFalse);
    expect(qa.body!.containsKey('htx_ref'), isFalse);
    await h.sync.syncOnce(force: true);
    expect(await h.actions.pendingCount(), 0);
    // Phát lại cùng Idempotency-Key (máy chủ chỉ thực hiện một lần).
    expect(
      h.fake.actionCalls.where((c) => c.$2 == qa.idempotencyKey),
      hasLength(2),
    );
    expect(
      h.fake.actionCalls.map((c) => c.$1),
      everyElement('/v1/kn/producer-records'),
    );
  });

  test('#161 thêm nông hộ: kind household, chủ hộ (holder), thuộc HTX (parent_ref), không tên/MST/địa chỉ; thiếu chủ hộ chặn trên máy', () async {
    expect(
      () => svc.createRecord(
        kind: KnRecordKind.household,
        communeCode: '30280',
        parentRef: 'c1',
      ),
      throwsA(isA<KnCommandError>()),
    );
    expect(h.fake.actionCalls, isEmpty);
    h.fake.failNextActions = 1;
    final r = await svc.createRecord(
      kind: KnRecordKind.household,
      communeCode: '30280',
      parentRef: 'c1',
      name: 'bỏ qua',
      address: 'bỏ qua',
      personName: ' Trần Thị B ',
      personPhone: '0911222333',
      areaHa: '1,25',
    );
    expect(r.queued, isTrue);
    final a = (await h.actions.pending()).single;
    expect(a.path, '/v1/kn/producer-records');
    expect(a.body, {
      'kind': 'household',
      'commune_code': '30280',
      'area_ha': '1.25',
      'holder': {'full_name': 'Trần Thị B', 'phone': '0911222333'},
      'parent_ref': 'c1',
    });
  });

  test('#161 diện tích: số thập phân thường, kiểm chuỗi trước khi đọc số (1e5, chuỗi dài, > 1.000.000, > 4 chữ số lẻ bị chặn)', () async {
    expect(normalizeAreaHa(' 12,5 '), ('12.5', null));
    expect(normalizeAreaHa(''), (null, null));
    expect(normalizeAreaHa('1000000'), ('1000000', null));
    expect(normalizeAreaHa('1000000.0000'), ('1000000.0000', null));
    for (final bad in [
      '1e5',
      '1e100000',
      '1e-100000',
      '-1',
      '+1',
      '1.23456',
      '1000000.0001',
      '9999999',
      '1' * 40,
      '0x10',
      '1,2,3',
      'NaN',
    ]) {
      expect(normalizeAreaHa(bad).$2, isNotNull, reason: bad);
      expect(
        () => svc.createRecord(
          kind: KnRecordKind.cooperative,
          communeCode: '30280',
          name: 'HTX A',
          personName: 'Nguyễn Văn A',
          personPhone: '0912345123',
          areaHa: bad,
        ),
        throwsA(isA<KnCommandError>()),
        reason: bad,
      );
    }
    expect(h.fake.actionCalls, isEmpty);
  });

  test('#161 sửa hồ sơ = POST /{ref} gửi đủ trường + base_version; ngoại tuyến xếp hàng, phát lại bằng POST cùng khóa', () async {
    const coop = {
      'ref': 'c1',
      'kind': 'cooperative',
      'commune_code': '30280',
      'name': 'HTX Mỹ Hòa',
      'area_ha': '12.5',
      'tax_code': '0123456789',
      'address': 'Ấp 3',
      'htx_ref': 'htx-pub-1',
      'parent_ref': null,
      'version': 4,
    };
    h.fake.failNextActions = 1;
    final r = await svc.updateRecord(coop, areaHa: '13,75', htxRef: '');
    expect(r.queued, isTrue);
    final a = (await h.actions.pending()).single;
    expect(a.kind, ApiActionKind.knRecordUpdate);
    expect(a.method, 'POST');
    expect(a.path, '/v1/kn/producer-records/c1');
    // Máy chủ thay toàn bộ trường: giữ tên/MST/địa chỉ hiện có, bỏ liên kết
    // HTX khi chọn "Không liên kết"; người đại diện không gửi = giữ nguyên.
    expect(a.body, {
      'kind': 'cooperative',
      'commune_code': '30280',
      'name': 'HTX Mỹ Hòa',
      'area_ha': '13.75',
      'tax_code': '0123456789',
      'address': 'Ấp 3',
      'base_version': 4,
    });
    await h.sync.syncOnce(force: true);
    expect(h.fake.actionCalls.last, (
      '/v1/kn/producer-records/c1',
      a.idempotencyKey,
    ));
    expect(h.fake.sendCalls, isEmpty);
    expect(await h.actions.pendingCount(), 0);

    // Nông hộ: chỉ đổi diện tích, giữ HTX cha; xung đột phiên bản → thông điệp.
    h.fake.actionErrors['/v1/kn/producer-records/hh-1'] = (
      409,
      'extension.kn_record_version_conflict',
    );
    await expectLater(
      svc.updateRecord(const {
        'ref': 'hh-1',
        'kind': 'household',
        'commune_code': '30280',
        'parent_ref': 'c1',
        'area_ha': '1',
        'version': 2,
      }, areaHa: '1,5'),
      throwsA(
        isA<KnCommandError>().having(
          (e) => e.message,
          'message',
          'Hồ sơ đã được người khác cập nhật — tải lại rồi sửa.',
        ),
      ),
    );
    final failed = (await h.actions.ofKind(ApiActionKind.knRecordUpdate)).last;
    expect(failed.body, {
      'kind': 'household',
      'commune_code': '30280',
      'area_ha': '1.5',
      'parent_ref': 'c1',
      'base_version': 2,
    });
    // Thiếu phiên bản (bản cũ) → không gửi.
    expect(
      () => svc.updateRecord(const {'ref': 'c9', 'kind': 'cooperative'}),
      throwsA(isA<KnCommandError>()),
    );
  });

  test('#161 vô hiệu hóa / kích hoạt lại: lý do 10–1000 ký tự kiểm trên máy, gửi base_version', () async {
    const rec = {'ref': 'c1', 'kind': 'cooperative', 'version': 3};
    expect(
      () => svc.setRecordStatus(rec, deactivate: true, reason: 'ngắn'),
      throwsA(isA<KnCommandError>()),
    );
    // 9 ký tự có dấu: chặn ngay trên máy, không xếp hàng.
    expect(
      () => svc.setRecordStatus(rec, deactivate: true, reason: 'giải thể'),
      throwsA(isA<KnCommandError>()),
    );
    expect(
      () => svc.setRecordStatus(rec, deactivate: true, reason: 'a' * 1001),
      throwsA(isA<KnCommandError>()),
    );
    expect(await h.actions.pending(), isEmpty);
    final bodies = <Map<String, Object?>?>[];
    h.fake.onPost = (path, body) async {
      bodies.add(body);
      return null;
    };
    await svc.setRecordStatus(
      rec,
      deactivate: true,
      reason: ' HTX giải thể theo QĐ 12 ',
    );
    await svc.setRecordStatus(
      const {'ref': 'c1', 'kind': 'cooperative', 'version': 4},
      deactivate: false,
      reason: 'Hoạt động trở lại',
    );
    expect(h.fake.actionCalls.map((c) => c.$1), [
      '/v1/kn/producer-records/c1/deactivate',
      '/v1/kn/producer-records/c1/reactivate',
    ]);
    expect(bodies, [
      {'reason': 'HTX giải thể theo QĐ 12', 'base_version': 3},
      {'reason': 'Hoạt động trở lại', 'base_version': 4},
    ]);
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

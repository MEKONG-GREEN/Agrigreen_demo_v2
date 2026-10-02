import 'package:agrigreen_field/application/kn_context_service.dart';
import 'package:agrigreen_field/application/kn_menu_service.dart';
import 'package:agrigreen_field/domain/kn_context.dart';
import 'package:agrigreen_field/domain/kn_report.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:agrigreen_mobile/testing.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/harness.dart';
import 'support/kn_fixtures.dart';

void main() {
  late Harness h;
  late KnFakeBackend kn;
  late KnContextService svc;

  setUp(() async {
    h = await Harness.create();
    kn = KnFakeBackend(h.fake);
    svc = KnContextService(db: h.db, api: h.server, clock: h.clock, ids: h.ids);
  });
  tearDown(() async {
    await svc.dispose();
    await h.dispose();
  });

  test(
    'KN P1 một cặp (vai trò, phòng ban) → tự chọn, lưu ngữ cảnh trên máy',
    () async {
      final st = await svc.resolve();
      expect(st.stage, KnContextStage.ready);
      expect(st.context!.contextId, 'ctx-1');
      expect(st.context!.communes.map((c) => c.code), [xaA, xaB]);
      expect(kn.selects.single, {
        'assignment_id': 'a-1',
        'department_code': 'trong_trot',
      });
      expect(await svc.contextId(), 'ctx-1');
      // Mở lại app (đối tượng mới, cùng CSDL): ngữ cảnh còn trên máy.
      final again = KnContextService(
        db: h.db,
        api: h.server,
        clock: h.clock,
        ids: h.ids,
      );
      expect(await again.contextId(), 'ctx-1');
      await again.dispose();
    },
  );

  test('KN P1 nhiều cặp → màn chọn; chọn → lưu', () async {
    kn.assignments = [
      assignment(
        'a-1',
        departments: [('trong_trot', 'Trồng trọt'), ('chan_nuoi', 'Chăn nuôi')],
      ),
      assignment('a-2', role: 'kn_tinh', communes: const []),
    ];
    final st = await svc.resolve();
    expect(st.stage, KnContextStage.picker);
    expect(st.choices, hasLength(3));
    expect(kn.selects, isEmpty);
    final ctx = await svc.select(st.choices[1]);
    expect(ctx.departmentCode, 'chan_nuoi');
    expect(await svc.contextId(), 'ctx-1');
    // Lần sau còn hiệu lực → không hỏi lại.
    final again = await svc.resolve();
    expect(again.stage, KnContextStage.ready);
    expect(kn.selects, hasLength(1));
  });

  test('KN P1 không có phân công KN (404) → màn hình cũ', () async {
    h.fake.onGet = null;
    final st = await svc.resolve();
    expect(st.stage, KnContextStage.none);
    expect(await svc.contextId(), isNull);
  });

  test('KN P1 ngoại tuyến trong 72 giờ: dùng ngữ cảnh đã lưu', () async {
    await svc.resolve();
    kn.offline = true;
    final again = KnContextService(
      db: h.db,
      api: h.server,
      clock: h.clock,
      ids: h.ids,
    );
    final st = await again.resolve();
    expect(st.stage, KnContextStage.ready);
    expect(st.offline, isTrue);
    expect(st.context!.contextId, 'ctx-1');
    await again.dispose();
  });

  test('KN P1 ngữ cảnh hết hạn khi mở lại → chọn lại cùng phân công', () async {
    await svc.resolve();
    h.clock.advance(const Duration(days: 2)); // qua expires_at
    final st = await svc.resolve();
    expect(st.context!.contextId, 'ctx-2');
    expect(kn.selects, hasLength(2));
    expect(kn.selects.last['assignment_id'], 'a-1');
  });

  test('KN P1 recover: máy chủ báo context_expired → chọn lại; phân công mất → màn chọn', () async {
    kn.assignments = [
      assignment('a-1'),
      assignment('a-2', departments: [('chan_nuoi', 'Chăn nuôi')]),
    ];
    final st = await svc.resolve();
    await svc.select(st.choices.first);
    final id = await svc.recover(ApiException(403, 'accounts.context_expired'));
    expect(id, 'ctx-2');
    expect(await svc.contextId(), 'ctx-2');

    final events = <KnContextState>[];
    final sub = svc.changes.listen(events.add);
    kn.assignments = [
      assignment('a-2', departments: [('chan_nuoi', 'Chăn nuôi')]),
      assignment('a-3', departments: [('chan_nuoi', 'Chăn nuôi')]),
    ];
    final none = await svc.recover(
      ApiException(403, 'accounts.context_revoked'),
    );
    await Future<void>.delayed(Duration.zero);
    expect(none, isNull);
    expect(events.single.stage, KnContextStage.picker);
    expect(await svc.contextId(), isNull);
    await sub.cancel();
  });

  test(
    'KN P1 đổi vai trò/phòng ban: phạm vi đổi → xóa dữ liệu KN đã pull',
    () async {
      kn.assignments = [
        assignment('a-1'),
        assignment('a-2', communes: const [(xaB, 'Xã B')]),
      ];
      final st = await svc.resolve();
      await svc.select(st.choices.first);
      await h.refs.applyPage(
        PullPage(
          changes: [
            change(KnEntity.reportTask, taskA2, 1, taskData(id: taskA2)),
            change(KnEntity.formLayout, ver1, 1, layoutData()),
          ],
          nextCursor: 'c1',
          hasMore: false,
        ),
      );
      expect(await h.refs.count(KnEntity.reportTask), 1);
      final ctx = await svc.switchTo(st.choices[1]);
      expect(ctx.communes.single.code, xaB);
      expect(await h.refs.count(KnEntity.reportTask), 0);
      expect(await h.refs.count(KnEntity.formLayout), 0);
    },
  );

  test(
    'KN P2 menu: khóa lạ bị ẩn, dùng bản đệm khi ngoại tuyến, badge tại máy',
    () async {
      final menu = KnMenuService(
        db: h.db,
        api: h.server,
        clock: h.clock,
        localTodoCount: () async => 7,
      );
      final online = await menu.load();
      expect(online.fromCache, isFalse);
      expect(online.entries.map((e) => e.key), [
        KnMenuKey.congViec,
        KnMenuKey.lichCongTac,
        KnMenuKey.htx,
      ]);
      expect((await menu.badges())[knBadgeTasksTodo], 3);
      kn.offline = true;
      final cached = await menu.load();
      expect(cached.fromCache, isTrue);
      expect(cached.has(KnMenuKey.htx), isTrue);
      expect(cached.has('xa.khong_biet'), isFalse);
      expect((await menu.badges())[knBadgeTasksTodo], 7);
    },
  );

  test('KN P1 expand: phòng ban không mở / phân công hết hiệu lực bị bỏ', () {
    final list = KnContextChoice.expand({
      'assignments': [
        {
          ...assignment('a-1'),
          'departments': [
            {'code': 'trong_trot', 'name': 'Trồng trọt', 'status': 'active'},
            {'code': 'thuy_san', 'name': 'Thủy sản', 'status': 'not_open'},
          ],
        },
        {...assignment('a-2'), 'active': false},
        {...assignment('a-3', role: 'admin'), 'departments': <Object?>[]},
      ],
    });
    expect(list.map((c) => c.key), ['a-1|trong_trot', 'a-3|']);
  });

  test('KN P1 FakeServer: ghi lại sendJson (PUT) theo khóa', () async {
    final r = await h.fake.sendJson(
      method: 'PUT',
      path: '/v1/kn/households/x',
      idempotencyKey: 'act:1',
      body: const {'cultivated_area_ha': '1.5'},
    );
    expect(r['path'], '/v1/kn/households/x');
    expect(h.fake.sendCalls.single.$1, 'PUT');
    expect(h.fake, isA<FakeServer>());
  });
}

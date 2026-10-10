import 'dart:io';

import 'package:agrigreen_field/application/app_services.dart';
import 'package:agrigreen_field/application/kn_context_service.dart';
import 'package:agrigreen_field/domain/kn_context.dart';
import 'package:agrigreen_field/domain/kn_report.dart';
import 'package:agrigreen_field/ui/app.dart';
import 'package:agrigreen_field/ui/kn/kn_cooperatives_screen.dart';
import 'package:agrigreen_field/ui/kn/kn_home_screen.dart';
import 'package:agrigreen_field/ui/kn/kn_report_entry_screen.dart';
import 'package:agrigreen_field/ui/providers.dart';
import 'package:agrigreen_mobile/application/auth_flow.dart';
import 'package:agrigreen_mobile/core/clock.dart';
import 'package:agrigreen_mobile/core/config.dart';
import 'package:agrigreen_mobile/core/ids.dart';
import 'package:agrigreen_mobile/data/photo/location_source.dart';
import 'package:agrigreen_mobile/data/photo/photo_pipeline.dart';
import 'package:agrigreen_mobile/data/secure/secure_store.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';
import 'package:agrigreen_mobile/testing.dart';
import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/ffi_opener.dart';
import 'support/kn_fixtures.dart';

class _NoCamera implements CameraSource {
  @override
  Future<CapturedImage?> capture() async => null;
}

/// Bộ điều khiển với trạng thái dựng sẵn (không chạy luồng đăng nhập, không
/// nghe kết nối mạng) — màn KN dùng dịch vụ thật trên SQLite FFI.
class _Fixed extends AppController {
  _Fixed(this.initial);
  final AppState initial;
  @override
  AppState build() => initial;
}

class _Env {
  _Env(this.dir, this.pf, this.server, this.kn);
  final Directory dir;
  final AppPlatform pf;
  final FakeServer server;
  final KnFakeBackend kn;
  late AppServices s;

  static Future<_Env> create() async {
    final dir = await Directory.systemTemp.createTemp('ag_kn_w_');
    final server = FakeServer();
    final kn = KnFakeBackend(server);
    final pf = AppPlatform(
      config: const AppConfig(
        apiBaseUrl: 'https://api.test',
        oidcIssuer: 'https://id.test',
      ),
      store: MemorySecureStore(),
      clock: FakeClock(DateTime.utc(2026, 12, 2, 1)),
      ids: UuidV7Generator(),
      opener: FfiOpener(),
      dataDir: dir,
      apiFactory: (_, _) => server,
      camera: _NoCamera(),
      location: const FixedLocationSource(LocationResult(GpsStatus.denied)),
    );
    final e = _Env(dir, pf, server, kn);
    e.s = await AppServices.open(pf, accessToken: () async => 'tok');
    return e;
  }

  Future<void> seedTasks() => s.refs.applyPage(
    PullPage(
      changes: [
        change(KnEntity.formLayout, ver1, 1, layoutData()),
        change(KnEntity.reportTask, taskA2, 2, taskData(id: taskA2)),
        change(
          KnEntity.reportTask,
          taskB2,
          3,
          taskData(id: taskB2, commune: xaB, communeName: 'Xã B'),
        ),
        change(
          KnEntity.reportTask,
          taskA1,
          4,
          taskData(id: taskA1, period: period1, slot: 1, status: 'submitted'),
        ),
        change(
          KnEntity.reportSubmission,
          taskA1,
          4,
          submissionData(
            taskId: taskA1,
            submissionId: 'sub-a1',
            input: const {'D': '12'},
          ),
        ),
        change(
          KnEntity.reportTask,
          taskClosed,
          5,
          taskData(id: taskClosed, formCode: 'T04', closed: true),
        ),
      ],
      nextCursor: 'c1',
      hasMore: false,
    ),
  );

  Future<void> dispose() async {
    await s.close();
    await dir.delete(recursive: true);
  }
}

Future<void> settle(WidgetTester t, {int rounds = 12}) async {
  for (var i = 0; i < rounds; i++) {
    await t.runAsync(
      () => Future<void>.delayed(const Duration(milliseconds: 15)),
    );
    await t.pump(const Duration(milliseconds: 50));
  }
}

Future<void> _finish(WidgetTester t, _Env env) async {
  await t.pumpWidget(const SizedBox());
  await t.pump(const Duration(seconds: 5));
  await t.runAsync(env.dispose);
}

Widget _app(_Env env, AppState st, {Widget? home}) => ProviderScope(
  overrides: [
    platformProvider.overrideWithValue(env.pf),
    oidcProvider.overrideWithValue(FakeOidc()),
    appControllerProvider.overrideWith(() => _Fixed(st)),
  ],
  child: home == null
      ? const FieldApp()
      : MaterialApp(theme: buildTheme(), home: home),
);

KnSessionContext _ctx() => const KnSessionContext(
  contextId: 'ctx-0',
  role: 'kn_xa',
  orgName: 'TTKN Cần Thơ',
  communes: [KnCommune(xaA, 'Xã A'), KnCommune(xaB, 'Xã B')],
  defaultRoute: '/kn/nhiem-vu/cong-viec',
  expiresAt: null,
  departmentCode: 'trong_trot',
  departmentName: 'Trồng trọt',
);

void main() {
  setUp(() {
    // Màn hình điện thoại phổ thông.
  });

  testWidgets(
    'KN P1 nhiều cặp → "Chọn vai trò và phòng ban" → chọn → Công việc cần làm (menu từ máy chủ, khóa lạ ẩn)',
    (tester) async {
      tester.view.physicalSize = const Size(720, 1600);
      tester.view.devicePixelRatio = 2;
      addTearDown(tester.view.reset);
      final env = (await tester.runAsync(_Env.create))!;
      env.kn.assignments = [
        assignment('a-1'),
        assignment('a-2', departments: [('chan_nuoi', 'Chăn nuôi')]),
      ];
      final choices = KnContextChoice.expand({
        'assignments': env.kn.assignments,
      });
      await tester.pumpWidget(
        _app(
          env,
          AppState(
            const AuthView(AuthStage.unlocked),
            services: env.s,
            kn: KnContextState(KnContextStage.picker, choices: choices),
          ),
        ),
      );
      await settle(tester);
      expect(find.text('Chọn vai trò và phòng ban'), findsOneWidget);
      expect(find.text('Cấp Xã · Chăn nuôi'), findsOneWidget);
      await tester.tap(find.byKey(const ValueKey('kn-choice-a-2|chan_nuoi')));
      await settle(tester, rounds: 20);
      expect(env.kn.selects.single, {
        'assignment_id': 'a-2',
        'department_code': 'chan_nuoi',
      });
      expect(find.byType(KnHomeScreen), findsOneWidget);
      expect(find.text('Chưa có công việc'), findsOneWidget);
      // Menu Cấp Xã từ GET /v1/menu: khóa lạ bị ẩn, badge việc cần làm.
      await tester.tap(find.byTooltip('Open navigation menu'));
      await settle(tester);
      expect(
        find.byKey(const ValueKey('kn-menu-xa.thu_thap.htx')),
        findsOneWidget,
      );
      expect(find.text('Mục lạ'), findsNothing);
      expect(find.text('Lịch công tác'), findsOneWidget);
      expect(find.text('3'), findsOneWidget);
      // kn.menu/v3: nhóm "Dùng chung", Contact "Sắp có" (không bấm được).
      expect(find.text('Dùng chung'), findsOneWidget);
      expect(find.text('Sắp có'), findsOneWidget);
      final contact = tester.widget<ListTile>(
        find.byKey(const ValueKey('kn-menu-chung.contact')),
      );
      expect(contact.enabled, isFalse);
      await _finish(tester, env);
    },
  );

  testWidgets('KN P1 không có phân công KN → giữ màn hình chủ cũ', (
    tester,
  ) async {
    final env = (await tester.runAsync(_Env.create))!;
    await tester.pumpWidget(
      _app(
        env,
        AppState(
          const AuthView(AuthStage.unlocked),
          services: env.s,
          kn: const KnContextState(KnContextStage.none),
        ),
      ),
    );
    await settle(tester);
    expect(find.text('AgriGreen Hiện trường'), findsOneWidget);
    expect(find.byType(KnHomeScreen), findsNothing);
    await _finish(tester, env);
  });

  testWidgets(
    'FN-XA-03.02 màn nhập: tab xã, chỉ khe kỳ mở sửa được, "chưa tính", lỗi tại ô, tự lưu nháp khi chuyển tab, gửi → hàng đợi',
    (tester) async {
      tester.view.physicalSize = const Size(720, 2400);
      tester.view.devicePixelRatio = 2;
      addTearDown(tester.view.reset);
      final env = (await tester.runAsync(_Env.create))!;
      await tester.runAsync(env.seedTasks);
      env.server.failNextPushes = 1000; // ngoại tuyến: bản ghi ở lại hàng đợi
      await tester.pumpWidget(
        _app(
          env,
          AppState(
            const AuthView(AuthStage.unlocked),
            services: env.s,
            kn: KnContextState(KnContextStage.ready, context: _ctx()),
          ),
          home: const KnReportEntryScreen(taskId: taskA2),
        ),
      );
      await settle(tester);
      expect(find.byKey(const ValueKey('kn-tab-$xaA')), findsOneWidget);
      expect(find.byKey(const ValueKey('kn-tab-$xaB')), findsOneWidget);
      expect(find.text('TIẾN ĐỘ XUỐNG GIỐNG THU ĐÔNG 2026'), findsOneWidget);
      // E (Tuần 37, khe đang mở) và H (nhập) sửa được; D (Tuần 36) chỉ xem.
      expect(find.byKey(const ValueKey('cell-$xaA-E')), findsOneWidget);
      expect(find.byKey(const ValueKey('cell-$xaA-H')), findsOneWidget);
      expect(find.byKey(const ValueKey('cell-$xaA-D')), findsNothing);
      expect(
        find.descendant(
          of: find.byKey(const ValueKey('ro-$xaA-D')),
          matching: find.text('12'),
        ),
        findsOneWidget,
      );
      expect(
        find.descendant(
          of: find.byKey(const ValueKey('ro-$xaA-G')),
          matching: find.text('chưa tính'),
        ),
        findsOneWidget,
      );
      expect(
        find.byKey(const ValueKey('cell-$xaA-F')),
        findsNothing,
      ); // khe 3 không dùng

      // Gửi khi thiếu ô bắt buộc → báo tại ô, không xếp hàng.
      await tester.enterText(find.byKey(const ValueKey('cell-$xaA-E')), '3,5');
      await tester.tap(find.byKey(const ValueKey('kn-submit')));
      await settle(tester);
      expect(find.text('Bắt buộc nhập'), findsOneWidget);
      expect((await tester.runAsync(env.s.outbox.list))!, isEmpty);

      // Chuyển tab → tự lưu nháp của xã A (chưa gửi) vào hàng đợi.
      await tester.enterText(find.byKey(const ValueKey('cell-$xaA-H')), '7');
      await tester.ensureVisible(find.byKey(const ValueKey('kn-tab-$xaB')));
      await tester.pump();
      await tester.tap(find.byKey(const ValueKey('kn-tab-$xaB')));
      await settle(tester, rounds: 20);
      final drafts = (await tester.runAsync(env.s.outbox.list))!;
      expect(drafts.single.payload['kind'], 'draft');
      expect(drafts.single.payload['task_id'], taskA2);
      expect(find.byKey(const ValueKey('cell-$xaB-E')), findsOneWidget);

      // Quay lại xã A: giá trị còn, gửi báo cáo → bản ghi "submitted" chờ đồng bộ.
      await tester.ensureVisible(find.byKey(const ValueKey('kn-tab-$xaA')));
      await tester.pump();
      await tester.tap(find.byKey(const ValueKey('kn-tab-$xaA')));
      await settle(tester, rounds: 20);
      expect(find.text('7'), findsOneWidget);
      // Đợi thông báo (SnackBar) tự ẩn để không che nút.
      ScaffoldMessenger.of(tester.element(find.byType(KnReportEntryScreen)))
          .clearSnackBars();
      await tester.pump(const Duration(seconds: 1));
      await settle(tester);
      await tester.tap(find.byKey(const ValueKey('kn-submit')));
      await settle(tester, rounds: 20);
      final recs = (await tester.runAsync(
        () => env.s.outbox.list(entity: PushEntity.reportSubmission),
      ))!;
      expect(recs.first.payload['kind'], 'submitted');
      expect(recs.first.payload['rows'], [
        {
          'input': {'E': '3.5', 'H': '7'},
        },
      ]);
      expect(
        find.descendant(
          of: find.byKey(const ValueKey('kn-sync-status')),
          matching: find.text('Chờ đồng bộ'),
        ),
        findsOneWidget,
      );
      await _finish(tester, env);
    },
  );

  testWidgets('FN-XA-03.02 kỳ đã chốt → chỉ xem (không có nút gửi)', (
    tester,
  ) async {
    final env = (await tester.runAsync(_Env.create))!;
    await tester.runAsync(env.seedTasks);
    await tester.pumpWidget(
      _app(
        env,
        AppState(
          const AuthView(AuthStage.unlocked),
          services: env.s,
          kn: KnContextState(KnContextStage.ready, context: _ctx()),
        ),
        home: const KnReportEntryScreen(taskId: taskClosed),
      ),
    );
    await settle(tester);
    expect(find.byKey(const ValueKey('kn-closed')), findsOneWidget);
    expect(find.byKey(const ValueKey('kn-submit')), findsNothing);
    expect(find.byType(TextField), findsNothing);
    await _finish(tester, env);
  });

  testWidgets('FN-XA-03.01 Công việc cần làm: đếm, nút Nhập / Xem', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(720, 2400);
    tester.view.devicePixelRatio = 2;
    addTearDown(tester.view.reset);
    final env = (await tester.runAsync(_Env.create))!;
    await tester.runAsync(env.seedTasks);
    await tester.pumpWidget(
      _app(
        env,
        AppState(
          const AuthView(AuthStage.unlocked),
          services: env.s,
          kn: KnContextState(KnContextStage.ready, context: _ctx()),
        ),
        home: const KnHomeScreen(),
      ),
    );
    await settle(tester);
    expect(find.text('Công việc cần làm'), findsOneWidget);
    expect(
      find.descendant(
        of: find.byKey(const ValueKey('kn-task-action-$taskA2')),
        matching: find.text('Nhập'),
      ),
      findsOneWidget,
    );
    expect(
      find.descendant(
        of: find.byKey(const ValueKey('kn-task-action-$taskClosed')),
        matching: find.text('Xem'),
      ),
      findsOneWidget,
    );
    await tester.tap(find.byKey(const ValueKey('kn-task-action-$taskA2')));
    await settle(tester);
    expect(find.byType(KnReportEntryScreen), findsOneWidget);
    await _finish(tester, env);
  });

  testWidgets('FN-XA-03.03 danh sách HTX: SĐT che; thêm HTX kiểm tên', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(720, 1600);
    tester.view.devicePixelRatio = 2;
    addTearDown(tester.view.reset);
    final env = (await tester.runAsync(_Env.create))!;
    env.kn.extraGets['/v1/kn/cooperatives'] = {
      'items': [
        {
          'ref': 'c1',
          'name': 'HTX Mỹ Hòa',
          'commune_code': xaA,
          'area_ha': '12.5',
          'household_count': 4,
          'active': true,
          'representative': {
            'label': 'Nguyễn V. A',
            'masked_phone': '0912•••123',
          },
        },
      ],
      'next_cursor': null,
    };
    await tester.pumpWidget(
      _app(
        env,
        AppState(
          const AuthView(AuthStage.unlocked),
          services: env.s,
          kn: KnContextState(KnContextStage.ready, context: _ctx()),
        ),
        home: const KnCooperativesScreen(),
      ),
    );
    await settle(tester);
    expect(find.text('HTX Mỹ Hòa'), findsOneWidget);
    expect(find.textContaining('0912•••123'), findsOneWidget);
    expect(find.textContaining('0912345'), findsNothing);
    await tester.tap(find.byKey(const ValueKey('kn-coop-add')));
    await settle(tester);
    await tester.tap(find.byKey(const ValueKey('kn-coop-f-save')));
    await settle(tester);
    expect(find.text('Tên HTX tối thiểu 3 ký tự.'), findsOneWidget);
    await tester.enterText(
      find.byKey(const ValueKey('kn-coop-f-name')),
      'HTX Tân Lập',
    );
    await tester.tap(find.byKey(const ValueKey('kn-coop-f-commune')));
    await settle(tester);
    await tester.tap(find.text('Xã B').last);
    await settle(tester);
    await tester.tap(find.byKey(const ValueKey('kn-coop-f-save')));
    await settle(tester);
    expect(env.server.actionCalls.single.$1, '/v1/kn/cooperatives');
    await _finish(tester, env);
  });
}

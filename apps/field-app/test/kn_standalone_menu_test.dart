import 'dart:convert';

import 'package:agrigreen_field/application/kn_menu_service.dart';
import 'package:agrigreen_field/application/kn_report_service.dart';
import 'package:agrigreen_field/application/cnts_period_service.dart';
import 'package:agrigreen_field/domain/cnts_period.dart';
import 'package:agrigreen_field/domain/kn_report.dart';
import 'package:agrigreen_field/domain/kn_context.dart';
import 'package:agrigreen_field/ui/kn/kn_home_screen.dart';
import 'package:agrigreen_field/ui/kn/kn_providers.dart';
import 'package:agrigreen_field/ui/kn/kn_tasks_screen.dart';
import 'package:agrigreen_field/ui/providers.dart';
import 'package:agrigreen_mobile/application/auth_flow.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/harness.dart';
import 'support/kn_fixtures.dart';

class _Selected extends AppController {
  @override
  AppState build() => const AppState(AuthView(AuthStage.unlocked));
}

void main() {
  late Harness h;
  late KnMenuService service;
  late Map<String, Object?> response;
  String? context;
  Object? failure;
  String department = 'trong_trot';

  Future<void> cache(Object value) async {
    await h.db.db.rawInsert(
      'INSERT OR REPLACE INTO catalog_cache (kind,key,json,fetched_at) VALUES (?,?,?,?)',
      [
        'kn.menu',
        'menu',
        jsonEncode(value),
        h.clock.now().millisecondsSinceEpoch,
      ],
    );
  }

  setUp(() async {
    h = await Harness.create();
    context = 'selected-context';
    failure = null;
    department = 'trong_trot';
    response = menuJson();
    h.fake.onGet = (path, query) async {
      if (failure != null) throw failure!;
      if (path == '/v1/menu') return response;
      return <String, Object?>{};
    };
    service = KnMenuService(
      db: h.db,
      api: h.server,
      clock: h.clock,
      context: () async => context == null
          ? null
          : KnSessionContext(
              contextId: context!,
              role: 'kn_xa',
              departmentCode: department,
              orgName: 'TTKN',
              communes: const [],
              defaultRoute: '/kn/nhiem-vu/cong-viec',
              expiresAt: null,
            ),
      localTodoCount: () async => 0,
    );
  });
  tearDown(() async => h.dispose());

  test(
    'S4 no-cache and missing context never grant default TT tasks',
    () async {
      failure = NetworkException('offline');
      for (final selected in ['TT', 'CNTS', 'KHTC', 'TTTT', 'HCTH', null]) {
        context = selected;
        expect((await service.load()).entries, isEmpty);
      }
      failure = null;
      context = null;
      expect((await service.load()).entries, isEmpty);
    },
  );

  test('S4 legacy cache cannot grant functions or wipe offline data', () async {
    await h.refs.applyPage(
      PullPage(
        changes: [
          change(KnEntity.reportTask, taskA2, 1, taskData(id: taskA2)),
          change(KnEntity.formLayout, ver1, 2, layoutData()),
        ],
        nextCursor: 'keep-cursor',
        hasMore: false,
      ),
    );
    final queued = await h.outbox.enqueue(
      entity: PushEntity.reportSubmission,
      payload: {'task_id': taskA2, 'kind': 'draft'},
    );
    final old = {...response, 'menu_version': 'kn.menu/v16'};
    failure = NetworkException('offline');
    // Old unbound caches and old catalogs with a binding both fail closed.
    for (final value in [
      old,
      {'context_id': context, 'menu': old},
    ]) {
      await cache(value);
      final before = await h.db.db.query('catalog_cache');
      expect((await service.load()).entries, isEmpty);
      expect(await h.db.db.query('catalog_cache'), before);
      expect(await h.refs.count(KnEntity.reportTask), 1);
      expect(await h.refs.count(KnEntity.formLayout), 1);
      final after = await h.outbox.list();
      expect(after.single.id, queued.id);
      expect(after.single.payload, queued.payload);
      expect(after.single.status, OutboxStatus.pending);
    }
  });

  test(
    'S4 current TT catalog stays usable offline only in its context',
    () async {
      final online = await service.load();
      expect(online.has(KnMenuKey.congViec), isTrue);
      expect(online.fromCache, isFalse);
      failure = NetworkException('offline');
      final offline = await service.load();
      expect(offline.has(KnMenuKey.congViec), isTrue);
      expect(offline.fromCache, isTrue);
      context = 'another-context';
      expect((await service.load()).entries, isEmpty);
      context = 'selected-context';
      expect((await service.load()).has(KnMenuKey.congViec), isTrue);
    },
  );

  test(
    'S4 current CNTS catalog keeps its own function without TT fallback',
    () async {
      response = {
        'menu_version': 'kn.menu/v17',
        'groups': [
          {
            'key': 'business',
            'items': [
              {
                'key': KnMenuKey.cntsBaoCaoKy,
                'label': 'Biểu kỳ CNTS',
                'enabled': true,
              },
            ],
          },
        ],
      };
      final online = await service.load();
      expect(online.entries.single.key, KnMenuKey.cntsBaoCaoKy);
      expect(online.has(KnMenuKey.congViec), isFalse);
      failure = NetworkException('offline');
      expect((await service.load()).entries.single.key, KnMenuKey.cntsBaoCaoKy);
    },
  );

  test(
    'S4 server denial and an old online catalog never reuse TT cache',
    () async {
      await service.load();
      failure = ApiException(403, 'forbidden');
      expect((await service.load()).entries, isEmpty);
      failure = null;
      response = {...response, 'menu_version': 'kn.menu/v16'};
      expect((await service.load()).entries, isEmpty);
    },
  );

  test(
    'S4 a context switch during menu fetch cannot bind the previous scope',
    () async {
      h.fake.onGet = (path, query) async {
        context = 'new-context';
        department = 'chan_nuoi_thuy_san';
        return response;
      };
      expect((await service.load()).entries, isEmpty);
      expect(await h.db.db.query('catalog_cache'), isEmpty);
    },
  );

  test('S4 recovering the same scope binds the new server context', () async {
    h.fake.onGet = (path, query) async {
      context = 'recovered-context';
      return response;
    };
    expect((await service.load()).has(KnMenuKey.congViec), isTrue);
    failure = NetworkException('offline');
    h.fake.onGet = (path, query) async => throw failure!;
    expect((await service.load()).has(KnMenuKey.congViec), isTrue);
  });

  test(
    'Q4 legacy TT ad hoc records stay readable and cannot queue new edits',
    () async {
      final reports = KnReportService(
        db: h.db,
        refs: h.refs,
        outbox: h.outbox,
        api: h.server,
        clock: h.clock,
      );
      var version = 0;
      for (final reportType in ['dot_xuat', 'adhoc']) {
        version++;
        await h.refs.applyPage(
          PullPage(
            changes: [
              change(KnEntity.formLayout, ver1, version, layoutData()),
              change(KnEntity.reportTask, taskA2, version, {
                ...taskData(id: taskA2),
                'report_type': reportType,
                'reopened_until': h.clock
                    .now()
                    .add(const Duration(days: 1))
                    .toIso8601String(),
              }),
            ],
            nextCursor: 'history-$version',
            hasMore: false,
          ),
        );
        await reports.saveWork(taskA2, {'$xaA/E': '7'});
        final before = await h.db.db.query(
          'catalog_cache',
          where: 'kind = ?',
          whereArgs: ['kn.report_work'],
        );
        final task = (await reports.task(taskA2))!;
        final entry = (await reports.entry(task))!;
        expect(task.reportType, reportType);
        expect(task.openAt(h.clock.now()), isTrue);
        expect(task.actionAt(h.clock.now()), 'Xem');
        expect(task.pendingAt(h.clock.now()), isFalse);
        expect(entry.readOnly, isTrue);
        expect(entry.layout.columns.any(entry.editable), isFalse);
        await expectLater(
          reports.queue(entry, entry.values, submit: false),
          throwsA(isA<KnValidationException>()),
        );
        expect(await h.outbox.list(), isEmpty);
        expect(
          await h.db.db.query(
            'catalog_cache',
            where: 'kind = ?',
            whereArgs: ['kn.report_work'],
          ),
          before,
        );
        expect(await h.refs.count(KnEntity.reportTask), 1);
        expect(await h.refs.count(KnEntity.formLayout), 1);
      }
    },
  );

  test(
    'Q4 native CNTS ad hoc forms keep their separate submission transport',
    () async {
      final native = CntsPeriodService(
        db: h.db,
        refs: h.refs,
        outbox: h.outbox,
        clock: h.clock,
      );
      final data = <String, Object?>{
        'id': taskA2,
        'profile': 'kn_cnts',
        'period_id': period1,
        'unit': xaA,
        'unit_kind': 'commune',
        'parent': xaA,
        'template_code': CntsForms.bm17,
        'period_kind': 'adhoc',
        'period_status': 'open',
        'open': true,
        'status': 'not_submitted',
        'due_at': h.clock.now().add(const Duration(days: 1)).toIso8601String(),
        'fields': [
          {
            'key': 'quantity',
            'type': 'decimal',
            'required': true,
            'non_negative': true,
          },
        ],
      };
      await h.refs.applyPage(
        PullPage(
          changes: [change(FormsEntity.task, taskA2, 1, data)],
          nextCursor: 'native',
          hasMore: false,
        ),
      );
      final task = (await native.task(taskA2))!;
      final record = await native.submit(
        task,
        FormsWork(values: {'quantity': '1'}),
      );
      expect(record.entity, PushEntity.formsSubmission);
      expect(record.payload['profile'], 'kn_cnts');
      expect((await h.outbox.list()).single.id, record.id);
    },
  );

  test('Q4 retired ad hoc key is absent even when marked coming soon', () {
    for (final soon in [false, true]) {
      final menu = KnMenu.fromJson({
        'menu_version': 'kn.menu/v17',
        'groups': [
          {
            'items': [
              {
                'key': 'xa.bao_cao.dot_xuat',
                'label': 'Báo cáo đột xuất',
                'enabled': true,
                if (soon) 'status': 'coming_soon',
              },
            ],
          },
        ],
      });
      expect(menu.entries, isEmpty);
      expect(
        knScreenFor(
          const KnMenuEntry(
            key: 'xa.bao_cao.dot_xuat',
            label: 'Retired',
            group: 'Reports',
          ),
        ),
        isNull,
      );
    }
  });

  for (final enabled in [null, false]) {
    testWidgets(
      'S4 home does not mount TT tasks without an enabled menu leaf: $enabled',
      (tester) async {
        final menu = KnMenu(
          enabled == null
              ? const [
                  KnMenuEntry(
                    key: KnMenuKey.cntsBaoCaoKy,
                    label: 'Biểu kỳ CNTS',
                    group: 'Nghiệp vụ',
                  ),
                ]
              : const [
                  KnMenuEntry(
                    key: KnMenuKey.congViec,
                    label: 'Công việc cần làm',
                    group: 'Điều hành',
                    enabled: false,
                  ),
                ],
          version: 'kn.menu/v17',
        );
        await tester.pumpWidget(
          ProviderScope(
            overrides: [
              appControllerProvider.overrideWith(_Selected.new),
              knMenuProvider.overrideWith((ref) async => menu),
              knBadgesProvider.overrideWith((ref) async => <String, int>{}),
            ],
            child: const MaterialApp(home: KnHomeScreen()),
          ),
        );
        await tester.pump();
        await tester.pump();
        expect(find.byType(KnTasksView), findsNothing);
        expect(find.text('Khuyến nông'), findsOneWidget);
        expect(tester.takeException(), isNull);
      },
    );
  }
}

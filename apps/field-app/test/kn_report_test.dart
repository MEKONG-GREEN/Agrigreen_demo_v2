import 'package:agrigreen_field/application/kn_report_service.dart';
import 'package:agrigreen_field/domain/kn_report.dart';
import 'package:agrigreen_mobile/data/outbox/outbox_repository.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/harness.dart';
import 'support/kn_fixtures.dart';

const _taskAdj = '01920000-0000-7000-8000-0000000e00dd';

Future<void> seed(Harness h) => h.refs.applyPage(
  PullPage(
    changes: [
      change(KnEntity.formLayout, ver1, 1, layoutData()),
      change(KnEntity.reportTask, taskA2, 2, taskData(id: taskA2)),
      change(
        KnEntity.reportTask,
        taskB2,
        3,
        taskData(
          id: taskB2,
          commune: xaB,
          communeName: 'Xã B',
          status: 'draft',
          due: DateTime.utc(2026, 12, 1),
          latestVersion: 1,
        ),
      ),
      change(
        KnEntity.reportTask,
        taskA1,
        4,
        taskData(
          id: taskA1,
          period: period1,
          slot: 1,
          status: 'submitted',
          due: DateTime.utc(2026, 11, 25),
          latestVersion: 1,
          latestSubmissionId: 'sub-a1',
        ),
      ),
      change(
        KnEntity.reportSubmission,
        taskA1,
        4,
        submissionData(
          taskId: taskA1,
          submissionId: 'sub-a1',
          input: const {'D': '12', 'H': '5'},
          cells: const {'G': '12'},
        ),
      ),
      change(
        KnEntity.reportTask,
        taskClosed,
        5,
        taskData(
          id: taskClosed,
          formCode: 'T04',
          closed: true,
          due: DateTime.utc(2026, 11, 20),
        ),
      ),
      change(
        KnEntity.reportTask,
        _taskAdj,
        6,
        taskData(
          id: _taskAdj,
          commune: xaB,
          communeName: 'Xã B',
          formCode: 'M01',
          periodType: 'month',
          status: 'adjustment_requested',
          adjustmentReason: 'Số liệu tuần 37 chưa khớp',
          due: DateTime.utc(2026, 12, 10),
        ),
      ),
    ],
    nextCursor: 'c1',
    hasMore: false,
  ),
);

void main() {
  late Harness h;
  late KnReportService svc;

  setUp(() async {
    h = await Harness.create(); // 2026-12-02 01:00 UTC
    svc = KnReportService(
      db: h.db,
      refs: h.refs,
      outbox: h.outbox,
      api: h.server,
      clock: h.clock,
    );
    await seed(h);
  });
  tearDown(() => h.dispose());

  test(
    'FN-XA-03.01 công việc cần làm: đếm, Trễ hạn, sắp theo hạn, nút, bộ lọc',
    () async {
      final l = await svc.list();
      expect(l.total, 5);
      expect(l.todo, 3);
      expect(l.overdue, 1);
      expect(l.adjustment, 1);
      expect(
        [for (final r in l.rows) r.task.id],
        [taskClosed, taskA1, taskB2, taskA2, _taskAdj],
      );
      expect(
        [for (final r in l.rows) r.display],
        [
          KnTaskStatus.closed,
          KnTaskStatus.submitted,
          KnTaskStatus.overdue,
          KnTaskStatus.notStarted,
          KnTaskStatus.adjustmentRequested,
        ],
      );
      expect(
        [for (final r in l.rows) r.action],
        ['Xem', 'Sửa', 'Tiếp tục', 'Nhập', 'Sửa'],
      );
      expect(l.rows.last.task.adjustmentReason, 'Số liệu tuần 37 chưa khớp');
      Future<List<String>> ids(KnTaskFilter f) async => [
        for (final r in (await svc.list(f)).rows) r.task.id,
      ];
      expect(await ids(const KnTaskFilter(commune: xaB)), [taskB2, _taskAdj]);
      expect(await ids(const KnTaskFilter(status: KnTaskStatus.overdue)), [
        taskB2,
      ]);
      expect(await ids(const KnTaskFilter(periodType: 'month')), [_taskAdj]);
      expect(await ids(const KnTaskFilter(formCode: 'T04')), [taskClosed]);
      // Trễ hạn vẫn nhập được đến khi chốt kỳ.
      final b2 = (await svc.task(taskB2))!;
      expect(b2.openAt(svc.now), isTrue);
    },
  );

  test(
    'FN-XA-03.02 tab xã: cùng biểu + cùng kỳ trên các xã phụ trách',
    () async {
      final tabs = await svc.tabsFor((await svc.task(taskA2))!);
      expect([for (final t in tabs) t.communeName], ['Xã A', 'Xã B']);
    },
  );

  test(
    'FN-XA-03.02 bố cục: nhãn kỳ + vụ, khe vượt số kỳ inactive (lazy)',
    () async {
      final t = (await svc.task(taskA2))!;
      final l = (await svc.layoutFor(t))!;
      expect(l.title, 'TIẾN ĐỘ XUỐNG GIỐNG THU ĐÔNG 2026');
      expect(l.sheetName, 'PL 01_Tđộ XG THU ĐÔNG 2026');
      expect(l.column('D')!.title, 'Tuần 36');
      expect(l.column('E')!.title, 'Tuần 37');
      expect(l.column('F')!.inactive, isTrue);
      expect(identical(await svc.layoutFor(t), l), isTrue);
      expect(l.identifying.map((c) => c.letter), ['A', 'B']);
      expect(l.columnsOf(l.groups.single).map((c) => c.letter), [
        'D',
        'E',
        'F',
        'G',
      ]);
    },
  );

  test('FN-XA-03.02 chỉ khe kỳ đang mở sửa được; kỳ khác lấy từ công việc cùng đợt; công thức "chưa tính"', () async {
    final e = (await svc.entry((await svc.task(taskA2))!))!;
    final r = e.rows.single;
    final ed = {
      for (final c in e.layout.columns)
        if (e.editable(c)) c.letter,
    };
    expect(ed, {'E', 'H'});
    expect(e.role(e.layout.column('D')!), KnCellRole.otherPeriod);
    expect(e.displayValue(r, e.layout.column('A')!), '1');
    expect(e.displayValue(r, e.layout.column('B')!), 'Xã A');
    expect(e.displayValue(r, e.layout.column('C')!), '120');
    // Tuần 36 (khe 1) — từ bài nộp của công việc khe 1 cùng xã.
    expect(e.displayValue(r, e.layout.column('D')!), '12');
    // Chưa có bài nộp đã tính → null (màn hình ghi "chưa tính").
    expect(e.displayValue(r, e.layout.column('G')!), isNull);
  });

  test(
    'FN-XA-03.02 kiểm trên máy: bắt buộc khi gửi, số nguyên, min/max',
    () async {
      final e = (await svc.entry((await svc.task(taskA2))!))!;
      final k = knCellKey(xaA, 'H');
      final errs = svc.validate(e, {knCellKey(xaA, 'E'): '-1'}, submit: true);
      expect(errs[k], 'Bắt buộc nhập');
      expect(errs[knCellKey(xaA, 'E')], 'Tối thiểu 0');
      expect(svc.validate(e, {k: '5.5'}, submit: false)[k], 'Nhập số nguyên');
      expect(svc.validate(e, {k: '101'}, submit: false)[k], 'Tối đa 100');
      expect(svc.validate(e, const {}, submit: false), isEmpty);
      await expectLater(
        svc.queue(e, {k: ''}, submit: true),
        throwsA(isA<KnValidationException>()),
      );
      expect(await h.outbox.list(), isEmpty);
    },
  );

  test('FN-XA-03.02 nhập ngoại tuyến → hàng đợi → push accepted → pull ô công thức đã tính', () async {
    final t = (await svc.task(taskA2))!;
    final e = (await svc.entry(t))!;
    final values = {knCellKey(xaA, 'E'): '3,5', knCellKey(xaA, 'H'): '7'};
    await svc.saveWork(t.id, values); // tự lưu khi chuyển tab
    expect(await svc.isDirty(t), isTrue);
    final rec = await svc.queue(e, values, submit: true);
    expect(rec.entity, PushEntity.reportSubmission);
    expect(rec.op, RecordOp.create);
    expect(rec.payload, {
      'task_id': taskA2,
      'kind': 'submitted',
      'rows': [
        {
          'input': {'E': '3.5', 'H': '7'},
        },
      ],
      'base_version': 0,
    });
    // Trước đồng bộ: trạng thái cục bộ "Đã nộp", "Chờ đồng bộ", công thức chưa tính.
    final before = (await svc.list()).rows.firstWhere(
      (r) => r.task.id == taskA2,
    );
    expect(before.display, KnTaskStatus.submitted);
    expect(before.sync.state, KnSyncState.pending);
    final pending = (await svc.entry(t))!;
    expect(pending.formulaPending, isTrue);
    expect(pending.values[knCellKey(xaA, 'E')], '3.5');

    // Máy chủ nhận rồi tính công thức; lượt pull mang bài nộp đã tính.
    h.fake.pages['c1'] = PullPage(
      changes: [
        change(
          KnEntity.reportTask,
          taskA2,
          10,
          taskData(
            id: taskA2,
            status: 'submitted',
            latestVersion: 1,
            latestSubmissionId: rec.id,
          ),
        ),
        change(
          KnEntity.reportSubmission,
          taskA2,
          10,
          submissionData(
            taskId: taskA2,
            submissionId: rec.id,
            input: const {'E': '3.5', 'H': '7'},
            cells: const {'G': '15.5'},
          ),
        ),
      ],
      nextCursor: 'c2',
      hasMore: false,
    );
    await h.sync.syncOnce(
      force: true,
      entities: KnReportService.focusedEntities,
    );
    expect(h.fake.pullEntities.last, ['report_submission', 'report_task']);
    final after = (await svc.entry((await svc.task(taskA2))!))!;
    expect(after.sync.state, KnSyncState.accepted);
    expect(after.formulaPending, isFalse);
    expect(
      after.displayValue(after.rows.single, after.layout.column('G')!),
      '15.5',
    );
    expect(after.values[knCellKey(xaA, 'H')], '7');
  });

  test(
    'FN-XA-03.02 xung đột: bản của máy lưu thành phiên bản mới, hiện cảnh báo',
    () async {
      final t = (await svc.task(taskB2))!;
      final e = (await svc.entry(t))!;
      h.fake.conflictIf = (w) => w['entity'] == PushEntity.reportSubmission;
      await svc.queue(e, {knCellKey(xaB, 'H'): '4'}, submit: false);
      expect((await h.outbox.list()).single.payload['base_version'], 1);
      await h.sync.syncOnce(force: true);
      final st = await svc.recordStatus(taskB2);
      expect(st.state, KnSyncState.conflict);
      expect(st.label, contains('bản mới hơn ở nơi khác'));
    },
  );

  test(
    'FN-XA-03.02 bị từ chối: lỗi tại ô map về trường rows.<row_key>.<chữ cái>',
    () async {
      final t = (await svc.task(taskA2))!;
      final e = (await svc.entry(t))!;
      h.fake.resultFor = (w) => PushResult(
        id: w['id']! as String,
        status: 'rejected',
        code: 'extension.submission_invalid',
        errors: const [
          {
            'field': 'rows.30280.E',
            'reason': 'Tổng các trà ≠ số đã xuống giống',
          },
        ],
      );
      await svc.queue(e, {
        knCellKey(xaA, 'E'): '9',
        knCellKey(xaA, 'H'): '1',
      }, submit: true);
      await h.sync.syncOnce(force: true);
      final r = (await svc.entry(t))!;
      expect(r.sync.state, KnSyncState.rejected);
      expect(r.sync.label, contains('vi phạm yêu cầu của mẫu'));
      expect(
        r.errorAt(r.rows.single, r.layout.column('E')!),
        'Tổng các trà ≠ số đã xuống giống',
      );
      // Giá trị của bản bị từ chối giữ trên máy để sửa và gửi lại.
      expect(r.values[knCellKey(xaA, 'E')], '9');
      expect(r.formulaPending, isTrue);
    },
  );

  test('FN-XA-03.02 kỳ đã chốt → chỉ xem, không xếp hàng được', () async {
    final t = (await svc.task(taskClosed))!;
    final e = (await svc.entry(t))!;
    expect(e.readOnly, isTrue);
    expect(e.layout.columns.where(e.editable), isEmpty);
    await expectLater(
      svc.queue(e, const {}, submit: false),
      throwsA(isA<KnValidationException>()),
    );
  });

  test('FN-XA-03.02 kỳ mở lại cho xã: nhập được tới reopened_until', () async {
    final k = KnReportTask({
      ...taskData(id: 'x', closed: true),
      'reopened_until': '2026-12-02T05:00:00Z',
      'open_slots': [2],
    });
    expect(k.openAt(DateTime.utc(2026, 12, 2, 4)), isTrue);
    expect(k.openAt(DateTime.utc(2026, 12, 2, 6)), isFalse);
    expect(k.displayAt(DateTime.utc(2026, 12, 2, 6)), KnTaskStatus.closed);
  });

  test('FN-XA-03.02 lịch sử sửa (trực tuyến)', () async {
    h.fake.getResponses['/v1/extension/report-tasks/$taskA1/history'] = {
      'items': [
        {'version_no': 1, 'kind': 'submitted', 'actor_display': 'Cán bộ A'},
      ],
    };
    final items = await svc.history(taskA1);
    expect(items.single['version_no'], 1);
  });

  test('FN-XA-03.02 lỗi tại ô: tách khóa dòng chứa chỉ tiêu', () {
    expect(
      knFieldErrors(const [
        {'field': 'rows.30280/rice_brown_planthopper.F', 'reason': 'x'},
        {'field': 'payload.kind', 'reason': 'y'},
      ]),
      {'30280/rice_brown_planthopper.F': 'x', 'payload.kind': 'y'},
    );
  });
}

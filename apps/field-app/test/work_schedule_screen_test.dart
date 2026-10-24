import 'package:agrigreen_field/application/work_schedule_service.dart';
import 'package:agrigreen_field/domain/work_schedule.dart';
import 'package:agrigreen_field/ui/screens/work_schedule_screen.dart';
import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

final _week = DateTime.utc(2026, 10, 5);

/// Dịch vụ giả trong bộ nhớ (màn hình chỉ phụ thuộc giao diện công khai).
class _FakeSchedules implements WorkScheduleService {
  final items = <WsItem>[];
  WsStatus? status;
  String? syncError;
  bool offline = false;
  int discards = 0;
  final saves = <bool>[];
  var _n = 0;

  WeekSchedule _view(DateTime week) => WeekSchedule(
    weekStart: week,
    submitDueAt: defaultSubmitDue(week),
    locked: false,
    lateNow: false,
    status: status,
    scheduleId: status == null ? null : '01920000-0000-7000-8000-00000000aaaa',
    items: markOverlaps(sortItems(items)),
    hasLocalDraft: syncError != null,
    dirty: syncError != null,
    syncError: syncError,
    offline: offline,
    communeCodes: const ['30280'],
  );

  @override
  DateTime defaultWeek() => _week;

  @override
  String newItemId() =>
      '01920000-0000-7000-8000-${(++_n).toString().padLeft(12, '0')}';

  @override
  Future<WeekSchedule> load(DateTime week, {bool refresh = true}) async =>
      _view(week);

  @override
  Future<WeekSchedule> upsertItem(DateTime week, WsItem item) async {
    validateItem(item, week);
    items
      ..removeWhere((i) => i.id == item.id)
      ..add(item);
    return _view(week);
  }

  @override
  Future<WeekSchedule> removeItem(DateTime week, String itemId) async {
    items.removeWhere((i) => i.id == itemId);
    return _view(week);
  }

  @override
  Future<WeekSchedule> save(DateTime week, {required bool submit}) async {
    saves.add(submit);
    status = submit ? WsStatus.pending : WsStatus.draft;
    return _view(week);
  }

  @override
  Future<WeekSchedule> discardLocal(DateTime week) async {
    discards++;
    syncError = null;
    return _view(week);
  }

  @override
  Future<List<WsLogEntry>> log(String scheduleId) async => [
    WsLogEntry(
      event: 'submitted',
      at: DateTime.utc(2026, 10, 2, 2),
      actor: 'Nguyễn Văn An',
    ),
  ];

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

Future<void> _pump(WidgetTester tester, _FakeSchedules fake) async {
  tester.view.physicalSize = const Size(720, 1600);
  tester.view.devicePixelRatio = 2;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(
    ProviderScope(
      overrides: [workScheduleServiceProvider.overrideWithValue(fake)],
      child: MaterialApp(theme: buildTheme(), home: const WorkScheduleScreen()),
    ),
  );
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('đăng ký lịch tuần: thêm mục, kiểm lỗi, gửi duyệt, xem log', (
    tester,
  ) async {
    final fake = _FakeSchedules();
    await _pump(tester, fake);

    expect(find.text('Lịch công tác'), findsOneWidget);
    expect(find.text('Tuần 05/10 – 11/10/2026'), findsOneWidget);
    expect(find.text('Chưa đăng ký'), findsOneWidget);
    expect(find.text('Hạn đăng ký: Thứ Sáu 02/10 17:00'), findsOneWidget);
    expect(find.text('Thứ Hai 05/10'), findsOneWidget);
    expect(find.text('Sao chép tuần trước'), findsOneWidget);

    // Thêm mục Sáng Thứ Hai.
    await tester.tap(find.byKey(const ValueKey('ws-add-2026-10-05-morning')));
    await tester.pumpAndSettle();
    expect(find.text('Thêm mục công tác'), findsOneWidget);
    await tester.enterText(find.byKey(const ValueKey('ws-f-content')), 'ab');
    await tester.tap(find.byKey(const ValueKey('ws-f-save')));
    await tester.pumpAndSettle();
    expect(find.text('Nội dung công việc tối thiểu 3 ký tự.'), findsOneWidget);

    await tester.enterText(
      find.byKey(const ValueKey('ws-f-content')),
      'Kiểm tra rầy nâu HTX Mỹ Hòa',
    );
    await tester.enterText(find.byKey(const ValueKey('ws-f-start')), '07:30');
    await tester.enterText(find.byKey(const ValueKey('ws-f-end')), '11:00');
    // Log bug TT-05/CN-04: Địa điểm bắt buộc.
    await tester.ensureVisible(find.byKey(const ValueKey('ws-f-save')));
    await tester.tap(find.byKey(const ValueKey('ws-f-save')));
    await tester.pumpAndSettle();
    expect(
      find.text('Địa điểm bắt buộc — nhập địa điểm hoặc chọn vị trí.'),
      findsOneWidget,
    );
    await tester.enterText(
      find.byKey(const ValueKey('ws-f-location')),
      'Cánh đồng ấp 1',
    );
    await tester.enterText(
      find.byKey(const ValueKey('ws-f-participants')),
      'Cán bộ HTX 0912345678',
    );
    await tester.ensureVisible(find.byKey(const ValueKey('ws-f-save')));
    await tester.tap(find.byKey(const ValueKey('ws-f-save')));
    await tester.pumpAndSettle();
    expect(
      find.text(
        'Thành phần phối hợp không được ghi số điện thoại hoặc số CCCD.',
      ),
      findsOneWidget,
    );
    await tester.enterText(
      find.byKey(const ValueKey('ws-f-participants')),
      'Cán bộ HTX, BVTV',
    );
    await tester.ensureVisible(find.byKey(const ValueKey('ws-f-save')));
    await tester.tap(find.byKey(const ValueKey('ws-f-save')));
    await tester.pumpAndSettle();

    expect(fake.items, hasLength(1));
    expect(find.text('Kiểm tra rầy nâu HTX Mỹ Hòa'), findsOneWidget);
    expect(find.textContaining('Sáng 07:30–11:00'), findsOneWidget);

    // Mục thứ hai trùng giờ → cảnh báo, không chặn.
    ScaffoldMessenger.of(tester.element(find.byType(WorkScheduleScreen)))
        .clearSnackBars();
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const ValueKey('ws-add-2026-10-05-morning')));
    await tester.pumpAndSettle();
    await tester.enterText(
      find.byKey(const ValueKey('ws-f-content')),
      'Họp tổ khuyến nông',
    );
    await tester.enterText(find.byKey(const ValueKey('ws-f-start')), '10:00');
    await tester.enterText(
      find.byKey(const ValueKey('ws-f-location')),
      'Trụ sở UBND xã',
    );
    await tester.ensureVisible(find.byKey(const ValueKey('ws-f-save')));
    await tester.tap(find.byKey(const ValueKey('ws-f-save')));
    await tester.pumpAndSettle();
    expect(fake.items, hasLength(2));
    expect(find.text('Cảnh báo: 2 mục trùng thời gian.'), findsOneWidget);

    await tester.tap(find.byKey(const ValueKey('ws-submit')));
    await tester.pumpAndSettle();
    expect(fake.saves, [true]);
    expect(find.text('Chờ duyệt'), findsOneWidget);

    ScaffoldMessenger.of(tester.element(find.byType(WorkScheduleScreen)))
        .clearSnackBars();
    await tester.pumpAndSettle();
    await tester.scrollUntilVisible(
      find.byKey(const ValueKey('ws-log')),
      300,
      scrollable: find.byType(Scrollable).first,
    );
    await tester.ensureVisible(find.byKey(const ValueKey('ws-log')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const ValueKey('ws-log')));
    await tester.pumpAndSettle();
    expect(find.text('Lịch sử'), findsOneWidget);
    expect(find.textContaining('Nguyễn Văn An'), findsOneWidget);
  });

  testWidgets(
    'R-B1 (review #99): cán bộ xã luôn thấy ô Xã/phường (bắt buộc), kể cả chưa có danh sách xã',
    (tester) async {
      _EditResultProbe? popped;
      Future<void> open(List<String> communes) async {
        await tester.pumpWidget(
          MaterialApp(
            key: ValueKey(communes.length),
            theme: buildTheme(),
            home: Builder(
              builder: (ctx) => TextButton(
                onPressed: () async {
                  final r = await Navigator.of(ctx).push<Object?>(
                    MaterialPageRoute(
                      builder: (_) => WorkScheduleItemScreen(
                        weekStart: _week,
                        day: _week,
                        newId: () => '01920000-0000-7000-8000-000000000099',
                        communeCodes: communes,
                        requireCommune: true,
                      ),
                    ),
                  );
                  popped = r == null ? null : _EditResultProbe();
                },
                child: const Text('mở'),
              ),
            ),
          ),
        );
        await tester.tap(find.text('mở'));
        await tester.pumpAndSettle();
      }

      tester.view.physicalSize = const Size(720, 1600);
      tester.view.devicePixelRatio = 2;
      addTearDown(tester.view.reset);

      // Chưa tải được danh sách xã: ô vẫn hiện, bắt buộc, không cho lưu.
      await open(const []);
      expect(find.byKey(const ValueKey('ws-f-commune')), findsOneWidget);
      expect(find.text('Xã/phường *'), findsOneWidget);
      await tester.enterText(
        find.byKey(const ValueKey('ws-f-content')),
        'Kiểm tra rầy nâu',
      );
      await tester.enterText(
        find.byKey(const ValueKey('ws-f-location')),
        'Cánh đồng ấp 1',
      );
      await tester.ensureVisible(find.byKey(const ValueKey('ws-f-save')));
      await tester.tap(find.byKey(const ValueKey('ws-f-save')));
      await tester.pumpAndSettle();
      expect(find.text('Xã/phường bắt buộc.'), findsOneWidget);
      expect(popped, isNull);

      // Một xã phụ trách: chọn sẵn, lưu được.
      await open(const ['30280']);
      await tester.enterText(
        find.byKey(const ValueKey('ws-f-content')),
        'Kiểm tra rầy nâu',
      );
      await tester.enterText(
        find.byKey(const ValueKey('ws-f-location')),
        'Cánh đồng ấp 1',
      );
      await tester.ensureVisible(find.byKey(const ValueKey('ws-f-save')));
      await tester.tap(find.byKey(const ValueKey('ws-f-save')));
      await tester.pumpAndSettle();
      expect(popped, isNotNull);
    },
  );

  testWidgets('xung đột phiên bản: báo tải lại, nút Tải lại bỏ bản trên máy', (
    tester,
  ) async {
    final fake = _FakeSchedules()
      ..status = WsStatus.draft
      ..syncError = WorkScheduleService.versionConflictCode
      ..offline = true
      ..items.add(
        WsItem(
          id: '01920000-0000-7000-8000-000000000009',
          day: _week,
          session: WsSession.afternoon,
          content: 'Nhập báo cáo tuần',
        ),
      );
    await _pump(tester, fake);
    expect(find.textContaining('Đang ngoại tuyến'), findsOneWidget);
    expect(
      find.text('Lịch đã thay đổi trên máy chủ — tải lại'),
      findsOneWidget,
    );
    await tester.scrollUntilVisible(
      find.byKey(const ValueKey('ws-submit')),
      200,
      scrollable: find.byType(Scrollable).first,
    );
    final submit = tester.widget<ButtonStyleButton>(
      find.byKey(const ValueKey('ws-submit')),
    );
    expect(submit.onPressed, isNull);

    await tester.tap(find.byKey(const ValueKey('ws-reload')));
    await tester.pumpAndSettle();
    expect(fake.discards, 1);
    expect(find.text('Lịch đã thay đổi trên máy chủ — tải lại'), findsNothing);
  });
}

class _EditResultProbe {}

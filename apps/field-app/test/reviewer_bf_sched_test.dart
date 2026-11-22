import 'package:agrigreen_field/application/work_schedule_service.dart';
import 'package:agrigreen_field/domain/work_schedule.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/harness.dart';

void main() {
  test('reviewer #99: pending-only amendment item cannot check in against approved version', () {
    final today = DateTime.utc(2026, 10, 14);
    final oldItem = WsItem(
      id: 'approved-item',
      day: today,
      session: WsSession.morning,
      content: 'Kiem tra lo mo',
      communeCode: '30001',
      location: 'Ap 2',
    );
    final newItem = WsItem(
      id: 'amendment-only',
      day: today,
      session: WsSession.afternoon,
      content: 'Kiem tra ho nuoi',
      communeCode: '30001',
      location: 'Ap 3',
    );
    final view = WeekSchedule(
      weekStart: DateTime.utc(2026, 10, 12),
      submitDueAt: today,
      locked: false,
      lateNow: false,
      scheduleId: 'schedule-1',
      status: WsStatus.pending,
      version: 3,
      approvedVersion: 2,
      amendment: true,
      checkinEnabled: true,
      items: [oldItem, newItem],
      approvedItems: [oldItem],
    );
    expect(view.canCheckin(oldItem, today), isTrue);
    expect(
      view.canCheckin(newItem, today),
      isFalse,
      reason: 'Server selects approved version 2, where amendment-only has no row; it returns 404 extension.schedule_item_not_found.',
    );
  });
  test(
    'reviewer #99: first new week must validate commune before queuing',
    () async {
      final now = DateTime.utc(2026, 10, 14, 2);
      final week = DateTime.utc(2026, 10, 19);
      final h = await Harness.create(now: now);
      try {
        h.fake.onGet = (path, query) async =>
            path == '/v1/extension/work-schedules/mine'
            ? <String, Object?>{
                'week_start': '2026-10-19',
                'submit_due_at': '2026-10-16T10:00:00Z',
                'locked': false,
                'late_now': false,
                'schedule': null,
              }
            : null;
        final svc = WorkScheduleService(
          db: h.db,
          api: h.server,
          actions: h.actions,
          clock: h.clock,
          ids: h.ids,
        );
        final view = await svc.load(week);
        expect(view.scheduleId, isNull);
        expect(view.communeCodes, isEmpty);
        final item = WsItem(
          id: svc.newItemId(),
          day: week,
          session: WsSession.morning,
          content: 'Kiem tra dong ruong',
          location: 'Ap 1',
        );
        await expectLater(
          svc.upsertItem(week, item),
          throwsA(
            isA<WorkScheduleError>().having(
              (e) => e.field,
              'field',
              'commune_code',
            ),
          ),
          reason: 'New commune schedule cannot omit the required commune; server rejects it with 422.',
        );
      } finally {
        await h.dispose();
      }
    },
  );
}

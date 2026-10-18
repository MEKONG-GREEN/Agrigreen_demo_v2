import 'package:agrigreen_field/application/cnts_period_service.dart';
import 'package:agrigreen_field/domain/cnts_period.dart';
import 'package:agrigreen_mobile/data/outbox/outbox_repository.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/harness.dart';
import 'support/kn_fixtures.dart';

const _p1 = '0192f000-0000-7000-8000-000000000a01';
const _p2 = '0192f000-0000-7000-8000-000000000a02';
const _t1 = '0192f000-0000-7000-8000-0000000000b1';
const _t2 = '0192f000-0000-7000-8000-0000000000b2';
const _tc = '0192f000-0000-7000-8000-0000000000bc';

final _fields = [
  {
    'key': 'ca',
    'label': 'Cá',
    'type': 'decimal',
    'required': true,
    'non_negative': true,
    'group': 'Sản lượng',
  },
  {
    'key': 'tom',
    'label': 'Tôm',
    'type': 'decimal',
    'required': true,
    'non_negative': true,
    'group': 'Sản lượng',
  },
];

Map<String, Object?> _task(
  String id,
  String period, {
  required String from,
  required DateTime due,
  int version = 0,
  Map<String, String> values = const {},
  String status = 'not_submitted',
  bool open = true,
  String periodStatus = 'open',
}) => {
  'id': id,
  'profile': 'kn_cnts',
  'period_id': period,
  'unit': '91301',
  'unit_kind': 'commune',
  'parent': '91301',
  'template_id': 't',
  'template_code': CntsForms.bm17,
  'template_title': 'BM-17 — Sản lượng khai thác thủy sản hằng tuần',
  'template_version': 1,
  'fields': _fields,
  'label': 'Tuần $from',
  'period_kind': 'week',
  'period_key': from,
  'period_from': from,
  'period_to': from,
  'due_at': due.toIso8601String(),
  'period_status': periodStatus,
  'open': open,
  'status': status,
  'version': version,
  'submission_id': null,
  'values': values,
  'nil_report': false,
  'note': '',
  'explanation': '',
  'late': false,
  'submitted_at': null,
  'return_note': null,
  'held_at': null,
};

void main() {
  late Harness h;
  late CntsPeriodService svc;

  setUp(() async {
    h = await Harness.create(); // 2026-12-02 01:00 UTC
    svc = CntsPeriodService(
      db: h.db,
      refs: h.refs,
      outbox: h.outbox,
      clock: h.clock,
    );
    await h.refs.applyPage(
      PullPage(
        changes: [
          change(
            FormsEntity.task,
            _t1,
            1,
            _task(
              _t1,
              _p1,
              from: '2026-11-18',
              due: DateTime.utc(2026, 11, 25, 5),
              version: 2,
              values: const {'ca': '1.5', 'tom': '0.2'},
              status: 'submitted',
            ),
          ),
          change(
            FormsEntity.task,
            _t2,
            2,
            _task(
              _t2,
              _p2,
              from: '2026-11-25',
              due: DateTime.utc(2026, 12, 2, 5),
            ),
          ),
          change(
            FormsEntity.task,
            _tc,
            3,
            _task(
              _tc,
              _p2,
              from: '2026-11-11',
              due: DateTime.utc(2026, 11, 18, 5),
              open: false,
              periodStatus: 'closed',
              status: 'missed',
            ),
          ),
        ],
        nextCursor: 'c1',
        hasMore: false,
      ),
    );
  });
  tearDown(() => h.dispose());

  test(
    'W11.42 danh sách: Trễ hạn theo giờ máy, sắp việc cần làm trước, đếm',
    () async {
      expect([for (final r in await svc.list()) r.task.id].first, _t2);
      expect(await svc.todoCount(), 1);
      h.clock.advance(const Duration(hours: 5));
      final r = (await svc.list()).firstWhere((x) => x.task.id == _t2);
      expect(r.status, FormsStatus.overdue);
    },
  );

  test(
    'W11.42 sao chép kỳ trước (trên máy), kiểm ô, lý do trễ, gửi ngoại tuyến',
    () async {
      final t = (await svc.task(_t2))!;
      final prev = await svc.previous(t);
      expect(prev?.id, _t1);
      final w = await svc.work(t)
        ..values = {'ca': '-1'};
      await expectLater(
        svc.submit(t, w),
        throwsA(isA<FormsValidationException>()),
      );
      w
        ..values = {}
        ..nilReport = true;
      await svc.saveWork(t, w);
      expect((await svc.work(t)).nilReport, isTrue);
      final rec = await svc.submit(t, w);
      expect(rec.entity, PushEntity.formsSubmission);
      expect(rec.payload['base_version'], 0);
      expect(rec.payload['values'], {'ca': '0', 'tom': '0'});
      expect((await svc.recordStatus(_t2)).state, FormsSyncState.pending);
      // Quá hạn: bắt buộc lý do trễ.
      h.clock.advance(const Duration(hours: 5));
      final w2 = FormsWork(values: {'ca': '1', 'tom': '1'});
      await expectLater(
        svc.submit(t, w2),
        throwsA(
          isA<FormsValidationException>().having(
            (e) => e.errors.keys,
            'errors',
            contains('explanation'),
          ),
        ),
      );
      w2.explanation = 'Mất mạng';
      expect((await svc.submit(t, w2)).payload['explanation'], 'Mất mạng');
    },
  );

  test('W11.42 kỳ đã chốt: vẫn xếp gửi (máy chủ giữ Chờ mở kỳ); xung đột phiên bản', () async {
    final tc = (await svc.task(_tc))!;
    final rec = await svc.submit(
      tc,
      FormsWork(
        values: {'ca': '1', 'tom': '1'},
        explanation: 'Gửi muộn do mất sóng',
      ),
    );
    expect(rec.payload['period_id'], _p2);
    h.fake.resultFor = (w) => PushResult(
      id: '${w['id']}',
      status: 'rejected',
      code: 'forms.submission_conflict',
    );
    await h.sync.syncOnce(force: true);
    expect((await svc.recordStatus(_tc)).state, FormsSyncState.conflict);
  });
}

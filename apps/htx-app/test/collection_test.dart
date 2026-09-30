import 'package:agrigreen_htx/domain/collection.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/test_env.dart';

const _stmtRef = '01920000-0000-7000-8000-00000000c501';

Map<String, Object?> _statement({int rev = 1, Map<String, Object?>? resp}) => {
  'entity': 'htx_collection_statement',
  'id': _stmtRef,
  'op': 'upsert',
  'data': {
    'statement_ref': _stmtRef,
    'cooperative_ref': coopRef,
    'plot_ref': plotRef,
    'plot_code': null,
    'collected_on': '2026-09-29',
    'rolls': 120,
    'revision': rev,
    'pending': true,
    'response': resp,
  },
};

void main() {
  test('RB-6 chiều mua (G3-2): quy tắc phản hồi giống máy chủ', () {
    String? v(String o, int? n, String r, {bool sup = false}) =>
        validateCollectionResponse(
          outcome: o,
          mkgRolls: 120,
          htxRolls: n,
          reason: r,
          superseding: sup,
        );
    expect(v('confirmed', null, ''), isNull);
    expect(v('disputed', null, 'Đếm lại thiếu cuộn'), msgDisputeRolls);
    expect(v('disputed', 120, 'Đếm lại thiếu cuộn'), msgDisputeSame);
    expect(v('disputed', 118, ''), msgReasonLength);
    expect(v('disputed', 118, 'gọi 0912345678 nhé'), msgNoPii);
    expect(v('disputed', 118, 'Đếm lại thiếu cuộn'), isNull);
    expect(v('confirmed', null, '', sup: true), msgSupersede);
  });

  testWidgets(
    'RB-6 chiều mua (G3-2): quản lý HTX xem lịch gom + bảng kê, tranh chấp '
    'khi mất mạng → "Đang chờ gửi" rồi tự gửi htx_collection_response',
    (tester) async {
      final env = TestEnv(roles: const ['coop_manager'])
        ..seed(
          extra: [
            {
              'entity': 'htx_collection_schedule',
              'id': '01920000-0000-7000-8000-00000000c401',
              'op': 'upsert',
              'data': {
                'schedule_ref': '01920000-0000-7000-8000-00000000c401',
                'commune_code': '31117',
                'window_from': '2026-10-01',
                'window_to': '2026-10-05',
                'status': 'in_progress',
                'revision': 3,
              },
            },
            _statement(),
          ],
        );
      final s = await loginToHome(tester, env);
      env.server.failNextPushes = 100;
      await openTile(tester, 'Thu gom rơm MKG');
      await settle(tester);
      expect(find.text('2026-10-01 → 2026-10-05'), findsOneWidget);
      expect(find.textContaining('Đang gom'), findsOneWidget);
      expect(find.text('Lô T1 · 120 cuộn'), findsOneWidget);
      expect(find.textContaining('Chờ HTX xác nhận'), findsOneWidget);

      await tapText(tester, 'Lô T1 · 120 cuộn');
      await tapText(tester, 'Tranh chấp — HTX đếm khác');
      await tester.enterText(
        find.byKey(const ValueKey('collection-rolls')),
        '120',
      );
      await tester.enterText(
        find.byKey(const ValueKey('collection-reason')),
        'Đếm lại tại ruộng thiếu hai cuộn',
      );
      await tapText(tester, 'Gửi tranh chấp');
      expect(find.text(msgDisputeSame), findsOneWidget);
      await tester.enterText(
        find.byKey(const ValueKey('collection-rolls')),
        '118',
      );
      await tapText(tester, 'Gửi tranh chấp');
      await settle(tester);

      final rec = (await s.outbox.list(
        entity: PushEntity.htxCollectionResponse,
      )).single;
      expect(rec.payload, {
        'statement_ref': _stmtRef,
        'revision': 1,
        'outcome': 'disputed',
        'htx_rolls': 118,
        'reason': 'Đếm lại tại ruộng thiếu hai cuộn',
      });
      expect(
        find.descendant(
          of: find.byKey(const ValueKey('stmt-$_stmtRef')),
          matching: find.textContaining('Đang chờ gửi'),
        ),
        findsOneWidget,
      );

      env.server.failNextPushes = 0;
      await s.sync.syncOnce(force: true);
      expect((await s.outbox.byId(rec.id))!.status, OutboxStatus.sent);
      expect(
        env.server.ledger.values.single['entity'],
        PushEntity.htxCollectionResponse,
      );
      await finish(tester);
    },
  );
}

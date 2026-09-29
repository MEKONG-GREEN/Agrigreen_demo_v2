import 'package:agrigreen_htx/domain/roles.dart';
import 'package:agrigreen_htx/ui/navigation.dart';
import 'package:agrigreen_htx/ui/screens/farm_log_detail_screen.dart';
import 'package:agrigreen_htx/ui/screens/news_screen.dart';
import 'package:agrigreen_htx/ui/screens/plots_screen.dart';
import 'package:agrigreen_htx/ui/screens/review_screen.dart';
import 'package:agrigreen_htx/ui/screens/supplement_screen.dart';
import 'package:agrigreen_mobile/data/reference/reference_repository.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:agrigreen_mobile/domain/deep_link.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/test_env.dart';

const seasonRef = '01920000-0000-7000-8000-00000000e001';
const logId = '01920000-0000-7000-8000-00000000f0d1';
const docRef = '01920000-0000-7000-8000-00000000d0c1';

Map<String, Object?> _log(String status, {String? note}) => {
  'entity': 'farm_log',
  'id': logId,
  'op': 'upsert',
  'data': {
    'id': logId,
    'log_ref': logId,
    'season_ref': seasonRef,
    'plot_ref': plotRef,
    'activity_code': 'bon_phan',
    'event_time': '2026-09-30T07:00:00+07:00',
    'gps_source': 'device',
    'location_status': 'match',
    'machine_codes': [],
    'inputs': [],
    'note': 'Bón NPK',
    'file_refs': [],
    'on_behalf': false,
    'status': status,
    'review_note': ?note,
  },
};

Map<String, Object?> _notice(String id, String kind, String link) => {
  'entity': 'notification',
  'id': id,
  'op': 'upsert',
  'data': {
    'id': id,
    'kind': kind,
    'severity': kind == 'alert' ? 'urgent' : 'normal',
    'title': 'Thông báo $kind',
    'body': 'Nội dung',
    'deep_link': link,
    'created_at': '2026-10-01T02:00:00Z',
  },
};

Map<String, Object?> _doc() => {
  'entity': 'document',
  'id': docRef,
  'op': 'upsert',
  'data': {
    'title': 'Cảnh báo rầy nâu bùng phát',
    'summary': 'Thăm đồng ngay',
    'body': 'Rầy nâu mật độ cao.',
    'severity': 'urgent',
    'published_at': '2026-10-01T01:00:00Z',
  },
};

void main() {
  testWidgets(
    'KN US-SYNC-02 / HTX: thông điệp sync_now có entities → pull chỉ các '
    'entity đó (?entities=), entity khác trong trang không được áp',
    (tester) async {
      final env = TestEnv()..seed(season: true);
      final s = await loginToHome(tester, env);
      env.server.pages['c1'] = PullPage(
        changes: [
          PullChange.fromJson({
            ..._log('needs_supplement', note: 'Chụp thêm'),
            'version': 50,
          }),
          PullChange.fromJson({
            'entity': 'plot',
            'id': '01920000-0000-7000-8000-0000000000b9',
            'version': 99,
            'op': 'upsert',
            'data': {'code': 'T9', 'household_ref': householdRef},
          }),
        ],
        nextCursor: 'c2',
        hasMore: false,
      );
      env.push.deliver({'kind': 'sync_now', 'entities': 'farm_log,season'});
      await settle(tester);
      expect(env.server.pullEntities.last, ['farm_log', 'season']);
      expect(await s.refs.count(RefEntity.farmLog), 1);
      expect(
        await s.refs.get(
          RefEntity.plot,
          '01920000-0000-7000-8000-0000000000b9',
        ),
        isNull,
      );

      // Không có entities → pull toàn bộ như cũ.
      env.push.deliver({'kind': 'sync_now'});
      await settle(tester);
      expect(env.server.pullEntities.last, isNull);
      await finish(tester);
    },
  );

  testWidgets(
    'HTX US-LOG-05 AC-3: chạm thông báo farm_log_reviewed (nông dân) → nhật '
    'ký có ghi chú quản lý + nút Bổ sung → màn bổ sung',
    (tester) async {
      final env = TestEnv()
        ..seed(
          season: true,
          extra: [
            _log('needs_supplement', note: 'Chụp thêm ảnh ruộng'),
            _notice('n1', 'farm_log_reviewed', '/cropcycle/farm-logs/$logId'),
          ],
        );
      await loginToHome(tester, env);
      env.notifier.tap('/cropcycle/farm-logs/$logId');
      await settle(tester);
      expect(find.byType(FarmLogDetailScreen), findsOneWidget);
      expect(find.text('Quản lý HTX: Chụp thêm ảnh ruộng'), findsOneWidget);
      await tapText(tester, 'Bổ sung');
      expect(find.byType(SupplementScreen), findsOneWidget);
      await finish(tester);
    },
  );

  testWidgets(
    'HTX US-LOG-05: thông báo farm_log_pending (quản lý) từ hộp thư → hàng '
    'chờ duyệt, nhật ký đó lên đầu, viền nổi bật',
    (tester) async {
      final env = TestEnv(roles: const ['coop_manager'])
        ..seed(
          season: true,
          extra: [
            _log('pending'),
            _notice('n2', 'farm_log_pending', '/cropcycle/farm-logs/$logId'),
          ],
        );
      await loginToHome(tester, env);
      await tapOn(tester, find.byTooltip('Thông báo'));
      await tapText(tester, 'Thông báo farm_log_pending');
      expect(find.byType(ReviewScreen), findsOneWidget);
      expect(find.byKey(const ValueKey('review-highlight')), findsOneWidget);
      expect(find.byKey(const ValueKey('approve-$logId')), findsOneWidget);
      await finish(tester);
    },
  );

  testWidgets(
    'HTX US-SEASON-01: thông báo season_opened (quản lý) → chi tiết lô của vụ',
    (tester) async {
      final env = TestEnv(roles: const ['coop_manager'])..seed(season: true);
      await loginToHome(tester, env);
      env.notifier.tap('/cropcycle/seasons/$seasonRef');
      await settle(tester);
      expect(find.byType(PlotDetailScreen), findsOneWidget);
      expect(find.text('Vụ TD 2026 · giống OM18'), findsOneWidget);
      await finish(tester);
    },
  );

  testWidgets(
    'HTX US-NEWS-03: chạm cảnh báo khẩn cấp (alert → /documents/{ref}) → bài viết',
    (tester) async {
      final env = TestEnv()
        ..seed(extra: [_doc(), _notice('n3', 'alert', '/documents/$docRef')]);
      await loginToHome(tester, env);
      env.notifier.tap('/documents/$docRef');
      await settle(tester);
      expect(find.byType(ArticleScreen), findsOneWidget);
      expect(find.text('Cảnh báo rầy nâu bùng phát'), findsOneWidget);
      await finish(tester);
    },
  );

  testWidgets(
    'deep link: vụ / bài chưa có trên máy → màn "chưa có"; task_assigned '
    '(không dành cho nông dân) → hộp thư',
    (tester) async {
      final env = TestEnv()..seed();
      final s = await loginToHome(tester, env);
      final w1 = await screenForDeepLink(
        s,
        HtxMode.farmer,
        DeepLink.parse('/cropcycle/seasons/0192-none'),
      );
      expect(w1, isA<LinkMissingScreen>());
      final w2 = await screenForDeepLink(
        s,
        HtxMode.farmer,
        DeepLink.parse('/documents/0192-none'),
      );
      expect(w2, isA<LinkMissingScreen>());
      final w3 = await screenForDeepLink(
        s,
        HtxMode.farmer,
        DeepLink.parse('/extension/tasks/t1'),
      );
      expect(w3.runtimeType.toString(), 'NotificationsScreen');
      final w4 = await screenForDeepLink(
        s,
        HtxMode.manager,
        DeepLink.parse('/cropcycle/farm-logs/$logId'),
      );
      expect(w4, isA<ReviewScreen>());
      await finish(tester);
    },
  );
}

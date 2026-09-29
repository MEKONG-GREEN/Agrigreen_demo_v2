import 'package:agrigreen_htx/application/manager_service.dart';
import 'package:agrigreen_htx/application/news_service.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/test_env.dart';

Map<String, Object?> _doc(
  String id,
  String title,
  String? category, {
  String? severity,
  String day = '20',
}) => {
  'entity': 'document',
  'id': id,
  'op': 'upsert',
  'data': {
    'title': title,
    'summary': 'Tóm tắt $title',
    'body': 'Nội dung $title',
    'published_at': '2026-09-${day}T00:00:00Z',
    'category': ?category,
    'severity': ?severity,
  },
};

void main() {
  testWidgets(
    'HTX US-NEWS-01: tin theo chuyên mục (document.category) — danh sách chia '
    'nhóm rõ ràng, chip lọc chỉ hiện đúng chuyên mục, không trộn lẫn; cảnh '
    'báo khẩn vẫn trên cùng',
    (tester) async {
      await tester.binding.setSurfaceSize(const Size(900, 1800));
      addTearDown(() => tester.binding.setSurfaceSize(null));
      final env = TestEnv()
        ..seed(
          extra: [
            _doc('doc-1', 'Sạ hàng tiết kiệm giống', 'technique', day: '21'),
            _doc('doc-2', 'Bón phân theo bảng so màu lá', 'technique'),
            _doc('doc-3', 'Rầy nâu cuối vụ', 'pest_warning'),
            _doc('doc-4', 'Hỗ trợ giá giống vụ Đông Xuân', 'policy'),
            _doc('doc-5', 'Tin chưa phân loại', null),
            _doc(
              'doc-6',
              'Khẩn: bão số 5',
              'event',
              severity: 'urgent',
              day: '10',
            ),
          ],
        );
      final s = await loginToHome(tester, env);
      final list = await s.news.articles();
      expect({
        for (final a in list) a.ref: a.category,
      }, containsPair('doc-4', 'policy'));
      final groups = groupByCategory(list.where((a) => !a.urgent).toList());
      expect(
        [for (final g in groups) g.$1],
        ['technique', 'pest_warning', 'policy', newsOtherCategory],
      );
      expect([for (final a in groups.first.$2) a.ref], ['doc-1', 'doc-2']);

      await openTile(tester, 'Tin tức');
      // AC-2: phân loại rõ theo chuyên mục (tiêu đề nhóm + số bài).
      expect(find.text('Cảnh báo khẩn (1)'), findsOneWidget);
      expect(find.text('Kỹ thuật (2)'), findsOneWidget);
      expect(find.text('Cảnh báo sâu bệnh (1)'), findsOneWidget);
      expect(find.text('Chính sách (1)'), findsOneWidget);
      expect(find.text('Khác (1)'), findsOneWidget);
      // Chỉ có chip cho chuyên mục đang có bài.
      expect(find.widgetWithText(ChoiceChip, 'Thị trường'), findsNothing);
      final urgentY = tester.getTopLeft(find.text('Khẩn: bão số 5')).dy;
      final firstY = tester.getTopLeft(find.text('Sạ hàng tiết kiệm giống')).dy;
      expect(urgentY, lessThan(firstY));

      await tapOn(tester, find.widgetWithText(ChoiceChip, 'Chính sách'));
      expect(find.text('Hỗ trợ giá giống vụ Đông Xuân'), findsOneWidget);
      expect(find.text('Sạ hàng tiết kiệm giống'), findsNothing);
      expect(find.text('Rầy nâu cuối vụ'), findsNothing);
      expect(find.text('Tin chưa phân loại'), findsNothing);

      await tapOn(tester, find.widgetWithText(ChoiceChip, 'Kỹ thuật'));
      expect(find.text('Sạ hàng tiết kiệm giống'), findsOneWidget);
      expect(find.text('Bón phân theo bảng so màu lá'), findsOneWidget);
      expect(find.text('Hỗ trợ giá giống vụ Đông Xuân'), findsNothing);

      await tapOn(tester, find.widgetWithText(ChoiceChip, 'Mọi chuyên mục'));
      expect(find.text('Hỗ trợ giá giống vụ Đông Xuân'), findsOneWidget);
      await finish(tester);
    },
  );

  const hhB = '01920000-0000-7000-8000-0000000000a2';

  TestEnv managerWithHouseholds({
    required Future<Map<String, Object?>?> Function(String ref) contact,
  }) {
    final env = TestEnv(roles: const ['coop_manager'])..seed();
    final base = env.server.onGet!;
    env.server.onGet = (path, query) async {
      if (path == '/v1/cooperatives/$coopRef/households') {
        return {
          'items': [
            {'ref': householdRef, 'label': 'Hộ N***n V*n A', 'code': 'HH-01'},
            {'ref': hhB, 'label': 'Hộ T***n T*i B', 'code': 'HH-02'},
          ],
          'next_cursor': null,
        };
      }
      final m = RegExp(r'^/v1/households/([^/]+)/contact$').firstMatch(path);
      if (m != null) {
        final r = await contact(m.group(1)!);
        if (r == null) throw ApiException(404, 'not_found');
        return r;
      }
      return base(path, query);
    };
    return env;
  }

  testWidgets(
    'HTX US-HH-03: bấm biểu tượng điện thoại cạnh nông hộ → mở trình gọi với '
    'SĐT đọc lúc bấm (không lưu trên máy); máy không gọi được → thông báo AC-2, '
    'không treo; nông hộ chưa có SĐT → báo rõ',
    (tester) async {
      final env = managerWithHouseholds(
        contact: (ref) async => ref == householdRef
            ? {'phone': '0912 345 678'}
            : throw ApiException(422, 'party.contact_unavailable'),
      );
      final s = await loginToHome(tester, env);
      await openTile(tester, 'Nông hộ');
      expect(find.text('Hộ N***n V*n A'), findsOneWidget);

      await tapOn(tester, find.byTooltip('Gọi Hộ N***n V*n A'));
      expect(env.dialer.opened.single, Uri.parse('tel:0912345678'));
      expect(
        env.server.gets.where(
          (g) => g == '/v1/households/$householdRef/contact',
        ),
        hasLength(1),
      );
      // SĐT không nằm trong CSDL trên máy (R-04).
      final meta = await s.db.db.query('meta');
      expect(meta.toString().contains('0912'), isFalse);

      // AC-2: thiết bị không hỗ trợ gọi.
      env.dialer.ok = false;
      await tapOn(tester, find.byTooltip('Gọi Hộ N***n V*n A'));
      expect(find.text(msgCannotCall), findsOneWidget);
      expect(find.byTooltip('Gọi Hộ T***n T*i B'), findsOneWidget);

      env.dialer.ok = true;
      await tapOn(tester, find.byTooltip('Gọi Hộ T***n T*i B'));
      expect(
        find.text('Nông hộ chưa có số điện thoại hợp lệ.'),
        findsOneWidget,
      );
      expect(env.dialer.opened, hasLength(2));
      await finish(tester);
    },
  );

  testWidgets(
    'HTX US-HH-03: máy chủ chưa có GET /v1/households/{ref}/contact (404 '
    'not_found) → ẩn nút gọi, không lỗi',
    (tester) async {
      final env = managerWithHouseholds(contact: (_) async => null);
      final s = await loginToHome(tester, env);
      await openTile(tester, 'Nông hộ');
      await tapOn(tester, find.byTooltip('Gọi Hộ N***n V*n A'));
      expect(s.manager.householdCallAvailable, isFalse);
      expect(env.dialer.opened, isEmpty);
      await tester.pageBack();
      await settle(tester);
      await openTile(tester, 'Nông hộ');
      expect(find.byTooltip('Gọi Hộ N***n V*n A'), findsNothing);
      expect(find.byTooltip('Gọi Hộ T***n T*i B'), findsNothing);
      await finish(tester);
    },
  );
}

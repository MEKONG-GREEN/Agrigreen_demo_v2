import 'package:agrigreen_htx/domain/support.dart';
import 'package:agrigreen_mobile/data/photo/location_source.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/test_env.dart';

void main() {
  testWidgets(
    'HTX US-SUPPORT-01: bắt buộc loại + mô tả; ≤ 5 ảnh; thiếu GPS vẫn gửi; '
    'mất mạng → "Đang chờ gửi" rồi tự gửi',
    (tester) async {
      final env = TestEnv()..seed();
      env.location.result = const LocationResult(GpsStatus.unavailable);
      final s = await loginToHome(tester, env);
      env.server.failNextPushes = 100;
      await openTile(tester, 'Yêu cầu hỗ trợ');

      await tapText(tester, 'Gửi');
      expect(find.text(msgSupportRequired), findsOneWidget);

      await tapText(tester, 'Dịch hại');
      await tester.enterText(
        find.byKey(const ValueKey('support-desc')),
        'Rầy nâu xuất hiện nhiều ở góc ruộng',
      );
      for (var i = 0; i < 6; i++) {
        await tapOn(tester, find.byKey(const ValueKey('photo-capture')));
      }
      expect(find.text('Chụp ảnh (5/5)'), findsOneWidget);
      expect(find.textContaining('tối đa 5'), findsOneWidget);
      expect(env.camera.shots, 5);
      await tapText(tester, 'Gửi');

      final rec = (await s.outbox.list(entity: PushEntity.supportRequest))
          .single;
      expect(rec.payload['category'], 'dich_hai');
      expect(rec.payload['urgency'], 'binh_thuong');
      expect(rec.payload['photo_refs'], hasLength(5));
      expect(rec.payload.containsKey('location'), isFalse);
      expect(rec.payload.containsKey('household_ref'), isFalse); // một hộ
      await settle(tester);
      expect(find.text('Đang chờ gửi'), findsOneWidget);
      expect(find.text('Thiếu vị trí GPS'), findsOneWidget);

      env.server.failNextPushes = 0;
      await s.sync.syncOnce(force: true);
      expect((await s.outbox.byId(rec.id))!.status, OutboxStatus.sent);
      expect(env.server.uploadKeys, hasLength(5));
      await finish(tester);
    },
  );

  testWidgets(
    'HTX US-SUPPORT-02: tra danh bạ, số đọc lúc bấm (không lưu), gọi tel:, '
    'chép số; không có số → ẩn nút gọi; mất mạng → thông báo',
    (tester) async {
      final env = TestEnv()..seed();
      final base = env.server.onGet!;
      final queries = <String?>[];
      var offline = false;
      env.server.onGet = (path, q) async {
        if (offline) throw NetworkException('offline');
        if (path == '/v1/farmer/coop-staff') {
          queries.add(q?['q']);
          return {
            'items': [
              {
                'user_ref': 'st-1',
                'display_name': 'Nguyễn Văn Bình',
                'title': 'Giám đốc HTX',
                'has_phone': true,
              },
              {
                'user_ref': 'st-2',
                'display_name': 'Lê Thị Cúc',
                'title': 'Kế toán',
                'has_phone': false,
              },
            ],
          };
        }
        if (path == '/v1/farmer/coop-staff/st-1/contact') {
          return {'user_ref': 'st-1', 'phone': '0912 345 678'};
        }
        return base(path, q);
      };
      final s = await loginToHome(tester, env);
      await openTile(tester, 'Danh bạ cán bộ');
      expect(find.text('Nguyễn Văn Bình'), findsOneWidget);
      expect(find.byKey(const ValueKey('call-st-1')), findsOneWidget);
      expect(find.byKey(const ValueKey('call-st-2')), findsNothing);
      expect(find.text('Chưa có số điện thoại'), findsOneWidget);

      await tapOn(tester, find.byKey(const ValueKey('call-st-1')));
      expect(env.dialer.opened.single.toString(), 'tel:0912345678');
      await tapOn(tester, find.byKey(const ValueKey('copy-st-1')));
      expect(env.copied, ['0912 345 678']);
      // Số điện thoại không nằm trong CSDL máy.
      final dump = await s.db.db.rawQuery(
        "SELECT value FROM meta WHERE value LIKE '%0912%'",
      );
      expect(dump, isEmpty);

      await tester.enterText(
        find.byKey(const ValueKey('staff-search')),
        'kế toán',
      );
      await tester.testTextInput.receiveAction(TextInputAction.search);
      await settle(tester);
      expect(queries.last, 'kế toán');

      offline = true;
      await tapOn(tester, find.byKey(const ValueKey('call-st-1')));
      expect(find.textContaining('Cần có mạng'), findsOneWidget);
      await finish(tester);
    },
  );
}

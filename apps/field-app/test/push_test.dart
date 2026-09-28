import 'package:agrigreen_field/application/inbox_service.dart';
import 'package:agrigreen_field/application/push_coordinator.dart';
import 'package:agrigreen_field/data/push/push_messaging.dart';
import 'package:agrigreen_field/data/push/push_token_registrar.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/harness.dart';

class _Host implements PushHost {
  int syncs = 0;
  final tokens = <String>[];
  InboxItem? item;
  @override
  Future<InboxItem?> notification(String id) async =>
      item?.id == id ? item : null;
  @override
  Future<void> setPushToken(String token) async => tokens.add(token);
  @override
  Future<void> syncNow() async => syncs++;
}

const _item = InboxItem(
  id: 'n1',
  kind: 'directive',
  severity: 'urgent',
  title: 'Chỉ đạo phòng rầy nâu xã Tân Thạnh',
  body: 'Kiểm tra ruộng trong 48 giờ',
  deepLink: '/extension/directives/d1',
  createdAt: null,
  read: false,
);

Future<void> _settle() => Future<void>.delayed(Duration.zero);

void main() {
  group('PushCoordinator — thông điệp chỉ dữ liệu (ADR-0015)', () {
    late FakePushMessaging fcm;
    late RecordingLocalNotifier notes;
    late PushCoordinator c;
    var foreground = true;

    setUp(() async {
      fcm = FakePushMessaging();
      notes = RecordingLocalNotifier();
      foreground = true;
      c = PushCoordinator(
        messaging: fcm,
        notifier: notes,
        isForeground: () => foreground,
      );
      await c.start();
    });
    tearDown(() => c.dispose());

    test('sync_now → đồng bộ ngay, không hiện thông báo', () async {
      final host = _Host();
      await c.attach(host);
      fcm.deliver({'kind': 'sync_now'});
      await _settle();
      expect(host.syncs, 1);
      expect(c.syncNowHandled, 1);
      expect(notes.shown, isEmpty);
    });

    test('sync_now khi đang khóa → bỏ qua (đồng bộ khi mở khóa)', () async {
      fcm.deliver({'kind': 'sync_now'});
      await _settle();
      expect(c.syncNowHandled, 0);
    });

    test(
      'notification_id → pull rồi hiện nội dung khi đã mở khóa và ở tiền cảnh',
      () async {
        final host = _Host()..item = _item;
        await c.attach(host);
        fcm.deliver({'kind': 'directive', 'notification_id': 'n1'});
        await _settle();
        expect(host.syncs, 1);
        final n = notes.shown.single;
        expect(n.title, _item.title);
        expect(n.payload, '/extension/directives/d1');
        expect(n.urgent, isTrue);
      },
    );

    test(
      'không lộ nội dung: khóa / nền / chưa đồng bộ về → "Có thông báo mới"',
      () async {
        // Đang khóa.
        fcm.deliver({'kind': 'directive', 'notification_id': 'n1'});
        await _settle();
        // Mở khóa nhưng app ở nền (màn hình khóa của máy).
        final host = _Host()..item = _item;
        await c.attach(host);
        foreground = false;
        fcm.deliver({'kind': 'directive', 'notification_id': 'n1'});
        await _settle();
        // Tiền cảnh nhưng nội dung chưa về.
        foreground = true;
        fcm.deliver({'kind': 'alert', 'notification_id': 'n-unknown'});
        await _settle();
        expect(notes.shown, hasLength(3));
        for (final n in notes.shown) {
          expect(n.title, genericNoticeTitle);
          expect(n.body, genericNoticeBody);
          expect(n.body, isNot(contains('rầy')));
        }
        expect(notes.shown[1].payload, '/extension/directives/d1');
        expect(notes.shown[2].payload, '/notifications');
      },
    );

    test('token: gắn khi mở khóa và khi FCM đổi token', () async {
      final host = _Host();
      await c.attach(host);
      expect(host.tokens, [fcm.initialToken]);
      fcm.refreshToken('fcm-token-2-xxxxxxxxxxxxxxxxxxx');
      await _settle();
      expect(host.tokens.last, 'fcm-token-2-xxxxxxxxxxxxxxxxxxx');
      expect(host.syncs, 1);
    });

    test('không có kênh đẩy (chưa cấu hình Firebase) → không lỗi', () async {
      final none = PushCoordinator(
        messaging: const NoopPushMessaging(),
        notifier: const NoopLocalNotifier(),
      );
      await none.start();
      expect(none.available, isFalse);
      await none.attach(_Host());
    });
  });

  group('PushTokenRegistrar — POST /v1/devices/{id}/push-token', () {
    late Harness h;
    setUp(() async => h = await Harness.create());
    tearDown(() => h.dispose());

    test(
      'gắn sau đăng ký thiết bị, mỗi lần khởi động; lỗi 503 không chặn đồng bộ',
      () async {
        var reg = PushTokenRegistrar(h.db, startedAt: h.clock.now());
        expect(await reg.flush(h.fake, testDeviceId), isFalse); // chưa có token
        await reg.setToken('tok-A-0123456789abcdefghij');
        h.fake.failPushToken = 1;
        expect(await reg.flush(h.fake, testDeviceId), isFalse);
        expect(await reg.flush(h.fake, testDeviceId), isTrue);
        expect(h.fake.pushTokens[testDeviceId], 'tok-A-0123456789abcdefghij');
        // Cùng lần khởi động: không gọi lại; cùng khóa cho lần thử lại.
        expect(await reg.flush(h.fake, testDeviceId), isFalse);
        expect(h.fake.pushTokenKeys[0], h.fake.pushTokenKeys[1]);
        expect(h.fake.pushTokenKeys[0], isNot(contains('tok-A')));

        // Khởi động lại → gắn lại với khóa mới.
        h.clock.advance(const Duration(hours: 1));
        reg = PushTokenRegistrar(h.db, startedAt: h.clock.now());
        expect(await reg.flush(h.fake, testDeviceId), isTrue);
        expect(h.fake.pushTokenKeys.last, isNot(h.fake.pushTokenKeys.first));

        // Token đổi → gắn lại ngay trong cùng lần khởi động.
        await reg.setToken('tok-B-0123456789abcdefghij');
        expect(await reg.flush(h.fake, testDeviceId), isTrue);
        expect(h.fake.pushTokens[testDeviceId], 'tok-B-0123456789abcdefghij');
      },
    );
  });
}

import 'package:agrigreen_field/application/auth_flow.dart';
import 'package:agrigreen_field/core/clock.dart';
import 'package:agrigreen_field/data/auth/pin_service.dart';
import 'package:agrigreen_field/data/auth/session_service.dart';
import 'package:agrigreen_field/data/secure/secure_store.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/fake_oidc.dart';

void main() {
  late MemorySecureStore store;
  late FakeClock clock;
  late FakeOidc oidc;
  late SessionService session;
  PinService pins({int iterations = 1000}) =>
      PinService(store: store, clock: clock, iterations: iterations);

  setUp(() {
    store = MemorySecureStore();
    clock = FakeClock(DateTime.utc(2026, 12, 1, 0, 0));
    oidc = FakeOidc();
    session = SessionService(store: store, clock: clock, oidc: oidc);
  });

  group('phiên ngoại tuyến tối đa 72 giờ (US-ADM-06)', () {
    test(
      'chưa từng đăng nhập trực tuyến → không cho đăng nhập ngoại tuyến',
      () async {
        expect(await session.offlineState(), OfflineSessionState.never);
      },
    );

    test('hợp lệ đến đúng 72 giờ, hết hạn sau đó', () async {
      await session.signInOnline();
      expect(await session.offlineState(), OfflineSessionState.valid);
      clock.advance(const Duration(hours: 72));
      expect(await session.offlineState(), OfflineSessionState.valid);
      clock.advance(const Duration(seconds: 1));
      expect(await session.offlineState(), OfflineSessionState.expired);
    });

    test('làm mới token trực tuyến gia hạn cửa sổ', () async {
      await session.signInOnline();
      clock.advance(const Duration(hours: 70));
      await session.refreshAccessToken();
      clock.advance(const Duration(hours: 70));
      expect(await session.offlineState(), OfflineSessionState.valid);
    });

    test('lùi đồng hồ để kéo dài phiên bị phát hiện', () async {
      await session.signInOnline();
      clock.advance(const Duration(hours: 60));
      await session.touch();
      clock.set(DateTime.utc(2026, 11, 30));
      expect(await session.offlineState(), OfflineSessionState.clockTampered);
    });

    test('token và mốc phiên chỉ nằm trong kho bí mật', () async {
      await session.signInOnline();
      expect(store.values[SecureKeys.accessToken], 'at1');
      expect(store.values[SecureKeys.refreshToken], 'rt1');
      expect(store.values[SecureKeys.lastOnlineAuthAt], isNotNull);
      await session.lockTokens();
      expect(store.values.containsKey(SecureKeys.accessToken), isFalse);
      expect(store.values.containsKey(SecureKeys.refreshToken), isFalse);
    });
  });

  group('PIN 6 số và khóa khi sai nhiều lần', () {
    test('chỉ nhận đúng 6 chữ số; lưu băm, không lưu PIN rõ', () async {
      final p = pins();
      expect(() => p.setPin('12345'), throwsA(isA<PinException>()));
      expect(() => p.setPin('12345a'), throwsA(isA<PinException>()));
      expect(() => p.setPin('1234567'), throwsA(isA<PinException>()));
      await p.setPin('246810');
      expect(store.values.values.any((v) => v.contains('246810')), isFalse);
      expect((await p.verify('246810')).check, PinCheck.ok);
    });

    test('sai 5 lần liên tiếp → khóa 15 phút; PIN đúng cũng bị từ chối trong lúc khóa', () async {
      final p = pins();
      await p.setPin('135790');
      for (var i = 1; i <= 4; i++) {
        final r = await p.verify('000000');
        expect(r.check, PinCheck.wrong);
        expect(r.remainingAttempts, 5 - i);
      }
      final locked = await p.verify('000000');
      expect(locked.check, PinCheck.lockedOut);
      expect(
        locked.lockedUntil!.difference(clock.now()),
        const Duration(minutes: 15),
      );
      expect((await p.verify('135790')).check, PinCheck.lockedOut);

      // Bộ đếm lưu bền: tạo lại dịch vụ (khởi động lại app) vẫn bị khóa.
      expect((await pins().verify('135790')).check, PinCheck.lockedOut);

      clock.advance(const Duration(minutes: 15));
      expect((await pins().verify('135790')).check, PinCheck.ok);
    });

    test('PIN đúng đặt lại bộ đếm sai liên tiếp', () async {
      final p = pins();
      await p.setPin('111222');
      for (var i = 0; i < 4; i++) {
        await p.verify('999999');
      }
      expect((await p.verify('111222')).check, PinCheck.ok);
      expect((await p.verify('999999')).remainingAttempts, 4);
    });

    test(
      'sai tổng cộng 10 lần → hủy PIN, bắt buộc đăng nhập trực tuyến',
      () async {
        final p = pins();
        await p.setPin('123789');
        PinResult? last;
        for (var i = 0; i < 10; i++) {
          last = await p.verify('000001');
          if (last.check == PinCheck.lockedOut) {
            clock.advance(const Duration(minutes: 16));
          }
        }
        expect(last!.check, PinCheck.mustReauthenticate);
        expect(await p.isSet(), isFalse);
        expect((await p.verify('123789')).check, PinCheck.notSet);
      },
    );
  });

  group('luồng đăng nhập', () {
    test('lần đầu → trực tuyến → đặt PIN → mở; khởi động lại → nhập PIN; quá 72 giờ → trực tuyến', () async {
      final flow = AuthFlow(session: session, pins: pins());
      expect((await flow.start()).stage, AuthStage.needsOnlineLogin);
      expect((await flow.loginOnline()).stage, AuthStage.needsPinSetup);
      expect(
        (await flow.setPin('123456', '654321')).stage,
        AuthStage.needsPinSetup,
      );
      expect((await flow.setPin('123456', '123456')).stage, AuthStage.unlocked);

      final restarted = AuthFlow(session: session, pins: pins());
      expect((await restarted.start()).stage, AuthStage.locked);
      expect((await restarted.unlock('000000')).stage, AuthStage.locked);
      expect((await restarted.unlock('123456')).stage, AuthStage.unlocked);

      clock.advance(const Duration(hours: 73));
      final v = await restarted.unlock('123456');
      expect(v.stage, AuthStage.needsOnlineLogin);
      expect(v.message, contains('72 giờ'));

      // Đăng nhập lại trực tuyến: PIN cũ vẫn dùng được, mở thẳng.
      expect((await restarted.loginOnline()).stage, AuthStage.unlocked);
      expect(oidc.signIns, 2);
    });

    test('đăng nhập trực tuyến thất bại hiển thị thông báo', () async {
      oidc.fail = true;
      final flow = AuthFlow(session: session, pins: pins());
      final v = await flow.loginOnline();
      expect(v.stage, AuthStage.needsOnlineLogin);
      expect(v.message, 'Đã hủy đăng nhập.');
    });
  });
}

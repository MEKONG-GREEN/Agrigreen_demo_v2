import '../data/auth/oidc_client.dart';
import '../data/auth/pin_service.dart';
import '../data/auth/session_service.dart';

enum AuthStage {
  loading,

  /// Cần đăng nhập trực tuyến (lần đầu, quá 72 giờ, sai PIN quá giới hạn, bị xóa).
  needsOnlineLogin,

  /// Vừa đăng nhập trực tuyến, chưa có PIN (chỉ app có PIN).
  needsPinSetup,

  /// Có phiên ngoại tuyến hợp lệ — nhập PIN để mở.
  locked,
  unlocked,
}

class AuthView {
  const AuthView(this.stage, {this.message, this.lockedUntil, this.remaining});
  final AuthStage stage;
  final String? message;
  final DateTime? lockedUntil;
  final int? remaining;
}

/// Luồng đăng nhập/mở khóa, không phụ thuộc Flutter để kiểm thử được.
///
/// [pins] null: app không có PIN (App KN — BRD 2.0 FN-CH-00.01/FN-05 "Mobile: không có đăng nhập bằng PIN", ADR-0056):
/// phiên ngoại tuyến hợp lệ (72 giờ từ lần xác thực trực tuyến cuối) mở thẳng, bảo vệ bằng khóa màn hình của thiết bị.
class AuthFlow {
  AuthFlow({required this.session, this.pins});
  final SessionService session;
  final PinService? pins;

  Future<AuthView> start() async {
    switch (await session.offlineState()) {
      case OfflineSessionState.never:
        return const AuthView(AuthStage.needsOnlineLogin);
      case OfflineSessionState.expired:
        return const AuthView(
          AuthStage.needsOnlineLogin,
          message: 'Phiên ngoại tuyến đã quá 72 giờ. Vui lòng kết nối mạng và đăng nhập lại.',
        );
      case OfflineSessionState.clockTampered:
        return const AuthView(
          AuthStage.needsOnlineLogin,
          message: 'Thời gian trên máy không hợp lệ. Vui lòng kết nối mạng và đăng nhập lại.',
        );
      case OfflineSessionState.valid:
        final p = pins;
        if (p == null) {
          await session.touch();
          return const AuthView(AuthStage.unlocked);
        }
        return await p.isSet()
            ? const AuthView(AuthStage.locked)
            : const AuthView(AuthStage.needsPinSetup);
    }
  }

  Future<AuthView> loginOnline() async {
    try {
      await session.signInOnline();
    } on OidcException catch (e) {
      return AuthView(AuthStage.needsOnlineLogin, message: e.message);
    } catch (_) {
      return const AuthView(
        AuthStage.needsOnlineLogin,
        message: 'Không đăng nhập được. Kiểm tra kết nối mạng.',
      );
    }
    final p = pins;
    return p == null || await p.isSet()
        ? const AuthView(AuthStage.unlocked)
        : const AuthView(AuthStage.needsPinSetup);
  }

  /// Đăng xuất: xóa token và mốc xác thực trực tuyến — lần sau phải đăng nhập trực tuyến.
  Future<AuthView> signOut() async {
    await session.signOut();
    return const AuthView(AuthStage.needsOnlineLogin);
  }

  Future<AuthView> setPin(String pin, String confirm) async {
    final p = pins;
    if (p == null) return start();
    if (pin != confirm) {
      return const AuthView(
        AuthStage.needsPinSetup,
        message: 'Hai lần nhập PIN không khớp.',
      );
    }
    if (await session.offlineState() != OfflineSessionState.valid) {
      return const AuthView(AuthStage.needsOnlineLogin);
    }
    try {
      await p.setPin(pin);
    } on PinException catch (e) {
      return AuthView(AuthStage.needsPinSetup, message: e.message);
    }
    await session.touch();
    return const AuthView(AuthStage.unlocked);
  }

  Future<AuthView> unlock(String pin) async {
    final st = await start();
    final p = pins;
    if (st.stage != AuthStage.locked || p == null) return st;
    final r = await p.verify(pin);
    switch (r.check) {
      case PinCheck.ok:
        await session.touch();
        return const AuthView(AuthStage.unlocked);
      case PinCheck.wrong:
        return AuthView(
          AuthStage.locked,
          message: 'PIN không đúng. Còn ${r.remainingAttempts} lần thử.',
          remaining: r.remainingAttempts,
        );
      case PinCheck.lockedOut:
        return AuthView(
          AuthStage.locked,
          message:
              'Nhập sai quá nhiều lần. Tạm khóa đến '
              '${_hm(r.lockedUntil!)}.',
          lockedUntil: r.lockedUntil,
        );
      case PinCheck.mustReauthenticate:
        await session.lockTokens();
        return const AuthView(
          AuthStage.needsOnlineLogin,
          message:
              'Đã hủy PIN do nhập sai quá nhiều lần. Vui lòng đăng nhập lại.',
        );
      case PinCheck.notSet:
        return const AuthView(AuthStage.needsOnlineLogin);
    }
  }

  static String _hm(DateTime t) {
    final l = t.toLocal();
    return '${l.hour.toString().padLeft(2, '0')}:${l.minute.toString().padLeft(2, '0')}';
  }
}

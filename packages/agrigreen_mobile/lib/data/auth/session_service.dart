import '../../core/clock.dart';
import '../secure/secure_store.dart';
import 'oidc_client.dart';

enum OfflineSessionState {
  /// Chưa từng đăng nhập trực tuyến trên máy — lần đầu bắt buộc có mạng.
  never,

  /// Còn trong cửa sổ 72 giờ — mở khóa bằng PIN.
  valid,

  /// Quá 72 giờ kể từ lần xác thực trực tuyến cuối — phải đăng nhập lại khi có mạng.
  expired,

  /// Đồng hồ máy bị lùi so với lần dùng trước — không tin được, coi như hết hạn.
  clockTampered,
}

/// Phiên đăng nhập: token OIDC + mốc xác thực trực tuyến cuối, tất cả trong
/// kho bí mật (HTX US-LOGIN-02 AC-1: phiên lưu có mã hóa).
class SessionService {
  SessionService({
    required SecureStore store,
    required Clock clock,
    required OidcClient oidc,
    this.offlineMax = const Duration(hours: 72),
    this.clockSkewTolerance = const Duration(minutes: 10),
  }) : _store = store,
       _clock = clock,
       _oidc = oidc;

  final SecureStore _store;
  final Clock _clock;
  final OidcClient _oidc;
  final Duration offlineMax;
  final Duration clockSkewTolerance;

  Future<DateTime?> _readTime(String key) async {
    final v = int.tryParse(await _store.read(key) ?? '');
    return v == null ? null : DateTime.fromMillisecondsSinceEpoch(v);
  }

  Future<void> _writeTime(String key, DateTime t) =>
      _store.write(key, '${t.millisecondsSinceEpoch}');

  Future<DateTime?> lastOnlineAuthAt() =>
      _readTime(SecureKeys.lastOnlineAuthAt);

  /// Đăng nhập trực tuyến bằng OIDC PKCE.
  Future<void> signInOnline() async {
    final t = await _oidc.signIn();
    await _saveTokens(t);
    final now = _clock.now();
    await _writeTime(SecureKeys.lastOnlineAuthAt, now);
    await _writeTime(SecureKeys.lastSeenAt, now);
  }

  Future<void> _saveTokens(OidcTokens t) async {
    await _store.write(SecureKeys.accessToken, t.accessToken);
    if (t.refreshToken != null) {
      await _store.write(SecureKeys.refreshToken, t.refreshToken!);
    }
    if (t.idToken != null) await _store.write(SecureKeys.idToken, t.idToken!);
    if (t.accessTokenExpiry != null) {
      await _writeTime(SecureKeys.accessExpiry, t.accessTokenExpiry!);
    }
  }

  /// Ghi nhận lần xác thực trực tuyến thành công bằng refresh token (vẫn là
  /// xác thực với máy chủ, nên gia hạn cửa sổ 72 giờ).
  Future<String?> refreshAccessToken() async {
    final rt = await _store.read(SecureKeys.refreshToken);
    if (rt == null) return null;
    final t = await _oidc.refresh(rt);
    await _saveTokens(t);
    await _writeTime(SecureKeys.lastOnlineAuthAt, _clock.now());
    return t.accessToken;
  }

  /// Access token còn hạn (làm mới nếu sắp hết). null nếu cần đăng nhập lại.
  Future<String?> accessToken() async {
    final at = await _store.read(SecureKeys.accessToken);
    if (at == null) return null;
    final exp = await _readTime(SecureKeys.accessExpiry);
    if (exp != null &&
        _clock.now().isAfter(exp.subtract(const Duration(seconds: 30)))) {
      try {
        return await refreshAccessToken();
      } on OidcException {
        return null;
      }
    }
    return at;
  }

  Future<OfflineSessionState> offlineState() async {
    final last = await lastOnlineAuthAt();
    if (last == null) return OfflineSessionState.never;
    final now = _clock.now();
    final seen = await _readTime(SecureKeys.lastSeenAt);
    if (seen != null && now.isBefore(seen.subtract(clockSkewTolerance))) {
      return OfflineSessionState.clockTampered;
    }
    if (now.isBefore(last.subtract(clockSkewTolerance))) {
      return OfflineSessionState.clockTampered;
    }
    if (now.difference(last) > offlineMax) return OfflineSessionState.expired;
    return OfflineSessionState.valid;
  }

  /// Gọi mỗi lần mở khóa để phát hiện đồng hồ bị lùi.
  Future<void> touch() async {
    final now = _clock.now();
    final seen = await _readTime(SecureKeys.lastSeenAt);
    if (seen == null || now.isAfter(seen)) {
      await _writeTime(SecureKeys.lastSeenAt, now);
    }
  }

  /// Thời điểm phiên ngoại tuyến hết hạn (để hiển thị).
  Future<DateTime?> offlineExpiresAt() async =>
      (await lastOnlineAuthAt())?.add(offlineMax);

  /// Khóa token: xóa access/refresh/id token (giữ PIN, dữ liệu).
  Future<void> lockTokens() async {
    await _store.delete(SecureKeys.accessToken);
    await _store.delete(SecureKeys.refreshToken);
    await _store.delete(SecureKeys.idToken);
    await _store.delete(SecureKeys.accessExpiry);
  }
}

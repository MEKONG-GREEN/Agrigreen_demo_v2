import 'package:flutter_appauth/flutter_appauth.dart';

import '../../core/config.dart';

class OidcTokens {
  const OidcTokens({
    required this.accessToken,
    this.refreshToken,
    this.idToken,
    this.accessTokenExpiry,
  });
  final String accessToken;
  final String? refreshToken;
  final String? idToken;
  final DateTime? accessTokenExpiry;
}

class OidcException implements Exception {
  OidcException(this.message);
  final String message;
  @override
  String toString() => message;
}

/// Đăng nhập OIDC (Authorization Code + PKCE) với Keycloak.
abstract class OidcClient {
  Future<OidcTokens> signIn();
  Future<OidcTokens> refresh(String refreshToken);
}

/// Hiện thực bằng `flutter_appauth` (AppAuth-Android). AppAuth tự sinh
/// `code_verifier`/`code_challenge` S256 cho mọi yêu cầu (PKCE).
/// Cần thiết bị/emulator; không chạy được trong kiểm thử Dart VM.
class AppAuthOidcClient implements OidcClient {
  AppAuthOidcClient(this._config, [FlutterAppAuth? appAuth])
    : _appAuth = appAuth ?? const FlutterAppAuth();
  final AppConfig _config;
  final FlutterAppAuth _appAuth;

  @override
  Future<OidcTokens> signIn() async {
    try {
      final r = await _appAuth.authorizeAndExchangeCode(
        AuthorizationTokenRequest(
          _config.oidcClientId,
          _config.oidcRedirectUrl,
          issuer: _config.oidcIssuer,
          scopes: _config.oidcScopes,
          // Buộc đăng nhập lại mật khẩu thay vì dùng phiên trình duyệt cũ.
          promptValues: const ['login'],
        ),
      );
      final at = r.accessToken;
      if (at == null) throw OidcException('Máy chủ không trả access token.');
      return OidcTokens(
        accessToken: at,
        refreshToken: r.refreshToken,
        idToken: r.idToken,
        accessTokenExpiry: r.accessTokenExpirationDateTime,
      );
    } on FlutterAppAuthUserCancelledException {
      throw OidcException('Đã hủy đăng nhập.');
    } on FlutterAppAuthPlatformException catch (e) {
      throw OidcException('Đăng nhập thất bại: ${e.code}');
    }
  }

  @override
  Future<OidcTokens> refresh(String refreshToken) async {
    try {
      final r = await _appAuth.token(
        TokenRequest(
          _config.oidcClientId,
          _config.oidcRedirectUrl,
          issuer: _config.oidcIssuer,
          refreshToken: refreshToken,
          scopes: _config.oidcScopes,
        ),
      );
      final at = r.accessToken;
      if (at == null) throw OidcException('Làm mới token thất bại.');
      return OidcTokens(
        accessToken: at,
        refreshToken: r.refreshToken ?? refreshToken,
        idToken: r.idToken,
        accessTokenExpiry: r.accessTokenExpirationDateTime,
      );
    } on FlutterAppAuthPlatformException catch (e) {
      throw OidcException('Làm mới token thất bại: ${e.code}');
    }
  }
}

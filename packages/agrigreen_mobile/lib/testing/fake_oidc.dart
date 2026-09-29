import '../data/auth/oidc_client.dart';

class FakeOidc implements OidcClient {
  int signIns = 0;
  bool fail = false;

  @override
  Future<OidcTokens> signIn() async {
    if (fail) throw OidcException('Đã hủy đăng nhập.');
    signIns++;
    return OidcTokens(
      accessToken: 'at$signIns',
      refreshToken: 'rt$signIns',
      accessTokenExpiry: DateTime.utc(2100),
    );
  }

  @override
  Future<OidcTokens> refresh(String refreshToken) async =>
      OidcTokens(accessToken: 'at-r', refreshToken: refreshToken);
}

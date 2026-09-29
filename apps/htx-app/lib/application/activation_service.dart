import 'package:agrigreen_mobile/data/dial/dialer.dart';

class ActivationException implements Exception {
  ActivationException(this.message);
  final String message;
  @override
  String toString() => message;
}

/// Kích hoạt tài khoản nông dân bằng liên kết/QR một lần (g2-conventions §1:
/// Keycloak action token do quản lý HTX phát). App **không** xử lý mật khẩu:
/// chỉ mở liên kết trong trình duyệt để nông dân tự đặt mật khẩu trên trang
/// Keycloak, rồi quay lại "Đăng nhập".
///
/// Chỉ mở liên kết `https` tới **đúng máy chủ đăng nhập** của app (chống mở
/// liên kết lạ do người khác gửi — lừa đảo).
class ActivationService {
  ActivationService({required String oidcIssuer, required Dialer opener})
    : _issuer = Uri.parse(oidcIssuer),
      _opener = opener;

  final Uri _issuer;
  final Dialer _opener;

  /// Kiểm liên kết dán/gõ/quét; trả Uri hợp lệ.
  Uri validate(String raw) {
    final t = raw.trim();
    if (t.isEmpty) {
      throw ActivationException('Dán hoặc nhập liên kết kích hoạt được cấp.');
    }
    final u = Uri.tryParse(t);
    if (u == null || !u.hasScheme || u.host.isEmpty) {
      throw ActivationException('Liên kết không hợp lệ.');
    }
    if (u.scheme != 'https') {
      throw ActivationException('Chỉ mở liên kết bảo mật (https).');
    }
    if (u.host.toLowerCase() != _issuer.host.toLowerCase()) {
      throw ActivationException(
        'Liên kết không thuộc máy chủ đăng nhập AgriGreen (${_issuer.host}). '
        'Không mở để tránh lừa đảo.',
      );
    }
    return u;
  }

  Future<void> open(String raw) async {
    final u = validate(raw);
    if (!await _opener.open(u)) {
      throw ActivationException('Máy không mở được trình duyệt.');
    }
  }
}

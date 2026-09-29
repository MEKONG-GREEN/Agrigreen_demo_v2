import 'dart:convert';

/// Vai của App HTX (g2-conventions §5). Quyền thật kiểm ở backend (R-55);
/// app chỉ chọn trang chủ và ẩn/hiện chức năng theo vai.
abstract final class HtxRole {
  static const farmer = 'farmer';
  static const coopManager = 'coop_manager';
}

enum HtxMode {
  /// Nông dân: ruộng của hộ mình.
  farmer,

  /// Quản lý HTX (bộ chức năng di động).
  manager,

  /// Tài khoản không có vai của App HTX.
  none,
}

HtxMode modeFor(Iterable<String> roles) {
  final r = roles.toSet();
  if (r.contains(HtxRole.coopManager)) return HtxMode.manager;
  if (r.contains(HtxRole.farmer)) return HtxMode.farmer;
  return HtxMode.none;
}

/// Vai trong access token (claim `realm_access.roles` của Keycloak). Chỉ đọc
/// phần thân JWT để chọn giao diện khi ngoại tuyến — **không** xác minh chữ ký
/// (máy chủ xác minh mỗi lời gọi). Token hỏng → rỗng.
List<String> rolesFromJwt(String? token) {
  if (token == null) return const [];
  final parts = token.split('.');
  if (parts.length < 2) return const [];
  try {
    final body = utf8.decode(base64Url.decode(base64Url.normalize(parts[1])));
    final j = jsonDecode(body);
    if (j is! Map) return const [];
    final ra = j['realm_access'];
    if (ra is! Map) return const [];
    final roles = ra['roles'];
    if (roles is! List) return const [];
    return [for (final r in roles) '$r'];
  } catch (_) {
    return const [];
  }
}

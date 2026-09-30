import 'dart:convert';

/// Vai của App hiện trường MKG (internal/mkg/fieldops/port). Quyền thật kiểm ở
/// api-internal (R-55); app chỉ ẩn/hiện chức năng theo vai.
abstract final class FieldRole {
  static const collector = 'field_collector';
  static const supervisor = 'field_supervisor';
}

enum FieldMode {
  /// Tổ gom: đếm cuộn, mở/đóng ca.
  collector,

  /// Giám sát gom: như tổ gom + duyệt lệnh gom (không phải của mình) và xem
  /// đối soát chiều mua.
  supervisor,

  /// Tài khoản không có vai của app.
  none,
}

FieldMode modeFor(Iterable<String> roles) {
  final r = roles.toSet();
  if (r.contains(FieldRole.supervisor)) return FieldMode.supervisor;
  if (r.contains(FieldRole.collector)) return FieldMode.collector;
  return FieldMode.none;
}

Map<String, Object?>? _jwtBody(String? token) {
  if (token == null) return null;
  final parts = token.split('.');
  if (parts.length < 2) return null;
  try {
    final body = utf8.decode(base64Url.decode(base64Url.normalize(parts[1])));
    final j = jsonDecode(body);
    return j is Map ? j.cast<String, Object?>() : null;
  } catch (_) {
    return null;
  }
}

/// Vai trong access token (claim `realm_access.roles` của Keycloak). Chỉ đọc
/// thân JWT để chọn giao diện khi ngoại tuyến — **không** xác minh chữ ký (máy
/// chủ xác minh mỗi lời gọi). Token hỏng → rỗng.
List<String> rolesFromJwt(String? token) {
  final ra = _jwtBody(token)?['realm_access'];
  if (ra is! Map) return const [];
  final roles = ra['roles'];
  if (roles is! List) return const [];
  return [for (final r in roles) '$r'];
}

/// `sub` của token = actor_ref mà api-internal ghi vào `created_by` (R-56).
String? subjectFromJwt(String? token) {
  final s = _jwtBody(token)?['sub'];
  return s is String && s.isNotEmpty ? s : null;
}

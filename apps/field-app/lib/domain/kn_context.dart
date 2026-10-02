/// Ngữ cảnh phiên KN v7.7 (P1): một tài khoản có thể có nhiều phân công (vai
/// trò × đơn vị) và mỗi phân công nhiều phòng ban; người dùng chọn một cặp
/// (vai trò, phòng ban) → máy chủ cấp `context_id` gửi lại qua `X-AG-Context`.
library;

class KnCommune {
  const KnCommune(this.code, this.name);
  final String code;
  final String name;

  Map<String, Object?> toJson() => {'code': code, 'name': name};

  static List<KnCommune> listOf(Object? j) => [
    for (final e in (j is List ? j : const []))
      if (e is Map) KnCommune('${e['code']}', '${e['name'] ?? e['code']}'),
  ];
}

/// Nhãn vai trò KN (BRD §5.1).
String knRoleLabel(String role) => switch (role) {
  'kn_xa' => 'Cấp Xã',
  'kn_tinh' => 'Cấp Tỉnh',
  'kn_tw' => 'Cấp Quốc gia',
  'admin' => 'Quản trị',
  _ => role,
};

/// Một cặp chọn được trên màn "Chọn vai trò và phòng ban".
class KnContextChoice {
  const KnContextChoice({
    required this.assignmentId,
    required this.role,
    required this.orgName,
    this.departmentCode,
    this.departmentName,
    this.communes = const [],
  });

  final String assignmentId;
  final String role;
  final String orgName;
  final String? departmentCode;
  final String? departmentName;
  final List<KnCommune> communes;

  String get key => '$assignmentId|${departmentCode ?? ''}';

  String get title =>
      '${knRoleLabel(role)}${departmentName == null ? '' : ' · $departmentName'}';

  Map<String, Object?> toJson() => {
    'assignment_id': assignmentId,
    'role': role,
    'org_name': orgName,
    'department_code': departmentCode,
    'department_name': departmentName,
    'communes': [for (final c in communes) c.toJson()],
  };

  factory KnContextChoice.fromJson(Map<String, Object?> j) => KnContextChoice(
    assignmentId: '${j['assignment_id']}',
    role: '${j['role']}',
    orgName: '${j['org_name'] ?? ''}',
    departmentCode: j['department_code'] as String?,
    departmentName: j['department_name'] as String?,
    communes: KnCommune.listOf(j['communes']),
  );

  /// Mở `GET /v1/me/contexts` thành các cặp chọn được: phân công còn hiệu lực
  /// × phòng ban đang mở (Admin không phòng ban → một cặp).
  static List<KnContextChoice> expand(Map<String, Object?> body) {
    final out = <KnContextChoice>[];
    for (final a in (body['assignments'] as List?) ?? const []) {
      if (a is! Map || a['active'] == false) continue;
      final deps = [
        for (final d in (a['departments'] as List?) ?? const [])
          if (d is Map && (d['status'] == null || d['status'] == 'active')) d,
      ];
      final base = (
        id: '${a['assignment_id']}',
        role: '${a['role']}',
        org: '${a['org_name'] ?? ''}',
        communes: KnCommune.listOf(a['communes']),
      );
      if (deps.isEmpty) {
        // Admin không gắn phòng ban; vai khác không có phòng ban mở → bỏ.
        if (base.role == 'admin') {
          out.add(
            KnContextChoice(
              assignmentId: base.id,
              role: base.role,
              orgName: base.org,
              communes: base.communes,
            ),
          );
        }
        continue;
      }
      for (final d in deps) {
        out.add(
          KnContextChoice(
            assignmentId: base.id,
            role: base.role,
            orgName: base.org,
            departmentCode: '${d['code']}',
            departmentName: '${d['name'] ?? d['code']}',
            communes: base.communes,
          ),
        );
      }
    }
    return out;
  }
}

/// `KNSessionContext` máy chủ cấp (`POST /v1/me/context`).
class KnSessionContext {
  const KnSessionContext({
    required this.contextId,
    required this.role,
    required this.orgName,
    required this.communes,
    required this.defaultRoute,
    required this.expiresAt,
    this.assignmentId,
    this.departmentCode,
    this.departmentName,
    this.supportSessionId,
  });

  final String contextId;
  final String? assignmentId;
  final String role;
  final String? departmentCode;
  final String? departmentName;
  final String orgName;
  final List<KnCommune> communes;
  final String defaultRoute;
  final DateTime? expiresAt;
  final String? supportSessionId;

  bool get isXa => role == 'kn_xa';

  bool expiredAt(DateTime now) =>
      expiresAt != null && !now.toUtc().isBefore(expiresAt!.toUtc());

  String get title =>
      '${knRoleLabel(role)}${departmentName == null ? '' : ' · $departmentName'}';

  /// Dấu vân tay phạm vi (vai + phòng ban + xã) — đổi = dữ liệu KN đã pull
  /// không còn đúng phạm vi.
  String get scopeKey =>
      '$role|${departmentCode ?? ''}|${[for (final c in communes) c.code].join(',')}';

  Map<String, Object?> toJson() => {
    'context_id': contextId,
    'assignment_id': assignmentId,
    'role': role,
    'department_code': departmentCode,
    'department_name': departmentName,
    'org_name': orgName,
    'communes': [for (final c in communes) c.toJson()],
    'default_route': defaultRoute,
    'expires_at': expiresAt?.toUtc().toIso8601String(),
    'support_session_id': supportSessionId,
  };

  factory KnSessionContext.fromJson(Map<String, Object?> j) => KnSessionContext(
    contextId: '${j['context_id']}',
    assignmentId: j['assignment_id'] as String?,
    role: '${j['role']}',
    departmentCode: j['department_code'] as String?,
    departmentName: j['department_name'] as String?,
    orgName: '${j['org_name'] ?? ''}',
    communes: KnCommune.listOf(j['communes']),
    defaultRoute: '${j['default_route'] ?? ''}',
    expiresAt: DateTime.tryParse('${j['expires_at']}'),
    supportSessionId: j['support_session_id'] as String?,
  );
}

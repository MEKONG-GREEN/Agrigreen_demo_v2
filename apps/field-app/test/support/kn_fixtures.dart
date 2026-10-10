import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:agrigreen_mobile/testing.dart';

/// Dữ liệu mẫu KN v7.7 P7 cho kiểm thử (không mạng).

const xaA = '30280';
const xaB = '30281';
const batch1 = '01920000-0000-7000-8000-0000000b0001';
const tpl1 = '01920000-0000-7000-8000-0000000a0001';
const ver1 = '01920000-0000-7000-8000-0000000c0001';
const period1 = '01920000-0000-7000-8000-0000000d0001';
const period2 = '01920000-0000-7000-8000-0000000d0002';
const taskA2 = '01920000-0000-7000-8000-0000000e00a2';
const taskB2 = '01920000-0000-7000-8000-0000000e00b2';
const taskA1 = '01920000-0000-7000-8000-0000000e00a1';
const taskClosed = '01920000-0000-7000-8000-0000000e00cc';

/// Bố cục PL 01 thu gọn: A STT, B Xã (cố định), C Tỉnh giao, D/E/F cột theo
/// tuần (khe 1..3), G công thức lũy kế, H nhập số nguyên bắt buộc 0..100.
Map<String, Object?> layoutData() => {
  'version_id': ver1,
  'template_id': tpl1,
  'code': 'T01',
  'name': 'Tiến độ xuống giống lúa',
  'version': 1,
  'definition': {
    'schema': 'form-template.v1',
    'code': 'T01',
    'title': 'TIẾN ĐỘ XUỐNG GIỐNG {season}',
    'sheet_name': 'PL 01_Tđộ XG {season}',
    'requirements': 'Nhập số tăng thêm của tuần.',
    'columns': [
      {
        'letter': 'A',
        'title': 'STT',
        'kind': 'fixed',
        'dtype': 'integer',
        'fixed_source': 'row_no',
      },
      {
        'letter': 'B',
        'title': 'Xã',
        'kind': 'fixed',
        'dtype': 'text',
        'fixed_source': 'commune_name',
      },
      {
        'letter': 'C',
        'title': 'Kế hoạch tỉnh giao',
        'kind': 'provincial',
        'dtype': 'number',
        'unit': 'ha',
      },
      for (final (l, s) in [('D', 1), ('E', 2), ('F', 3)])
        {
          'letter': l,
          'title': '',
          'kind': 'per_period',
          'dtype': 'number',
          'unit': 'ha',
          'period_block': 'w',
          'period_slot': s,
          'min': '0',
        },
      {
        'letter': 'G',
        'title': 'Lũy kế',
        'kind': 'formula',
        'dtype': 'number',
        'unit': 'ha',
        'formula': 'SUM(D#:F#)',
      },
      {
        'letter': 'H',
        'title': 'Số hộ',
        'kind': 'input',
        'dtype': 'integer',
        'required': true,
        'min': '0',
        'max': '100',
      },
    ],
    'header': {'rows': 1, 'cells': <Object?>[], 'merges': <Object?>[]},
    'period_blocks': [
      {
        'id': 'w',
        'first_letter': 'D',
        'width': 1,
        'slots': 3,
        'title_pattern': 'Tuần {n}',
      },
    ],
    'rows': {'kind': 'per_commune', 'indicators': <Object?>[]},
    'totals': <Object?>[],
    'groups': [
      {'id': 'g1', 'title': 'Tiến độ', 'from': 'D', 'to': 'G'},
    ],
    'freeze_through': 'B',
    'rules': <Object?>[],
    'pending': <Object?>[],
    'completeness': 'complete',
  },
};

Map<String, Object?> taskData({
  required String id,
  String commune = xaA,
  String communeName = 'Xã A',
  String period = period2,
  int slot = 2,
  String status = 'not_started',
  DateTime? due,
  bool closed = false,
  String? adjustmentReason,
  int latestVersion = 0,
  String? latestSubmissionId,
  String periodType = 'week',
  String formCode = 'T01',
}) => {
  'id': id,
  'batch_id': batch1,
  'period_id': period,
  'template_id': tpl1,
  'version_id': ver1,
  'form_code': formCode,
  'form_name': 'Tiến độ xuống giống lúa',
  'commune_code': commune,
  'commune_name': communeName,
  'department_code': 'trong_trot',
  'report_type': 'dinh_ky',
  'period_type': periodType,
  'slot': slot,
  'period_block': true,
  'period_label': 'Tuần ${35 + slot}',
  'period_labels': ['Tuần 36', 'Tuần 37'],
  'season_label': 'THU ĐÔNG 2026',
  'period_from': '2026-09-28',
  'period_to': '2026-10-04',
  'due_at': (due ?? DateTime.utc(2026, 12, 5)).toIso8601String(),
  'auto_close_at': DateTime.utc(2027, 1, 30).toIso8601String(),
  'period_closed': closed,
  'reopened_until': null,
  'status': status,
  'display_status': status,
  'open': !closed,
  'open_slots': closed ? <int>[] : [slot],
  'adjustment_reason': adjustmentReason,
  'latest_version': latestVersion,
  'latest_submission_id': latestSubmissionId,
  'submitted_submission_id': null,
  'submitted_at': null,
  'provincial': {'C': '120'},
  'updated_at': '2026-12-01T01:00:00Z',
};

Map<String, Object?> submissionData({
  required String taskId,
  required String submissionId,
  String commune = xaA,
  int versionNo = 1,
  String kind = 'submitted',
  Map<String, String> input = const {},
  Map<String, String> cells = const {},
  List<Map<String, Object?>> issues = const [],
}) => {
  'task_id': taskId,
  'submission_id': submissionId,
  'version_no': versionNo,
  'kind': kind,
  'rows': [
    {'key': commune, 'input': input, 'cells': cells},
  ],
  'issues': issues,
  'event_time': '2026-12-01T01:00:00Z',
  'recorded_at': '2026-12-01T01:00:00Z',
};

PullChange change(String entity, String id, int v, Map<String, Object?> d) =>
    PullChange(entity: entity, id: id, version: v, op: 'upsert', data: d);

Map<String, Object?> assignment(
  String id, {
  String role = 'kn_xa',
  List<(String, String)> departments = const [('trong_trot', 'Trồng trọt')],
  List<(String, String)> communes = const [(xaA, 'Xã A'), (xaB, 'Xã B')],
}) => {
  'assignment_id': id,
  'user_ref': '01920000-0000-7000-8000-000000000u01',
  'role': role,
  'org_ref': null,
  'org_name': 'TTKN Cần Thơ',
  'mgmt_level': null,
  'mgmt_level_code': null,
  'communes': [
    for (final (c, n) in communes) {'code': c, 'name': n},
  ],
  'departments': [
    for (final (c, n) in departments)
      {'code': c, 'name': n, 'description': null, 'status': 'active'},
  ],
  'valid_from': '2026-01-01T00:00:00Z',
  'valid_to': null,
  'active': true,
};

Map<String, Object?> menuJson() => {
  'menu_version': 'kn.menu/v3',
  'audience': 'kn_xa',
  'org_ref': null,
  'department': null,
  'default_route': '/kn/nhiem-vu/cong-viec',
  'groups': [
    {
      'key': 'dieu_hanh',
      'label': 'Điều hành',
      'items': [
        {
          'key': 'xa.nhiem_vu',
          'label': 'Nhiệm vụ',
          'route': null,
          'icon': 'task',
          'badge_source': null,
          'enabled': true,
          'children': [
            {
              'key': 'xa.nhiem_vu.cong_viec',
              'label': 'Công việc cần làm',
              'route': '/kn/nhiem-vu/cong-viec',
              'icon': 'bell',
              'badge_source': 'extension.report_tasks.todo',
              'enabled': true,
              'children': <Object?>[],
            },
            {
              'key': 'xa.nhiem_vu.lich_cong_tac',
              'label': 'Lịch công tác',
              'route': '/kn/nhiem-vu/lich-cong-tac',
              'icon': 'calendar',
              'badge_source': null,
              'enabled': true,
              'children': <Object?>[],
            },
          ],
        },
      ],
    },
    {
      'key': 'du_lieu',
      'label': 'Dữ liệu',
      'items': [
        {
          'key': 'xa.thu_thap.htx',
          'label': 'Danh sách HTX',
          'route': '/kn/du-lieu/htx',
          'icon': 'coop',
          'badge_source': null,
          'enabled': true,
          'children': <Object?>[],
        },
        {
          'key': 'xa.khong_biet',
          'label': 'Mục lạ',
          'route': '/kn/x',
          'icon': 'x',
          'badge_source': null,
          'enabled': true,
          'children': <Object?>[],
        },
      ],
    },
    // kn.menu/v3 (W11.06): nhóm "Dùng chung" — Contact "Sắp có".
    {
      'key': 'dung_chung',
      'label': 'Dùng chung',
      'items': [
        {
          'key': 'chung.contact',
          'label': 'Contact',
          'route': '/kn/dung-chung/contact',
          'icon': 'phone',
          'badge_source': null,
          'enabled': false,
          'status': 'coming_soon',
          'status_label': 'Sắp có',
          'children': <Object?>[],
        },
      ],
    },
  ],
};

/// Máy chủ KN giả gắn lên [FakeServer]: P1 ngữ cảnh, P2 menu, badge.
class KnFakeBackend {
  KnFakeBackend(this.server, {List<Map<String, Object?>>? assignments})
    : assignments = assignments ?? [assignment('a-1')] {
    server.onGet = (path, q) async {
      if (offline) throw NetworkException('offline');
      switch (path) {
        case '/v1/me/contexts':
          return {'assignments': this.assignments};
        case '/v1/menu':
          return menuJson();
        case '/v1/extension/report-badges':
          return {
            'counts': {'extension.report_tasks.todo': badgeTodo},
          };
      }
      return extraGets[path];
    };
    server.onPost = (path, body) async {
      if (offline) throw NetworkException('offline');
      if (path == '/v1/me/context') {
        selects.add(body ?? const {});
        final a = this.assignments.firstWhere(
          (x) => x['assignment_id'] == body?['assignment_id'],
          orElse: () => throw ApiException(403, 'accounts.context_invalid'),
        );
        return {
          'context_id': 'ctx-${selects.length}',
          'assignment_id': a['assignment_id'],
          'role': a['role'],
          'department_code': body?['department_code'],
          'department_name': body?['department_code'] == null
              ? null
              : 'Phòng ${body?['department_code']}',
          'org_ref': null,
          'org_name': a['org_name'],
          'communes': a['communes'],
          'default_route': '/kn/nhiem-vu/cong-viec',
          'expires_at': expiresAt.toIso8601String(),
          'support_session_id': null,
          'on_behalf_of': null,
        };
      }
      return null;
    };
  }

  final FakeServer server;
  List<Map<String, Object?>> assignments;
  final selects = <Map<String, Object?>>[];
  final extraGets = <String, Map<String, Object?>>{};
  bool offline = false;
  int badgeTodo = 3;
  DateTime expiresAt = DateTime.utc(2026, 12, 3);
}

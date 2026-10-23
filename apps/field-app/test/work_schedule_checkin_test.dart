import 'package:agrigreen_field/application/work_schedule_service.dart';
import 'package:agrigreen_field/domain/work_schedule.dart';
import 'package:agrigreen_mobile/data/actions/action_queue.dart';
import 'package:agrigreen_mobile/data/crypto/envelope.dart';
import 'package:agrigreen_mobile/data/photo/location_source.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/harness.dart';

// W11.46 (BRD CNTS FN-12, CV-07): check-in GPS tại địa điểm công tác trên App hiện trường — xếp lệnh idempotent khi
// mất mạng, chỉ mục ngoài cơ quan của hôm nay, sau khi đã gửi lịch.

/// 09:00 Thứ Tư 14/10/2026 giờ Việt Nam.
final _now = DateTime.utc(2026, 10, 14, 2, 0);
final _week = DateTime.utc(2026, 10, 12);
const _sid = '01920000-0000-7000-8000-0000000000s1';

Map<String, Object?> _item(
  String id,
  String day, {
  String? commune,
  Map<String, Object?>? place,
}) => {
  'id': id,
  'day': day,
  'session': 'morning',
  'content': 'Kiểm tra lò mổ',
  'commune_code': commune,
  'place': place,
  'changed': false,
  'conflict': false,
};

Map<String, Object?> _mine({
  required String status,
  List<Object?> checkins = const [],
  int? approvedVersion,
}) => {
  'week_start': '2026-10-12',
  'submit_due_at': '2026-10-09T10:00:00Z',
  'review_due_at': '2026-10-12T00:00:00Z',
  'locked': false,
  'late_now': false,
  'previous_available': false,
  'checkin_enabled': true,
  'schedule': {
    'schedule': {
      'id': _sid,
      'status': status,
      'version': 2,
      'approved_version': approvedVersion,
      'commune_codes': ['30001'],
    },
    'items': [
      _item(
        'i-today',
        '2026-10-14',
        commune: '30001',
        place: {'lat': 10.0, 'lon': 105.0, 'source': 'map'},
      ),
      _item('i-office', '2026-10-14'),
      _item('i-tomorrow', '2026-10-15', commune: '30001'),
    ],
    'approved_items': <Object?>[],
    'warnings': <Object?>[],
    'can': {'edit': true, 'submit': true},
    'checkin_enabled': true,
    'checkins': checkins,
  },
};

void main() {
  late Harness h;
  late ActionQueue actions;
  late WorkScheduleService svc;
  var status = 'approved';
  var checkins = <Object?>[];
  int? approvedVersion;

  setUp(() async {
    h = await Harness.create(now: _now);
    h.fake.onGet = (path, q) async =>
        path == '/v1/extension/work-schedules/mine'
        ? _mine(
            status: status,
            checkins: checkins,
            approvedVersion: approvedVersion,
          )
        : null;
    actions = ActionQueue(
      db: h.db,
      clock: h.clock,
      ids: h.ids,
      extraKinds: CntsActionKind.values,
    );
    svc = WorkScheduleService(
      db: h.db,
      api: h.server,
      actions: actions,
      clock: h.clock,
      ids: h.ids,
      deviceId: 'dev-1',
      location: const FixedLocationSource(
        LocationResult(
          GpsStatus.ok,
          GpsFix(lat: 10.001, lon: 105.0, accuracyM: 8),
        ),
      ),
    );
    status = 'approved';
    checkins = [];
    approvedVersion = null;
  });

  test('log bug TT-07/CN-06: chỉ check-in theo lịch đã duyệt; điều chỉnh chờ duyệt còn bản đã duyệt thì vẫn được', () async {
    status = 'pending';
    var v = await svc.load(_week, refresh: true);
    final today = v.items.firstWhere((i) => i.id == 'i-today');
    expect(
      v.canCheckin(today, _now),
      isFalse,
      reason: 'gửi duyệt nhưng chưa được duyệt',
    );
    approvedVersion = 1;
    v = await svc.load(_week, refresh: true);
    expect(
      v.canCheckin(v.items.firstWhere((i) => i.id == 'i-today'), _now),
      isTrue,
      reason: 'bản đã duyệt v1 còn hiệu lực trong lúc điều chỉnh chờ duyệt',
    );
  });
  tearDown(() => h.dispose());

  test('mục ngoài cơ quan hôm nay: xếp lệnh check-in (giờ thiết bị, sai số), hiện "chờ đồng bộ"', () async {
    var v = await svc.load(_week);
    expect(v.checkinEnabled, isTrue);
    final today = v.items.firstWhere((i) => i.id == 'i-today');
    expect(today.place?.lat, 10.0);
    expect(v.canCheckin(today, _now), isTrue);
    expect(
      v.canCheckin(v.items.firstWhere((i) => i.id == 'i-office'), _now),
      isFalse,
      reason: 'làm việc tại cơ quan',
    );
    expect(
      v.canCheckin(v.items.firstWhere((i) => i.id == 'i-tomorrow'), _now),
      isFalse,
      reason: 'không phải hôm nay',
    );

    v = await svc.checkin(_week, today);
    final acts = await actions.ofKind(CntsActionKind.workScheduleCheckin);
    expect(acts, hasLength(1));
    expect(
      acts.single.path,
      '/v1/extension/work-schedules/$_sid/items/i-today/checkin',
    );
    expect(acts.single.body?['accuracy_m'], 8);
    expect(acts.single.body?['captured_at'], _now.toIso8601String());
    expect(
      v.items.firstWhere((i) => i.id == 'i-today').checkin?.pending,
      isTrue,
    );
    expect(
      v.canCheckin(v.items.firstWhere((i) => i.id == 'i-today'), _now),
      isFalse,
      reason: 'một mục một check-in',
    );
  });

  test('máy chủ trả check-in: hiện khoảng cách, cờ xa; lịch nháp không check-in được', () async {
    checkins = [
      {
        'id': 'c1',
        'item_id': 'i-today',
        'captured_at': '2026-10-14T01:55:00Z',
        'distance_m': 612.4,
        'far': true,
      },
    ];
    var v = await svc.load(_week);
    final ck = v.items.firstWhere((i) => i.id == 'i-today').checkin!;
    expect(ck.far, isTrue);
    expect(ck.label, contains('612 m'));

    status = 'draft';
    checkins = [];
    v = await svc.load(_week);
    await expectLater(
      svc.checkin(_week, v.items.firstWhere((i) => i.id == 'i-today')),
      throwsA(isA<WorkScheduleError>()),
    );
  });

  test('sửa mục trên app giữ địa điểm bản đồ trong thân lệnh', () {
    final i = WsItem.fromJson(
      _item('x', '2026-10-14', place: {'lat': 10.5, 'lon': 106.0}),
    )!;
    expect(i.toInput()['place'], {'lat': 10.5, 'lon': 106.0, 'source': 'map'});
  });
}

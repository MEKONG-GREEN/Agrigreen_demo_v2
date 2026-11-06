import 'dart:convert';
import 'dart:typed_data';

import 'package:agrigreen_field/application/visit_photo.dart';
import 'package:agrigreen_field/application/work_schedule_service.dart';
import 'package:agrigreen_field/domain/work_schedule.dart';
import 'package:agrigreen_mobile/data/actions/action_queue.dart';
import 'package:agrigreen_mobile/data/crypto/envelope.dart';
import 'package:agrigreen_mobile/data/photo/location_source.dart';
import 'package:agrigreen_mobile/data/photo/photo_pipeline.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:image/image.dart' as img;

import 'support/harness.dart';

// W11.46 (BRD CNTS FN-12, CV-07): check-in GPS tại địa điểm công tác trên App hiện trường — xếp lệnh idempotent khi
// mất mạng, chỉ mục ngoài cơ quan của hôm nay, sau khi đã gửi lịch.
// CNTS-V4-CHECKOUT-PHOTO (#249, BRD CNTS v4.0 FN-12 CV-07/CV-08): check-in, check-out bắt buộc ảnh (nén ≤ 500 KB),
// check-out chỉ sau check-in, lệnh chờ ngoại tuyến giữ thời điểm lúc bấm.

/// Camera giả: ảnh JPEG nhỏ, hoặc null (người dùng hủy chụp).
class _Camera implements CameraSource {
  _Camera();
  bool cancel = false;
  int shots = 0;
  static final jpeg = Uint8List.fromList(
    img.encodeJpg(img.Image(width: 32, height: 24), quality: 80),
  );
  @override
  Future<CapturedImage?> capture() async {
    shots++;
    return cancel ? null : CapturedImage(jpeg, 'image/jpeg');
  }
}

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

final _todayApproved = _item(
  'i-today',
  '2026-10-14',
  commune: '30001',
  place: {'lat': 10.0, 'lon': 105.0, 'source': 'map'},
);

Map<String, Object?> _mine({
  required String status,
  List<Object?> checkins = const [],
  int? approvedVersion,
  List<Object?>? items,
  List<Object?> approvedItems = const [],
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
    'items':
        items ??
        [
          _todayApproved,
          _item('i-office', '2026-10-14'),
          _item('i-tomorrow', '2026-10-15', commune: '30001'),
        ],
    'approved_items': approvedItems,
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
  late _Camera camera;
  var status = 'approved';
  var checkins = <Object?>[];
  int? approvedVersion;
  List<Object?>? items;
  var approvedItems = <Object?>[];

  setUp(() async {
    h = await Harness.create(now: _now);
    h.fake.onGet = (path, q) async =>
        path == '/v1/extension/work-schedules/mine'
        ? _mine(
            status: status,
            checkins: checkins,
            approvedVersion: approvedVersion,
            items: items,
            approvedItems: approvedItems,
          )
        : null;
    actions = ActionQueue(
      db: h.db,
      clock: h.clock,
      ids: h.ids,
      extraKinds: CntsActionKind.values,
    );
    camera = _Camera();
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
      camera: camera,
    );
    status = 'approved';
    checkins = [];
    approvedVersion = null;
    items = null;
    approvedItems = [];
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
    expect(
      v.checkinBlockedReason(today, _now),
      contains('lịch chưa được duyệt'),
    );
    approvedVersion = 1;
    approvedItems = [_todayApproved];
    v = await svc.load(_week, refresh: true);
    expect(
      v.canCheckin(v.items.firstWhere((i) => i.id == 'i-today'), _now),
      isTrue,
      reason: 'bản đã duyệt v1 còn hiệu lực trong lúc điều chỉnh chờ duyệt',
    );
  });

  test('R-B3 (review #99): mục chỉ có ở bản điều chỉnh chờ duyệt KHÔNG check-in được, báo lý do; máy không xếp lệnh', () async {
    status = 'pending';
    approvedVersion = 1;
    approvedItems = [_todayApproved];
    items = [_todayApproved, _item('i-new', '2026-10-14', commune: '30001')];
    final v = await svc.load(_week, refresh: true);
    final added = v.items.firstWhere((i) => i.id == 'i-new');
    expect(added.fieldWork, isTrue);
    expect(v.canCheckin(added, _now), isFalse);
    expect(v.checkinBlockedReason(added, _now), contains('điều chỉnh'));
    expect(
      v.canCheckin(v.items.firstWhere((i) => i.id == 'i-today'), _now),
      isTrue,
      reason: 'mục có trong bản đã duyệt vẫn check-in được',
    );
    await expectLater(
      svc.checkin(_week, added),
      throwsA(isA<WorkScheduleError>()),
    );
    expect(await actions.ofKind(CntsActionKind.workScheduleCheckin), isEmpty);
  });

  test('R-B3 (review #99): điều chỉnh đổi ngày/địa điểm — xét theo bản ĐÃ DUYỆT như máy chủ', () async {
    status = 'pending';
    approvedVersion = 1;
    // Bản đã duyệt: A hôm nay ngoài cơ quan, B ngày mai. Bản điều chỉnh: A dời sang ngày mai, B dời về hôm nay.
    approvedItems = [
      _todayApproved,
      _item('i-b', '2026-10-15', commune: '30001'),
    ];
    items = [
      _item('i-today', '2026-10-15', commune: '30001'),
      _item('i-b', '2026-10-14', commune: '30001'),
    ];
    var v = await svc.load(_week, refresh: true);
    expect(
      v.canCheckin(v.items.firstWhere((i) => i.id == 'i-today'), _now),
      isTrue,
      reason: 'bản đã duyệt: A là hôm nay',
    );
    final b = v.items.firstWhere((i) => i.id == 'i-b');
    expect(
      v.canCheckin(b, _now),
      isFalse,
      reason: 'bản đã duyệt: B là ngày mai',
    );
    expect(v.checkinBlockedReason(b, _now), contains('bản đã duyệt'));
    // Bản đã duyệt: C hôm nay nhưng tại cơ quan; điều chỉnh thêm địa điểm → vẫn chưa check-in được.
    approvedItems = [_item('i-office', '2026-10-14')];
    items = [_item('i-office', '2026-10-14', commune: '30001')];
    v = await svc.load(_week, refresh: true);
    expect(
      v.canCheckin(v.items.firstWhere((i) => i.id == 'i-office'), _now),
      isFalse,
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
      base64Decode(acts.single.body!['photo']! as String),
      _Camera.jpeg,
      reason: 'CNTS v4.0: check-in kèm ảnh chụp tại chỗ',
    );
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

  test(
    'CNTS v4.0 FN-12: hủy chụp ảnh → không check-in, không xếp lệnh',
    () async {
      camera.cancel = true;
      final v = await svc.load(_week);
      await expectLater(
        svc.checkin(_week, v.items.firstWhere((i) => i.id == 'i-today')),
        throwsA(
          isA<WorkScheduleError>().having((e) => e.field, 'field', 'photo'),
        ),
      );
      expect(await actions.ofKind(CntsActionKind.workScheduleCheckin), isEmpty);
    },
  );

  test('CV-08: nút Check-out chỉ sau check-in; check-out ngoại tuyến kèm ảnh, giữ thời điểm lúc bấm (AC-11)', () async {
    var v = await svc.load(_week);
    final today = v.items.firstWhere((i) => i.id == 'i-today');
    expect(v.canCheckout(today, _now), isFalse, reason: 'chưa check-in');
    await expectLater(
      svc.checkout(_week, today),
      throwsA(isA<WorkScheduleError>()),
    );
    v = await svc.checkin(_week, today);
    expect(
      v.canCheckout(v.items.firstWhere((i) => i.id == 'i-today'), _now),
      isTrue,
      reason: 'check-in đang chờ đồng bộ vẫn check-out được',
    );
    h.clock.advance(const Duration(hours: 2, minutes: 30));
    v = await svc.checkout(_week, today);
    final acts = await actions.ofKind(CntsActionKind.workScheduleCheckout);
    expect(acts, hasLength(1));
    expect(
      acts.single.path,
      '/v1/extension/work-schedules/$_sid/items/i-today/checkout',
    );
    expect(
      acts.single.body?['captured_at'],
      _now.add(const Duration(hours: 2, minutes: 30)).toIso8601String(),
    );
    expect(base64Decode(acts.single.body!['photo']! as String), _Camera.jpeg);
    final ck = v.items.firstWhere((i) => i.id == 'i-today').checkin!;
    expect(ck.checkout?.pending, isTrue);
    expect(ck.label, contains('Đã check-out (chờ đồng bộ)'));
    expect(
      v.canCheckout(
        v.items.firstWhere((i) => i.id == 'i-today'),
        h.clock.now(),
      ),
      isFalse,
      reason: 'một mục một check-out',
    );
    expect(camera.shots, 2);
  });

  test('máy chủ trả check-out: thời gian ở địa điểm, Đồng bộ trễ (AC-12, AC-13); check-in cũ không ảnh', () async {
    checkins = [
      {
        'id': 'c1',
        'item_id': 'i-today',
        'captured_at': '2026-10-14T01:00:00Z',
        'distance_m': 20,
        'far': false,
        'legacy_no_photo': false,
        'checkout': {
          'captured_at': '2026-10-14T04:30:00Z',
          'far': true,
          'stay_minutes': 210,
          'synced_late': true,
        },
      },
      {
        'id': 'c2',
        'item_id': 'i-tomorrow',
        'captured_at': '2026-10-15T01:00:00Z',
        'legacy_no_photo': true,
        'checkout': null,
      },
    ];
    final v = await svc.load(_week);
    final ck = v.items.firstWhere((i) => i.id == 'i-today').checkin!;
    expect(ck.checkout?.stayMinutes, 210);
    expect(
      ck.label,
      contains('Check-out xa địa điểm · ở 3 giờ 30 phút · Đồng bộ trễ'),
    );
    expect(
      v.canCheckout(v.items.firstWhere((i) => i.id == 'i-today'), _now),
      isFalse,
    );
    expect(
      v.items.firstWhere((i) => i.id == 'i-tomorrow').checkin!.label,
      contains('dữ liệu cũ, không ảnh'),
    );
    expect(stayLabel(120), '2 giờ');
    expect(stayLabel(45), '45 phút');
  });

  test('ảnh lớn được nén về JPEG ≤ 500 KB; dữ liệu không phải ảnh → null', () {
    final noisy = img.Image(width: 2400, height: 1800);
    var seed = 7;
    for (final p in noisy) {
      seed = (seed * 1103515245 + 12345) & 0x7fffffff;
      p
        ..r = seed & 0xff
        ..g = (seed >> 8) & 0xff
        ..b = (seed >> 16) & 0xff;
    }
    final big = Uint8List.fromList(img.encodePng(noisy));
    expect(big.length, greaterThan(maxVisitPhotoBytes));
    final out = compressVisitPhotoSync(big)!;
    expect(out.length, lessThanOrEqualTo(maxVisitPhotoBytes));
    expect(out.sublist(0, 3), [0xFF, 0xD8, 0xFF]);
    expect(compressVisitPhotoSync(_Camera.jpeg), same(_Camera.jpeg));
    expect(
      compressVisitPhotoSync(Uint8List.fromList(utf8.encode('không phải ảnh'))),
      isNull,
    );
  });
}

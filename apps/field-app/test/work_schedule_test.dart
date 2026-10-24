import 'package:agrigreen_field/application/work_schedule_service.dart';
import 'package:agrigreen_field/domain/work_schedule.dart';
import 'package:agrigreen_field/ui/navigation.dart';
import 'package:agrigreen_mobile/core/ids.dart';
import 'package:agrigreen_mobile/data/actions/action_queue.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/harness.dart';

/// 01:00 Thứ Sáu 02/10/2026 giờ Việt Nam (UTC vẫn là Thứ Năm 01/10).
final _now = DateTime.utc(2026, 10, 1, 18, 0);
final _week = DateTime.utc(2026, 10, 5); // tuần kế tiếp
final _prev = DateTime.utc(2026, 9, 28);

/// Máy chủ lịch công tác giả theo hợp đồng `/v1/extension/work-schedules*`.
class _WsServer {
  _WsServer(this.h) {
    h.fake.onGet = (path, q) async {
      if (offline) throw NetworkException('offline');
      if (path == '/v1/extension/work-schedules/mine') {
        mineCalls.add(q?['week_start']);
        return mine(q!['week_start']!);
      }
      if (path.endsWith('/log')) {
        return {
          'items': [
            {
              'seq': 2,
              'event': 'submitted',
              'actor_ref': '01920000-0000-7000-8000-0000000000e1',
              'actor_display': 'Nguyễn Văn An',
              'at': '2026-10-02T09:00:00+07:00',
              'recorded_at': '2026-10-02T09:00:00+07:00',
              'changes': <Object?>[],
              'reason': null,
            },
            {
              'seq': 1,
              'event': 'draft_saved',
              'actor_ref': '01920000-0000-7000-8000-0000000000e1',
              'actor_display': 'Nguyễn Văn An',
              'at': '2026-10-02T08:00:00+07:00',
              'recorded_at': '2026-10-02T08:00:00+07:00',
              'changes': <Object?>[],
              'reason': null,
            },
          ],
          'next_cursor': null,
        };
      }
      return null;
    };
    h.fake.onPost = (path, body) async {
      if (offline) throw NetworkException('offline');
      if (path == '/v1/extension/work-schedules') return _save(body!);
      if (path == '/v1/extension/work-schedules/copy-previous') {
        return _copy(body!['week_start']! as String);
      }
      return null;
    };
  }

  final Harness h;
  bool offline = false;
  final Set<String> lockedWeeks = {};
  final Map<String, Map<String, Object?>> detail = {};
  final List<Map<String, Object?>> saves = [];
  final List<String?> mineCalls = [];

  /// Phạm vi người gọi theo phiên KN (`owner_level`, `commune_codes` ở cấp
  /// trên của `mine`) — null: bản máy chủ cũ chưa trả.
  Map<String, Object?>? scope;

  Map<String, Object?> mine(String week) {
    final ws = parseYmd(week)!;
    return {
      ...?scope,
      'week_start': week,
      'week_end': ymd(ws.add(const Duration(days: 6))),
      'submit_due_at': '2026-10-02T17:00:00+07:00',
      'review_due_at': '2026-10-05T00:00:00+07:00',
      'locked': lockedWeeks.contains(week),
      'late_now': false,
      'schedule': detail[week],
      'previous_week_start': ymd(ws.subtract(const Duration(days: 7))),
      'previous_available':
          detail[ymd(ws.subtract(const Duration(days: 7)))] != null,
    };
  }

  /// Ghi trực tiếp một phiên bản (thiết bị khác / web).
  Map<String, Object?> put(
    String week, {
    required String id,
    required int version,
    String status = 'draft',
    List<Map<String, Object?>> items = const [],
  }) => detail[week] = {
    'schedule': {
      'id': id,
      'week_start': week,
      'status': status,
      'version': version,
      'approved_version': status == 'approved' ? version : null,
      'amendment': false,
      'late': false,
      'locked': false,
      'last_reason': null,
      'commune_codes': ['30280'],
    },
    'items': [
      for (final i in items) {...i, 'changed': false, 'conflict': false},
    ],
    'approved_items': <Object?>[],
    'warnings': <Object?>[],
    'versions': <Object?>[],
    'can': {
      'edit': true,
      'submit': true,
      'approve': false,
      'request_changes': false,
    },
  };

  Map<String, Object?> _save(Map<String, Object?> b) {
    saves.add(b);
    final week = b['week_start']! as String;
    if (lockedWeeks.contains(week)) {
      throw ApiException(409, WorkScheduleService.weekLockedCode);
    }
    final cur = detail[week]?['schedule'] as Map<String, Object?>?;
    final v = cur?['version'] as int?;
    if (b['base_version'] != v) {
      throw ApiException(409, WorkScheduleService.versionConflictCode);
    }
    return put(
      week,
      id: (cur?['id'] ?? b['id'])! as String,
      version: (v ?? 0) + 1,
      status: b['action'] == 'submit' ? 'pending' : 'draft',
      items: (b['items']! as List).cast<Map<String, Object?>>(),
    );
  }

  Map<String, Object?> _copy(String week) {
    final ws = parseYmd(week)!;
    final prev = detail[ymd(ws.subtract(const Duration(days: 7)))];
    if (prev == null) throw ApiException(404, 'extension.schedule_not_found');
    return put(
      week,
      id: '01920000-0000-7000-8000-00000000c0c0',
      version: 1,
      items: [
        for (final i in (prev['items']! as List).cast<Map<String, Object?>>())
          {
            ...i,
            'day': ymd(
              parseYmd(i['day']! as String)!.add(const Duration(days: 7)),
            ),
          },
      ],
    );
  }
}

WsItem _item(
  String id,
  DateTime day, {
  WsSession session = WsSession.morning,
  String? start = '07:30',
  String? end = '11:00',
  String content = 'Kiểm tra rầy nâu HTX Mỹ Hòa',
  String? participants,
  String? note,
  String? commune = '30280',
  String? location = 'Ấp 1',
}) => WsItem(
  id: id,
  day: day,
  session: session,
  startTime: start,
  endTime: end,
  content: content,
  communeCode: commune,
  location: location,
  participants: participants,
  note: note,
);

void main() {
  group('tuần (giờ Việt Nam)', () {
    test('mặc định tuần kế tiếp, tính theo ngày Việt Nam', () {
      expect(currentWeekStart(_now), DateTime.utc(2026, 9, 28));
      expect(nextWeekStart(_now), _week);
      expect(_week.weekday, DateTime.monday);
      // 00:30 Thứ Hai 05/10 giờ VN (UTC còn Chủ Nhật 04/10).
      final mon = DateTime.utc(2026, 10, 4, 17, 30);
      expect(currentWeekStart(mon), _week);
      expect(nextWeekStart(mon), DateTime.utc(2026, 10, 12));
    });

    test(
      'hạn đăng ký mặc định 17:00 Thứ Sáu tuần trước; khóa sau Chủ Nhật',
      () {
        expect(defaultSubmitDue(_week), DateTime.utc(2026, 10, 2, 10));
        expect(vnDateTimeLabel(defaultSubmitDue(_week)), 'Thứ Sáu 02/10 17:00');
        expect(weekEnded(_week, DateTime.utc(2026, 10, 11, 16, 59)), isFalse);
        expect(weekEnded(_week, DateTime.utc(2026, 10, 11, 17)), isTrue);
        expect(weekLabel(_week), '05/10 – 11/10/2026');
      },
    );

    test('parseYmd từ chối ngày sai', () {
      expect(parseYmd('2026-10-05'), _week);
      expect(parseYmd('2026-02-30'), isNull);
      expect(parseYmd('05/10/2026'), isNull);
    });
  });

  group('kiểm mục như máy chủ', () {
    void bad(WsItem it, String field) => expect(
      () => validateItem(it, _week),
      throwsA(isA<WorkScheduleError>().having((e) => e.field, 'field', field)),
    );

    test('hợp lệ', () {
      validateItem(_item('a', _week), _week);
      validateItem(
        _item('b', _week.add(const Duration(days: 6)), start: null, end: null),
        _week,
      );
    });

    test('nội dung 3..500, ngày trong tuần, giờ, kết thúc ≥ bắt đầu', () {
      bad(_item('a', _week, content: 'ab'), 'content');
      bad(_item('a', _week, content: 'x' * 501), 'content');
      bad(_item('a', _prev), 'day');
      bad(_item('a', _week.add(const Duration(days: 7))), 'day');
      bad(_item('a', _week, start: '7:30'), 'start_time');
      bad(_item('a', _week, end: '24:00'), 'end_time');
      bad(_item('a', _week, start: '10:00', end: '09:00'), 'end_time');
      bad(_item('a', _week, start: null, end: '09:00'), 'start_time');
      // Log bug TT-05/CN-04: Địa điểm bắt buộc (chữ hoặc vị trí); cán bộ có xã phụ trách bắt buộc Xã/phường.
      bad(_item('a', _week, location: ' '), 'location');
      validateItem(
        WsItem(
          id: 'g',
          day: _week,
          session: WsSession.morning,
          content: 'Thăm đồng',
          place: const WsPlace(lat: 10, lon: 105),
        ),
        _week,
      );
      expect(
        () => validateItem(
          _item('a', _week, commune: null),
          _week,
          requireCommune: true,
        ),
        throwsA(
          isA<WorkScheduleError>().having(
            (e) => e.field,
            'field',
            'commune_code',
          ),
        ),
      );
      validateItem(_item('a', _week, commune: null), _week);
      validateItem(_item('a', _week, start: '09:00', end: '09:00'), _week);
    });

    test('không ghi SĐT/CCCD trong thành phần phối hợp, ghi chú', () {
      bad(
        _item('a', _week, participants: 'Anh Ba 0912 345 678'),
        'participants',
      );
      bad(_item('a', _week, note: 'CCCD 079123456789'), 'note');
      validateItem(_item('a', _week, participants: 'Cán bộ HTX, BVTV'), _week);
    });
  });

  group('trùng thời gian (cảnh báo)', () {
    test('giờ chồng nhau cùng ngày', () {
      final ids = overlappingIds([
        _item('a', _week, start: '07:30', end: '09:00'),
        _item('b', _week, start: '08:30', end: '10:00'),
        _item('c', _week, start: '09:00', end: '09:30'), // chồng b
        _item('d', _week.add(const Duration(days: 1))),
      ]);
      expect(ids, {'a', 'b', 'c'});
    });

    test('nối tiếp không trùng; cùng buổi không giờ là trùng', () {
      expect(
        overlappingIds([
          _item('a', _week, start: '07:00', end: '09:00'),
          _item('b', _week, start: '09:00', end: '11:00'),
        ]),
        isEmpty,
      );
      expect(
        overlappingIds([
          _item('a', _week, start: null, end: null),
          _item('b', _week, start: null, end: null),
          _item(
            'c',
            _week,
            session: WsSession.afternoon,
            start: null,
            end: null,
          ),
        ]),
        {'a', 'b'},
      );
    });
  });

  group('WorkScheduleService', () {
    late Harness h;
    late _WsServer srv;
    late WorkScheduleService svc;

    setUp(() async {
      h = await Harness.create(now: _now);
      srv = _WsServer(h);
      svc = WorkScheduleService(
        db: h.db,
        api: h.server,
        actions: h.actions,
        clock: h.clock,
        ids: h.ids,
        deviceId: testDeviceId,
        syncNow: () async {
          await h.sync.syncOnce(force: true);
          await svc.refreshAfterSync();
        },
      );
    });
    tearDown(() => h.dispose());

    test(
      'có mạng: lưu nháp rồi gửi duyệt — lệnh idempotent, base_version',
      () async {
        expect(svc.defaultWeek(), _week);
        var v = await svc.load(_week);
        expect(srv.mineCalls, ['2026-10-05']);
        expect(v.status, isNull);
        expect(v.statusLabel, 'Chưa đăng ký');
        expect(v.submitDueAt, DateTime.parse('2026-10-02T17:00:00+07:00'));
        expect(v.editable, isTrue);

        v = await svc.upsertItem(_week, _item(svc.newItemId(), _week));
        expect(v.dirty, isTrue);
        expect(v.items, hasLength(1));

        v = await svc.save(_week, submit: false);
        expect(srv.saves, hasLength(1));
        final b1 = srv.saves.single;
        expect(isUuidV7(b1['id']! as String), isTrue);
        expect(b1['week_start'], '2026-10-05');
        expect(b1['action'], 'draft');
        expect(b1['base_version'], isNull);
        expect(b1['device_id'], testDeviceId);
        expect(
          (b1['items']! as List).single,
          containsPair('content', 'Kiểm tra rầy nâu HTX Mỹ Hòa'),
        );
        // Đã lên máy chủ: bỏ bản nháp trên máy, hiện phiên bản 1.
        expect(v.hasLocalDraft, isFalse);
        expect(v.status, WsStatus.draft);
        expect(v.version, 1);
        expect(v.pendingSync, isFalse);

        v = await svc.upsertItem(
          _week,
          _item(
            svc.newItemId(),
            _week.add(const Duration(days: 1)),
            session: WsSession.afternoon,
            start: '13:30',
            end: '16:00',
            content: 'Điều tra sâu bệnh ấp 3',
          ),
        );
        v = await svc.save(_week, submit: true);
        final b2 = srv.saves.last;
        expect(b2['action'], 'submit');
        expect(b2['base_version'], 1);
        expect(b2['id'], b1['id']);
        expect(b2['items'] as List, hasLength(2));
        expect(v.status, WsStatus.pending);
        expect(v.statusLabel, 'Chờ duyệt');
        expect(v.version, 2);

        // Mỗi lệnh một Idempotency-Key, không gửi lặp ở lượt đồng bộ sau.
        final calls = h.fake.actionCalls
            .where((c) => c.$1 == '/v1/extension/work-schedules')
            .toList();
        expect(calls, hasLength(2));
        expect(calls[0].$2, isNot(calls[1].$2));
        await h.sync.syncOnce(force: true);
        expect(srv.saves, hasLength(2));
        final acts = await h.actions.ofKind(ApiActionKind.workScheduleSave);
        expect(acts.map((a) => a.status), everyElement(ApiActionStatus.done));
        expect(acts.first.path, '/v1/extension/work-schedules');
        expect(acts.first.target, '2026-10-05');
      },
    );

    test('ngoại tuyến: sửa, lưu nhiều lần → một POST khi có mạng', () async {
      srv.offline = true;
      var v = await svc.load(_week);
      expect(v.offline, isTrue);
      expect(v.fetchedAt, isNull);
      expect(v.editable, isTrue);
      final a = svc.newItemId();
      await svc.upsertItem(_week, _item(a, _week));
      v = await svc.save(_week, submit: false);
      expect(v.pendingSync, isTrue);
      expect(v.pendingAction, 'draft');
      expect(v.hasLocalDraft, isTrue);
      expect(v.statusLabel, 'Nháp (trên máy)');

      await svc.upsertItem(
        _week,
        _item(a, _week, content: 'Họp tổ khuyến nông'),
      );
      v = await svc.save(_week, submit: true);
      expect(v.pendingAction, 'submit');
      expect(v.statusLabel, 'Chờ đồng bộ để gửi duyệt');
      expect(await h.actions.pendingCount(), 1);
      final all = await h.actions.ofKind(ApiActionKind.workScheduleSave);
      expect(all.first.status, ApiActionStatus.failed);
      expect(all.first.lastError, ActionQueue.supersededCode);

      // App bị tắt rồi mở lại: bản nháp và lệnh còn nguyên trên đĩa.
      await h.reopen();
      svc = WorkScheduleService(
        db: h.db,
        api: h.server,
        actions: h.actions,
        clock: h.clock,
        ids: h.ids,
      );
      v = await svc.load(_week, refresh: false);
      expect(v.pendingAction, 'submit');
      expect(v.items.single.content, 'Họp tổ khuyến nông');

      srv.offline = false;
      await h.sync.syncOnce(force: true);
      await svc.refreshAfterSync();
      expect(srv.saves, hasLength(1));
      expect(srv.saves.single['action'], 'submit');
      expect(srv.saves.single['base_version'], isNull);
      v = await svc.load(_week);
      expect(v.hasLocalDraft, isFalse);
      expect(v.status, WsStatus.pending);
      expect(v.items.single.content, 'Họp tổ khuyến nông');

      // Mất mạng lần nữa: hiện bản đệm.
      srv.offline = true;
      v = await svc.load(_week);
      expect(v.offline, isTrue);
      expect(v.fetchedAt, isNotNull);
      expect(v.status, WsStatus.pending);
    });

    test('R-B1 (review #99): tuần mới (schedule null) — cán bộ xã vẫn có xã phụ trách, Xã/phường bắt buộc', () async {
      srv.scope = {
        'owner_level': 'commune',
        'commune_codes': ['30280', '30281'],
      };
      var v = await svc.load(_week);
      expect(v.scheduleId, isNull, reason: 'tuần chưa đăng ký');
      expect(v.communeCodes, ['30280', '30281']);
      expect(v.requireCommune, isTrue);
      await expectLater(
        svc.upsertItem(_week, _item(svc.newItemId(), _week, commune: null)),
        throwsA(
          isA<WorkScheduleError>().having(
            (e) => e.field,
            'field',
            'commune_code',
          ),
        ),
      );
      v = await svc.upsertItem(
        _week,
        _item(svc.newItemId(), _week, commune: '30281'),
      );
      expect(v.items.single.communeCode, '30281');
      v = await svc.save(_week, submit: true);
      expect(srv.saves.single['items'], [
        isA<Map<String, Object?>>().having(
          (m) => m['commune_code'],
          'commune_code',
          '30281',
        ),
      ]);

      // Ngoại tuyến, mở tuần khác chưa từng tải: dùng phạm vi đã đệm.
      srv.offline = true;
      v = await svc.load(_week.add(const Duration(days: 7)));
      expect(v.offline, isTrue);
      expect(v.communeCodes, ['30280', '30281']);
      expect(v.requireCommune, isTrue);
    });

    test('R-B1: cán bộ tỉnh (owner_level province) — Xã/phường không bắt buộc (giả định chờ PO)', () async {
      srv.scope = {'owner_level': 'province', 'commune_codes': <Object?>[]};
      var v = await svc.load(_week);
      expect(v.communeCodes, isEmpty);
      expect(v.requireCommune, isFalse);
      v = await svc.upsertItem(
        _week,
        _item(svc.newItemId(), _week, commune: null),
      );
      expect(v.items, hasLength(1));
    });

    test(
      '409 version conflict: giữ bản nháp, tải lại hoặc giữ bản của tôi',
      () async {
        const id = '01920000-0000-7000-8000-00000000aaaa';
        srv.put(
          '2026-10-05',
          id: id,
          version: 1,
          items: [
            _item('01920000-0000-7000-8000-000000000001', _week).toInput(),
          ],
        );
        var v = await svc.load(_week);
        expect(v.version, 1);
        expect(v.communeCodes, ['30280']);
        // Thiết bị khác sửa lên phiên bản 2.
        srv.put('2026-10-05', id: id, version: 2);
        srv.offline = true;
        v = await svc.upsertItem(
          _week,
          _item(svc.newItemId(), _week.add(const Duration(days: 2))),
        );
        expect(v.items, hasLength(2));
        v = await svc.save(_week, submit: true);
        expect(v.pendingSync, isTrue);
        srv.offline = false;
        await h.sync.syncOnce(force: true);
        v = await svc.load(_week);
        expect(v.versionConflict, isTrue);
        expect(v.items, hasLength(2), reason: 'bản trên máy được giữ');
        expect(v.canSubmit, isFalse);
        await expectLater(
          svc.save(_week, submit: true),
          throwsA(isA<WorkScheduleError>()),
        );

        v = await svc.keepMine(_week);
        expect(v.versionConflict, isFalse);
        expect(v.version, 2);
        v = await svc.save(_week, submit: true);
        expect(srv.saves.last['base_version'], 2);
        expect(srv.saves.last['id'], id);
        expect(v.version, 3);
        expect(v.status, WsStatus.pending);
        expect(v.hasLocalDraft, isFalse);
      },
    );

    test('409 version conflict → "Tải lại" bỏ bản trên máy', () async {
      const id = '01920000-0000-7000-8000-00000000bbbb';
      srv.put('2026-10-05', id: id, version: 1);
      await svc.load(_week);
      srv.put(
        '2026-10-05',
        id: id,
        version: 2,
        items: [_item('01920000-0000-7000-8000-000000000002', _week).toInput()],
      );
      await svc.upsertItem(
        _week,
        _item(svc.newItemId(), _week.add(const Duration(days: 3))),
      );
      var v = await svc.save(_week, submit: false);
      expect(v.versionConflict, isTrue);
      v = await svc.discardLocal(_week);
      expect(v.hasLocalDraft, isFalse);
      expect(v.versionConflict, isFalse);
      expect(v.version, 2);
      expect(v.items.single.day, _week);
    });

    test(
      'sao chép tuần trước: máy chủ khi có mạng, trên máy khi ngoại tuyến',
      () async {
        srv.put(
          '2026-09-28',
          id: '01920000-0000-7000-8000-00000000cccc',
          version: 2,
          status: 'approved',
          items: [
            _item('01920000-0000-7000-8000-000000000003', _prev).toInput(),
            _item(
              '01920000-0000-7000-8000-000000000004',
              _prev.add(const Duration(days: 4)),
              session: WsSession.afternoon,
              start: '14:00',
              end: '16:00',
            ).toInput(),
          ],
        );
        // Có mạng, tuần chưa có lịch → POST copy-previous.
        var v = await svc.load(_week);
        expect(v.previousAvailable, isTrue);
        v = await svc.copyPrevious(_week);
        expect(
          h.fake.actionCalls.map((c) => c.$1),
          contains('/v1/extension/work-schedules/copy-previous'),
        );
        expect(v.status, WsStatus.draft);
        expect(v.items.map((i) => i.day), [
          _week,
          _week.add(const Duration(days: 4)),
        ]);
        // Đã có mục → phải xác nhận thay.
        await expectLater(
          svc.copyPrevious(_week),
          throwsA(isA<WorkScheduleError>()),
        );

        // Ngoại tuyến, tuần sau nữa: sao chép trên máy từ bản đệm.
        final w2 = _week.add(const Duration(days: 7));
        await svc.load(w2);
        srv.offline = true;
        v = await svc.copyPrevious(w2);
        expect(v.items, hasLength(2));
        expect(v.items.first.day, w2);
        expect(v.items.first.content, 'Kiểm tra rầy nâu HTX Mỹ Hòa');
        expect(
          v.items.map((i) => i.id).toSet().intersection({
            '01920000-0000-7000-8000-000000000003',
            '01920000-0000-7000-8000-000000000004',
          }),
          isEmpty,
          reason: 'mục sao chép mang id mới',
        );
        expect(v.dirty, isTrue);
        expect(srv.saves, isEmpty);

        // Tuần trước không có trên máy → báo cần mạng.
        final w4 = _week.add(const Duration(days: 21));
        await expectLater(
          svc.copyPrevious(w4),
          throwsA(
            isA<WorkScheduleError>().having(
              (e) => e.message,
              'message',
              contains('Cần có mạng'),
            ),
          ),
        );
      },
    );

    test('tuần đã kết thúc bị khóa; 409 week_locked → chỉ xem', () async {
      var v = await svc.load(DateTime.utc(2026, 9, 21));
      expect(v.locked, isTrue);
      expect(v.editable, isFalse);
      await expectLater(
        svc.upsertItem(
          DateTime.utc(2026, 9, 21),
          _item('x', DateTime.utc(2026, 9, 21)),
        ),
        throwsA(isA<WorkScheduleError>()),
      );

      await svc.upsertItem(_week, _item(svc.newItemId(), _week));
      srv.lockedWeeks.add('2026-10-05');
      v = await svc.save(_week, submit: true);
      expect(v.syncError, WorkScheduleService.weekLockedCode);
      expect(v.locked, isTrue);
      expect(v.items, hasLength(1));
    });

    test('Log khi có mạng; ngoại tuyến báo cần mạng', () async {
      final log = await svc.log('01920000-0000-7000-8000-00000000aaaa');
      expect(log.map((e) => e.label), ['Gửi duyệt', 'Lưu nháp']);
      expect(log.first.actor, 'Nguyễn Văn An');
      srv.offline = true;
      await expectLater(
        svc.log('01920000-0000-7000-8000-00000000aaaa'),
        throwsA(isA<WorkScheduleError>()),
      );
    });

    test('đánh dấu trùng giờ trên máy, không chặn lưu', () async {
      await svc.upsertItem(
        _week,
        _item(svc.newItemId(), _week, start: '07:30', end: '09:30'),
      );
      var v = await svc.upsertItem(
        _week,
        _item(svc.newItemId(), _week, start: '09:00', end: '10:00'),
      );
      expect(v.overlapIds, hasLength(2));
      v = await svc.save(_week, submit: true);
      expect(v.status, WsStatus.pending);
      expect(v.overlapIds, hasLength(2));
    });
  });

  group('đường dẫn thông báo', () {
    test('nhận diện đường dẫn và kiểu thông báo lịch công tác', () {
      expect(workScheduleLinkOf('/extension/work-schedules').$1, isTrue);
      expect(workScheduleLinkOf('/extension/work-schedules/mine').$1, isTrue);
      final (ok, week) = workScheduleLinkOf(
        '/extension/work-schedules/01920000-0000-7000-8000-00000000aaaa?week_start=2026-10-07',
      );
      expect(ok, isTrue);
      expect(week, _week);
      expect(workScheduleLinkOf('/extension/tasks/abc').$1, isFalse);
      expect(
        workScheduleLinkOf('/notifications', kind: 'work_schedule_due').$1,
        isTrue,
      );
      expect(
        workScheduleLinkOf(null, kind: 'work_schedule_reviewed').$1,
        isTrue,
      );
      expect(workScheduleLinkOf(null, kind: 'directive').$1, isFalse);
    });
  });
}

import 'package:agrigreen_field/core/ids.dart';
import 'package:agrigreen_field/data/outbox/outbox_repository.dart';
import 'package:agrigreen_field/data/secure/secure_store.dart';
import 'package:agrigreen_field/domain/outbox_record.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/harness.dart';

void main() {
  late Harness h;
  setUp(() async => h = await Harness.create());
  tearDown(() => h.dispose());

  Future<OutboxRecord> add([String note = 'x']) => h.outbox.enqueue(
    entity: PushEntity.fieldReport,
    payload: {'note': note, 'plot_id': 'p1'},
  );

  group('định danh bản ghi', () {
    test(
      'id là UUIDv7, idem_key = dev:<device_id>:<seq>, seq tăng dần',
      () async {
        final a = await add();
        final b = await add();
        expect(isUuidV7(a.id), isTrue, reason: a.id);
        expect(isUuidV7(b.id), isTrue);
        expect(a.seq, 1);
        expect(b.seq, 2);
        expect(a.idemKey, 'dev:$testDeviceId:1');
        expect(b.idemKey, 'dev:$testDeviceId:2');
        expect(a.op, RecordOp.create);
        expect(
          a.eventTime,
          matches(RegExp(r'^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d[+-]\d\d:\d\d$')),
        );
      },
    );

    test('khóa CSDL lấy từ kho bí mật (256 bit), không ở nơi khác', () async {
      final key = await h.store.read(SecureKeys.dbKey);
      expect(key, isNotNull);
      expect(key, hasLength(64));
      expect(h.opener.keysSeen.last, "x'$key'");
    });
  });

  group('crash / khởi động lại', () {
    test('bản ghi đã lưu còn nguyên sau khi app chết; seq không lặp', () async {
      for (var i = 0; i < 5; i++) {
        await add('a$i');
      }
      await h.reopen();
      for (var i = 0; i < 5; i++) {
        await add('b$i');
      }
      final all = await h.outbox.list();
      expect(all, hasLength(10));
      expect(all.map((r) => r.seq).toSet(), {for (var i = 1; i <= 10; i++) i});
      expect(all.map((r) => r.idemKey).toSet(), hasLength(10));
      expect(all.map((r) => r.id).toSet(), hasLength(10));
      expect(all.every((r) => r.status == OutboxStatus.pending), isTrue);
    });

    test('giao dịch hỏng giữa chừng không để lại nửa bản ghi', () async {
      await add('ok');
      await expectLater(
        h.db.db.transaction((tx) async {
          await h.outbox.enqueue(
            entity: PushEntity.fieldReport,
            payload: {'n': 1},
            tx: tx,
          );
          throw StateError('mất điện');
        }),
        throwsStateError,
      );
      final all = await h.outbox.list();
      expect(all, hasLength(1));
      final next = await add('sau');
      expect(next.seq, 2); // bộ đếm cũng được rollback, không có lỗ hổng
    });

    test('seq không tái dùng sau khi purge bản ghi đã gửi', () async {
      final a = await add();
      final b = await add();
      final batch = await h.outbox.claimBatch(10);
      await h.outbox.applyResults(batch!.batchKey, [
        PushResult(id: a.id, status: 'accepted'),
        PushResult(id: b.id, status: 'accepted'),
      ]);
      h.clock.advance(const Duration(hours: 49));
      expect(await h.outbox.purgeAcked(const Duration(hours: 48)), 2);
      await h.reopen();
      final c = await add();
      expect(c.seq, 3);
      expect(c.idemKey, 'dev:$testDeviceId:3');
    });
  });

  group('chỉ ghi thêm — sửa sai bằng bản ghi bù', () {
    test('không sửa được nội dung bản ghi đã lưu (trigger CSDL)', () async {
      final a = await add('gốc');
      await expectLater(
        h.db.db.rawUpdate("UPDATE outbox SET payload = '{}' WHERE id = ?", [
          a.id,
        ]),
        throwsA(anything),
      );
      await expectLater(
        h.db.db.rawUpdate(
          "UPDATE outbox SET event_time = '2000-01-01T00:00:00+07:00' WHERE id = ?",
          [a.id],
        ),
        throwsA(anything),
      );
      expect((await h.outbox.byId(a.id))!.payload['note'], 'gốc');
    });

    test('correct tạo bản ghi mới op=correct, supersedes_id, lý do; bản gốc giữ nguyên', () async {
      final a = await add('sai');
      final c = await h.outbox.correct(
        originalId: a.id,
        reason: 'Ghi nhầm ghi chú',
        payload: {...a.payload, 'note': 'đúng'},
      );
      expect(c.id, isNot(a.id));
      expect(c.op, RecordOp.correct);
      expect(c.supersedesId, a.id);
      expect(c.correctionReason, 'Ghi nhầm ghi chú');
      expect(c.eventTime, a.eventTime);
      expect(c.entity, a.entity);
      expect(c.seq, a.seq + 1);
      expect(c.toWire()['op'], 'correct');
      final orig = await h.outbox.byId(a.id);
      expect(orig!.payload['note'], 'sai');
      expect(await h.outbox.list(), hasLength(2));
    });

    test('bản ghi bù bắt buộc có lý do và bản gốc phải tồn tại', () async {
      final a = await add();
      expect(
        () => h.outbox.correct(originalId: a.id, reason: '  ', payload: {}),
        throwsA(isA<OutboxException>()),
      );
      expect(
        () => h.outbox.correct(
          originalId: '0192ffff-0000-7000-8000-000000000000',
          reason: 'x',
          payload: {},
        ),
        throwsA(isA<OutboxException>()),
      );
    });
  });
}

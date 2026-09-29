import 'dart:math';

import 'package:agrigreen_mobile/data/sync/sync_service.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/harness.dart';

void main() {
  late Harness h;
  setUp(() async => h = await Harness.create());
  tearDown(() => h.dispose());

  Future<List<OutboxRecord>> addN(int n, {String prefix = 'r'}) async => [
    for (var i = 0; i < n; i++)
      await h.outbox.enqueue(
        entity: PushEntity.fieldReport,
        payload: {'note': '$prefix$i', 'plot_id': 'p1'},
      ),
  ];

  /// Máy chủ đã ghi đúng một lần cho mỗi bản ghi cục bộ.
  void expectExactlyOnce(Iterable<OutboxRecord> local) {
    final keys = local.map((r) => r.idemKey).toSet();
    expect(h.fake.ledger.keys.toSet(), keys);
    expect(h.fake.ledger.length, local.length);
  }

  test('đẩy theo lô 100, mọi bản ghi được gửi đúng một lần', () async {
    final recs = await addN(250);
    final rep = await h.sync.syncOnce();
    expect(rep, isNotNull);
    expect(h.fake.pushCalls, 3);
    expect(rep!.pushed, 250);
    expectExactlyOnce(recs);
    final c = await h.outbox.counts();
    expect(c.pending, 0);
    expect(c.sent, 250);
    // idempotency-key của lô là UUID do máy sinh, khác nhau giữa các lô
    expect(h.fake.pushBatchKeys.toSet(), hasLength(3));
  });

  group('xử lý kết quả từng bản ghi', () {
    test('accepted/duplicate → đã gửi; conflict → đã gửi + hiển thị; rejected → giữ + lỗi', () async {
      final recs = await addN(4);
      // Bản ghi thứ hai đã có trên máy chủ (ví dụ gửi từ lượt trước).
      h.fake.ledger[recs[1].idemKey] = recs[1].toWire();
      h.fake.conflictIf = (w) => (w['payload'] as Map)['note'] == 'r2';
      h.fake.rejectIf = (w) => (w['payload'] as Map)['note'] == 'r3';
      await h.sync.syncOnce();

      final by = {for (final r in await h.outbox.list()) r.id: r};
      expect(by[recs[0].id]!.status, OutboxStatus.sent);
      expect(by[recs[1].id]!.status, OutboxStatus.sent); // duplicate
      expect(by[recs[2].id]!.status, OutboxStatus.conflict);
      expect(by[recs[2].id]!.conflictId, 'c-${recs[2].id}');
      expect(by[recs[3].id]!.status, OutboxStatus.rejected);
      expect(by[recs[3].id]!.errorCode, 'validation_failed');
      expect(by[recs[3].id]!.errors, isNotEmpty);
      expect(by[recs[0].id]!.ackedAt!.isAtSameMomentAs(h.clock.now()), isTrue);

      // Lượt sau không gửi lại bản ghi bị từ chối.
      final calls = h.fake.pushCalls;
      await h.sync.syncOnce(force: true);
      expect(h.fake.pushCalls, calls);
    });

    test('giữ bản ghi đã xác nhận 48 giờ rồi mới xóa; không bao giờ xóa bản bị từ chối', () async {
      final recs = await addN(3);
      h.fake.conflictIf = (w) => (w['payload'] as Map)['note'] == 'r1';
      h.fake.rejectIf = (w) => (w['payload'] as Map)['note'] == 'r2';
      await h.sync.syncOnce();
      h.clock.advance(const Duration(hours: 47, minutes: 59));
      await h.sync.syncOnce(force: true);
      expect(await h.outbox.list(), hasLength(3));
      h.clock.advance(const Duration(minutes: 2));
      final rep = await h.sync.syncOnce(force: true);
      expect(rep!.purged, 2);
      final left = await h.outbox.list();
      expect(left.single.id, recs[2].id);
      expect(left.single.status, OutboxStatus.rejected);
    });

    test('bản ghi bù cho bản bị từ chối: khi được nhận, bản gốc thành superseded rồi được purge', () async {
      final recs = await addN(1);
      h.fake.rejectIf = (w) => w['op'] == 'create';
      await h.sync.syncOnce();
      expect((await h.outbox.byId(recs[0].id))!.status, OutboxStatus.rejected);
      final c = await h.outbox.correct(
        originalId: recs[0].id,
        reason: 'Bổ sung trường bắt buộc',
        payload: {...recs[0].payload, 'note': 'đã sửa'},
      );
      await h.sync.syncOnce(force: true);
      expect((await h.outbox.byId(c.id))!.status, OutboxStatus.sent);
      expect(h.fake.ledger[c.idemKey]!['supersedes_id'], recs[0].id);
      expect(
        h.fake.ledger[c.idemKey]!['correction_reason'],
        'Bổ sung trường bắt buộc',
      );
      expect(
        (await h.outbox.byId(recs[0].id))!.status,
        OutboxStatus.superseded,
      );
      h.clock.advance(const Duration(hours: 49));
      await h.sync.syncOnce(force: true);
      expect(await h.outbox.list(), isEmpty);
    });

    test(
      'máy chủ bỏ sót kết quả → bản ghi vẫn chờ, gửi lại ở lô sau, không trùng',
      () async {
        final recs = await addN(3);
        h.fake.omitResultsFor = {recs[1].id};
        await h.sync.syncOnce();
        expect((await h.outbox.byId(recs[1].id))!.status, OutboxStatus.pending);
        h.fake.omitResultsFor = {};
        await h.sync.syncOnce(force: true);
        expect((await h.outbox.byId(recs[1].id))!.status, OutboxStatus.sent);
        expectExactlyOnce(recs);
      },
    );
  });

  group('không mất, không trùng qua crash/khởi động lại', () {
    test('mất phản hồi sau khi máy chủ đã ghi + app chết → gửi lại y nguyên lô, máy chủ phát lại', () async {
      await addN(30);
      h.fake.dropNextResponses = 1;
      await h.sync.syncOnce();
      expect((await h.outbox.counts()).pending, 30);
      expect(h.fake.ledger, hasLength(30)); // máy chủ đã ghi

      await h.reopen(); // app bị tắt
      await addN(5, prefix: 'new'); // ghi thêm sau khi mở lại

      await h.sync.syncOnce(force: true);
      // Lần 2 gửi lại đúng lô cũ (cùng key, cùng thân), rồi lô mới.
      expect(h.fake.pushBatchKeys[0], h.fake.pushBatchKeys[1]);
      expect(h.fake.pushBodies[0], h.fake.pushBodies[1]);
      expect(h.fake.pushCalls, 3);
      final all = await h.outbox.list();
      expect(all, hasLength(35));
      expect(all.every((r) => r.status == OutboxStatus.sent), isTrue);
      expectExactlyOnce(all);
    });

    test(
      'lỗi mạng trước khi tới máy chủ → giữ nguyên, lùi thử lại lũy thừa',
      () async {
        await addN(2);
        h.fake.failNextPushes = 3;
        final t0 = h.clock.now();
        await h.sync.syncOnce();
        var st = await h.sync.status();
        expect(st.state, SyncState.offline);
        expect(st.failures, 1);
        expect(st.nextAttemptAt!.difference(t0), const Duration(seconds: 5));

        // Chưa tới giờ → không gọi máy chủ.
        final calls = h.fake.pushCalls;
        expect(await h.sync.syncOnce(), isNull);
        expect(h.fake.pushCalls, calls);

        h.clock.advance(const Duration(seconds: 5));
        await h.sync.syncOnce();
        st = await h.sync.status();
        expect(st.failures, 2);
        expect(
          st.nextAttemptAt!.difference(h.clock.now()),
          const Duration(seconds: 10),
        );

        // Trạng thái lùi sống qua khởi động lại.
        await h.reopen();
        expect((await h.sync.status()).failures, 2);
        h.clock.advance(const Duration(seconds: 10));
        await h.sync.syncOnce();
        expect((await h.sync.status()).failures, 3);
        expect(
          (await h.sync.status()).nextAttemptAt!.difference(h.clock.now()),
          const Duration(seconds: 20),
        );

        h.clock.advance(const Duration(seconds: 20));
        await h.sync.syncOnce();
        st = await h.sync.status();
        expect(st.state, SyncState.idle);
        expect(st.failures, 0);
        expect((await h.outbox.counts()).sent, 2);
        expect(h.fake.ledger, hasLength(2));
      },
    );

    test(
      '422 idempotency_key_reused → tách lô, gửi lô mới, idem_key giữ nguyên',
      () async {
        final recs = await addN(3);
        h.fake.dropNextResponses = 1;
        await h.sync.syncOnce(); // máy chủ ghi, mất phản hồi
        // Giả lập thân lô đổi (ví dụ nâng cấp app đổi cách mã hóa JSON):
        // chèn thẳng một bản ghi mới vào cùng lô.
        final extra = await h.outbox.enqueue(
          entity: PushEntity.fieldReport,
          payload: {'note': 'x'},
        );
        final key = h.fake.pushBatchKeys.first;
        await h.db.db.rawUpdate(
          'UPDATE outbox SET batch_key = ? WHERE id = ?',
          [key, extra.id],
        );
        await h.sync.syncOnce(force: true);
        final all = await h.outbox.list();
        expect(all.every((r) => r.status == OutboxStatus.sent), isTrue);
        expectExactlyOnce([...recs, extra]);
        expect(h.fake.pushBatchKeys.toSet().length, greaterThanOrEqualTo(2));
      },
    );

    test(
      'ngẫu nhiên hóa: 25 vòng lỗi mạng/mất phản hồi/crash vẫn đúng một lần',
      () async {
        final rnd = Random(20261202);
        final local = <OutboxRecord>[];
        for (var round = 0; round < 25; round++) {
          local.addAll(await addN(rnd.nextInt(40), prefix: 'k$round-'));
          switch (rnd.nextInt(4)) {
            case 0:
              h.fake.failNextPushes = 1;
            case 1:
              h.fake.dropNextResponses = 1;
            default:
              break;
          }
          await h.sync.syncOnce(force: true);
          if (rnd.nextBool()) await h.reopen();
        }
        h.fake.failNextPushes = 0;
        h.fake.dropNextResponses = 0;
        await h.sync.syncOnce(force: true);
        final all = await h.outbox.list();
        expect(all, hasLength(local.length));
        expect(all.every((r) => r.status == OutboxStatus.sent), isTrue);
        expectExactlyOnce(local);
      },
    );

    test('gọi đồng bộ chồng nhau dùng chung một lượt', () async {
      final recs = await addN(10);
      await Future.wait([
        h.sync.syncOnce(force: true),
        h.sync.syncOnce(force: true),
        h.sync.syncOnce(force: true),
      ]);
      expectExactlyOnce(recs);
      expect(h.fake.pushCalls, 1);
    });
  });
}

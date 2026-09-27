import 'dart:convert';

import 'package:agrigreen_field/data/reference/reference_repository.dart';
import 'package:agrigreen_field/data/sync/sync_api.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/harness.dart';

PullChange up(String entity, String id, int v, Map<String, Object?> data) =>
    PullChange(entity: entity, id: id, version: v, op: 'upsert', data: data);
PullChange tomb(String entity, String id, int v) =>
    PullChange(entity: entity, id: id, version: v, op: 'tombstone');

void main() {
  late Harness h;
  setUp(() async => h = await Harness.create());
  tearDown(() => h.dispose());

  test('phân trang theo cursor mờ: đi hết has_more, lưu next_cursor, lần sau bắt đầu từ đó', () async {
    h.fake.pages[null] = PullPage(
      changes: [
        up('plot', 'p1', 1, {'label': 'Thửa 1'}),
      ],
      nextCursor: 'c1',
      hasMore: true,
    );
    h.fake.pages['c1'] = PullPage(
      changes: [
        up('plot', 'p2', 1, {'label': 'Thửa 2'}),
      ],
      nextCursor: 'c2',
      hasMore: false,
    );
    final rep = await h.sync.syncOnce();
    expect(rep!.pulled, 2);
    expect(h.fake.pullCursors, [null, 'c1']);
    expect(await h.refs.cursor(), 'c2');
    expect((await h.refs.list('plot')).map((r) => r.id), ['p1', 'p2']);

    await h.reopen();
    h.fake.pages['c2'] = PullPage(
      changes: [
        up('task', 't1', 1, {'title': 'Kiểm tra rầy'}),
      ],
      nextCursor: 'c3',
      hasMore: false,
    );
    await h.sync.syncOnce(force: true);
    expect(h.fake.pullCursors.last, 'c2');
    expect(await h.refs.cursor(), 'c3');
    expect(await h.refs.count('task'), 1);
  });

  test(
    'upsert theo version; bản cũ hơn bị bỏ qua; tombstone ẩn và bỏ dữ liệu',
    () async {
      await h.refs.applyPage(
        PullPage(
          changes: [
            up('plot', 'p1', 3, {'label': 'v3'}),
          ],
          nextCursor: 'a',
          hasMore: false,
        ),
      );
      // Phát lại bản cũ (ví dụ cursor bị lùi) — không đè bản mới.
      await h.refs.applyPage(
        PullPage(
          changes: [
            up('plot', 'p1', 2, {'label': 'v2'}),
          ],
          nextCursor: 'b',
          hasMore: false,
        ),
      );
      expect((await h.refs.get('plot', 'p1'))!.data['label'], 'v3');

      await h.refs.applyPage(
        PullPage(
          changes: [tomb('plot', 'p1', 4)],
          nextCursor: 'c',
          hasMore: false,
        ),
      );
      expect(await h.refs.get('plot', 'p1'), isNull);
      final raw = await h.db.db.query('ref_entity');
      expect(raw.single['deleted'], 1);
      expect(raw.single['data'], isNull);

      // Upsert cũ hơn tombstone không làm sống lại.
      await h.refs.applyPage(
        PullPage(
          changes: [
            up('plot', 'p1', 3, {'label': 'v3'}),
          ],
          nextCursor: 'd',
          hasMore: false,
        ),
      );
      expect(await h.refs.get('plot', 'p1'), isNull);
      // Quay lại phạm vi với version mới hơn → hiện lại.
      await h.refs.applyPage(
        PullPage(
          changes: [
            up('plot', 'p1', 5, {'label': 'v5'}),
          ],
          nextCursor: 'e',
          hasMore: false,
        ),
      );
      expect((await h.refs.get('plot', 'p1'))!.data['label'], 'v5');
    },
  );

  test(
    'nông hộ: chỉ lưu nhãn hiển thị tối thiểu, bỏ họ tên, SĐT, CCCD (mục 10.4)',
    () async {
      await h.refs.applyPage(
        PullPage(
          changes: [
            up('household', 'h1', 1, {
              'label': 'Hộ HTX-A-017',
              'cooperative_id': 'coop1',
              'full_name': 'Nguyễn Văn A',
              'phone': '0901234567',
              'national_id': '079000000001',
              'address': 'Ấp 3',
            }),
          ],
          nextCursor: 'x',
          hasMore: false,
        ),
      );
      final r = await h.refs.get('household', 'h1');
      expect(r!.data, {'label': 'Hộ HTX-A-017', 'cooperative_id': 'coop1'});
      final raw = await h.db.db.query('ref_entity');
      final text = jsonEncode(raw);
      expect(text, isNot(contains('Nguyễn')));
      expect(text, isNot(contains('0901234567')));
      expect(text, isNot(contains('079000000001')));
    },
  );

  test(
    'khóa công khai của hộ: nhận hex hoặc base64, tombstone gỡ khóa',
    () async {
      final hex = List.filled(32, 'ab').join();
      final b64 = base64.encode(List<int>.filled(32, 7));
      await h.refs.applyPage(
        PullPage(
          changes: [
            up('pubkey', 'k1', 1, {'subject_ref': 'h1', 'public_key': hex}),
            up('pubkey', 'k2', 1, {'subject_ref': 'h2', 'public_key': b64}),
            up('pubkey', 'k3', 1, {'subject_ref': 'h3', 'public_key': 'hỏng'}),
          ],
          nextCursor: 'x',
          hasMore: false,
        ),
      );
      expect(await h.refs.publicKeyFor('h1'), List<int>.filled(32, 0xab));
      expect(await h.refs.publicKeyFor('h2'), List<int>.filled(32, 7));
      expect(await h.refs.publicKeyFor('h3'), isNull);
      await h.refs.applyPage(
        PullPage(
          changes: [
            PullChange(
              entity: 'pubkey',
              id: 'k1',
              version: 2,
              op: 'tombstone',
              data: const {'subject_ref': 'h1'},
            ),
          ],
          nextCursor: 'y',
          hasMore: false,
        ),
      );
      expect(await h.refs.publicKeyFor('h1'), isNull);
    },
  );

  test('trang lỗi giữa chừng → cả trang và cursor cùng rollback', () async {
    await h.refs.applyPage(
      PullPage(
        changes: [
          up('plot', 'p0', 1, {'label': 'trước'}),
        ],
        nextCursor: 'c0',
        hasMore: false,
      ),
    );
    final bad = PullPage(
      changes: [
        up('plot', 'p1', 1, {'label': 'ok'}),
        up('plot', 'p2', 1, {'label': Object()}), // không mã hóa JSON được
      ],
      nextCursor: 'c1',
      hasMore: false,
    );
    await expectLater(h.refs.applyPage(bad), throwsA(anything));
    expect(await h.refs.cursor(), 'c0');
    expect(await h.refs.get('plot', 'p1'), isNull);
    expect(await h.refs.count(RefEntity.plot), 1);
  });
}

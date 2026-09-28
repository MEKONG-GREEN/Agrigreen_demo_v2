import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:agrigreen_field/application/task_service.dart';
import 'package:agrigreen_field/data/crypto/envelope.dart';
import 'package:agrigreen_field/data/photo/location_source.dart';
import 'package:agrigreen_field/data/photo/photo_pipeline.dart';
import 'package:agrigreen_field/data/sync/sync_api.dart';
import 'package:agrigreen_field/domain/outbox_record.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:pinenacl/x25519.dart' as nacl;

import 'support/harness.dart';

class FakeCamera implements CameraSource {
  FakeCamera(this.bytes);
  final Uint8List bytes;
  @override
  Future<CapturedImage?> capture() async =>
      CapturedImage(Uint8List.fromList(bytes), 'image/jpeg');
}

bool _contains(Uint8List hay, List<int> needle) {
  outer:
  for (var i = 0; i + needle.length <= hay.length; i++) {
    for (var j = 0; j < needle.length; j++) {
      if (hay[i + j] != needle[j]) continue outer;
    }
    return true;
  }
  return false;
}

void main() {
  late Harness h;
  final sk = nacl.PrivateKey.generate();
  // "JPEG" giả có chuỗi nhận dạng để dò bản rõ trên đĩa.
  final marker = utf8.encode('PLAINTEXT-MARKER-AgriGreen-ho-017');
  final jpeg = Uint8List.fromList([
    0xFF,
    0xD8,
    ...marker,
    ...List.filled(4000, 7),
    0xFF,
    0xD9,
  ]);

  setUp(() async {
    h = await Harness.create();
    await h.refs.applyPage(
      PullPage(
        changes: [
          PullChange(
            entity: 'pubkey',
            id: 'k1',
            version: 1,
            op: 'upsert',
            data: {
              'subject_ref': 'hh1',
              'public_key': base64.encode(sk.publicKey.asTypedList),
            },
          ),
        ],
        nextCursor: 'c',
        hasMore: false,
      ),
    );
  });
  tearDown(() => h.dispose());

  PhotoPipeline pipeline(LocationResult loc) => PhotoPipeline(
    camera: FakeCamera(jpeg),
    location: FixedLocationSource(loc),
    clock: h.clock,
    ids: h.ids,
    cipher: EnvelopeCipher(),
    refs: h.refs,
    evidence: h.evidence,
  );

  test('chụp → gắn GPS/thời gian → mã hóa ngay → chỉ lưu ciphertext', () async {
    final out = await pipeline(
      const LocationResult(
        GpsStatus.ok,
        GpsFix(lat: 10.03, lon: 105.78, accuracyM: 4),
      ),
    ).capture(subjectRef: 'hh1', parentEntity: 'field_report', parentId: 'r1');
    final f = out!.file;
    expect(out.gpsStatus, GpsStatus.ok);
    expect(f.meta['gps'], {'lat': 10.03, 'lon': 105.78, 'accuracy_m': 4.0});
    expect(f.meta['captured_at'], isNotNull);
    expect(f.meta['subject_ref'], 'hh1');
    expect(f.status, 'pending');

    // Không có bản rõ ở bất kỳ tệp nào trong thư mục dữ liệu (kể cả CSDL).
    for (final e in h.dir.listSync(recursive: true).whereType<File>()) {
      expect(
        _contains(e.readAsBytesSync(), marker),
        isFalse,
        reason: 'bản rõ lọt vào ${e.path}',
      );
    }
    // Vault mở được.
    final ct = await h.blobs.read(f.blobPath);
    final plain = await EnvelopeCipher.openForTest(
      SealedEnvelope(meta: f.meta, ciphertext: ct),
      Uint8List.fromList(sk.asTypedList),
    );
    expect(plain, jpeg);
  });

  test(
    'chưa có khóa công khai của hộ → không chụp (không lưu bản rõ tạm)',
    () async {
      await expectLater(
        pipeline(const LocationResult(GpsStatus.denied))
            .capture(subjectRef: 'hh-unknown'),
        throwsA(isA<PhotoPipelineException>()),
      );
      expect(await h.evidence.pending(), isEmpty);
    },
  );

  test('đồng bộ: tải tệp (multipart, Idempotency-Key file:<id>, file_ref = id) idempotent qua crash; ảnh lẻ không tạo evidence (chỉ biểu mẫu MRV tạo)', () async {
    final out = await pipeline(
      const LocationResult(GpsStatus.denied),
    ).capture(subjectRef: 'hh1', parentEntity: 'field_report', parentId: 'r1');
    final f = out!.file;
    expect(f.meta['gps'], isNull);

    // Lần 1: máy chủ lưu tệp nhưng mất phản hồi; app chết.
    h.fake.dropNextUploadResponses = 1;
    await h.sync.syncOnce();
    expect(await h.outbox.list(), isEmpty);
    await h.reopen();

    // Lần 2: tải lại cùng khóa → cùng file_ref (= id máy sinh).
    await h.sync.syncOnce(force: true);
    expect(h.fake.uploadKeys, ['file:${f.id}', 'file:${f.id}']);
    expect(h.fake.uploadFileRefs, [f.id, f.id]);
    expect(h.fake.files['file:${f.id}']!['ref'], f.id);
    // Ciphertext cục bộ đã được bỏ sau khi tải lên.
    expect(File(f.blobPath).existsSync(), isFalse);
    final up = (await h.evidence.byId(f.id))!;
    expect(up.status, 'uploaded');
    expect(up.fileRef, f.id);
    // Bản ghi evidence chỉ do biểu mẫu MRV tạo (payload_contract_test).
    expect(await h.outbox.list(entity: PushEntity.evidence), isEmpty);
  });

  group('nhiệm vụ hỗ trợ (US-TASK-02)', () {
    test('chỉ đóng khi có kết quả; chuyển tiếp cần ghi chú; trạng thái cục bộ phủ lên', () async {
      await h.refs.applyPage(
        PullPage(
          changes: [
            PullChange(
              entity: 'task',
              id: 't1',
              version: 1,
              op: 'upsert',
              data: {'title': 'Hỗ trợ phòng rầy nâu', 'status': 'assigned'},
            ),
          ],
          nextCursor: 'c2',
          hasMore: false,
        ),
      );
      final svc = TaskService(outbox: h.outbox, refs: h.refs);
      expect(await svc.openCount(), 1);
      expect(
        () => svc.update(taskId: 't1', status: TaskStatus.done),
        throwsA(isA<TaskException>()),
      );
      expect(
        () => svc.update(taskId: 't1', status: TaskStatus.escalated),
        throwsA(isA<TaskException>()),
      );
      await svc.update(taskId: 't1', status: TaskStatus.accepted);
      await svc.update(
        taskId: 't1',
        status: TaskStatus.done,
        result: 'Đã hướng dẫn phun thuốc',
      );
      final t = (await svc.list()).single;
      expect(t.status, TaskStatus.done);
      expect(t.pendingLocal, isTrue);
      expect(await svc.openCount(), 0);
    });
  });
}

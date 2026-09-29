import 'package:agrigreen_htx/application/crop_service.dart';
import 'package:agrigreen_htx/domain/cropcycle.dart';
import 'package:agrigreen_mobile/data/photo/location_source.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/test_env.dart';

const logId = '01920000-0000-7000-8000-00000000f001';
const seasonRef = '01920000-0000-7000-8000-00000000e001';
const logTime = '2026-09-28T08:00:00+07:00';

Map<String, Object?> _needsSupplementLog() => {
  'entity': 'farm_log',
  'id': logId,
  'op': 'upsert',
  'data': {
    'id': logId,
    'log_ref': logId,
    'version': 1,
    'season_ref': seasonRef,
    'plot_ref': plotRef,
    'activity_code': 'phun_thuoc',
    'event_time': logTime,
    'gps_source': 'device',
    'location_status': 'mismatch',
    'machine_codes': ['drone'],
    'inputs': [],
    'note': 'Phun rầy',
    'file_refs': [],
    'on_behalf': false,
    'status': 'needs_supplement',
    'review_note': 'Bổ sung ảnh bình thuốc',
  },
};

void main() {
  testWidgets(
    'HTX US-LOG-01/04 + US-GPS-01 AC-4: ghi bằng icon khi bị từ chối quyền GPS '
    '(không chặn, source = denied), máy móc, ảnh mã hóa + tải trước push',
    (tester) async {
      final env = TestEnv()..seed(season: true);
      env.location.result = const LocationResult(GpsStatus.denied);
      final s = await loginToHome(tester, env);

      await openTile(tester, 'Ghi nhật ký');
      await tapText(tester, 'Lô T1');
      await tapOn(tester, find.byKey(const ValueKey('act-bon_phan')));
      await tapText(tester, 'Máy cày');
      await tester.enterText(
        find.byKey(const ValueKey('log-note')),
        'Bón thúc đợt 1',
      );
      await tapOn(tester, find.byKey(const ValueKey('photo-capture')));
      expect(find.text('Chụp ảnh (1/20)'), findsOneWidget);
      await tapText(tester, 'Lưu nhật ký');

      final rec = (await s.outbox.list(entity: PushEntity.farmLog)).single;
      final photo = (await s.evidence.pending()).single;
      expect(rec.status, OutboxStatus.pending);
      expect(rec.payload['season_ref'], seasonRef);
      expect(rec.payload['activity_code'], 'bon_phan');
      expect(rec.payload['machine_codes'], ['may_cay']);
      expect(rec.payload['note'], 'Bón thúc đợt 1');
      expect(rec.payload['gps'], {'source': 'denied'});
      expect(rec.payload['file_refs'], [photo.id]);
      // Chỉ ciphertext trên máy; mở bằng khóa bí mật của hộ ra đúng ảnh.
      final blob = env.blobs.blobs[photo.blobPath]!;
      expect(photo.subjectRef, householdRef);
      final plain = await openEnvelope(env, photo.meta, blob);
      expect(plain, List.filled(64, 1));

      await s.sync.syncOnce(force: true);
      expect(env.server.uploadKeys, ['file:${photo.id}']);
      expect(env.server.uploadFileRefs, [photo.id]);
      expect((await s.outbox.byId(rec.id))!.status, OutboxStatus.sent);
      await finish(tester);
    },
  );

  testWidgets(
    'HTX US-LOG-05 AC-3 + US-LOG-01 AC-3: bổ sung khi bị yêu cầu — bản ghi bù, '
    'GPS/thời gian khóa; hai máy cùng sửa → conflict giữ cả hai',
    (tester) async {
      final env = TestEnv()..seed(season: true, extra: [_needsSupplementLog()]);
      final s = await loginToHome(tester, env);
      expect(find.text('1 nhật ký cần bổ sung'), findsOneWidget);
      await tapText(tester, '1 nhật ký cần bổ sung');
      await tapText(tester, 'Bổ sung');
      expect(find.text('Bổ sung ảnh bình thuốc'), findsOneWidget);
      await tapOn(tester, find.byKey(const ValueKey('locked-gps')));
      expect(find.text(msgLockedGpsTime), findsOneWidget);

      await tester.enterText(
        find.byKey(const ValueKey('supplement-note')),
        'Phun rầy, đã chụp bình thuốc',
      );
      await tapOn(tester, find.byKey(const ValueKey('photo-capture')));
      await tester.enterText(
        find.byKey(const ValueKey('supplement-reason')),
        'ngắn',
      );
      await tapText(tester, 'Lưu bản bổ sung');
      expect(find.text('Lý do sửa cần ít nhất 10 ký tự.'), findsOneWidget);
      await tester.enterText(
        find.byKey(const ValueKey('supplement-reason')),
        'Bổ sung ảnh theo yêu cầu HTX',
      );
      await tapText(tester, 'Lưu bản bổ sung');

      final rec = (await s.outbox.list(entity: PushEntity.farmLog)).single;
      expect(rec.op, RecordOp.correct);
      expect(rec.supersedesId, logId);
      expect(rec.correctionReason, 'Bổ sung ảnh theo yêu cầu HTX');
      expect(DateTime.parse(rec.eventTime), DateTime.parse(logTime));
      expect(rec.payload.containsKey('gps'), isFalse);
      expect(rec.payload['machine_codes'], ['drone']);
      expect(rec.payload['note'], 'Phun rầy, đã chụp bình thuốc');
      expect(rec.payload['file_refs'], hasLength(1));

      env.server.conflictIf = (w) => w['op'] == 'correct';
      await s.sync.syncOnce(force: true);
      expect((await s.outbox.byId(rec.id))!.status, OutboxStatus.conflict);
      await tester.pageBack();
      await settle(tester);
      await openTile(tester, 'Hàng đợi đồng bộ');
      expect(find.textContaining('Cả hai được giữ lại'), findsOneWidget);
      await finish(tester);
    },
  );

  testWidgets(
    'HTX US-LOG-01 AC-3 / US-LOG-02 AC-2: đổi GPS → chặn; chưa mở vụ → chặn',
    (tester) async {
      final env = TestEnv()..seed(season: true);
      final s = await loginToHome(tester, env);
      await expectLater(
        s.crop.recordLog(
          season: await s.crop.openSeasonFor(plot2Ref),
          activity: FarmActivity.lamDat,
        ),
        throwsA(
          isA<CropError>().having(
            (e) => e.message,
            'message',
            msgSeasonNotOpen,
          ),
        ),
      );
      final season = (await s.crop.openSeasonFor(plotRef))!;
      final rec = await s.crop.recordLog(
        season: season,
        activity: FarmActivity.kiemTra,
      );
      final view = (await s.crop.logs(seasonRef: season.seasonRef)).single.view;
      expect(view.id, rec.id);
      await expectLater(
        s.crop.supplementLog(
          log: view,
          reason: 'Sửa vị trí cho đúng',
          changedGpsOrTime: true,
        ),
        throwsA(
          isA<CropError>().having(
            (e) => e.message,
            'message',
            msgLockedGpsTime,
          ),
        ),
      );
      final fixed = await s.crop.supplementLog(
        log: view,
        reason: 'Thêm ghi chú kiểm tra',
        note: 'Có rầy',
      );
      expect(fixed.op, RecordOp.correct);
      expect(fixed.payload['gps'], rec.payload['gps']);
      await finish(tester);
    },
  );
}

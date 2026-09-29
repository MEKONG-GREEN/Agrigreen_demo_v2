import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:agrigreen_field/application/consent_service.dart';
import 'package:agrigreen_field/application/evidence_service.dart';
import 'package:agrigreen_field/application/field_report_service.dart';
import 'package:agrigreen_mobile/data/catalog/catalog_cache.dart';
import 'package:agrigreen_mobile/data/crypto/envelope.dart';
import 'package:agrigreen_mobile/data/outbox/outbox_repository.dart';
import 'package:agrigreen_mobile/data/photo/location_source.dart';
import 'package:agrigreen_mobile/data/photo/photo_pipeline.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:agrigreen_mobile/domain/mrv.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';
import 'package:agrigreen_mobile/testing.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:pinenacl/x25519.dart' as nacl;

import 'support/harness.dart';

/// Payload `field_report` và `evidence` so với fixture JSON rút từ quy tắc
/// kiểm của máy chủ Go (máy chủ là hợp đồng).
Map<String, Object?> _fixture(String name) =>
    (jsonDecode(File('test/fixtures/contract/$name').readAsStringSync()) as Map)
        .cast<String, Object?>();

/// So sánh sau khi mã hóa JSON (phân biệt "5" với 5, null với thiếu khóa).
void expectSameJson(Object? actual, Object? expected) =>
    expect(jsonDecode(jsonEncode(actual)), jsonDecode(jsonEncode(expected)));

MrvCatalog _catalog(String type) => MrvCatalog.fromJson(
  ((_fixture('mrv_catalogs_g1.json')['catalogs']! as List)
          .cast<Map<String, Object?>>())
      .firstWhere((c) => c['evidence_type'] == type),
);

const _plot = '01920000-0000-7000-8000-0000000000b1';
const _hh = '01920000-0000-7000-8000-0000000000a1';
const _f1 = '01920000-0000-7000-8000-0000000000f1';
const _f2 = '01920000-0000-7000-8000-0000000000f2';
const _sha1 =
    '3f0a9a1c5a4b8e2d7c6f1e0d9b8a7c6e5f4d3c2b1a0918273645546372819a0b';
const _sha2 =
    '0000000000000000000000000000000000000000000000000000000000000abc';

MrvEvidenceDraft _awd({
  Map<String, Object>? values,
  List<MrvFileRef>? files,
  MrvGps? gps = const MrvGps('10.0452', '105.7701', '4.5'),
  String? season = 'DX',
  String? plot = _plot,
}) => MrvEvidenceDraft(
  evidenceType: 'awd_log',
  catalogVersion: 1,
  adminUnitCode: '31117',
  plotRef: plot,
  householdRef: _hh,
  seasonCode: season,
  values:
      values ??
      {
        'stage': 'de_nhanh',
        'awd_water_level_cm': '-12.5',
        'awd_dry_days': 5,
        'field_surface': 'am',
        'note': 'Ống quan trắc số 2',
      },
  fileRefs: files ?? const [MrvFileRef(_f1, _sha1)],
  gps: gps,
);

void main() {
  final now = DateTime.utc(2026, 12, 2, 1); // 08:00 giờ Việt Nam

  group('field_report = ReportPayload (pf/extension)', () {
    test('sowing_progress khớp fixture và qua kiểm như Report.Validate', () {
      final d = FieldReportDraft(
        kind: FieldReportKind.sowing,
        communeCode: '31117',
        cooperativeRef: '01920000-0000-7000-8000-0000000000c1',
        period: periodOf(now),
        seasonCode: 'DX',
        varietyCode: 'OM5451',
        sownAreaHa: normalizeDecimal('12,50'),
        sownOn: '2026-11-20',
        note: '  Sạ lan, đủ nước ',
      );
      d.validate(now);
      expectSameJson(
        d.toPayload(),
        _fixture('field_report_sowing_progress.json')['payload'],
      );
    });

    test('growth_pest khớp fixture; trường của loại kia gửi rỗng/null', () {
      final d = FieldReportDraft(
        kind: FieldReportKind.growthPest,
        communeCode: '31117',
        period: '2026-12',
        seasonCode: 'BO_QUA',
        growthStage: 'de_nhanh',
        pestCode: 'RAY_NAU',
        severity: PestSeverity.trungBinh,
        affectedAreaHa: '3.25',
        urgent: true,
      );
      d.validate(now);
      expectSameJson(
        d.toPayload(),
        _fixture('field_report_growth_pest.json')['payload'],
      );
    });

    test('lỗi theo đúng trường và lý do của máy chủ', () {
      FieldReportDraft sow({
        String commune = '31117',
        String period = '2026-12',
        String? coop = '01920000-0000-7000-8000-0000000000c1',
        String season = 'DX',
        String variety = 'OM5451',
        String? area = '1',
        String? sownOn,
        String note = '',
      }) => FieldReportDraft(
        kind: FieldReportKind.sowing,
        communeCode: commune,
        period: period,
        cooperativeRef: coop,
        seasonCode: season,
        varietyCode: variety,
        sownAreaHa: area,
        sownOn: sownOn,
        note: note,
      );
      String? field(FieldReportDraft d) {
        try {
          d.validate(now);
          return null;
        } on FieldReportError catch (e) {
          return e.field;
        }
      }

      expect(field(sow()), isNull);
      expect(field(sow(commune: ' ')), 'commune_code');
      expect(field(sow(period: '2026-13')), 'period');
      expect(field(sow(period: '2027-01')), 'period'); // kỳ tương lai
      expect(field(sow(coop: null)), 'cooperative_ref');
      expect(field(sow(coop: 'abc')), 'cooperative_ref');
      expect(field(sow(season: 'Đông Xuân')), 'season_code');
      expect(field(sow(variety: '')), 'variety_code');
      expect(field(sow(area: null)), 'sown_area_ha');
      expect(field(sow(area: '0')), 'sown_area_ha');
      expect(field(sow(area: '12.12345')), 'sown_area_ha');
      expect(field(sow(area: '100001')), 'sown_area_ha');
      expect(field(sow(sownOn: '2026-12-03')), 'sown_on');
      expect(field(sow(sownOn: '20/11/2026')), 'sown_on');
      expect(field(sow(note: 'Gọi 0912 345 678')), 'note');
      expect(field(sow(note: 'CCCD 079123456789')), 'note');
      final pest = FieldReportDraft(
        kind: FieldReportKind.growthPest,
        communeCode: '31117',
        period: '2026-12',
        growthStage: 'đẻ nhánh',
        pestCode: 'RAY_NAU',
        severity: PestSeverity.nhe,
      );
      expect(field(pest), 'growth_stage');
    });

    test('kỳ báo cáo theo giờ Việt Nam (R-12)', () {
      expect(periodOf(DateTime.utc(2026, 11, 30, 17, 30)), '2026-12');
      expect(periodOf(DateTime.utc(2026, 11, 30, 16, 59)), '2026-11');
    });

    test('bản ghi bù giữ loại/xã/HTX/kỳ (+ vụ/giống khi gieo sạ); lý do ≥ 10 ký tự', () async {
      final h = await Harness.create();
      addTearDown(h.dispose);
      final svc = FieldReportService(outbox: h.outbox, clock: h.clock);
      final r = await svc.create(
        FieldReportDraft(
          kind: FieldReportKind.sowing,
          communeCode: '31117',
          cooperativeRef: '01920000-0000-7000-8000-0000000000c1',
          period: periodOf(h.clock.now()),
          seasonCode: 'DX',
          varietyCode: 'OM5451',
          sownAreaHa: '12.5',
        ),
      );
      expect(r.entity, PushEntity.fieldReport);
      expect(r.payload.containsKey('gps'), isFalse);
      await expectLater(
        svc.correct(original: r, reason: 'ngắn', note: 'x'),
        throwsA(isA<FieldReportError>()),
      );
      final c = await svc.correct(
        original: r,
        reason: 'Đo lại diện tích sạ thực tế',
        changes: FieldReportDraft(
          kind: FieldReportKind.growthPest, // bị bỏ qua: khóa
          communeCode: '99999',
          period: '2020-01',
          cooperativeRef: '01920000-0000-7000-8000-0000000000c9',
          seasonCode: 'HT',
          varietyCode: 'ST25',
          sownAreaHa: '13',
        ),
      );
      expect(c.op, RecordOp.correct);
      expect(c.supersedesId, r.id);
      expect(c.payload['kind'], 'sowing_progress');
      expect(c.payload['commune_code'], '31117');
      expect(c.payload['period'], r.payload['period']);
      expect(c.payload['cooperative_ref'], r.payload['cooperative_ref']);
      expect(c.payload['season_code'], 'DX');
      expect(c.payload['variety_code'], 'OM5451');
      // Đổi thẳng khóa bị chặn ở tầng hàng đợi.
      expect(
        () => h.outbox.correct(
          originalId: r.id,
          reason: 'Sửa sai xã đã chọn nhầm',
          payload: {...r.payload, 'commune_code': '99999'},
          lockedPayloadKeys: fieldReportLockedKeys,
        ),
        throwsA(isA<LockedFieldException>()),
      );
    });
  });

  group('evidence = CapturePayload (pf/mrv)', () {
    final fx = _fixture('evidence_payloads.json');

    test('danh mục API → mô hình; khóa dành riêng', () {
      final c = _catalog('awd_log');
      expect(c.version, 1);
      expect(c.provisional, isTrue);
      expect(c.fields.first.key, 'plot_ref');
      expect(c.field('photo')!.reserved, isTrue);
      expect(c.field('awd_water_level_cm')!.maxScale, 1);
      expect(c.field('awd_water_level_cm')!.min, '-30');
    });

    test('awd_log khớp fixture và qua ValidateRecord + ValidateEnvelope', () {
      final d = _awd();
      expect(d.validateEnvelope(), isEmpty);
      expect(_catalog('awd_log').validate(d), isEmpty);
      expectSameJson(d.toPayload(), fx['awd_log']);
      // Đọc lại (lưu biểu mẫu trên máy) không đổi payload.
      expectSameJson(
        MrvEvidenceDraft.fromPayload(d.toPayload()).toPayload(),
        fx['awd_log'],
      );
    });

    test('field_checklist khớp fixture (boolean, date, decimal 2 chữ số lẻ, GPS không sai số)', () {
      final d = MrvEvidenceDraft(
        evidenceType: 'field_checklist',
        catalogVersion: 1,
        adminUnitCode: '31117',
        plotRef: _plot,
        householdRef: _hh,
        seasonCode: 'DX',
        values: const {
          'check_boundary_ok': true,
          'check_interview_done': false,
          'check_water_regime': 'awd',
          'check_estimated_yield_t_ha': '6.75',
          'check_visit_date': '2026-12-01',
        },
        fileRefs: const [MrvFileRef(_f1, _sha1), MrvFileRef(_f2, _sha2)],
        gps: MrvGps.fromDegrees(10.0452, 105.7701),
      );
      expect(_catalog('field_checklist').validate(d), isEmpty);
      expectSameJson(d.toPayload(), fx['field_checklist']);
    });

    test('lỗi như checkValue/checkReserved của máy chủ', () {
      final c = _catalog('awd_log');
      Set<String> errs(MrvEvidenceDraft d) => {
        for (final e in [...d.validateEnvelope(), ...c.validate(d)]) e.field,
      };
      Map<String, Object> vals(Map<String, Object> over) => {
        'stage': 'ma',
        'awd_water_level_cm': '1',
        'awd_dry_days': 1,
        ...over,
      };
      expect(errs(_awd(values: vals({}))), isEmpty);
      // decimal gửi số JSON → cấm (R-10); quá số lẻ; ngoài khoảng.
      expect(errs(_awd(values: vals({'awd_water_level_cm': 12}))), {
        'values.awd_water_level_cm',
      });
      expect(errs(_awd(values: vals({'awd_water_level_cm': '1.25'}))), {
        'values.awd_water_level_cm',
      });
      expect(errs(_awd(values: vals({'awd_water_level_cm': '-30.1'}))), {
        'values.awd_water_level_cm',
      });
      expect(errs(_awd(values: vals({'awd_water_level_cm': '-30'}))), isEmpty);
      expect(errs(_awd(values: vals({'awd_dry_days': '61'}))), {
        'values.awd_dry_days',
      });
      expect(errs(_awd(values: vals({'awd_dry_days': '7'}))), isEmpty);
      expect(errs(_awd(values: vals({'stage': 'lua'}))), {'values.stage'});
      // Thiếu bắt buộc; khóa lạ; khóa dành riêng trong values.
      expect(errs(_awd(values: {'stage': 'ma'})), {
        'values.awd_water_level_cm',
        'values.awd_dry_days',
      });
      expect(errs(_awd(values: vals({'foo': 'x'}))), {'values.foo'});
      expect(errs(_awd(values: vals({'gps': 'x'}))), {'values.gps'});
      // Cột dành riêng bắt buộc.
      expect(errs(_awd(values: vals({}), gps: null)), {'gps'});
      expect(errs(_awd(values: vals({}), season: null)), {'season_code'});
      expect(errs(_awd(values: vals({}), plot: null)), {'plot_ref'});
      expect(errs(_awd(values: vals({}), files: const [])), {'file_refs'});
      // Phong bì: sha256 phải hex thường, không trùng file_ref, GPS trong phạm vi.
      expect(
        errs(_awd(values: vals({}), files: const [MrvFileRef(_f1, 'ABC')])),
        {'file_refs'},
      );
      expect(
        errs(
          _awd(
            values: vals({}),
            files: const [MrvFileRef(_f1, _sha1), MrvFileRef(_f1, _sha1)],
          ),
        ),
        {'file_refs'},
      );
      expect(errs(_awd(values: vals({}), gps: const MrvGps('91', '105'))), {
        'gps',
      });
      // text max 1000 ký tự.
      expect(errs(_awd(values: vals({'note': 'x' * 1001}))), {'values.note'});
    });

    test('so sánh decimal chính xác', () {
      expect(compareDecimal('-30', '-30.0'), 0);
      expect(compareDecimal('-30.1', '-30'), -1);
      expect(compareDecimal('10', '9.99'), 1);
      expect(compareDecimal('0.5', '-0.5'), 1);
      expect(normalizeFixed('10.0452000'), '10.0452');
      expect(MrvGps.fromDegrees(10.0452, 105.7701, 4.5).toJson(), {
        'lat': '10.0452',
        'lon': '105.7701',
        'accuracy_m': '4.5',
      });
    });
  });

  group('biểu mẫu MRV đầu–cuối: danh mục đệm → đồng ý → ảnh → evidence', () {
    late Harness h;
    final sk = nacl.PrivateKey.generate();
    setUp(() async {
      h = await Harness.create();
      await h.refs.applyPage(
        PullPage(
          changes: [
            PullChange(
              entity: 'pubkey',
              id: _hh,
              version: 1,
              op: 'upsert',
              data: {
                'ref': _hh,
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

    EvidenceService svc(CatalogCache cache, ConsentService consents) =>
        EvidenceService(
          catalogs: cache,
          evidence: h.evidence,
          photos: PhotoPipeline(
            camera: _Cam(),
            location: const FixedLocationSource(
              LocationResult(
                GpsStatus.ok,
                GpsFix(lat: 10.0452, lon: 105.7701, accuracyM: 4.5),
              ),
            ),
            clock: h.clock,
            ids: h.ids,
            cipher: EnvelopeCipher(),
            refs: h.refs,
            evidence: h.evidence,
            consent: consents,
          ),
          location: const FixedLocationSource(
            LocationResult(
              GpsStatus.ok,
              GpsFix(lat: 10.0452, lon: 105.7701, accuracyM: 4.5),
            ),
          ),
          consent: consents,
          clock: h.clock,
          ids: h.ids,
        );

    test('tải danh mục qua API; bản ghi evidence chỉ tạo khi mọi ảnh đã tải, file_refs = file_ref + sha256', () async {
      final cache = CatalogCache(h.db, h.clock);
      for (final c
          in (_fixture('mrv_catalogs_g1.json')['catalogs']! as List)
              .cast<Map<String, Object?>>()) {
        h.fake.getResponses['/v1/mrv/catalog?evidence_type=${c['evidence_type']}'] =
            c;
      }
      h.fake.getResponses['/v1/masterdata/catalogs/season'] = {
        'items': [
          {'catalog': 'season', 'code': 'DX', 'name': 'Đông Xuân'},
        ],
        'next_cursor': null,
      };
      await cache.refresh(h.fake); // rice_variety/pest → 404, bỏ qua
      expect((await cache.mrvCatalogs()).map((c) => c.evidenceType), [
        'awd_log',
        'field_checklist',
      ]);
      expect((await cache.entries('season')).single.name, 'Đông Xuân');

      final consents = ConsentService(
        db: h.db,
        outbox: h.outbox,
        clock: h.clock,
      );
      final s = svc(cache, consents);
      final form = s.newFormId();
      expect(await s.blockReason(_hh), contains('mrv.evidence'));
      await consents.record(subjectRef: _hh, status: ConsentStatus.granted);
      await s.capturePhoto(formId: form, householdRef: _hh);
      await s.capturePhoto(formId: form, householdRef: _hh);
      final draft = MrvEvidenceDraft(
        evidenceType: 'awd_log',
        catalogVersion: 1,
        adminUnitCode: '31117',
        plotRef: _plot,
        householdRef: _hh,
        seasonCode: 'DX',
        values: const {
          'stage': 'ma',
          'awd_water_level_cm': '-5',
          'awd_dry_days': 3,
        },
        gps: await s.currentGps(),
      );
      // Sai phiên bản danh mục → chặn.
      await expectLater(
        s.submit(
          formId: form,
          draft: MrvEvidenceDraft.fromPayload({
            ...draft.toPayload(),
            'catalog_version': 2,
          }),
        ),
        throwsA(isA<EvidenceFormException>()),
      );
      await s.submit(formId: form, draft: draft);

      // Lần 1: tải ảnh thứ hai lỗi mạng → chưa có bản ghi evidence.
      final photos = await s.photosOf(form);
      expect(photos, hasLength(2));
      final failing = _FailSecondUpload();
      h.server = failing;
      await h.reopen();
      await h.sync.syncOnce();
      expect(await h.outbox.list(entity: PushEntity.evidence), isEmpty);
      expect((await h.evidence.forms()).single.status, 'waiting');

      // Lần 2: tải xong → bản ghi evidence đúng CapturePayload, gửi đi.
      await h.sync.syncOnce(force: true);
      final ev = (await h.outbox.list(entity: PushEntity.evidence)).single;
      expect(ev.status, OutboxStatus.sent);
      expect((await h.evidence.forms()).single.status, 'queued');
      final p = ev.payload;
      expect(p['evidence_type'], 'awd_log');
      expect(p['catalog_version'], 1);
      expect(p['admin_unit_code'], '31117');
      expect(p['plot_ref'], _plot);
      expect(p['household_ref'], _hh);
      expect(p['season_code'], 'DX');
      expectSameJson(p['values'], {
        'awd_dry_days': 3,
        'awd_water_level_cm': '-5',
        'stage': 'ma',
      });
      expectSameJson(p['gps'], {
        'lat': '10.0452',
        'lon': '105.7701',
        'accuracy_m': '4.5',
      });
      final refs = (p['file_refs']! as List).cast<Map<String, Object?>>();
      expect(refs, hasLength(2));
      for (final r in refs) {
        final f = photos.firstWhere((x) => x.id == r['file_ref']);
        expect(r['sha256'], f.meta['sha256_ciphertext']);
        expect(failing.uploadFileRefs, contains(r['file_ref']));
      }
      expect(ev.eventTime, isNotEmpty);
      // Chạy lại không tạo bản thứ hai.
      await h.sync.syncOnce(force: true);
      expect(await h.outbox.list(entity: PushEntity.evidence), hasLength(1));
      // Lưu ý: không có trường cũ (subject_ref, parent_*, gps_status).
      expect(p.containsKey('parent_entity'), isFalse);
    });
  });
}

class _Cam implements CameraSource {
  @override
  Future<CapturedImage?> capture() async =>
      CapturedImage(Uint8List.fromList(List.filled(500, 9)), 'image/jpeg');
}

/// Lần tải thứ hai lỗi mạng một lần (ảnh 1 xong, ảnh 2 chưa).
class _FailSecondUpload extends FakeServer {
  int n = 0;
  @override
  Future<String> uploadFile({
    required String idempotencyKey,
    required Map<String, Object?> meta,
    required Uint8List ciphertext,
    String? fileRef,
  }) {
    n++;
    if (n == 2) throw NetworkException('mất mạng giữa hai ảnh');
    return super.uploadFile(
      idempotencyKey: idempotencyKey,
      meta: meta,
      ciphertext: ciphertext,
      fileRef: fileRef,
    );
  }
}

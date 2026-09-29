import 'package:agrigreen_htx/domain/cropcycle.dart';
import 'package:agrigreen_htx/domain/support.dart';
import 'package:agrigreen_mobile/data/crypto/envelope.dart';
import 'package:agrigreen_mobile/data/photo/location_source.dart';
import 'package:flutter_test/flutter_test.dart';

SopVersion _sop({int? hOff, int? hWin}) => SopVersion.fromJson({
  'id': 's',
  'name': 'SOP',
  'steps': [
    {
      'seq': 2,
      'name': 'Gieo sạ',
      'activity_code': 'gieo_sa',
      'day_offset': 0,
      'window_days': 1,
    },
    {
      'seq': 1,
      'name': 'Làm đất',
      'activity_code': 'lam_dat',
      'day_offset': -7,
      'window_days': 3,
    },
    {
      'seq': 3,
      'name': 'Thu hoạch',
      'activity_code': 'thu_hoach',
      'day_offset': 95,
      'window_days': 7,
    },
  ],
  'harvest_day_offset': hOff,
  'harvest_window_days': hWin,
});

Season _season({String? open, String status = 'open'}) => Season.fromJson({
  'season_ref': 'r',
  'plot_ref': 'p',
  'season_code': 'TD',
  'season_year': 2026,
  'variety_code': 'OM18',
  'sowing_date': '2026-07-01',
  'harvest_open_date': open,
  'area_ha': '2.0000',
  'status': status,
});

void main() {
  group('HTX US-YIELD-01 cổng thu hoạch (SOP đã ghim)', () {
    test('mốc máy chủ ưu tiên; chưa tới → chặn đúng thông báo', () {
      final s = _season(open: '2026-10-05');
      expect(
        harvestBlockReason(s, null, DateTime.utc(2026, 10, 4)),
        msgHarvestTooEarly,
      );
      expect(harvestBlockReason(s, null, DateTime.utc(2026, 10, 5)), isNull);
    });
    test('vụ mở trên máy: tính từ bước thu hoạch (offset − cửa sổ), không cứng 60 ngày', () {
      final s = _season();
      final sop = _sop();
      expect(formatDay(harvestOpenDate(s, sop)!), '2026-09-27'); // 01/07 + 88
      expect(
        harvestBlockReason(s, sop, DateTime.utc(2026, 9, 26)),
        msgHarvestTooEarly,
      );
      expect(harvestBlockReason(s, sop, DateTime.utc(2026, 9, 27)), isNull);
      // harvest_day_offset/window của phiên bản ghi đè bước.
      expect(
        formatDay(harvestOpenDate(s, _sop(hOff: 100, hWin: 5))!),
        '2026-10-04',
      );
    });
    test('vụ đã hoàn thành không khai nữa', () {
      expect(
        harvestBlockReason(
          _season(status: 'completed'),
          null,
          DateTime.utc(2027),
        ),
        isNotNull,
      );
    });
    test('bước hiện tại theo ngày sau sạ; bước sắp theo seq', () {
      final sop = _sop();
      expect(sop.steps.map((e) => e.seq), [1, 2, 3]);
      expect(
        sop
            .currentStep(DateTime.utc(2026, 7, 1), DateTime.utc(2026, 6, 20))!
            .seq,
        1,
      );
      expect(
        sop
            .currentStep(DateTime.utc(2026, 7, 1), DateTime.utc(2026, 7, 1))!
            .seq,
        2,
      );
      expect(
        sop
            .currentStep(DateTime.utc(2026, 7, 1), DateTime.utc(2026, 9, 28))!
            .seq,
        3,
      );
    });
  });

  group('HTX US-YIELD-01/03 sản lượng kg → t/ha, không số thực', () {
    test('kg dạng chuỗi decimal ≤ 3 chữ số lẻ; dấu phẩy Việt Nam', () {
      expect(MassKg.parse('12500').wire, '12500');
      expect(MassKg.parse('12 500,50').wire, '12500.5');
      expect(MassKg.parse('0.125').wire, '0.125');
      expect(() => MassKg.parse('1.2345'), throwsA(isA<YieldError>()));
      expect(() => MassKg.parse('-5'), throwsA(isA<YieldError>()));
      expect(() => MassKg.parse('abc'), throwsA(isA<YieldError>()));
    });
    test('t/ha = kg / 1000 / ha, làm tròn', () {
      expect(formatYield(MassKg.parse('12500'), '2.0000'), '6,25 t/ha');
      expect(formatYield(MassKg.parse('1000'), '3'), '0,33 t/ha');
      expect(formatYield(MassKg.parse('1000'), null), isNull);
    });
    test('bất thường: 0 kg, vượt ngưỡng theo giống (US-YIELD-03 AC-2)', () {
      expect(yieldAnomaly(MassKg.parse('0'), '2', '12'), YieldAnomaly.zero);
      expect(yieldAnomaly(MassKg.parse('18000'), '2', '9'), YieldAnomaly.none);
      expect(yieldAnomaly(MassKg.parse('18001'), '2', '9'), YieldAnomaly.above);
      expect(yieldAnomaly(MassKg.parse('18001'), '2', '12'), YieldAnomaly.none);
    });
  });

  group('HTX US-GPS-01 GPS không bao giờ chặn', () {
    test('có vị trí → decimal dạng chuỗi, source device', () {
      final p = gpsPayload(
        const LocationResult(
          GpsStatus.weak,
          GpsFix(lat: 10.5, lon: 105.25, accuracyM: 35.26),
        ),
      );
      expect(p, {
        'lat': '10.5000000',
        'lon': '105.2500000',
        'accuracy_m': '35.3',
        'source': 'device',
      });
    });
    test('từ chối quyền → denied; không GPS → no_device; không toạ độ', () {
      expect(gpsPayload(const LocationResult(GpsStatus.denied)), {
        'source': 'denied',
      });
      expect(gpsPayload(const LocationResult(GpsStatus.unavailable)), {
        'source': 'no_device',
      });
    });
    test('nhãn người duyệt (HTX US-LOG-05 AC-1)', () {
      expect(locationLabel('mismatch', 'device'), 'Vị trí không khớp');
      expect(locationLabel('missing', 'denied'), 'Thiếu GPS');
      expect(locationLabel('match', 'device'), isNull);
    });
  });

  group('HTX US-SUPPORT-01 kiểm yêu cầu hỗ trợ', () {
    test('thiếu loại / mô tả → thông báo AC-3', () {
      expect(
        validateSupport(
          category: null,
          description: 'Lúa bị rầy nâu',
          photos: 0,
        ),
        msgSupportRequired,
      );
      expect(
        validateSupport(category: 'dich_hai', description: '  ', photos: 0),
        msgSupportRequired,
      );
    });
    test('quá 5 ảnh; không chứa SĐT/CCCD', () {
      expect(
        validateSupport(
          category: 'dich_hai',
          description: 'Lúa bị rầy nâu nặng',
          photos: 6,
        ),
        msgSupportTooManyPhotos,
      );
      expect(
        validateSupport(
          category: 'dich_hai',
          description: 'Gọi tôi 0912 345 678 nhé',
          photos: 0,
        ),
        msgSupportNoPii,
      );
      expect(
        validateSupport(
          category: 'dich_hai',
          description: 'Lúa bị rầy nâu nặng',
          photos: 5,
        ),
        isNull,
      );
    });
  });

  group('HTX US-GPS-02: nguồn xác thực vị trí', () {
    test('nhãn: không có GPS thiết bị; nguồn luôn có nhãn (AC-2)', () {
      expect(locationLabel('missing', 'no_device'), 'Không có GPS thiết bị');
      expect(locationLabel('missing', 'denied'), 'Thiếu GPS');
      for (final src in [
        'device',
        'photo_exif',
        'manual_pick',
        'no_device',
        'denied',
        '',
      ]) {
        expect(gpsSourceLabel(src), startsWith('Nguồn vị trí: '));
      }
      expect(gpsSourceLabel('manual_pick'), contains('chọn lô từ danh sách'));
      expect(gpsSourceLabel('photo_exif'), contains('tọa độ ảnh'));
    });

    test('GPS tốt → device; yếu mà ảnh chính xác hơn → photo_exif; không '
        'ảnh → như cũ', () {
      const good = LocationResult(
        GpsStatus.ok,
        GpsFix(lat: 10, lon: 105, accuracyM: 5),
      );
      final photo = [
        {'lat': 10.1, 'lon': 105.1, 'accuracy_m': 8.0},
      ];
      expect(gpsWithPhotoFallback(good, photo)['source'], 'device');
      const weak = LocationResult(
        GpsStatus.weak,
        GpsFix(lat: 10, lon: 105, accuracyM: 60),
      );
      expect(gpsWithPhotoFallback(weak, photo), {
        'lat': '10.1000000',
        'lon': '105.1000000',
        'accuracy_m': '8.0',
        'source': 'photo_exif',
      });
      expect(gpsWithPhotoFallback(weak, const [])['source'], 'device');
      expect(
        gpsWithPhotoFallback(const LocationResult(GpsStatus.denied), [
          {'lat': null},
        ]),
        {'source': 'denied'},
      );
    });
  });

  test('HTX sửa/hủy vụ: lý do ≥ 10 ký tự; phủ bản sửa trên máy lên vụ', () {
    expect(checkReason('  ngắn  '), msgReasonTooShort);
    expect(checkReason('Ghi nhầm ngày sạ'), isNull);
    final s = Season.fromJson({
      'season_ref': 'r',
      'plot_ref': 'p',
      'season_code': 'TD',
      'season_year': 2026,
      'variety_code': 'OM18',
      'sowing_date': '2026-09-25',
      'harvest_open_date': '2027-01-10',
      'status': 'open',
      'current_event_id': 'e1',
    });
    final fixed = s.applyLocal(
      eventId: 'e2',
      pending: true,
      sowingDate: DateTime.utc(2026, 9, 20),
    );
    expect(fixed.sowingDate, DateTime.utc(2026, 9, 20));
    expect(fixed.harvestOpenDate, isNull, reason: 'tính lại theo SOP');
    expect(fixed.currentEventId, 'e2');
    expect(fixed.localPending, isTrue);
    expect(fixed.varietyCode, 'OM18');
    final cancelled = fixed.applyLocal(
      eventId: 'e3',
      pending: true,
      cancel: true,
    );
    expect(cancelled.isOpen, isFalse);
  });
}

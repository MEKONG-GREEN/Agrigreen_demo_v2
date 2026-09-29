import 'package:agrigreen_mobile/data/photo/photo_pipeline.dart';
import 'package:agrigreen_mobile/data/sync/sync_service.dart';
import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../domain/cropcycle.dart';
import 'providers.dart';

/// Biểu tượng hoạt động (ghi nhật ký bằng icon — HTX US-LOG-01).
IconData activityIcon(FarmActivity a) => switch (a) {
  FarmActivity.lamDat => Icons.agriculture,
  FarmActivity.gieoSa => Icons.grass,
  FarmActivity.tuoiNuoc => Icons.water_drop,
  FarmActivity.bonPhan => Icons.science,
  FarmActivity.phunThuoc => Icons.sanitizer,
  FarmActivity.kiemTra => Icons.search,
  FarmActivity.thuHoach => Icons.inventory_2,
};

/// Lý do từ chối của máy chủ → lời dễ hiểu (docs/errors.md, cropcycle.*).
String rejectMessage(String? code) => switch (code) {
  'cropcycle.sowing_date_out_of_window' => msgSowingWindow,
  'cropcycle.season_already_open' =>
    'Lô ruộng đang canh tác vụ khác, không thể mở vụ mới.',
  'cropcycle.season_not_open' => msgSeasonNotOpen,
  'cropcycle.harvest_too_early' => msgHarvestTooEarly,
  'cropcycle.yield_anomaly' =>
    'Sản lượng bất thường — cần xác nhận lại trước khi lưu.',
  'cropcycle.yield_negative' => 'Sản lượng không được âm.',
  'cropcycle.locked_fields' => msgLockedGpsTime,
  'cropcycle.concurrent_edit' =>
    'Dữ liệu đã được sửa ở nơi khác — đồng bộ lại rồi sửa tiếp.',
  'cropcycle.season_state' =>
    'Vụ không còn mở (đã khai sản lượng hoặc đã hủy) — không sửa/hủy được.',
  'cropcycle.batch_empty' => 'Không còn lô hợp lệ trong lượt ghi hàng loạt (chưa mở vụ hoặc ngoài phạm vi).',
  'cropcycle.previous_season_not_completed' => msgPreviousNotDone,
  'cropcycle.no_previous_season' => msgNoPreviousSeason,
  'cropcycle.out_of_scope' =>
    'Nông hộ hoặc lô ruộng này không thuộc phạm vi quản lý của bạn.',
  'cropcycle.phi_violation' =>
    'Chưa hết thời gian cách ly sau phun thuốc — chưa được thu hoạch.',
  'cropcycle.self_review' => 'Người ghi nhật ký không được tự duyệt.',
  'cropcycle.log_state' => 'Nhật ký không còn ở trạng thái chờ duyệt.',
  'cropcycle.catalog_invalid' => 'Mã vụ/giống/máy không có hoặc đã ngừng dùng.',
  'geo.overlap_needs_confirm' =>
    'Ranh giới bị chồng lấn với lô đã đăng ký, vui lòng kiểm tra lại.',
  'geo.area_deviation_needs_confirm' =>
    'Diện tích vẽ lệch quá ngưỡng so với diện tích khai báo — cần xác nhận.',
  'geo.overlap_cross_coop' =>
    'Ranh giới chồng lấn với lô của HTX khác — không lưu được.',
  'geo.out_of_scope' => 'Bạn không có quyền truy cập lô ruộng này.',
  'geo.outside_commune' => 'Ranh giới nằm ngoài xã của lô ruộng.',
  'geo.area_out_of_range' => 'Diện tích lô ngoài giới hạn cho phép.',
  'geo.version_conflict' =>
    'Ranh giới đã được sửa ở nơi khác — đồng bộ lại rồi sửa tiếp.',
  'validation_failed' => 'Dữ liệu chưa hợp lệ.',
  null => 'Bị từ chối.',
  _ => 'Bị từ chối ($code).',
};

/// Thẻ trạng thái đồng bộ trên trang chủ.
class SyncCard extends ConsumerWidget {
  const SyncCard({super.key, required this.summary});
  final HomeSummary summary;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final s = summary.status;
    final c = summary.counts;
    final color = switch (s.state) {
      SyncState.idle => AgColors.g700,
      SyncState.running => AgColors.blue,
      SyncState.revoked || SyncState.needsLogin => AgColors.red,
      _ => AgColors.amber,
    };
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(Icons.circle, size: 14, color: color),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    s.label,
                    style: const TextStyle(
                      fontSize: 18,
                      fontWeight: FontWeight.w700,
                    ),
                  ),
                ),
              ],
            ),
            const SizedBox(height: 6),
            Text(
              'Lần đồng bộ cuối: ${formatLocal(s.lastSuccessAt)}',
              style: const TextStyle(color: AgColors.mute),
            ),
            const SizedBox(height: 8),
            Wrap(
              spacing: 16,
              runSpacing: 8,
              children: [
                _Count('Chờ đồng bộ', summary.waiting, AgColors.amber),
                _Count('Bị từ chối', c.rejected, AgColors.red),
                _Count('Xung đột', c.conflict, AgColors.amber),
              ],
            ),
            const SizedBox(height: 8),
            SizedBox(
              width: double.infinity,
              child: FilledButton.icon(
                onPressed: s.state == SyncState.running
                    ? null
                    : () async {
                        await ref
                            .read(appControllerProvider)
                            .services
                            ?.sync
                            .syncOnce(force: true);
                        ref.invalidate(homeSummaryProvider);
                      },
                icon: const Icon(Icons.sync),
                label: const Text('Đồng bộ ngay'),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _Count extends StatelessWidget {
  const _Count(this.label, this.n, this.color);
  final String label;
  final int n;
  final Color color;
  @override
  Widget build(BuildContext context) => Column(
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      Text(
        '$n',
        style: TextStyle(
          fontSize: 22,
          fontWeight: FontWeight.w800,
          color: color,
        ),
      ),
      Text(label, style: const TextStyle(fontSize: 14)),
    ],
  );
}

/// Thẻ thời tiết (HTX US-WEATHER-01): nhà cung cấp chưa chốt (RB-7) → nói rõ,
/// không để trống màn hình, không chặn thẻ lời khuyên.
class WeatherCard extends StatelessWidget {
  const WeatherCard({super.key});

  @override
  Widget build(BuildContext context) => const Card(
    child: ListTile(
      leading: Icon(Icons.cloud_off_outlined, size: 36, color: AgColors.mute),
      title: Text('Thời tiết'),
      subtitle: Text(
        'Chưa có nguồn thời tiết — đang chờ chốt nhà cung cấp dữ liệu. '
        'Theo dõi bản tin khuyến nông để biết cảnh báo mưa, triều cường, độ mặn.',
      ),
    ),
  );
}

/// Thẻ lời khuyên nông vụ theo bước SOP hiện tại (HTX US-WEATHER-02).
class AdviceCard extends StatelessWidget {
  const AdviceCard({
    super.key,
    required this.season,
    required this.step,
    required this.today,
  });
  final Season season;
  final SopStep step;
  final DateTime today;

  @override
  Widget build(BuildContext context) {
    final days = season.sowingDate == null
        ? null
        : daysBetween(season.sowingDate!, today);
    return Card(
      key: const ValueKey('advice-card'),
      color: AgColors.g100,
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                const Icon(Icons.tips_and_updates, color: AgColors.g700),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    'Lời khuyên nông vụ · Lô ${season.plotCode}',
                    style: const TextStyle(
                      fontSize: 17,
                      fontWeight: FontWeight.w700,
                    ),
                  ),
                ),
              ],
            ),
            const SizedBox(height: 8),
            Text(
              'Giai đoạn: ${step.name}'
              '${days != null && days >= 0 ? ' · ngày thứ $days sau sạ' : ''}',
              style: const TextStyle(fontWeight: FontWeight.w600),
            ),
            if (step.instruction.isNotEmpty) ...[
              const SizedBox(height: 6),
              Text(step.instruction),
            ],
          ],
        ),
      ),
    );
  }
}

/// Nhãn nhỏ màu.
class Tag extends StatelessWidget {
  const Tag(this.text, {super.key, this.color = AgColors.amber});
  final String text;
  final Color color;
  @override
  Widget build(BuildContext context) => Container(
    padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
    decoration: BoxDecoration(
      color: color.withValues(alpha: 0.12),
      borderRadius: BorderRadius.circular(20),
    ),
    child: Text(
      text,
      style: TextStyle(color: color, fontSize: 14, fontWeight: FontWeight.w600),
    ),
  );
}

/// Nút chụp ảnh minh chứng: mã hóa ngay bằng khóa công khai của hộ. Thiếu
/// khóa (chưa đồng bộ) → nêu lý do, vẫn lưu được bản ghi không ảnh.
class PhotoCapture extends ConsumerStatefulWidget {
  const PhotoCapture({
    super.key,
    required this.householdRef,
    required this.photoIds,
    required this.onChanged,
    this.max = 20,
    this.parentEntity,
  });
  final String? householdRef;
  final List<String> photoIds;
  final ValueChanged<List<String>> onChanged;
  final int max;
  final String? parentEntity;

  @override
  ConsumerState<PhotoCapture> createState() => _PhotoCaptureState();
}

class _PhotoCaptureState extends ConsumerState<PhotoCapture> {
  String? _block;
  bool _busy = false;

  Future<void> _shoot() async {
    final s = ref.services;
    final hh = widget.householdRef;
    if (hh == null) {
      setState(() => _block = 'Chưa xác định nông hộ của lô ruộng.');
      return;
    }
    if (widget.photoIds.length >= widget.max) {
      setState(
        () => _block =
            'Vượt quá số lượng ảnh cho phép (tối đa ${widget.max}) hoặc dung lượng ảnh quá lớn.',
      );
      return;
    }
    final reason = await s.photos.blockReason(hh);
    if (reason != null) {
      setState(() => _block = reason);
      return;
    }
    setState(() => _busy = true);
    try {
      final out = await s.photos.capture(
        subjectRef: hh,
        parentEntity: widget.parentEntity,
      );
      if (out != null) {
        widget.onChanged([...widget.photoIds, out.file.id]);
        setState(() => _block = null);
      }
    } on PhotoPipelineException catch (e) {
      setState(() => _block = e.message);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) => Column(
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      OutlinedButton.icon(
        key: const ValueKey('photo-capture'),
        onPressed: _busy ? null : _shoot,
        icon: const Icon(Icons.photo_camera),
        label: Text('Chụp ảnh (${widget.photoIds.length}/${widget.max})'),
      ),
      if (_block != null)
        Padding(
          padding: const EdgeInsets.only(top: 6),
          child: Text(
            _block!,
            style: const TextStyle(color: AgColors.amber, fontSize: 15),
          ),
        ),
    ],
  );
}

Future<bool> confirmDialog(
  BuildContext context, {
  required String title,
  required String message,
  String ok = 'Xác nhận',
  String cancel = 'Hủy',
}) async =>
    await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(title),
        content: Text(message),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: Text(cancel),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(ctx, true),
            child: Text(ok),
          ),
        ],
      ),
    ) ??
    false;

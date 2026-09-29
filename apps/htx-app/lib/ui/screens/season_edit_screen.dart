import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/crop_service.dart';
import '../../domain/cropcycle.dart';
import '../providers.dart';
import 'open_season_screen.dart' show parseDisplayDay;

class _EditData {
  const _EditData(this.season, this.varieties);
  final Season? season;
  final List<Choice> varieties;
}

final _editProvider = FutureProvider.autoDispose.family<_EditData, String>((
  ref,
  seasonRef,
) async {
  final s = servicesOf(ref);
  return _EditData(
    await s.crop.seasonByRef(seasonRef),
    await s.crop.varieties(),
  );
});

/// Sửa vụ đang mở bằng bản ghi bù (`season_open` op correct, `supersedes_id`
/// = sự kiện hiện hành): đổi giống (SOP gán lại theo giống) và/hoặc ngày sạ
/// trong [−15, +30]; lý do ≥ 10 ký tự. Nông dân và quản lý HTX; làm được khi
/// ngoại tuyến. Máy chủ từ chối nếu vụ đã khai sản lượng / đã sửa ở nơi khác.
class SeasonEditScreen extends ConsumerStatefulWidget {
  const SeasonEditScreen({super.key, required this.seasonRef});
  final String seasonRef;

  @override
  ConsumerState<SeasonEditScreen> createState() => _SeasonEditScreenState();
}

class _SeasonEditScreenState extends ConsumerState<SeasonEditScreen> {
  String? _variety;
  final _date = TextEditingController();
  final _reason = TextEditingController();
  bool _init = false;
  String? _error;
  bool _busy = false;

  @override
  void dispose() {
    _date.dispose();
    _reason.dispose();
    super.dispose();
  }

  Future<void> _save(Season season) async {
    final d = parseDisplayDay(_date.text);
    if (d == null) {
      setState(
        () => _error = 'Nhập ngày theo dạng ngày/tháng/năm, ví dụ 05/10/2026.',
      );
      return;
    }
    setState(() => _busy = true);
    try {
      await ref.services.crop.correctSeason(
        season: season,
        reason: _reason.text,
        varietyCode: _variety,
        sowingDate: d,
      );
      if (!mounted) return;
      showMessage(context, 'Đã lưu sửa vụ — Chờ đồng bộ.');
      Navigator.of(context).pop(true);
    } on CropError catch (e) {
      setState(() => _error = e.message);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final data = ref.watch(_editProvider(widget.seasonRef));
    return Scaffold(
      appBar: AppBar(title: const Text('Sửa vụ')),
      body: data.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (e, _) => Center(child: Text('Lỗi: $e')),
        data: (d) {
          final se = d.season;
          if (se == null) {
            return const Center(child: Text('Không tìm thấy vụ.'));
          }
          if (!_init) {
            _init = true;
            _variety = se.varietyCode;
            _date.text = displayDay(se.sowingDate);
          }
          final today = ref.read(appControllerProvider).services!.crop.today();
          return ListView(
            padding: const EdgeInsets.all(16),
            children: [
              Text(
                'Lô ${se.plotCode} · vụ ${se.seasonCode} ${se.seasonYear}',
                style: Theme.of(context).textTheme.titleMedium,
              ),
              const SizedBox(height: 12),
              Text('Giống lúa', style: Theme.of(context).textTheme.titleMedium),
              Wrap(
                spacing: 8,
                runSpacing: 8,
                children: [
                  for (final v in d.varieties)
                    ChoiceChip(
                      label: Text(v.name, style: const TextStyle(fontSize: 17)),
                      selected: _variety == v.code,
                      onSelected: (_) => setState(() => _variety = v.code),
                    ),
                ],
              ),
              const SizedBox(height: 12),
              TextField(
                key: const ValueKey('edit-sowing-date'),
                controller: _date,
                keyboardType: TextInputType.datetime,
                decoration: InputDecoration(
                  labelText: 'Ngày gieo sạ (ngày/tháng/năm)',
                  helperText:
                      'Từ ${displayDay(addDays(today, -sowingPastDays))} đến ${displayDay(addDays(today, sowingFutureDays))}',
                ),
              ),
              const SizedBox(height: 12),
              TextField(
                key: const ValueKey('edit-season-reason'),
                controller: _reason,
                maxLines: 2,
                decoration: const InputDecoration(
                  labelText: 'Lý do sửa (ít nhất 10 ký tự)',
                ),
              ),
              if (_error != null)
                Padding(
                  padding: const EdgeInsets.only(top: 8),
                  child: Text(
                    _error!,
                    key: const ValueKey('edit-season-error'),
                    style: const TextStyle(color: AgColors.red, fontSize: 16),
                  ),
                ),
              const SizedBox(height: 16),
              FilledButton.icon(
                onPressed: _busy ? null : () => _save(se),
                icon: const Icon(Icons.save),
                label: const Text('Lưu sửa vụ'),
              ),
              const SizedBox(height: 8),
              const Text(
                'Bản sửa là bản ghi bù — bản gốc vẫn được giữ. Nhật ký đã ghi '
                'giữ nguyên; ngày được khai sản lượng tính lại theo SOP.',
                style: TextStyle(color: AgColors.mute),
              ),
            ],
          );
        },
      ),
    );
  }
}

/// Hỏi lý do rồi hủy vụ mở nhầm (bản ghi bù `cancel: true`). true = đã lưu.
Future<bool> confirmCancelSeason(
  BuildContext context,
  WidgetRef ref,
  Season season,
) async {
  final reason = TextEditingController();
  String? error;
  final ok = await showDialog<bool>(
    context: context,
    builder: (ctx) => StatefulBuilder(
      builder: (ctx, setState) => AlertDialog(
        title: Text('Hủy vụ lô ${season.plotCode}?'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text(
              'Chỉ hủy vụ mở nhầm, chưa khai sản lượng. Lô trở lại '
              '"Chưa mở vụ"; dữ liệu cũ vẫn được lưu để đối soát.',
            ),
            TextField(
              key: const ValueKey('cancel-season-reason'),
              controller: reason,
              maxLines: 2,
              decoration: InputDecoration(
                labelText: 'Lý do hủy (ít nhất 10 ký tự)',
                errorText: error,
              ),
            ),
          ],
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: const Text('Không hủy'),
          ),
          FilledButton(
            style: FilledButton.styleFrom(backgroundColor: AgColors.red),
            onPressed: () {
              final e = checkReason(reason.text);
              if (e != null) {
                setState(() => error = e);
                return;
              }
              Navigator.pop(ctx, true);
            },
            child: const Text('Hủy vụ'),
          ),
        ],
      ),
    ),
  );
  if (ok != true || !context.mounted) return false;
  try {
    await ref.services.crop.cancelSeason(season: season, reason: reason.text);
    if (context.mounted) showMessage(context, 'Đã hủy vụ — Chờ đồng bộ.');
    return true;
  } on CropError catch (e) {
    if (context.mounted) showMessage(context, e.message, error: true);
    return false;
  }
}

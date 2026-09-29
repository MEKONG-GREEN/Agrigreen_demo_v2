import 'dart:async';

import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/crop_service.dart';
import '../../application/profile_service.dart';
import '../../application/support_service.dart';
import '../../domain/support.dart';
import '../providers.dart';
import '../widgets.dart';

class _SupportData {
  const _SupportData(this.households, this.plots, this.items);
  final List<FarmerHousehold> households;
  final List<PlotItem> plots;
  final List<SupportItem> items;
}

final _supportProvider = FutureProvider.autoDispose<_SupportData>((ref) async {
  final s = servicesOf(ref);
  return _SupportData(
    await s.profile.households(),
    await s.crop.plots(),
    await s.support.list(),
  );
});

/// Gửi yêu cầu hỗ trợ (HTX US-SUPPORT-01): loại sự cố + mô tả bắt buộc,
/// ≤ 5 ảnh, GPS tùy chọn; mất mạng → "Đang chờ gửi", tự gửi khi có mạng.
class SupportScreen extends ConsumerStatefulWidget {
  const SupportScreen({super.key});

  @override
  ConsumerState<SupportScreen> createState() => _SupportScreenState();
}

class _SupportScreenState extends ConsumerState<SupportScreen> {
  String? _category;
  bool _urgent = false;
  bool _withGps = true;
  String? _plot;
  final _desc = TextEditingController();
  List<String> _photos = const [];
  String? _error;
  bool _sending = false;

  @override
  void dispose() {
    _desc.dispose();
    super.dispose();
  }

  Future<void> _send(_SupportData d) async {
    final s = ref.services;
    setState(() => _sending = true);
    try {
      final plot = d.plots.where((p) => p.ref == _plot).firstOrNull;
      // Một nông hộ → bỏ trống (máy chủ tự lấy); nhiều hộ → theo lô đã chọn.
      final hh = d.households.length > 1 ? plot?.householdRef : null;
      final loc = _withGps ? await s.support.locate() : null;
      await s.support.submit(
        category: _category,
        description: _desc.text,
        urgency: _urgent ? 'khan' : 'binh_thuong',
        householdRef: hh,
        plotRef: _plot,
        photoRefs: _photos,
        location: loc,
      );
      if (!mounted) return;
      showMessage(context, 'Đã lưu yêu cầu — Đang chờ gửi.');
      setState(() {
        _category = null;
        _desc.clear();
        _photos = const [];
        _error = null;
      });
      ref.invalidate(_supportProvider);
      unawaited(s.sync.syncOnce(force: true));
    } on SupportError catch (e) {
      setState(() => _error = e.message);
    } finally {
      if (mounted) setState(() => _sending = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final data = ref.watch(_supportProvider);
    return Scaffold(
      appBar: AppBar(title: const Text('Gửi yêu cầu hỗ trợ')),
      body: data.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (e, _) => Center(child: Text('Lỗi: $e')),
        data: (d) {
          final photoSubject =
              d.plots.where((p) => p.ref == _plot).firstOrNull?.householdRef ??
              d.households.firstOrNull?.ref;
          return ListView(
            padding: const EdgeInsets.all(16),
            children: [
              Text(
                'Loại sự cố',
                style: Theme.of(context).textTheme.titleMedium,
              ),
              Wrap(
                spacing: 8,
                runSpacing: 8,
                children: [
                  for (final e in supportCategories.entries)
                    ChoiceChip(
                      label: Text(
                        e.value,
                        style: const TextStyle(fontSize: 16),
                      ),
                      selected: _category == e.key,
                      onSelected: (_) => setState(() => _category = e.key),
                    ),
                ],
              ),
              SwitchListTile(
                contentPadding: EdgeInsets.zero,
                title: const Text('Khẩn cấp'),
                value: _urgent,
                onChanged: (v) => setState(() => _urgent = v),
              ),
              if (d.plots.isNotEmpty)
                DropdownButtonFormField<String?>(
                  initialValue: _plot,
                  isExpanded: true,
                  decoration: const InputDecoration(
                    labelText: 'Lô ruộng (không bắt buộc)',
                  ),
                  items: [
                    const DropdownMenuItem<String?>(
                      child: Text('— Không chọn —'),
                    ),
                    for (final p in d.plots)
                      DropdownMenuItem<String?>(
                        value: p.ref,
                        child: Text(p.title),
                      ),
                  ],
                  onChanged: (v) => setState(() => _plot = v),
                ),
              const SizedBox(height: 12),
              TextField(
                key: const ValueKey('support-desc'),
                controller: _desc,
                maxLines: 4,
                maxLength: 2000,
                decoration: const InputDecoration(
                  labelText: 'Mô tả sự cố',
                  helperText: 'Không ghi số điện thoại, số CCCD.',
                ),
              ),
              PhotoCapture(
                householdRef: photoSubject,
                photoIds: _photos,
                max: supportMaxPhotos,
                onChanged: (p) => setState(() => _photos = p),
                parentEntity: 'support_request',
              ),
              SwitchListTile(
                contentPadding: EdgeInsets.zero,
                title: const Text('Gắn vị trí hiện tại'),
                subtitle: const Text(
                  'Không lấy được vị trí vẫn gửi được (nhãn "Thiếu vị trí GPS").',
                ),
                value: _withGps,
                onChanged: (v) => setState(() => _withGps = v),
              ),
              if (_error != null)
                Padding(
                  padding: const EdgeInsets.only(bottom: 8),
                  child: Text(
                    _error!,
                    key: const ValueKey('support-error'),
                    style: const TextStyle(color: AgColors.red, fontSize: 16),
                  ),
                ),
              FilledButton.icon(
                onPressed: _sending ? null : () => _send(d),
                icon: const Icon(Icons.send),
                label: const Text('Gửi'),
              ),
              const Divider(height: 32),
              Text(
                'Yêu cầu đã gửi',
                style: Theme.of(context).textTheme.titleMedium,
              ),
              if (d.items.isEmpty) const Text('Chưa có yêu cầu nào.'),
              for (final it in d.items)
                Card(
                  child: ListTile(
                    title: Text(supportCategories[it.category] ?? it.category),
                    subtitle: Text(
                      [
                        it.description,
                        if (it.lastNote != null) 'Cán bộ: ${it.lastNote}',
                      ].join('\n'),
                      maxLines: 3,
                      overflow: TextOverflow.ellipsis,
                    ),
                    trailing: Column(
                      mainAxisAlignment: MainAxisAlignment.center,
                      crossAxisAlignment: CrossAxisAlignment.end,
                      children: [
                        Tag(
                          it.statusLabel,
                          color: it.pending ? AgColors.amber : AgColors.g700,
                        ),
                        if (it.gpsMissing)
                          const Text(
                            'Thiếu vị trí GPS',
                            style: TextStyle(
                              fontSize: 13,
                              color: AgColors.mute,
                            ),
                          ),
                      ],
                    ),
                  ),
                ),
            ],
          );
        },
      ),
    );
  }
}

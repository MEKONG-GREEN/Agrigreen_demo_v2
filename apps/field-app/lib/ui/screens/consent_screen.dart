import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/consent_service.dart';
import '../providers.dart';

final consentStateProvider = FutureProvider.autoDispose
    .family<ConsentState?, String>(
      (ref, subjectRef) => servicesOf(ref).consents.current(subjectRef),
    );

/// Ghi nhận đồng ý của nông hộ cho mục đích `mrv.evidence` (kênh `field_app`)
/// trước khi thu ảnh minh chứng. Bản ghi `consent` vào hàng đợi, gửi khi có
/// mạng; máy chặn chụp ảnh khi chưa có đồng ý.
class ConsentScreen extends ConsumerStatefulWidget {
  const ConsentScreen({super.key, required this.subjectRef, this.label});
  final String subjectRef;
  final String? label;

  @override
  ConsumerState<ConsentScreen> createState() => _ConsentScreenState();
}

class _ConsentScreenState extends ConsumerState<ConsentScreen> {
  bool _read = false;
  bool _busy = false;

  Future<void> _record(ConsentStatus st) async {
    setState(() => _busy = true);
    try {
      await ref.services.consents.record(
        subjectRef: widget.subjectRef,
        status: st,
      );
      ref.invalidate(consentStateProvider(widget.subjectRef));
      if (!mounted) return;
      showMessage(
        context,
        st == ConsentStatus.granted
            ? 'Đã ghi nhận đồng ý. Có thể chụp ảnh minh chứng.'
            : 'Đã ghi nhận rút lại đồng ý.',
      );
      Navigator.of(context).pop(st == ConsentStatus.granted);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final cur = ref.watch(consentStateProvider(widget.subjectRef));
    final granted = cur.value?.granted ?? false;
    return Scaffold(
      appBar: AppBar(title: const Text('Đồng ý thu minh chứng')),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          if (widget.label != null)
            Text(
              'Nông hộ: ${widget.label}',
              style: const TextStyle(fontSize: 18, fontWeight: FontWeight.w700),
            ),
          const SizedBox(height: 8),
          Card(
            child: Padding(
              padding: const EdgeInsets.all(16),
              child: Text(
                mrvConsentText,
                style: const TextStyle(fontSize: 18, height: 1.4),
              ),
            ),
          ),
          Text(
            'Mục đích: $mrvEvidencePurpose · Phiên bản văn bản: $mrvConsentTextVersion · Kênh: ứng dụng hiện trường',
            style: const TextStyle(color: AgColors.mute, fontSize: 14),
          ),
          const SizedBox(height: 8),
          cur.when(
            loading: () => const SizedBox.shrink(),
            error: (e, _) => Text('Lỗi: $e'),
            data: (c) => Text(
              c == null
                  ? 'Chưa ghi nhận đồng ý trên máy này.'
                  : '${c.status.label} lúc ${c.eventTime}',
              style: TextStyle(
                fontSize: 16,
                fontWeight: FontWeight.w600,
                color: c?.granted ?? false ? AgColors.g700 : AgColors.amber,
              ),
            ),
          ),
          const SizedBox(height: 12),
          CheckboxListTile(
            value: _read,
            onChanged: (v) => setState(() => _read = v ?? false),
            controlAffinity: ListTileControlAffinity.leading,
            title: const Text(
              'Tôi đã đọc văn bản trên cho nông hộ nghe và hộ hiểu nội dung.',
            ),
          ),
          const SizedBox(height: 8),
          FilledButton.icon(
            onPressed: _busy || !_read || granted
                ? null
                : () => _record(ConsentStatus.granted),
            icon: const Icon(Icons.how_to_reg),
            label: const Text('Nông hộ đồng ý'),
          ),
          const SizedBox(height: 8),
          OutlinedButton.icon(
            onPressed: _busy || !granted
                ? null
                : () => _record(ConsentStatus.withdrawn),
            icon: const Icon(Icons.person_off),
            label: const Text('Nông hộ rút lại đồng ý'),
          ),
          const SizedBox(height: 8),
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: const Text('Nông hộ không đồng ý — quay lại'),
          ),
        ],
      ),
    );
  }
}

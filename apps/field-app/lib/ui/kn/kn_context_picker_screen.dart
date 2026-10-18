import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/kn_context_service.dart';
import '../../domain/kn_context.dart';
import '../providers.dart';

/// "Chọn vai trò và phòng ban" (KN v7.7 P1): tài khoản có nhiều cặp (vai
/// trò, phòng ban). [switching] = mở từ menu tài khoản "Đổi vai trò/phòng
/// ban" (đóng màn sau khi chọn).
class KnContextPickerScreen extends ConsumerStatefulWidget {
  const KnContextPickerScreen({
    super.key,
    required this.choices,
    this.switching = false,
    this.currentKey,
    this.message,
  });

  final List<KnContextChoice> choices;
  final bool switching;
  final String? currentKey;
  final String? message;

  @override
  ConsumerState<KnContextPickerScreen> createState() => _PickerState();
}

class _PickerState extends ConsumerState<KnContextPickerScreen> {
  bool _busy = false;

  Future<void> _pick(KnContextChoice c) async {
    setState(() => _busy = true);
    try {
      await ref.read(appControllerProvider.notifier).chooseContext(c);
      if (!mounted) return;
      if (widget.switching) {
        Navigator.of(context).pop(true);
        showMessage(
          context,
          'Đã chuyển sang ${c.title}. Đang đồng bộ lại dữ liệu.',
        );
      }
    } on KnContextException catch (e) {
      if (mounted) showMessage(context, e.message, error: true);
    } on Object {
      if (mounted) {
        showMessage(
          context,
          'Không chọn được vai trò. Kiểm tra kết nối mạng rồi thử lại.',
          error: true,
        );
      }
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Chọn vai trò và phòng ban'),
        automaticallyImplyLeading: widget.switching,
        actions: [
          if (!widget.switching)
            IconButton(
              tooltip: 'Khóa',
              iconSize: 28,
              icon: const Icon(Icons.lock),
              onPressed: () => ref.read(appControllerProvider.notifier).lock(),
            ),
        ],
      ),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          if (widget.message != null)
            Card(
              color: AgColors.amberBg,
              child: Padding(
                padding: const EdgeInsets.all(12),
                child: Text(
                  widget.message!,
                  style: const TextStyle(fontSize: 15),
                ),
              ),
            ),
          const Text(
            'Tài khoản có nhiều vai trò hoặc phòng ban. Chọn nơi làm việc của phiên này:',
            style: TextStyle(fontSize: 16),
          ),
          const SizedBox(height: 12),
          if (widget.choices.isEmpty)
            const Text(
              'Không có phân công nào đang hiệu lực.',
              style: TextStyle(color: AgColors.mute),
            ),
          for (final c in widget.choices)
            Card(
              child: ListTile(
                key: ValueKey('kn-choice-${c.key}'),
                enabled: !_busy,
                leading: Icon(
                  c.key == widget.currentKey
                      ? Icons.radio_button_checked
                      : Icons.radio_button_unchecked,
                  color: AgColors.g700,
                ),
                title: Text(c.title, style: const TextStyle(fontSize: 17)),
                subtitle: Text(
                  [
                    c.orgName,
                    if (c.communes.isNotEmpty)
                      'Xã: ${c.communes.map((x) => x.name).join(', ')}',
                    if (c.readOnly) 'Chỉ xem',
                  ].join('\n'),
                ),
                isThreeLine: c.communes.isNotEmpty || c.readOnly,
                onTap: () => _pick(c),
              ),
            ),
          if (_busy)
            const Padding(
              padding: EdgeInsets.all(16),
              child: Center(child: CircularProgressIndicator()),
            ),
        ],
      ),
    );
  }
}

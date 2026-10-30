import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/kn_collection_service.dart';
import '../../domain/kn_context.dart';
import '../providers.dart';
import '../../domain/pii_hint.dart';

/// Xã phụ trách của ngữ cảnh hiện hành.
final knCommunesProvider = Provider<List<KnCommune>>(
  (ref) => ref.watch(appControllerProvider).kn?.context?.communes ?? const [],
);

String knCommuneName(List<KnCommune> list, String? code) =>
    list.where((c) => c.code == code).firstOrNull?.name ?? (code ?? '');

/// Chạy một lệnh ghi và báo kết quả: đã lưu / đã xếp hàng / lỗi máy chủ.
Future<KnCommandResult?> knRunCommand(
  BuildContext context,
  Future<KnCommandResult> Function() f, {
  String done = 'Đã lưu.',
}) async {
  try {
    final r = await f();
    if (context.mounted) {
      showMessage(
        context,
        r.queued ? 'Mất kết nối — đã xếp hàng, sẽ tự gửi khi có mạng.' : done,
      );
    }
    return r;
  } on KnCommandError catch (e) {
    if (context.mounted) showMessage(context, e.message, error: true);
    return null;
  }
}

/// Hộp thoại nhập lý do (≥ [min] ký tự). Bộ điều khiển ô nhập thuộc về
/// hộp thoại (hủy cùng nó, sau hiệu ứng đóng — không hủy sớm khi còn vẽ).
Future<String?> knAskReason(
  BuildContext context, {
  required String title,
  String? body,
  int min = 10,
}) => showDialog<String>(
  context: context,
  builder: (_) => _KnReasonDialog(title: title, body: body, min: min),
);

class _KnReasonDialog extends StatefulWidget {
  const _KnReasonDialog({required this.title, this.body, required this.min});
  final String title;
  final String? body;
  final int min;
  @override
  State<_KnReasonDialog> createState() => _KnReasonDialogState();
}

class _KnReasonDialogState extends State<_KnReasonDialog> {
  final _ctl = TextEditingController();

  @override
  void dispose() {
    _ctl.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: Text(widget.title),
    content: Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        if (widget.body != null) Text(widget.body!),
        TextField(
          key: const ValueKey('kn-reason'),
          controller: _ctl,
          maxLines: 2,
          onChanged: (_) => setState(() {}),
          decoration: InputDecoration(
            labelText: 'Lý do (ít nhất ${widget.min} ký tự)',
            helperText: piiWarning(_ctl.text), // X-02
            helperMaxLines: 3,
          ),
        ),
      ],
    ),
    actions: [
      TextButton(
        onPressed: () => Navigator.pop(context),
        child: const Text('Hủy'),
      ),
      FilledButton(
        key: const ValueKey('kn-reason-ok'),
        onPressed: _ctl.text.trim().length >= widget.min
            ? () => Navigator.pop(context, _ctl.text.trim())
            : null,
        child: const Text('Xác nhận'),
      ),
    ],
  );
}

/// Danh sách phân trang (cursor) tải dần — ListView.builder, nút "Tải thêm".
class KnPagedList extends StatefulWidget {
  const KnPagedList({
    super.key,
    required this.load,
    required this.itemBuilder,
    this.empty = 'Chưa có dữ liệu.',
    this.header,
  });

  final Future<KnPage> Function(String? cursor) load;
  final Widget Function(BuildContext, Map<String, Object?>) itemBuilder;
  final String empty;
  final Widget? header;

  @override
  State<KnPagedList> createState() => KnPagedListState();
}

class KnPagedListState extends State<KnPagedList> {
  final _items = <Map<String, Object?>>[];
  String? _next;
  bool _loading = false;
  bool _cached = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    reload();
  }

  Future<void> reload() async {
    _items.clear();
    _next = null;
    await _more(first: true);
  }

  Future<void> _more({bool first = false}) async {
    if (_loading) return;
    setState(() => _loading = true);
    try {
      final p = await widget.load(first ? null : _next);
      if (!mounted) return;
      setState(() {
        _items.addAll(p.items);
        _next = p.nextCursor;
        _cached = p.fromCache;
        _error = null;
      });
    } on KnCommandError catch (e) {
      if (mounted) setState(() => _error = e.message);
    } on Object {
      if (mounted) {
        setState(() => _error = 'Không tải được — cần kết nối mạng.');
      }
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final extra = (widget.header == null ? 0 : 1) + 1;
    return RefreshIndicator(
      onRefresh: reload,
      child: ListView.builder(
        padding: const EdgeInsets.only(bottom: 96),
        itemCount: _items.length + extra + 1,
        itemBuilder: (ctx, i) {
          if (widget.header != null && i == 0) return widget.header!;
          final j = i - (widget.header == null ? 0 : 1);
          if (j == 0) {
            if (_cached) {
              return const Padding(
                padding: EdgeInsets.all(8),
                child: Text('Ngoại tuyến — đang xem dữ liệu đã lưu trên máy.'),
              );
            }
            if (_error != null) {
              return Padding(
                padding: const EdgeInsets.all(12),
                child: Text(_error!),
              );
            }
            if (_items.isEmpty && !_loading) {
              return Padding(
                padding: const EdgeInsets.all(24),
                child: Text(widget.empty, textAlign: TextAlign.center),
              );
            }
            return const SizedBox.shrink();
          }
          final k = j - 1;
          if (k < _items.length) return widget.itemBuilder(ctx, _items[k]);
          if (_loading) {
            return const Padding(
              padding: EdgeInsets.all(16),
              child: Center(child: CircularProgressIndicator()),
            );
          }
          if (_next != null && _next!.isNotEmpty) {
            return Center(
              child: TextButton(
                key: const ValueKey('kn-load-more'),
                onPressed: _more,
                child: const Text('Tải thêm'),
              ),
            );
          }
          return const SizedBox.shrink();
        },
      ),
    );
  }
}

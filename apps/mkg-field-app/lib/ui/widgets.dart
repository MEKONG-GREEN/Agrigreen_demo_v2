import 'package:flutter/material.dart';

/// Hộp thoại nhập một dòng; trả chuỗi đã nhập hoặc null (Hủy). Bộ điều khiển
/// thuộc về hộp thoại nên sống tới khi hoạt ảnh đóng kết thúc.
Future<String?> promptText(
  BuildContext context, {
  required String title,
  required String label,
  required String ok,
  Key? fieldKey,
  TextInputType? keyboardType,
  int maxLines = 1,
}) => showDialog<String>(
  context: context,
  builder: (_) => _PromptDialog(
    title: title,
    label: label,
    ok: ok,
    fieldKey: fieldKey,
    keyboardType: keyboardType,
    maxLines: maxLines,
  ),
);

class _PromptDialog extends StatefulWidget {
  const _PromptDialog({
    required this.title,
    required this.label,
    required this.ok,
    this.fieldKey,
    this.keyboardType,
    this.maxLines = 1,
  });
  final String title;
  final String label;
  final String ok;
  final Key? fieldKey;
  final TextInputType? keyboardType;
  final int maxLines;

  @override
  State<_PromptDialog> createState() => _PromptDialogState();
}

class _PromptDialogState extends State<_PromptDialog> {
  final _ctl = TextEditingController();

  @override
  void dispose() {
    _ctl.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: Text(widget.title),
    content: TextField(
      key: widget.fieldKey,
      controller: _ctl,
      keyboardType: widget.keyboardType,
      maxLines: widget.maxLines,
      decoration: InputDecoration(labelText: widget.label),
    ),
    actions: [
      TextButton(
        onPressed: () => Navigator.of(context).pop(),
        child: const Text('Hủy'),
      ),
      FilledButton(
        key: const ValueKey('prompt-ok'),
        onPressed: () => Navigator.of(context).pop(_ctl.text),
        child: Text(widget.ok),
      ),
    ],
  );
}

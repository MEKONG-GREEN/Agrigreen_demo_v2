import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/activation_service.dart';
import '../providers.dart';

/// "Kích hoạt tài khoản": mở liên kết/QR một lần do quản lý HTX cấp để tự đặt
/// mật khẩu trên trang đăng nhập AgriGreen, rồi quay lại "Đăng nhập".
///
/// Quét QR: dùng ứng dụng máy ảnh/quét QR có sẵn trên điện thoại (mở thẳng
/// liên kết), hoặc chép liên kết rồi dán vào đây. App không nhúng thư viện
/// quét QR (tránh thêm phụ thuộc native nặng).
class ActivationScreen extends ConsumerStatefulWidget {
  const ActivationScreen({super.key});

  @override
  ConsumerState<ActivationScreen> createState() => _ActivationScreenState();
}

class _ActivationScreenState extends ConsumerState<ActivationScreen> {
  final _link = TextEditingController();
  String? _error;

  @override
  void dispose() {
    _link.dispose();
    super.dispose();
  }

  Future<void> _paste() async {
    final d = await Clipboard.getData(Clipboard.kTextPlain);
    if (d?.text != null) setState(() => _link.text = d!.text!.trim());
  }

  Future<void> _open() async {
    try {
      await ref.read(activationProvider).open(_link.text);
      setState(() => _error = null);
      if (mounted) {
        showMessage(
          context,
          'Đã mở trang kích hoạt. Đặt mật khẩu xong, quay lại bấm "Đăng nhập".',
        );
      }
    } on ActivationException catch (e) {
      setState(() => _error = e.message);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Kích hoạt tài khoản')),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          const Card(
            child: Padding(
              padding: EdgeInsets.all(16),
              child: Text(
                '1. Quét mã QR quản lý HTX đưa bằng máy ảnh của điện thoại, '
                'hoặc chép liên kết kích hoạt.\n'
                '2. Dán liên kết vào ô dưới rồi bấm "Mở trang kích hoạt".\n'
                '3. Đặt mật khẩu mới trên trang AgriGreen.\n'
                '4. Quay lại app, bấm "Đăng nhập" bằng số điện thoại và mật khẩu vừa đặt.\n\n'
                'Liên kết chỉ dùng một lần. Mất hoặc hết hạn: nhờ quản lý HTX cấp lại.',
                style: TextStyle(fontSize: 16),
              ),
            ),
          ),
          const SizedBox(height: 12),
          TextField(
            key: const ValueKey('activation-link'),
            controller: _link,
            keyboardType: TextInputType.url,
            minLines: 1,
            maxLines: 3,
            decoration: InputDecoration(
              labelText: 'Liên kết kích hoạt',
              errorText: _error,
              errorMaxLines: 3,
              suffixIcon: IconButton(
                tooltip: 'Dán',
                icon: const Icon(Icons.content_paste),
                onPressed: _paste,
              ),
            ),
          ),
          const SizedBox(height: 16),
          FilledButton.icon(
            onPressed: _open,
            icon: const Icon(Icons.open_in_browser),
            label: const Text('Mở trang kích hoạt'),
          ),
          const SizedBox(height: 12),
          const Text(
            'Chỉ mở liên kết của máy chủ đăng nhập AgriGreen. Không đưa liên kết '
            'cho người khác.',
            style: TextStyle(color: AgColors.mute),
          ),
        ],
      ),
    );
  }
}

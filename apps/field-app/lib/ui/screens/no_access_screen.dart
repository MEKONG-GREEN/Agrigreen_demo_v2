import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../providers.dart';

/// Tài khoản đã đăng nhập nhưng không có ngữ cảnh Khuyến nông (không có phân
/// công vai trò/phòng ban KN, hoặc chưa chọn được vai trò khi ngoại tuyến):
/// App KN chỉ có màn theo BRD 2.0 nên không còn màn nào để vào
/// (KN-STANDALONE-9 #270) — chỉ đăng xuất được.
class NoKnAccessScreen extends ConsumerWidget {
  const NoKnAccessScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final kn = ref.watch(appControllerProvider).kn;
    return Scaffold(
      body: SafeArea(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              const Spacer(),
              const Icon(Icons.lock_outline, size: 88, color: AgColors.mute),
              const SizedBox(height: 16),
              Text(
                'Không có quyền',
                textAlign: TextAlign.center,
                style: Theme.of(context).textTheme.titleLarge,
              ),
              const SizedBox(height: 8),
              Text(
                kn?.offline == true && kn?.message != null
                    ? kn!.message!
                    : 'Tài khoản chưa được phân công vai trò Khuyến nông. '
                          'Liên hệ quản trị viên để được cấp quyền.',
                textAlign: TextAlign.center,
                style: const TextStyle(fontSize: 16, color: AgColors.mute),
              ),
              const Spacer(),
              FilledButton.icon(
                key: const ValueKey('no-access-signout'),
                onPressed: () =>
                    ref.read(appControllerProvider.notifier).signOut(),
                icon: const Icon(Icons.logout),
                label: const Text('Đăng xuất'),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

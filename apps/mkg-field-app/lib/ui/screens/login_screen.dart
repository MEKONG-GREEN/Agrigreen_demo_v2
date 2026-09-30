import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../providers.dart';

/// Đăng nhập trực tuyến OIDC (Keycloak, PKCE; client `agrigreen-mkg-field-app`).
/// Lần đầu bắt buộc có mạng; sau đó dùng PIN ngoại tuyến tối đa 72 giờ.
class LoginScreen extends ConsumerWidget {
  const LoginScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final st = ref.watch(appControllerProvider);
    return Scaffold(
      body: SafeArea(
        child: LayoutBuilder(
          builder: (context, c) => SingleChildScrollView(
            padding: const EdgeInsets.all(24),
            child: ConstrainedBox(
              constraints: BoxConstraints(minHeight: c.maxHeight - 48),
              child: IntrinsicHeight(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    const Spacer(),
                    const Icon(
                      Icons.agriculture,
                      size: 80,
                      color: AgColors.g700,
                    ),
                    const SizedBox(height: 16),
                    Text(
                      'MKG Gom rơm',
                      textAlign: TextAlign.center,
                      style: Theme.of(context).textTheme.titleLarge,
                    ),
                    const SizedBox(height: 8),
                    const Text(
                      'Đăng nhập bằng tài khoản Mekong Green được cấp. '
                      'Lần đăng nhập đầu tiên cần có mạng.',
                      textAlign: TextAlign.center,
                      style: TextStyle(fontSize: 16, color: AgColors.mute),
                    ),
                    const SizedBox(height: 24),
                    if (st.view.message != null)
                      Container(
                        padding: const EdgeInsets.all(14),
                        decoration: BoxDecoration(
                          color: AgColors.amberBg,
                          borderRadius: BorderRadius.circular(12),
                        ),
                        child: Text(
                          st.view.message!,
                          style: const TextStyle(fontSize: 16),
                        ),
                      ),
                    const Spacer(),
                    FilledButton.icon(
                      onPressed: st.busy
                          ? null
                          : () => ref
                                .read(appControllerProvider.notifier)
                                .loginOnline(),
                      icon: const Icon(Icons.login),
                      label: const Text('Đăng nhập'),
                    ),
                  ],
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }
}

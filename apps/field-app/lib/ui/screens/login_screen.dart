import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../providers.dart';
import '../theme.dart';

/// Đăng nhập trực tuyến OIDC (Keycloak, PKCE). Lần đầu bắt buộc có mạng.
class LoginScreen extends ConsumerWidget {
  const LoginScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final st = ref.watch(appControllerProvider);
    return Scaffold(
      body: SafeArea(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              const Spacer(),
              const Icon(Icons.eco, size: 88, color: AgColors.g700),
              const SizedBox(height: 16),
              Text(
                'AgriGreen Hiện trường',
                textAlign: TextAlign.center,
                style: Theme.of(context).textTheme.titleLarge,
              ),
              const SizedBox(height: 8),
              const Text(
                'Đăng nhập bằng tài khoản được cấp. Lần đăng nhập đầu tiên cần có mạng.',
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
                icon: st.busy
                    ? const SizedBox(
                        width: 22,
                        height: 22,
                        child: CircularProgressIndicator(strokeWidth: 3),
                      )
                    : const Icon(Icons.login),
                label: const Text('Đăng nhập'),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

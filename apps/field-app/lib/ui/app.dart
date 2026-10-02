import 'package:agrigreen_mobile/application/auth_flow.dart';
import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../application/kn_context_service.dart';
import 'kn/kn_context_picker_screen.dart';
import 'kn/kn_home_screen.dart';
import 'providers.dart';
import 'screens/home_screen.dart';
import 'screens/login_screen.dart';
import 'screens/pin_screen.dart';

class FieldApp extends StatelessWidget {
  const FieldApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'AgriGreen Hiện trường',
      debugShowCheckedModeBanner: false,
      theme: buildTheme(),
      locale: const Locale('vi'),
      home: const _AuthGate(),
    );
  }
}

class _AuthGate extends ConsumerWidget {
  const _AuthGate();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final st = ref.watch(appControllerProvider);
    return switch (st.view.stage) {
      AuthStage.loading => const Scaffold(
        body: Center(child: CircularProgressIndicator()),
      ),
      AuthStage.needsOnlineLogin => const LoginScreen(),
      AuthStage.needsPinSetup => const PinScreen(setup: true),
      AuthStage.locked => const PinScreen(setup: false),
      AuthStage.unlocked =>
        st.services == null
            ? const LoginScreen()
            : switch (st.kn?.stage) {
                // KN v7.7: nhiều (vai trò, phòng ban) → chọn; có ngữ cảnh →
                // trang Cấp Xã; tài khoản không có phân công KN → màn cũ.
                KnContextStage.picker => KnContextPickerScreen(
                  choices: st.kn!.choices,
                  message: st.kn!.message,
                ),
                KnContextStage.ready => const KnHomeScreen(),
                _ => const HomeScreen(),
              },
    };
  }
}

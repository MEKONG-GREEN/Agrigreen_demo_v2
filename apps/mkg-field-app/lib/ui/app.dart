import 'package:agrigreen_mobile/application/auth_flow.dart';
import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'providers.dart';
import 'screens/home_screen.dart';
import 'screens/login_screen.dart';
import 'screens/pin_screen.dart';
import 'screens/trip_list_screen.dart';

class MkgFieldApp extends StatelessWidget {
  const MkgFieldApp({super.key});

  @override
  Widget build(BuildContext context) => MaterialApp(
    title: 'MKG Gom rơm',
    debugShowCheckedModeBanner: false,
    theme: buildTheme(),
    locale: const Locale('vi'),
    home: const _AuthGate(),
  );
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
            : st.driverOnly
            ? const TripListScreen(asHome: true)
            : const HomeScreen(),
    };
  }
}

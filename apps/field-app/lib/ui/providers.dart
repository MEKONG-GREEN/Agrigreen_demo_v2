import 'dart:async';

import 'package:connectivity_plus/connectivity_plus.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../application/app_services.dart';
import '../application/auth_flow.dart';
import '../data/auth/oidc_client.dart';
import '../data/auth/pin_service.dart';
import '../data/auth/session_service.dart';
import '../data/outbox/outbox_repository.dart';
import '../data/reference/reference_repository.dart';
import '../data/sync/sync_service.dart';

/// Ghi đè trong `main.dart` (thiết bị) hoặc trong kiểm thử widget.
final platformProvider = Provider<AppPlatform>(
  (ref) => throw UnimplementedError('platformProvider'),
);
final oidcProvider = Provider<OidcClient>(
  (ref) => throw UnimplementedError('oidcProvider'),
);

final sessionServiceProvider = Provider<SessionService>((ref) {
  final pf = ref.watch(platformProvider);
  return SessionService(
    store: pf.store,
    clock: pf.clock,
    oidc: ref.watch(oidcProvider),
    offlineMax: pf.config.offlineSessionMax,
  );
});

final pinServiceProvider = Provider<PinService>((ref) {
  final pf = ref.watch(platformProvider);
  return PinService(
    store: pf.store,
    clock: pf.clock,
    maxAttempts: pf.config.pinMaxAttempts,
    lockout: pf.config.pinLockout,
    hardLimit: pf.config.pinHardLimit,
  );
});

final authFlowProvider = Provider<AuthFlow>(
  (ref) => AuthFlow(
    session: ref.watch(sessionServiceProvider),
    pins: ref.watch(pinServiceProvider),
  ),
);

class AppState {
  const AppState(this.view, {this.services, this.busy = false});
  final AuthView view;
  final AppServices? services;
  final bool busy;

  AppState copyWith({AuthView? view, AppServices? services, bool? busy}) =>
      AppState(
        view ?? this.view,
        services: services ?? this.services,
        busy: busy ?? this.busy,
      );
}

class AppController extends Notifier<AppState> {
  StreamSubscription<List<ConnectivityResult>>? _conn;
  Timer? _timer;

  @override
  AppState build() {
    ref.onDispose(_stopAutoSync);
    Future.microtask(_init);
    return const AppState(AuthView(AuthStage.loading));
  }

  AuthFlow get _flow => ref.read(authFlowProvider);

  Future<void> _init() async => _apply(await _flow.start());

  Future<void> _apply(AuthView v) async {
    if (v.stage == AuthStage.unlocked) {
      final existing = state.services;
      final services =
          existing ??
          await AppServices.open(
            ref.read(platformProvider),
            accessToken: ref.read(sessionServiceProvider).accessToken,
            onWiped: _onWiped,
          );
      state = AppState(v, services: services);
      _startAutoSync(services);
      unawaited(services.sync.syncOnce(force: true));
    } else {
      // Ngoài trạng thái mở khóa không giữ CSDL mở.
      _stopAutoSync();
      final old = state.services;
      state = AppState(v);
      await old?.close();
    }
  }

  Future<void> _busy(Future<AuthView> Function() f) async {
    state = state.copyWith(busy: true);
    try {
      await _apply(await f());
    } finally {
      state = state.copyWith(busy: false);
    }
  }

  Future<void> loginOnline() => _busy(_flow.loginOnline);
  Future<void> setPin(String pin, String confirm) =>
      _busy(() => _flow.setPin(pin, confirm));
  Future<void> unlock(String pin) => _busy(() => _flow.unlock(pin));

  /// Khóa màn hình: đóng CSDL, về màn PIN.
  Future<void> lock() async {
    _stopAutoSync();
    await state.services?.close();
    state = const AppState(AuthView(AuthStage.locked));
  }

  void _onWiped() {
    _stopAutoSync();
    state = const AppState(
      AuthView(
        AuthStage.needsOnlineLogin,
        message: 'Thiết bị đã bị thu hồi. Dữ liệu trên máy đã được xóa. Liên hệ quản trị để được cấp lại.',
      ),
    );
  }

  void _startAutoSync(AppServices s) {
    _stopAutoSync();
    // Có mạng trở lại → đồng bộ ngay (HTX US-LOGIN-02 AC-3).
    _conn = Connectivity().onConnectivityChanged.listen((r) {
      if (r.any((c) => c != ConnectivityResult.none)) {
        unawaited(s.sync.syncOnce(force: true));
      }
    });
    // Thử lại theo chu kỳ; SyncService tự tôn trọng thời điểm lùi.
    _timer = Timer.periodic(
      const Duration(minutes: 2),
      (_) => unawaited(s.sync.syncOnce()),
    );
  }

  void _stopAutoSync() {
    unawaited(_conn?.cancel());
    _conn = null;
    _timer?.cancel();
    _timer = null;
  }
}

final appControllerProvider = NotifierProvider<AppController, AppState>(
  AppController.new,
);

AppServices servicesOf(Ref ref) {
  final s = ref.watch(appControllerProvider).services;
  if (s == null) throw StateError('Chưa mở khóa');
  return s;
}

class HomeSummary {
  const HomeSummary({
    required this.counts,
    required this.status,
    required this.openTasks,
    required this.plots,
    required this.pendingFiles,
  });
  final OutboxCounts counts;
  final SyncStatus status;
  final int openTasks;
  final int plots;
  final int pendingFiles;
}

/// Làm mới khi trạng thái đồng bộ đổi.
final syncStatusStreamProvider = StreamProvider<SyncStatus>((ref) {
  final s = servicesOf(ref);
  return s.sync.statusStream;
});

final homeSummaryProvider = FutureProvider<HomeSummary>((ref) async {
  ref.watch(syncStatusStreamProvider);
  final s = servicesOf(ref);
  return HomeSummary(
    counts: await s.outbox.counts(),
    status: await s.sync.status(),
    openTasks: await s.tasks.openCount(),
    plots: await s.refs.count(RefEntity.plot),
    pendingFiles: (await s.evidence.pending()).length,
  );
});

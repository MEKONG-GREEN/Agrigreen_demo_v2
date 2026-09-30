import 'dart:async';

import 'package:agrigreen_mobile/application/auth_flow.dart';
import 'package:agrigreen_mobile/data/auth/oidc_client.dart';
import 'package:agrigreen_mobile/data/auth/pin_service.dart';
import 'package:agrigreen_mobile/data/auth/session_service.dart';
import 'package:agrigreen_mobile/data/outbox/outbox_repository.dart';
import 'package:agrigreen_mobile/data/sync/sync_service.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../application/app_services.dart';
import '../domain/fieldops.dart';
import '../domain/roles.dart';

/// Ghi đè trong `main.dart` (thiết bị) hoặc trong kiểm thử.
final platformProvider = Provider<MkgPlatform>(
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
  const AppState(
    this.view, {
    this.services,
    this.busy = false,
    this.mode = FieldMode.none,
  });
  final AuthView view;
  final MkgServices? services;
  final bool busy;
  final FieldMode mode;

  AppState copyWith({bool? busy}) =>
      AppState(view, services: services, busy: busy ?? this.busy, mode: mode);
}

/// Đăng nhập OIDC → phiên ngoại tuyến 72 giờ + PIN (lõi AuthFlow) → mở CSDL
/// SQLCipher → đồng bộ tự động.
class AppController extends Notifier<AppState> {
  StreamSubscription<bool>? _conn;
  Timer? _timer;

  @override
  AppState build() {
    ref.onDispose(_stopAutoSync);
    Future.microtask(() async => _apply(await _flow.start()));
    return const AppState(AuthView(AuthStage.loading));
  }

  AuthFlow get _flow => ref.read(authFlowProvider);

  Future<void> _apply(AuthView v) async {
    if (v.stage == AuthStage.unlocked) {
      final services =
          state.services ??
          await MkgServices.open(
            ref.read(platformProvider),
            accessToken: ref.read(sessionServiceProvider).accessToken,
            onWiped: _onWiped,
          );
      state = AppState(v, services: services, mode: await services.mode());
      _startAutoSync(services);
      unawaited(services.sync.syncOnce(force: true));
    } else {
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
        message: 'Thiết bị đã bị thu hồi. Dữ liệu trên máy đã được xóa. Liên hệ giám sát gom.',
      ),
    );
  }

  void _startAutoSync(MkgServices s) {
    _stopAutoSync();
    final pf = s.platform;
    _conn = pf.onlineChanges?.call().listen((online) {
      if (online) unawaited(s.sync.syncOnce(force: true));
    });
    if (!pf.autoSync) return;
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

extension WidgetServices on WidgetRef {
  MkgServices get services {
    final s = read(appControllerProvider).services;
    if (s == null) throw StateError('Chưa mở khóa');
    return s;
  }
}

MkgServices servicesOf(Ref ref) {
  final s = ref.watch(appControllerProvider).services;
  if (s == null) throw StateError('Chưa mở khóa');
  return s;
}

final syncStatusStreamProvider = StreamProvider<SyncStatus>(
  (ref) => servicesOf(ref).sync.statusStream,
);

class HomeData {
  const HomeData(this.orders, this.counts, this.status, this.pendingActions);
  final List<CollectionOrder> orders;
  final OutboxCounts counts;
  final SyncStatus status;
  final int pendingActions;
  int get waiting => counts.pending + pendingActions;
}

final homeProvider = FutureProvider<HomeData>((ref) async {
  ref.watch(syncStatusStreamProvider);
  final s = servicesOf(ref);
  return HomeData(
    await s.field.orders(),
    await s.outbox.counts(),
    await s.sync.status(),
    await s.actions.pendingCount(),
  );
});

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/auth_flow.dart';
import '../providers.dart';
import '../theme.dart';

/// Nhập PIN 6 số (mở khóa) hoặc đặt PIN (lần đầu, sau đăng nhập trực tuyến).
class PinScreen extends ConsumerStatefulWidget {
  const PinScreen({super.key, required this.setup});
  final bool setup;

  @override
  ConsumerState<PinScreen> createState() => _PinScreenState();
}

class _PinScreenState extends ConsumerState<PinScreen> {
  String _pin = '';
  String? _first;

  void _press(String d) {
    if (_pin.length >= 6) return;
    setState(() => _pin += d);
    if (_pin.length == 6) _submit();
  }

  void _back() {
    if (_pin.isEmpty) return;
    setState(() => _pin = _pin.substring(0, _pin.length - 1));
  }

  Future<void> _submit() async {
    final ctl = ref.read(appControllerProvider.notifier);
    final pin = _pin;
    if (widget.setup) {
      if (_first == null) {
        setState(() {
          _first = pin;
          _pin = '';
        });
        return;
      }
      final first = _first!;
      setState(() {
        _first = null;
        _pin = '';
      });
      await ctl.setPin(first, pin);
    } else {
      setState(() => _pin = '');
      await ctl.unlock(pin);
    }
  }

  @override
  Widget build(BuildContext context) {
    final st = ref.watch(appControllerProvider);
    final lockedUntil = st.view.lockedUntil;
    final locked = lockedUntil != null && DateTime.now().isBefore(lockedUntil);
    final title = widget.setup
        ? (_first == null ? 'Tạo mã PIN 6 số' : 'Nhập lại mã PIN')
        : 'Nhập mã PIN';
    return Scaffold(
      body: SafeArea(
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 16),
          child: Column(
            children: [
              const SizedBox(height: 24),
              const Icon(Icons.lock_outline, size: 56, color: AgColors.g700),
              const SizedBox(height: 12),
              Text(title, style: Theme.of(context).textTheme.titleLarge),
              const SizedBox(height: 20),
              Row(
                mainAxisAlignment: MainAxisAlignment.center,
                children: [
                  for (var i = 0; i < 6; i++)
                    Container(
                      margin: const EdgeInsets.all(8),
                      width: 20,
                      height: 20,
                      decoration: BoxDecoration(
                        shape: BoxShape.circle,
                        color: i < _pin.length ? AgColors.g700 : Colors.white,
                        border: Border.all(color: AgColors.g700, width: 2),
                      ),
                    ),
                ],
              ),
              const SizedBox(height: 12),
              SizedBox(
                height: 48,
                child: st.view.message == null
                    ? null
                    : Text(
                        st.view.message!,
                        textAlign: TextAlign.center,
                        style: const TextStyle(
                          color: AgColors.red,
                          fontSize: 16,
                        ),
                      ),
              ),
              const Spacer(),
              _Pad(
                enabled: !st.busy && !locked,
                onDigit: _press,
                onBack: _back,
              ),
              if (!widget.setup && st.view.stage == AuthStage.locked)
                TextButton(
                  onPressed: st.busy
                      ? null
                      : () => ref
                            .read(appControllerProvider.notifier)
                            .loginOnline(),
                  child: const Text(
                    'Quên PIN? Đăng nhập bằng mật khẩu',
                    style: TextStyle(fontSize: 16),
                  ),
                ),
            ],
          ),
        ),
      ),
    );
  }
}

class _Pad extends StatelessWidget {
  const _Pad({
    required this.enabled,
    required this.onDigit,
    required this.onBack,
  });
  final bool enabled;
  final void Function(String) onDigit;
  final VoidCallback onBack;

  @override
  Widget build(BuildContext context) {
    Widget key(String label, {VoidCallback? onTap, IconData? icon}) => Expanded(
      child: Padding(
        padding: const EdgeInsets.all(6),
        child: SizedBox(
          height: 68,
          child: OutlinedButton(
            onPressed: enabled ? onTap : null,
            child: icon != null
                ? Icon(icon, size: 28)
                : Text(
                    label,
                    style: const TextStyle(
                      fontSize: 26,
                      fontWeight: FontWeight.w700,
                    ),
                  ),
          ),
        ),
      ),
    );
    Widget row(List<String> ds) =>
        Row(children: [for (final d in ds) key(d, onTap: () => onDigit(d))]);
    return Column(
      children: [
        row(['1', '2', '3']),
        row(['4', '5', '6']),
        row(['7', '8', '9']),
        Row(
          children: [
            const Expanded(child: SizedBox()),
            key('0', onTap: () => onDigit('0')),
            key('', onTap: onBack, icon: Icons.backspace_outlined),
          ],
        ),
      ],
    );
  }
}

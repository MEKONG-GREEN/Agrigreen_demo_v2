import 'package:flutter/material.dart';

/// Màu theo prototype App_Khuyen_Nong_v5.0 (mobile).
abstract final class AgColors {
  static const g900 = Color(0xFF0B4A2F);
  static const g700 = Color(0xFF146B41);
  static const g600 = Color(0xFF1B8A54);
  static const g100 = Color(0xFFE7F5EC);
  static const amber = Color(0xFFD9880C);
  static const amberBg = Color(0xFFFEF6E7);
  static const red = Color(0xFFD93636);
  static const redBg = Color(0xFFFDECEC);
  static const blue = Color(0xFF0B82AD);
  static const ink = Color(0xFF111827);
  static const mute = Color(0xFF6B7280);
  static const bg = Color(0xFFF5F8F6);
}

/// Chủ đề chữ lớn, nút cao ≥ 56 dp cho máy Android cấu hình thấp, dùng ngoài nắng.
ThemeData buildTheme() {
  final scheme = ColorScheme.fromSeed(
    seedColor: AgColors.g700,
    primary: AgColors.g700,
    error: AgColors.red,
    surface: Colors.white,
  );
  const big = Size(64, 56);
  return ThemeData(
    colorScheme: scheme,
    scaffoldBackgroundColor: AgColors.bg,
    visualDensity: VisualDensity.standard,
    materialTapTargetSize: MaterialTapTargetSize.padded,
    textTheme: const TextTheme(
      bodyLarge: TextStyle(fontSize: 18, color: AgColors.ink),
      bodyMedium: TextStyle(fontSize: 16, color: AgColors.ink),
      titleLarge: TextStyle(fontSize: 22, fontWeight: FontWeight.w700),
      titleMedium: TextStyle(fontSize: 18, fontWeight: FontWeight.w600),
      labelLarge: TextStyle(fontSize: 18, fontWeight: FontWeight.w600),
    ),
    appBarTheme: const AppBarTheme(
      backgroundColor: AgColors.g700,
      foregroundColor: Colors.white,
      titleTextStyle: TextStyle(
        fontSize: 20,
        fontWeight: FontWeight.w700,
        color: Colors.white,
      ),
    ),
    filledButtonTheme: FilledButtonThemeData(
      style: FilledButton.styleFrom(
        minimumSize: big,
        textStyle: const TextStyle(fontSize: 18, fontWeight: FontWeight.w600),
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
      ),
    ),
    outlinedButtonTheme: OutlinedButtonThemeData(
      style: OutlinedButton.styleFrom(
        minimumSize: big,
        textStyle: const TextStyle(fontSize: 18, fontWeight: FontWeight.w600),
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
      ),
    ),
    inputDecorationTheme: const InputDecorationTheme(
      border: OutlineInputBorder(),
      filled: true,
      fillColor: Colors.white,
      contentPadding: EdgeInsets.symmetric(horizontal: 16, vertical: 18),
    ),
    listTileTheme: const ListTileThemeData(
      minVerticalPadding: 14,
      titleTextStyle: TextStyle(
        fontSize: 18,
        fontWeight: FontWeight.w600,
        color: AgColors.ink,
      ),
      subtitleTextStyle: TextStyle(fontSize: 15, color: AgColors.mute),
    ),
  );
}

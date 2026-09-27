plugins {
    id("com.android.application")
    // The Flutter Gradle Plugin must be applied after the Android and Kotlin Gradle plugins.
    id("dev.flutter.flutter-gradle-plugin")
}

android {
    namespace = "vn.agrigreen.agrigreen_field"
    compileSdk = flutter.compileSdkVersion
    ndkVersion = flutter.ndkVersion

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    defaultConfig {
        // Trùng scheme redirect OIDC vn.agrigreen.field:/oauth2redirect.
        applicationId = "vn.agrigreen.field"
        // Android 8.0 (API 26) trở lên — máy cấu hình thấp của cán bộ xã.
        minSdk = 26
        targetSdk = flutter.targetSdkVersion
        // Uses the version code from pubspec.yaml. When using split APKs, 1000 * ABI_VERSION
        // is added automatically by Flutter. (https://developer.android.com/studio/build/configure-apk-splits#configure-APK-versions)
        // You can force using the value of versionCode by specifying the `-P force-version-code-ignoring-abi=true`
        // flag during build.
        versionCode = flutter.versionCode
        versionName = flutter.versionName
        // flutter_appauth: scheme nhận redirect từ Keycloak.
        manifestPlaceholders["appAuthRedirectScheme"] = "vn.agrigreen.field"
    }

    buildTypes {
        release {
            // TODO(ci): ký bản phát hành bằng keystore từ secret CI (không commit keystore).
            // Tạm ký bằng khóa debug để `flutter run --release` chạy được.
            signingConfig = signingConfigs.getByName("debug")
        }
    }
}

kotlin {
    compilerOptions {
        jvmTarget = org.jetbrains.kotlin.gradle.dsl.JvmTarget.JVM_17
    }
}

flutter {
    source = "../.."
}

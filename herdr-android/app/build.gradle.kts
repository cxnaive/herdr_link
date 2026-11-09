import org.jetbrains.kotlin.gradle.dsl.JvmTarget

plugins { id("com.android.application"); id("org.jetbrains.kotlin.android") }

android {
    namespace = "dev.herdr.companion"
    buildFeatures { buildConfig = true }
    testOptions { unitTests.isIncludeAndroidResources = true }
    compileSdk = 35
    buildToolsVersion = "35.0.0"
    splits {
        abi {
            isEnable = providers.gradleProperty("splitApks").orNull == "true"
            reset()
            include("armeabi-v7a", "arm64-v8a", "x86_64")
            isUniversalApk = false
        }
    }
    defaultConfig {
        applicationId = "dev.herdr.companion"
        minSdk = 28
        targetSdk = 35
        versionCode = 35
        versionName = "0.3.0"
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
    }
    compileOptions { sourceCompatibility = JavaVersion.VERSION_17; targetCompatibility = JavaVersion.VERSION_17 }
}
kotlin { compilerOptions { jvmTarget.set(JvmTarget.JVM_17) } }

dependencies {
    // Native lifecycle, encrypted WebSocket transport, WebRTC and scoped sharing.
    implementation("androidx.activity:activity-ktx:1.10.1")
    implementation("com.squareup.okhttp3:okhttp:4.12.0")
    implementation("io.github.webrtc-sdk:android:144.7559.15")
    implementation("androidx.core:core-ktx:1.16.0")
    // Bundled on-device barcode model: no first-run model download.
    implementation("com.google.mlkit:barcode-scanning:17.3.0")
    implementation("androidx.camera:camera-camera2:1.4.2")
    implementation("androidx.camera:camera-lifecycle:1.4.2")
    implementation("androidx.camera:camera-view:1.4.2")
    // Native Markdown rendering, including fenced code and tables.
    implementation("io.noties.markwon:core:4.6.2")
    implementation("io.noties.markwon:ext-tables:4.6.2")
    implementation("io.noties.markwon:ext-strikethrough:4.6.2")
    testImplementation("junit:junit:4.13.2")
    // Exercise native Canvas typography and both disclosure levels without a personal device.
    testImplementation("org.robolectric:robolectric:4.14.1")
    testImplementation("org.json:json:20240303")
    testImplementation("com.squareup.okhttp3:mockwebserver:4.12.0")
    testImplementation("com.squareup.okhttp3:okhttp-tls:4.12.0")
    androidTestImplementation("androidx.test:runner:1.6.2")
    androidTestImplementation("androidx.test.ext:junit:1.2.1")
    androidTestImplementation("androidx.test:core-ktx:1.6.1")
    androidTestImplementation("androidx.test.uiautomator:uiautomator:2.3.0")
    // Generates deterministic QR fixtures only; production detection uses ML Kit.
    androidTestImplementation("com.google.zxing:core:3.5.3")
    androidTestImplementation("com.squareup.okhttp3:mockwebserver:4.12.0")
    androidTestImplementation("com.squareup.okhttp3:okhttp-tls:4.12.0")
}

// Optional local build proxy is supplied by the caller, never stored as a workstation setting.
tasks.withType<Test>().configureEach {
    for (key in listOf("socksProxyHost", "socksProxyPort", "http.nonProxyHosts", "https.proxyHost", "https.proxyPort")) {
        System.getProperty(key)?.let { systemProperty(key, it) }
    }
}

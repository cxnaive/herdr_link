# Third-party code · 0.2.0

- Herdr Mobile Relay, https://github.com/0cv/herdr-mobile-relay, revision `09b52781399114a7576d40fcdf91ccefd7686be6`: GNU AGPL-3.0-or-later. Its original LICENSE is the root LICENSE. Full corresponding upstream source is included in `third_party/herdr-mobile-relay-09b5278.zip`; Android modifications and relay patch are supplied in this source package.
- The upstream web client is reference source, no longer bundled or executed in the Android application. Its source archive retains its dependency manifests and notices.
- Termux terminal emulator and renderer, https://github.com/termux/termux-app, revision `8629e632fcb95da272221be327db653fb24befe9`: GPL-3.0-only with Apache-2.0 exceptions documented in its original notice. Java files are included unchanged under `app/src/main/java/com/termux/`. See `third_party/termux/LICENSE.md`, `UPSTREAM.md`, and `termux-terminal-source.zip` for notices, provenance and corresponding module sources. No local shell or Termux PTY JNI library is used.
- OkHttp 4.12.0 and Okio: Apache-2.0. OkHttp provides native TLS/WebSocket transport; MockWebServer and okhttp-tls are test-only dependencies. https://github.com/square/okhttp
- WebRTC Android SDK `io.github.webrtc-sdk:android:144.7559.15`: native WebRTC and its third-party dependencies. The SDK project uses BSD-3-Clause; WebRTC has its own BSD-style license and third-party notices. See https://github.com/webrtc-sdk/android and https://webrtc.googlesource.com/src/+/refs/heads/main/LICENSE. Exact binary artifact version is pinned in Gradle.
- AndroidX Activity, Core, CameraX 1.4.2 and AndroidX test libraries: Apache-2.0.
- Google ML Kit Barcode Scanning 17.3.0: Google-provided SDK and bundled on-device model, distributed under Google's applicable ML Kit / Android SDK terms. See https://developers.google.com/ml-kit/terms. This SDK/model is not covered by this application's AGPL license.
- ZXing core 3.5.3: Apache-2.0. Used only to generate QR fixtures in the instrumentation test APK; production detection uses ML Kit.
- Markwon 4.6.2 (core, tables, strikethrough): Apache-2.0. Used for native Markdown rendering; includes commonmark-java (BSD-2-Clause).
- Kotlin: Apache-2.0. Gradle / Android Gradle Plugin: Apache-2.0.
- JUnit 4: EPL-1.0, test-only. JSON-java: public domain, JVM tests only.

Direct artifact versions are pinned in `app/build.gradle.kts`. Gradle/Maven dependencies retain their embedded license metadata. The source archive includes the original Herdr and Termux notices and the sources described above. Keep corresponding source and applicable notices available when redistributing binaries.

The old TypeScript connection bridge and frontend bundles are not required to build 0.2.0. Their sources and license information remain available in the prior 0.1.x source releases and the original upstream archive.

## Inconsolata

Bundled terminal typeface, SIL Open Font License 1.1. See
`third_party/inconsolata/UPSTREAM.md` and `OFL.txt`; the license is also included
in APK assets as `Inconsolata-OFL.txt`.

## Native mathematics

- Markwon LaTeX extension and inline-parser **4.6.2**: Apache-2.0, matching the existing Markwon core. https://github.com/noties/Markwon/tree/v4.6.2
- JLaTeXMath Android **0.2.0**: GPL-2.0-or-later with the upstream linking exception. Unmodified corresponding source, license and provenance are in `third_party/jlatexmath/`; the license is also included in APK assets.

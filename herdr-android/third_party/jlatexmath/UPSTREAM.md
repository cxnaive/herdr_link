# JLaTeXMath Android 0.2.0

Unmodified native math renderer used by Markwon's LaTeX extension. It is needed
for offline formula layout and drawing to Android Canvas; no WebView, TeX
process or remote image service is used.

- Upstream: https://github.com/noties/jlatexmath-android/tree/v0.2.0
- Maven: `ru.noties:jlatexmath-android:0.2.0` (pinned by Markwon 4.6.2).
- Corresponding Java source is included as `jlatexmath-android-0.2.0-sources.jar`,
  downloaded from https://repo.maven.apache.org/maven2/ru/noties/jlatexmath-android/0.2.0/jlatexmath-android-0.2.0-sources.jar .
- License: GPL-2.0-or-later with the upstream independent-module linking
  exception; see `LICENSE` and individual source-file notices. `LICENSE` is
  also included in APK assets as `JLaTeXMath-LICENSE.txt`.

The app rejects macro definitions, resource-loading commands, overly long and
deeply nested inputs before passing them to the renderer. One background
executor serializes the library's shared font/parser state. Failures retain
source text. No changes were made to the library.

Source JAR SHA-256: `e3bccfd6fcc38b0fc85307278e5c9024fd6710311fa172790d2629467670b7cc`.

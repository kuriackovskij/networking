# IP-Beamer — Android client

Native Android app that sends authenticated beams to your IP-Beamer server on a
timer, keeping your phone's public IP on the firewall allow-list. Kotlin +
Jetpack Compose, `minSdk 24` (Android 7) → `targetSdk 35`, so it runs on the
Pixel 10 and virtually every Android phone since 2016.

## Features

- **One-screen setup:** server host, port, password, device label.
- **Status-bar icon:** an ongoing foreground-service notification shows a small
  icon up top (next to clock/VPN/wifi) whenever beaming is active.
- **Live status:** acknowledged / sent-no-ack / error, the allowed public IP,
  and the last/next beam times — in the app and in the notification.
- **Configurable timing:** beam interval (minutes, default 45) and ack wait.
- **Auto-start on boot** (toggle).
- **In-app logs** with a clear button.
- **Battery-optimisation exemption** button for reliable timing.
- **Encrypted password storage** (EncryptedSharedPreferences).
- **Exact alarms** (with graceful fallback) so beams fire on time even in Doze.
- Nice adaptive launcher icon, incl. a themed/monochrome variant for Android 13+.

The beam protocol (`protocol/Protocol.kt`) is byte-compatible with the Go server;
the PBKDF2-HMAC-SHA256 key derivation is implemented with `Mac` so it matches the
server exactly and works on all API levels.

## Build

Open the `android/` folder in **Android Studio** (Koala or newer) and press Run,
or from the command line with a JDK 17 and the Android SDK available:

```sh
cd android
./gradlew assembleDebug        # -> app/build/outputs/apk/debug/app-debug.apk
```

Or, from the repo root, `make android` builds and copies a versioned APK to
`dist/client/ipbeamer-<version>.apk`.

Set `sdk.dir` in `android/local.properties` (or the `ANDROID_HOME` env var) to
your SDK location. Requires SDK Platform 35 + Build-Tools 35.0.0.

## Install on the phone

```sh
adb install -r app/build/outputs/apk/debug/app-debug.apk
```

Or copy the APK to the phone and tap it (enable "install unknown apps"). Then:

1. Open **IP-Beamer**, enter server `host`, `port`, and the same `password` as
   the server config.
2. Tap **Start**. Grant the notification permission when asked.
3. (Recommended) tap **Ignore battery optimisation** so beams fire on schedule.
4. Leave **Start automatically on boot** on so it resumes after a reboot.

## Notes

- The status-bar notification is required by Android for a long-running service;
  it's what keeps the app allowed to beam in the background.
- For a store-signed / release build, configure a signing key and run
  `./gradlew assembleRelease`.

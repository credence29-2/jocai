#!/usr/bin/env bash
# Installs the APK on a running emulator, opens the app, taps through every
# tab, and fails if the app crashes. Prints the crash log when it does.
set -u
PKG=com.credence.cpc
APK=app/build/outputs/apk/release/app-release.apk
OUT=smoke
mkdir -p "$OUT"

alive() { [ -n "$(adb shell pidof "$PKG" | tr -d '\r')" ]; }

dump_crash() {
  echo "===== crash buffer ====="
  adb logcat -d -b crash
  echo "===== app log ====="
  adb logcat -d | grep -E "AndroidRuntime|$PKG|FATAL" | tail -200
}

tap_text() {
  adb shell uiautomator dump /sdcard/ui.xml >/dev/null 2>&1
  local b
  b=$(adb shell cat /sdcard/ui.xml | tr '>' '\n' | grep "text=\"$1\"" | head -1 | grep -o 'bounds="[^"]*"' | grep -o '[0-9]\+')
  if [ -z "$b" ]; then echo "could not find \"$1\" on screen"; return 1; fi
  set -- $b
  adb shell input tap $(( ($1 + $3) / 2 )) $(( ($2 + $4) / 2 ))
}

adb install -r "$APK" || exit 1
adb logcat -c
adb shell am start -W -n "$PKG/.MainActivity"
sleep 6
adb exec-out screencap -p > "$OUT/home.png"
if ! alive; then echo "CRASHED on launch"; dump_crash; exit 1; fi
echo "Home opened OK"

for tab in Junk Apps Updates Home; do
  tap_text "$tab" || { dump_crash; exit 1; }
  sleep 3
  adb exec-out screencap -p > "$OUT/$tab.png"
  if ! alive; then echo "CRASHED on $tab tab"; dump_crash; exit 1; fi
  echo "$tab tab OK"
done

# Rotate and come back, which recreates nothing but relayouts everything.
adb shell settings put system accelerometer_rotation 0
adb shell settings put system user_rotation 1; sleep 2
adb shell settings put system user_rotation 0; sleep 2
if ! alive; then echo "CRASHED on rotation"; dump_crash; exit 1; fi

# Background and resume, which re-runs every onShow.
adb shell input keyevent KEYCODE_HOME; sleep 2
adb shell am start -W -n "$PKG/.MainActivity"; sleep 3
if ! alive; then echo "CRASHED on resume"; dump_crash; exit 1; fi

echo "Smoke test passed"
dump_crash

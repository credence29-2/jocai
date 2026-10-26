#!/usr/bin/env bash
# Installs the APK on the emulator, opens it with sample accounts, and saves
# screenshots of the main screens. Fails if the app crashes or the mail
# engine doesn't start.
set -euo pipefail
cd "$(dirname "$0")/.."
PKG=com.credence.echomike
mkdir -p smoke

adb install -r apk/*.apk
adb shell pm grant $PKG android.permission.POST_NOTIFICATIONS || true
adb logcat -c

shot() { sleep "${2:-2}"; adb exec-out screencap -p > "smoke/$1.png"; }
size=$(adb shell wm size | sed 's/.*: //' | tr -d '\r')
W=${size%x*}
H=${size#*x}
tap() { adb shell input tap "$(( W * $1 / 100 ))" "$(( H * $2 / 100 ))"; }

adb shell am start -W -n $PKG/.MainActivity --ez demo true
shot 1-inbox 12

# Open the first message.
tap 50 16
shot 2-message 4

# Reply from the bottom reply bar's alias-aware Reply button: scroll to it.
adb shell input swipe "$(( W / 2 ))" "$(( H * 70 / 100 ))" "$(( W / 2 ))" "$(( H * 30 / 100 ))" 300
shot 3-message-bottom 2

# Back to the list, then the menu.
adb shell input keyevent KEYCODE_BACK
shot 4-back-to-list 2
tap 5 4
shot 5-menu 2
adb shell input keyevent KEYCODE_BACK

# The app must still be running and the engine must have started.
if ! adb shell pidof $PKG >/dev/null; then
  echo "Project Echo Mike isn't running"
  adb logcat -d | tail -200
  exit 1
fi
if adb logcat -d | grep -E "FATAL EXCEPTION|couldn't start" ; then
  adb logcat -d | tail -200
  exit 1
fi
echo "Smoke test passed"

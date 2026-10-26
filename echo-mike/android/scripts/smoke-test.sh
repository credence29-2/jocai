#!/usr/bin/env bash
# Installs the APK on the emulator, opens it with sample accounts, and checks
# the inbox, a message and the menu show up. Prints what's on screen at each
# step, and saves screenshots.
set -uo pipefail
cd "$(dirname "$0")/.."
PKG=com.credence.echomike
mkdir -p smoke
fail=0

adb install -r apk/*.apk
adb shell pm grant $PKG android.permission.POST_NOTIFICATIONS || true
adb logcat -c

size=$(adb shell wm size | sed 's/.*: //' | tr -d '\r')
W=${size%x*}
H=${size#*x}
tap() { adb shell input tap "$(( W * $1 / 100 ))" "$(( H * $2 / 100 ))"; }

# screen NAME: screenshot, and print the visible text (WebView text included).
screen() {
  adb exec-out screencap -p > "smoke/$1.png"
  adb shell uiautomator dump /sdcard/ui.xml >/dev/null 2>&1
  adb shell cat /sdcard/ui.xml | grep -o 'text="[^"]\+"' | sed 's/text="\(.*\)"/\1/' > "smoke/$1.txt"
  echo "---- $1 ----"
  head -40 "smoke/$1.txt"
}
expect() {
  if grep -qF "$2" "smoke/$1.txt"; then echo "OK: $1 shows \"$2\""
  else echo "FAIL: $1 doesn't show \"$2\""; fail=1; fi
}

adb shell am start -W -n $PKG/.MainActivity --ez demo true
sleep 12
screen 1-inbox
expect 1-inbox "Maria Lopez"
expect 1-inbox "Terminal 3 won't print kitchen tickets"

# Open the newest message (Maria's).
adb shell input tap "$(( W / 2 ))" "$(( H * 17 / 100 ))"
sleep 4
screen 2-message
expect 2-message "Can someone call me before the lunch rush?"

adb shell input keyevent KEYCODE_BACK
sleep 2
screen 3-back-to-list
expect 3-back-to-list "Sunday dinner"

# Open the menu with the button at the top left.
adb shell input tap 70 "$(( H * 6 / 100 ))"
sleep 2
screen 4-menu
expect 4-menu "Notifications on"

echo "---- app log ----"
pid=$(adb shell pidof $PKG | tr -d '\r')
if [ -z "$pid" ]; then
  echo "FAIL: Project Echo Mike isn't running"
  fail=1
else
  adb logcat -d --pid="$pid" | grep -v " V \| D " | tail -60
fi
if adb logcat -d -b crash | grep -A20 "Process: $PKG"; then
  echo "FAIL: the app crashed"
  fail=1
fi

[ $fail = 0 ] && echo "Smoke test passed"
exit $fail

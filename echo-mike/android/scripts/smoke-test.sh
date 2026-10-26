#!/usr/bin/env bash
# Installs the APK on the emulator, opens it with sample accounts, and checks
# the inbox, a message and the menu show up. Waits for each screen and taps
# on-screen text, so it works on slow emulators and any screen size. Prints
# what's on screen at each step, and saves screenshots.
set -uo pipefail
cd "$(dirname "$0")/.."
PKG=com.credence.echomike
mkdir -p smoke
fail=0

adb install -r apk/*.apk
adb shell pm grant $PKG android.permission.POST_NOTIFICATIONS || true
adb logcat -c

dump() {
  adb shell uiautomator dump /sdcard/ui.xml >/dev/null 2>&1
  adb shell cat /sdcard/ui.xml > smoke/ui.xml 2>/dev/null
}
# wait_for TEXT [seconds]: wait until TEXT is on screen.
wait_for() {
  local end=$(( SECONDS + ${2:-60} ))
  while [ $SECONDS -lt $end ]; do
    dump
    grep -qF "\"$1\"" smoke/ui.xml && return 0
    sleep 2
  done
  return 1
}
# tap_on TEXT: tap the middle of the element whose text or label is TEXT.
tap_on() {
  dump
  local b
  b=$(grep -o "<node [^>]*\(text\|content-desc\)=\"$1\"[^>]*>" smoke/ui.xml | head -1 | grep -o 'bounds="[^"]*"')
  if [ -z "$b" ]; then echo "can't find \"$1\" to tap"; return 1; fi
  local n
  n=$(echo "$b" | grep -o '[0-9]\+' | tr '\n' ' ')
  set -- $n
  adb shell input tap $(( ($1 + $3) / 2 )) $(( ($2 + $4) / 2 ))
}
# screen NAME: screenshot and print the visible text.
screen() {
  dump
  adb exec-out screencap -p > "smoke/$1.png"
  grep -o 'text="[^"]\+"' smoke/ui.xml | sed 's/text="\(.*\)"/\1/' > "smoke/$1.txt"
  echo "---- $1 ----"
  head -30 "smoke/$1.txt"
}
check() { # check STEP TEXT [seconds]
  if wait_for "$2" "${3:-30}"; then echo "OK: $1 shows \"$2\""
  else echo "FAIL: $1 doesn't show \"$2\""; fail=1; fi
}

adb shell am start -W -n $PKG/.MainActivity --ez demo true

check inbox "Maria Lopez" 90
screen 1-inbox

tap_on "Maria Lopez"
check message "Forward"
sleep 3
screen 2-message

adb shell input keyevent KEYCODE_BACK
check back-to-list "Priya Nair"
screen 3-back-to-list

tap_on "Menu"
check menu "Notifications on"
screen 4-menu
adb shell input keyevent KEYCODE_BACK

tap_on "Compose"
check compose "Subject"
screen 5-compose
adb shell input keyevent KEYCODE_BACK

echo "---- app log ----"
pid=$(adb shell pidof $PKG | tr -d '\r')
if [ -z "$pid" ]; then
  echo "FAIL: Project Echo Mike isn't running"
  fail=1
else
  adb logcat -d --pid="$pid" | grep -v " V \| D \|glBindAttrib" | tail -40
fi
if adb logcat -d -b crash | grep -A20 "Process: $PKG"; then
  echo "FAIL: the app crashed"
  fail=1
fi

[ $fail = 0 ] && echo "Smoke test passed"
exit $fail

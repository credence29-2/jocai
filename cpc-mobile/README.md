# Kempt Mobile

The Android side of Kempt. A small cleaner that only does things that actually work on modern Android, and says so when something isn't possible.

## What it does

**Home.** Storage and memory at a glance, plus whether the security patch is current. Quick buttons for a junk scan and clearing app caches.

**Junk.** Scans shared storage (Download, DCIM, Pictures and so on) and groups what it finds. Nothing is deleted until you confirm.

| Group | Ticked by default |
| --- | --- |
| Gallery trash (already deleted photos waiting in the trash) | Yes |
| Thumbnail cache | Yes |
| Installer files (APK) | Yes |
| Log and temp files | Yes |
| Empty folders (standard folders like Download are never removed) | Yes |
| Old downloads, older than 90 days | No, review first |
| Large files over 100 MB | No, review first |

Tap a group to pick individual files. App data under `Android/` is never touched.

**Apps.** Every app with its cache size, total size and when you last opened it. Sort by cache, total size, or "not used" to find apps worth uninstalling. **Clear all app caches** uses Android's own system dialog to clear every app's cache in one step (Android 11+). Tapping an app opens its App info page or the uninstall prompt.

**Updates.** Android version, security patch date and age, a button to the system update screen, and a shortcut to Play Store for app updates.

## What Android doesn't allow, and why Kempt doesn't fake it

- **Clearing one app's cache from another app.** Android removed this in version 6. Kempt uses the system's clear-all dialog instead, or sends you to App info for a single app.
- **"RAM boosting."** Android 14 and newer won't let an app close other apps at all. On older versions, closed apps just restart. Android manages memory well on its own, so Kempt shows memory use but doesn't pretend to boost it.
- **Installing app updates.** Only Play Store or the phone's own updater can do that. Kempt takes you straight there.

## Permissions

Kempt asks for two special permissions, each only when you use the feature that needs it.

- **All files access.** Needed to scan shared storage and for the clear-all-caches dialog.
- **Usage access.** Needed to read app cache sizes and last used dates.

There's no internet permission at all, so nothing can leave the phone.

## Installing

1. Download the APK from the latest **Kempt Mobile build** run under the repo's Actions tab. It's in the **Kempt-Mobile** artifact, zipped.
2. Copy it to the phone, or download it there, and open it.
3. Allow "Install unknown apps" for your browser or file manager when Android asks.

Every build installs over the previous one because they share the same signing key.

Android 8.0 or newer is required.

## Building

Open the `cpc-mobile` folder in Android Studio, or run:

```
./gradlew assembleRelease
```

The APK lands in `app/build/outputs/apk/release/`. Unit tests run with `./gradlew testReleaseUnitTest`.

The UI is written in code with plain Android views and no extra libraries, so the APK is tiny and there's nothing to keep updated.

## About the signing key

`app/cpc-sideload.jks` is checked in so every build can update the last one without uninstalling. That's fine for sideloading to your own or your team's phones. Anyone with access to this repo could sign an "update" with it, so keep the repo private. Before publishing on Google Play, create a new key, keep it out of the repo, and set `CPC_KEYSTORE_PASSWORD`. Play also restricts All files access, so the junk scanner would likely need to change for a Play release.

## Layout

| Path | What's in it |
| --- | --- |
| `core/JunkRules.kt` | What counts as junk. Change thresholds and categories here |
| `core/JunkScanner.kt` | Storage walk and delete, with safety checks |
| `core/AppScanner.kt` | App sizes and last used dates |
| `core/DeviceInfo.kt` | Storage, memory and patch level |
| `ui/*Screen.kt` | One file per tab |
| `MainActivity.kt` | Navigation, permissions and the clear-all-caches request |

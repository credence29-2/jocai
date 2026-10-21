package com.credence.cpc.ui

import android.content.Intent
import android.net.Uri
import android.provider.Settings
import android.view.View
import android.widget.LinearLayout
import com.credence.cpc.MainActivity
import com.credence.cpc.core.DeviceInfo
import com.credence.cpc.core.Format

class UpdatesScreen(act: MainActivity) : Screen(act) {
    private val body: LinearLayout = ui.column(16)
    override val root: View = ui.scroll(body)

    override fun onShow() {
        val d = DeviceInfo.read(act)
        val cpcVersion = try {
            act.packageManager.getPackageInfo(act.packageName, 0).versionName ?: ""
        } catch (e: Exception) {
            ""
        }
        body.removeAllViews()
        body.addView(ui.title("Updates"))
        body.addView(View(act), ui.spaced(8))

        body.addView(
            ui.card(
                ui.heading("This phone"),
                ui.body(d.model),
                ui.caption("Android ${d.androidVersion}. CPC Mobile $cpcVersion."),
            ),
        )

        val age = d.patchAgeDays()
        val upToDate = age != null && age <= DeviceInfo.PATCH_WARN_DAYS
        body.addView(
            ui.card(
                ui.heading("System and security updates"),
                ui.body("Security patch: ${d.patchText()}"),
                ui.status(
                    when {
                        age == null -> "Patch date unknown"
                        upToDate -> "Up to date (${Format.daysAgo(age)})"
                        else -> "Out of date, last patched ${Format.daysAgo(age)}"
                    },
                    upToDate,
                ),
                ui.caption(
                    "Phones should get a security patch at least every 3 months. If yours is older and no update shows up, " +
                        "the manufacturer may have stopped supporting it.",
                ),
                ui.primary("Check for system update") { openSystemUpdate() },
                ui.caption("If this opens the main Settings page, look for Software update or System update."),
            ),
        )

        body.addView(
            ui.card(
                ui.heading("App updates"),
                ui.body("Google Play handles app updates. Android doesn't let other apps install them."),
                ui.caption("In Play Store, tap your profile picture, then Manage apps & device, then Update all."),
                ui.primary("Open Play Store") { openPlayStore() },
            ),
        )
    }

    private fun openSystemUpdate() {
        act.open(
            Intent("android.settings.SYSTEM_UPDATE_SETTINGS"),
            Intent(Settings.ACTION_DEVICE_INFO_SETTINGS),
            Intent(Settings.ACTION_SETTINGS),
        )
    }

    private fun openPlayStore() {
        val launch = act.packageManager.getLaunchIntentForPackage("com.android.vending")
        val web = Intent(Intent.ACTION_VIEW, Uri.parse("https://play.google.com/store/apps"))
        if (launch != null) act.open(launch, web) else act.open(web)
    }
}

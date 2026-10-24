package com.credence.cpc.ui

import android.view.View
import android.widget.LinearLayout
import com.credence.cpc.MainActivity
import com.credence.cpc.core.DeviceInfo
import com.credence.cpc.core.Format

class HomeScreen(act: MainActivity) : Screen(act) {
    private val body: LinearLayout = ui.column(16)
    override val root: View = ui.scroll(body)

    override fun onShow() {
        val d = DeviceInfo.read(act)
        body.removeAllViews()
        body.addView(ui.title("Kempt Mobile"))
        body.addView(ui.caption("Kempt for your phone"), ui.spaced(2))
        body.addView(View(act), ui.spaced(8))

        val storageFrac = if (d.storageTotal > 0) d.storageUsed.toDouble() / d.storageTotal else 0.0
        body.addView(
            ui.card(
                ui.heading("Storage"),
                ui.body("${Format.bytes(d.storageUsed)} used of ${Format.bytes(d.storageTotal)}"),
                ui.bar(storageFrac, warn = storageFrac > 0.9),
                ui.caption("${Format.bytes(d.storageFree)} free" + if (storageFrac > 0.9) ". Running low, a cleanup will help." else ""),
            ),
        )

        val memFrac = if (d.memTotal > 0) d.memUsed.toDouble() / d.memTotal else 0.0
        body.addView(
            ui.card(
                ui.heading("Memory"),
                ui.body("${Format.bytes(d.memUsed)} in use of ${Format.bytes(d.memTotal)}"),
                ui.bar(memFrac),
                ui.caption(
                    "Android manages memory on its own and keeps recent apps ready to reopen. " +
                        "Closing them doesn't speed the phone up, so Kempt doesn't pretend to \"boost\" RAM.",
                ),
            ),
        )

        val age = d.patchAgeDays()
        val upToDate = age != null && age <= DeviceInfo.PATCH_WARN_DAYS
        val statusText = when {
            age == null -> "Patch date unknown"
            upToDate -> "Up to date (${Format.daysAgo(age)})"
            else -> "Out of date, last patched ${Format.daysAgo(age)}"
        }
        body.addView(
            ui.card(
                ui.heading("Security updates"),
                ui.body("Patch level: ${d.patchText()}"),
                ui.status(statusText, upToDate),
                ui.secondary("See update options") { act.show(3) },
            ),
        )

        body.addView(ui.primary("Scan for junk files") { act.show(1); act.junk.startScanIfReady() }, ui.spaced(4))
        body.addView(ui.primary("Clear all app caches") { act.clearAllAppCaches() }, ui.spaced(10))
        body.addView(ui.secondary("See which apps use the most space") { act.show(2) }, ui.spaced(10))
    }
}

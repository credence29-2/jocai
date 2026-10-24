package com.credence.cpc.ui

import android.app.AlertDialog
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.provider.Settings
import android.view.View
import android.view.ViewGroup
import android.widget.BaseAdapter
import android.widget.ImageView
import android.widget.LinearLayout
import android.widget.ListView
import android.widget.TextView
import com.credence.cpc.MainActivity
import com.credence.cpc.core.Access
import com.credence.cpc.core.AppEntry
import com.credence.cpc.core.AppScanner
import com.credence.cpc.core.Format

class AppsScreen(act: MainActivity) : Screen(act) {
    private enum class Sort(val label: String) { CACHE("Cache"), SIZE("Total size"), UNUSED("Not used") }

    private val header: LinearLayout = ui.column(16)
    private val list = ListView(act).apply {
        divider = null
        dividerHeight = 0
        addHeaderView(header, null, false)
        setOnItemClickListener { _, _, position, _ ->
            val i = position - headerViewsCount
            if (i in shown.indices) actions(shown[i])
        }
    }
    override val root: View = list

    private var apps: List<AppEntry>? = null
    private var shown: List<AppEntry> = emptyList()
    private var sort = Sort.CACHE
    private var loading = false
    private var loadedAt = 0L

    private val adapter = object : BaseAdapter() {
        override fun getCount() = shown.size
        override fun getItem(position: Int) = shown[position]
        override fun getItemId(position: Int) = position.toLong()
        override fun getView(position: Int, convertView: View?, parent: ViewGroup?): View {
            val row = (convertView as? AppRow) ?: AppRow()
            row.bind(shown[position])
            return row
        }
    }

    init {
        list.adapter = adapter
    }

    /** Forces a reload next time the tab is shown, e.g. after clearing caches. */
    fun invalidate() {
        loadedAt = 0
    }

    override fun onShow() {
        renderHeader()
        if (!Access.hasUsage(act)) return
        // Reload when stale, so sizes are fresh after visiting App info.
        if (!loading && System.currentTimeMillis() - loadedAt > 5_000) load()
    }

    private fun load() {
        loading = true
        renderHeader()
        act.background({ AppScanner.load(act) }) { r ->
            loading = false
            r.onSuccess {
                apps = it
                loadedAt = System.currentTimeMillis()
            }.onFailure { act.toast("Couldn't read app sizes: ${it.message}") }
            applySort()
            renderHeader()
        }
    }

    private fun applySort() {
        val all = apps ?: emptyList()
        shown = when (sort) {
            Sort.CACHE -> all.sortedByDescending { it.cacheBytes }
            Sort.SIZE -> all.sortedByDescending { it.totalBytes }
            Sort.UNUSED -> all.filter { !it.isSystem }.sortedBy { it.lastUsedMs }
        }
        adapter.notifyDataSetChanged()
    }

    private fun renderHeader() {
        header.removeAllViews()
        header.addView(ui.title("Apps"))
        header.addView(ui.caption("See which apps hold the most space and clean them up."), ui.spaced(2))
        header.addView(View(act), ui.spaced(8))

        if (!Access.hasUsage(act)) {
            shown = emptyList()
            adapter.notifyDataSetChanged()
            header.addView(
                ui.card(
                    ui.heading("Allow usage access"),
                    ui.body("To show how much space each app uses and when you last opened it, Android needs you to turn on \"Usage access\" for Kempt. Nothing leaves your phone."),
                    ui.primary("Allow usage access") { act.requestUsageAccess() },
                ),
            )
            header.addView(cacheCard(null))
            return
        }

        header.addView(cacheCard(apps?.sumOf { it.cacheBytes }))

        val sorts = ui.row()
        Sort.values().forEachIndexed { i, s ->
            val b = if (s == sort) ui.primary(s.label) {} else ui.secondary(s.label) { sort = s; applySort(); renderHeader() }
            sorts.addView(b, LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f).apply {
                if (i > 0) leftMargin = ui.dp(8)
            })
        }
        header.addView(sorts, ui.spaced(4))

        val note = when {
            loading -> "Reading app sizes..."
            sort == Sort.UNUSED -> "Apps you haven't opened in a while are good candidates to uninstall. Tap an app for options."
            else -> "Tap an app to clear its cache or uninstall it."
        }
        header.addView(ui.caption(note), ui.spaced(10))
        if (loading) header.addView(ui.spinner(), ui.spaced(6))
    }

    private fun cacheCard(total: Long?): View {
        val text = when {
            total == null -> "Clear the cache of every app in one step. Apps stay signed in and keep their data, they just rebuild temporary files."
            else -> "Apps are holding ${Format.bytes(total)} of cache. Clearing it is safe. Apps stay signed in and rebuild what they need."
        }
        val views = mutableListOf<View>(ui.heading("App cache"), ui.body(text))
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
            views += ui.primary("Clear all app caches") { act.clearAllAppCaches() }
        } else {
            views += ui.caption("Clearing all caches at once needs Android 11 or newer. Tap the biggest apps below to clear them one at a time.")
        }
        return ui.card(*views.toTypedArray())
    }

    private fun details(a: AppEntry): String {
        val used = if (a.lastUsedMs > 0) {
            "Opened " + Format.daysAgo((System.currentTimeMillis() - a.lastUsedMs) / 86_400_000L)
        } else {
            "Not opened in the last year"
        }
        return "Cache ${Format.bytes(a.cacheBytes)}, total ${Format.bytes(a.totalBytes)}. $used."
    }

    private fun actions(a: AppEntry) {
        val b = AlertDialog.Builder(act)
            .setTitle(a.label)
            .setMessage(details(a) + "\n\nTo clear this app's cache, open App info, tap Storage, then Clear cache.")
            .setPositiveButton("Open App info") { _, _ ->
                act.open(Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, Uri.parse("package:" + a.packageName)))
            }
            .setNegativeButton("Close", null)
        if (!a.isSystem) {
            b.setNeutralButton("Uninstall") { _, _ ->
                act.open(Intent(Intent.ACTION_DELETE, Uri.parse("package:" + a.packageName)))
            }
        }
        b.show()
    }

    private inner class AppRow : LinearLayout(act) {
        private val icon = ImageView(act)
        private val name: TextView = ui.body("")
        private val info: TextView = ui.caption("")

        init {
            orientation = HORIZONTAL
            gravity = android.view.Gravity.CENTER_VERTICAL
            setPadding(ui.dp(16), ui.dp(10), ui.dp(16), ui.dp(10))
            addView(icon, LayoutParams(ui.dp(40), ui.dp(40)).apply { rightMargin = ui.dp(14) })
            val texts = ui.column()
            texts.addView(name)
            texts.addView(info)
            addView(texts, LayoutParams(0, LayoutParams.WRAP_CONTENT, 1f))
        }

        fun bind(a: AppEntry) {
            icon.setImageDrawable(a.icon)
            name.text = a.label
            info.text = details(a)
        }
    }
}

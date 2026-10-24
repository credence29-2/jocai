package com.credence.cpc

import android.app.Activity
import android.app.AlertDialog
import android.content.ActivityNotFoundException
import android.content.Intent
import android.graphics.Color
import android.os.Build
import android.os.Bundle
import android.os.Environment
import android.os.StatFs
import android.os.storage.StorageManager
import android.util.TypedValue
import android.view.Gravity
import android.view.View
import android.view.ViewGroup
import android.view.WindowInsets
import android.view.WindowInsetsController
import android.widget.FrameLayout
import android.widget.LinearLayout
import android.widget.TextView
import android.widget.Toast
import com.credence.cpc.core.Access
import com.credence.cpc.core.Format
import com.credence.cpc.ui.AppsScreen
import com.credence.cpc.ui.HomeScreen
import com.credence.cpc.ui.JunkScreen
import com.credence.cpc.ui.Palette
import com.credence.cpc.ui.Screen
import com.credence.cpc.ui.Ui
import com.credence.cpc.ui.UpdatesScreen
import java.util.concurrent.ExecutorService
import java.util.concurrent.Executors

class MainActivity : Activity() {

    lateinit var ui: Ui
        private set
    private val pool: ExecutorService = Executors.newFixedThreadPool(2)

    private lateinit var content: FrameLayout
    private lateinit var tabs: List<Pair<String, Screen>>
    private val tabViews = mutableListOf<TextView>()
    private var current = 0
    private var freeBeforeCacheClear = 0L

    lateinit var junk: JunkScreen
        private set
    lateinit var apps: AppsScreen
        private set

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        ui = Ui(this, Palette.of(this))
        val p = ui.p

        junk = JunkScreen(this)
        apps = AppsScreen(this)
        tabs = listOf(
            "Home" to HomeScreen(this),
            "Junk" to junk,
            "Apps" to apps,
            "Updates" to UpdatesScreen(this),
        )

        val root = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setBackgroundColor(p.bg)
        }
        content = FrameLayout(this)
        root.addView(content, LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, 0, 1f))

        val nav = LinearLayout(this).apply {
            orientation = LinearLayout.HORIZONTAL
            setBackgroundColor(p.nav)
            elevation = ui.dp(8).toFloat()
        }
        tabs.forEachIndexed { i, (name, _) ->
            val tv = TextView(this).apply {
                text = name
                gravity = Gravity.CENTER
                setTextSize(TypedValue.COMPLEX_UNIT_SP, 14f)
                setPadding(0, ui.dp(14), 0, ui.dp(14))
                setOnClickListener { show(i) }
            }
            tabViews += tv
            nav.addView(tv, LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1f))
        }
        root.addView(nav)

        // Pad content for the status bar and the nav row for the gesture bar.
        root.setOnApplyWindowInsetsListener { _, insets ->
            val (top, bottom) = systemBars(insets)
            content.setPadding(0, top, 0, 0)
            nav.setPadding(0, 0, 0, bottom)
            insets
        }
        setContentView(root)
        // Must run after setContentView: the window's decor view (and its
        // insets controller) doesn't exist before that and Android crashes.
        goEdgeToEdge(p.night)
        show(savedInstanceState?.getInt("tab") ?: 0)
    }

    private fun systemBars(insets: WindowInsets): Pair<Int, Int> =
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
            val b = insets.getInsets(WindowInsets.Type.systemBars())
            b.top to b.bottom
        } else {
            @Suppress("DEPRECATION")
            insets.systemWindowInsetTop to insets.systemWindowInsetBottom
        }

    @Suppress("DEPRECATION")
    private fun goEdgeToEdge(night: Boolean) {
        window.statusBarColor = Color.TRANSPARENT
        window.navigationBarColor = Color.TRANSPARENT
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
            window.setDecorFitsSystemWindows(false)
            val light = WindowInsetsController.APPEARANCE_LIGHT_STATUS_BARS or WindowInsetsController.APPEARANCE_LIGHT_NAVIGATION_BARS
            window.insetsController?.setSystemBarsAppearance(if (night) 0 else light, light)
        } else {
            var flags = View.SYSTEM_UI_FLAG_LAYOUT_STABLE or View.SYSTEM_UI_FLAG_LAYOUT_FULLSCREEN or
                View.SYSTEM_UI_FLAG_LAYOUT_HIDE_NAVIGATION
            if (!night) {
                flags = flags or View.SYSTEM_UI_FLAG_LIGHT_STATUS_BAR
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O_MR1) flags = flags or View.SYSTEM_UI_FLAG_LIGHT_NAVIGATION_BAR
            }
            window.decorView.systemUiVisibility = flags
        }
    }

    fun show(index: Int) {
        current = index
        val screen = tabs[index].second
        content.removeAllViews()
        (screen.root.parent as? ViewGroup)?.removeView(screen.root)
        content.addView(screen.root)
        tabViews.forEachIndexed { i, tv ->
            tv.setTextColor(if (i == index) ui.p.accent else ui.p.subtext)
            tv.typeface = if (i == index) android.graphics.Typeface.DEFAULT_BOLD else android.graphics.Typeface.DEFAULT
        }
        screen.onShow()
    }

    override fun onResume() {
        super.onResume()
        // Permissions may have changed in Settings while we were away.
        if (::tabs.isInitialized) tabs[current].second.onShow()
    }

    override fun onSaveInstanceState(outState: Bundle) {
        super.onSaveInstanceState(outState)
        outState.putInt("tab", current)
    }

    override fun onDestroy() {
        pool.shutdownNow()
        super.onDestroy()
    }

    /** Runs [work] off the main thread, then [done] on it if the screen is still alive. */
    fun <T> background(work: () -> T, done: (Result<T>) -> Unit) {
        pool.execute {
            val r = runCatching(work)
            runOnUiThread { if (!isDestroyed) done(r) }
        }
    }

    fun toast(msg: String) = Toast.makeText(this, msg, Toast.LENGTH_LONG).show()

    fun open(vararg intents: Intent): Boolean {
        for (i in intents) {
            try {
                startActivity(i)
                return true
            } catch (e: ActivityNotFoundException) {
                // try the next one
            } catch (e: SecurityException) {
                // try the next one
            }
        }
        toast("Couldn't open that screen on this phone.")
        return false
    }

    /** Ask for file access the right way for this Android version. */
    fun requestFileAccess() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
            toast("Turn on \"Allow access to manage all files\" for Kempt, then come back.")
            open(Access.filesSettingsIntent(this), Access.filesSettingsFallback())
        } else {
            requestPermissions(Access.legacyStoragePermissions, REQ_STORAGE)
        }
    }

    fun requestUsageAccess() {
        toast("Find Kempt Mobile in the list and turn on usage access, then come back.")
        open(Access.usageSettingsIntent())
    }

    override fun onRequestPermissionsResult(requestCode: Int, permissions: Array<out String>, grantResults: IntArray) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults)
        tabs[current].second.onShow()
    }

    /**
     * Android 11+ has a system dialog that clears the cache of every app at
     * once. It needs All files access, which Kempt already uses for junk files.
     */
    fun clearAllAppCaches() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.R) {
            AlertDialog.Builder(this)
                .setTitle("Not available on this Android version")
                .setMessage("Clearing every app's cache at once needs Android 11 or newer. Use the Apps tab to clear the biggest ones one at a time.")
                .setPositiveButton("Open Apps tab") { _, _ -> show(2) }
                .setNegativeButton("Close", null)
                .show()
            return
        }
        if (!Access.hasFiles(this)) {
            AlertDialog.Builder(this)
                .setTitle("One permission first")
                .setMessage("Android only lets apps with \"All files access\" clear every app's cache. Kempt uses the same permission to find junk files. Nothing leaves your phone.")
                .setPositiveButton("Allow") { _, _ -> requestFileAccess() }
                .setNegativeButton("Not now", null)
                .show()
            return
        }
        freeBeforeCacheClear = freeBytes()
        try {
            @Suppress("DEPRECATION")
            startActivityForResult(Intent(StorageManager.ACTION_CLEAR_APP_CACHE), REQ_CLEAR_CACHE)
        } catch (e: Exception) {
            toast("This phone doesn't offer the clear-all-caches screen. Use the Apps tab instead.")
        }
    }

    private fun freeBytes(): Long = StatFs(Environment.getDataDirectory().path).availableBytes

    @Deprecated("Activity result API without AndroidX")
    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        @Suppress("DEPRECATION")
        super.onActivityResult(requestCode, resultCode, data)
        if (requestCode != REQ_CLEAR_CACHE) return
        if (resultCode == RESULT_OK) {
            val freed = (freeBytes() - freeBeforeCacheClear).coerceAtLeast(0)
            toast(if (freed > 0) "App caches cleared. Freed about ${Format.bytes(freed)}." else "App caches cleared.")
            apps.invalidate()
        }
        tabs[current].second.onShow()
    }

    companion object {
        const val REQ_STORAGE = 10
        const val REQ_CLEAR_CACHE = 11
    }
}

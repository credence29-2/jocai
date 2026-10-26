package com.credence.echomike

import android.Manifest
import android.app.Activity
import android.content.ContentValues
import android.content.Intent
import android.content.pm.PackageManager
import android.graphics.Color
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.provider.MediaStore
import android.view.Gravity
import android.view.View
import android.view.WindowInsets
import android.webkit.URLUtil
import android.webkit.ValueCallback
import android.webkit.WebChromeClient
import android.webkit.WebResourceRequest
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.FrameLayout
import android.widget.TextView
import android.widget.Toast
import java.net.HttpURLConnection
import java.net.URL
import mobile.Mobile

/**
 * One full-screen WebView showing the same interface as the Windows app,
 * served by the Go engine on the phone's loopback address.
 */
class MainActivity : Activity() {

    private lateinit var root: FrameLayout
    private lateinit var web: WebView
    private var origin = ""
    private var fileCallback: ValueCallback<Array<Uri>>? = null

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        Echo.init(this)
        Echo.attach(this)

        root = FrameLayout(this).apply { setBackgroundColor(getColor(R.color.bg)) }
        web = WebView(this).apply {
            setBackgroundColor(getColor(R.color.bg))
            settings.javaScriptEnabled = true
            settings.domStorageEnabled = true
            settings.setSupportMultipleWindows(false)
            settings.allowFileAccess = false
            webViewClient = object : WebViewClient() {
                override fun shouldOverrideUrlLoading(view: WebView, req: WebResourceRequest): Boolean {
                    val u = req.url.toString()
                    if (origin.isNotEmpty() && u.startsWith(origin)) return false
                    Echo.openURL(u)
                    return true
                }
            }
            webChromeClient = object : WebChromeClient() {
                override fun onShowFileChooser(view: WebView, cb: ValueCallback<Array<Uri>>, params: FileChooserParams): Boolean {
                    fileCallback?.onReceiveValue(null)
                    fileCallback = cb
                    return try {
                        startActivityForResult(params.createIntent().putExtra(Intent.EXTRA_ALLOW_MULTIPLE, true), REQ_FILES)
                        true
                    } catch (e: Exception) {
                        fileCallback = null
                        false
                    }
                }
            }
            setDownloadListener { url, _, disposition, mime, _ -> saveDownload(url, disposition, mime) }
        }
        root.addView(web, FrameLayout.LayoutParams(-1, -1))
        setContentView(root)
        edgeToEdge()

        val demo = intent.getBooleanExtra("demo", false)
        val dir = Echo.dataDir(this)
        Thread {
            try {
                val u = Mobile.start(dir, demo, Echo)
                origin = u.substringBefore('#').trimEnd('/')
                runOnUiThread { web.loadUrl(u) }
            } catch (e: Exception) {
                runOnUiThread { showError(e.message ?: e.toString()) }
            }
        }.start()

        askForNotifications()
        MailCheckJob.schedule(this)
    }

    /** Draw behind the system bars and pad by them, and by the keyboard. */
    private fun edgeToEdge() {
        window.statusBarColor = Color.TRANSPARENT
        window.navigationBarColor = Color.TRANSPARENT
        if (Build.VERSION.SDK_INT >= 30) {
            window.setDecorFitsSystemWindows(false)
        } else {
            @Suppress("DEPRECATION")
            root.systemUiVisibility = View.SYSTEM_UI_FLAG_LAYOUT_STABLE or
                View.SYSTEM_UI_FLAG_LAYOUT_FULLSCREEN or View.SYSTEM_UI_FLAG_LAYOUT_HIDE_NAVIGATION
        }
        root.setOnApplyWindowInsetsListener { v, insets ->
            if (Build.VERSION.SDK_INT >= 30) {
                val bars = insets.getInsets(WindowInsets.Type.systemBars() or WindowInsets.Type.displayCutout())
                val ime = insets.getInsets(WindowInsets.Type.ime())
                v.setPadding(bars.left, bars.top, bars.right, maxOf(bars.bottom, ime.bottom))
            } else {
                @Suppress("DEPRECATION")
                v.setPadding(insets.systemWindowInsetLeft, insets.systemWindowInsetTop,
                    insets.systemWindowInsetRight, insets.systemWindowInsetBottom)
            }
            insets
        }
    }

    private fun showError(msg: String) {
        root.removeAllViews()
        root.addView(TextView(this).apply {
            text = "Project Echo Mike couldn't start:\n\n$msg"
            setTextColor(Color.WHITE)
            textSize = 16f
            gravity = Gravity.CENTER
            setPadding(48, 48, 48, 48)
        }, FrameLayout.LayoutParams(-1, -1))
    }

    private fun askForNotifications() {
        if (Build.VERSION.SDK_INT >= 33 &&
            checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED
        ) {
            requestPermissions(arrayOf(Manifest.permission.POST_NOTIFICATIONS), REQ_NOTIFY)
        }
    }

    /** Saves an attachment to Downloads and opens it. */
    private fun saveDownload(url: String, disposition: String?, mime: String?) {
        val name = Uri.parse(url).getQueryParameter("name")?.takeIf { it.isNotBlank() }
            ?: URLUtil.guessFileName(url, disposition, mime)
        Toast.makeText(this, "Saving $name", Toast.LENGTH_SHORT).show()
        Thread {
            try {
                val conn = URL(url).openConnection() as HttpURLConnection
                val type = Uri.parse(url).getQueryParameter("type")?.takeIf { it.isNotBlank() } ?: "application/octet-stream"
                val values = ContentValues().apply {
                    put(MediaStore.Downloads.DISPLAY_NAME, name)
                    put(MediaStore.Downloads.MIME_TYPE, type)
                }
                val dest = contentResolver.insert(MediaStore.Downloads.EXTERNAL_CONTENT_URI, values)
                    ?: throw IllegalStateException("Downloads isn't available")
                conn.inputStream.use { input ->
                    contentResolver.openOutputStream(dest)!!.use { input.copyTo(it) }
                }
                runOnUiThread {
                    Toast.makeText(this, "Saved $name to Downloads", Toast.LENGTH_SHORT).show()
                    try {
                        startActivity(Intent(Intent.ACTION_VIEW).setDataAndType(dest, type)
                            .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION))
                    } catch (_: Exception) {
                        // Nothing installed opens this type; it's still in Downloads.
                    }
                }
            } catch (e: Exception) {
                runOnUiThread { Toast.makeText(this, "Couldn't save $name: ${e.message}", Toast.LENGTH_LONG).show() }
            }
        }.start()
    }

    /** Called by the engine when new mail arrives. */
    fun refreshMail() {
        runOnUiThread { web.evaluateJavascript("window.onNewMail && window.onNewMail()", null) }
    }

    override fun onResume() {
        super.onResume()
        // Back from the browser after signing in, or from the background.
        SignInService.stop(this)
        if (origin.isNotEmpty()) refreshMail()
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
    }

    @Deprecated("Still the simplest hook that works on every Android version")
    override fun onBackPressed() {
        // Let the page close whatever is on top. When nothing is, go to the
        // background instead of finishing, so mail keeps checking.
        web.evaluateJavascript("window.onBack ? window.onBack() : false") { handled ->
            if (handled != "true") moveTaskToBack(true)
        }
    }

    @Deprecated("Matches startActivityForResult above")
    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        super.onActivityResult(requestCode, resultCode, data)
        if (requestCode != REQ_FILES) return
        val cb = fileCallback ?: return
        fileCallback = null
        if (resultCode != RESULT_OK || data == null) {
            cb.onReceiveValue(null)
            return
        }
        val clip = data.clipData
        val uris = if (clip != null) Array(clip.itemCount) { clip.getItemAt(it).uri }
        else data.data?.let { arrayOf(it) }
        cb.onReceiveValue(uris)
    }

    override fun onDestroy() {
        Echo.attach(null)
        web.destroy()
        super.onDestroy()
    }

    companion object {
        private const val REQ_FILES = 1
        private const val REQ_NOTIFY = 2
    }
}

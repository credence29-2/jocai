package com.credence.victorcharlie

import android.Manifest
import android.annotation.SuppressLint
import android.app.Activity
import android.app.DownloadManager
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.graphics.Color
import android.media.AudioDeviceInfo
import android.media.AudioManager
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.os.Environment
import android.text.InputType
import android.view.Gravity
import android.view.ViewGroup
import android.view.inputmethod.EditorInfo
import android.webkit.CookieManager
import android.webkit.JavascriptInterface
import android.webkit.PermissionRequest
import android.webkit.URLUtil
import android.webkit.ValueCallback
import android.webkit.WebChromeClient
import android.webkit.WebResourceRequest
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.Button
import android.widget.EditText
import android.widget.LinearLayout
import android.widget.TextView
import android.widget.Toast

/**
 * Shows a Victor Charlie server's web app full screen and fills in what a WebView
 * doesn't do on its own: mic and camera permission, file uploads, downloads,
 * notifications and keeping voice alive in the background.
 */
class MainActivity : Activity() {
    private var web: WebView? = null
    private var pendingPermission: PermissionRequest? = null
    private var fileCallback: ValueCallback<Array<Uri>>? = null
    private val prefs by lazy { getSharedPreferences("victorcharlie", Context.MODE_PRIVATE) }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val server = prefs.getString("server", null)
        if (server == null) showSetup(null) else showWeb(server, intent)
        if (Build.VERSION.SDK_INT >= 33 && checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED) {
            requestPermissions(arrayOf(Manifest.permission.POST_NOTIFICATIONS), REQ_NOTIFY)
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        intent.getStringExtra(EXTRA_CHANNEL)?.let { openChannel(it) }
    }

    private fun openChannel(id: String) {
        val safe = id.filter { it.isDigit() }
        web?.evaluateJavascript("window.vcOpenChannel && window.vcOpenChannel('$safe')", null)
    }

    // --- first run ---

    private fun showSetup(error: String?) {
        web?.destroy()
        web = null
        val pad = (24 * resources.displayMetrics.density).toInt()
        val box = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            gravity = Gravity.CENTER_VERTICAL
            setPadding(pad, pad, pad, pad)
            setBackgroundColor(Color.parseColor("#1E1F22"))
        }
        val title = TextView(this).apply {
            text = "Connect to your server"
            textSize = 24f
            setTextColor(Color.WHITE)
        }
        val note = TextView(this).apply {
            text = "Enter the address you use in the browser, like https://chat.example.com, or paste an invite link."
            setTextColor(Color.parseColor("#949BA4"))
            setPadding(0, pad / 3, 0, pad)
        }
        val input = EditText(this).apply {
            setText(prefs.getString("server", "https://"))
            setTextColor(Color.WHITE)
            setHintTextColor(Color.GRAY)
            hint = "https://"
            inputType = InputType.TYPE_TEXT_VARIATION_URI
            imeOptions = EditorInfo.IME_ACTION_GO
            setSingleLine()
        }
        val err = TextView(this).apply {
            text = error ?: ""
            setTextColor(Color.parseColor("#F23F43"))
            setPadding(0, pad / 3, 0, pad / 3)
        }
        val go = Button(this).apply { text = "Connect" }
        val submit = {
            var v = input.text.toString().trim()
            if (!v.startsWith("http://") && !v.startsWith("https://")) v = "https://$v"
            val uri = Uri.parse(v)
            if (uri.host.isNullOrEmpty()) {
                err.text = "That doesn't look like an address."
            } else {
                val origin = "${uri.scheme}://${uri.authority}"
                prefs.edit().putString("server", origin).apply()
                showWeb(if (uri.path?.startsWith("/invite/") == true) v else origin, null)
            }
        }
        go.setOnClickListener { submit() }
        input.setOnEditorActionListener { _, _, _ -> submit(); true }
        box.addView(title)
        box.addView(note)
        box.addView(input, LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT))
        box.addView(err)
        box.addView(go)
        setContentView(box)
    }

    // --- the app ---

    @SuppressLint("SetJavaScriptEnabled")
    private fun showWeb(url: String, launch: Intent?) {
        val origin = prefs.getString("server", "")!!
        val w = WebView(this)
        web = w
        w.setBackgroundColor(Color.parseColor("#1E1F22"))
        w.settings.apply {
            javaScriptEnabled = true
            domStorageEnabled = true
            mediaPlaybackRequiresUserGesture = false
            allowFileAccess = false
            setSupportMultipleWindows(false)
            userAgentString = "$userAgentString VictorCharlieAndroid/1"
        }
        CookieManager.getInstance().setAcceptCookie(true)
        CookieManager.getInstance().setAcceptThirdPartyCookies(w, false)
        w.addJavascriptInterface(Bridge(), "VictorCharlieAndroid")

        w.webViewClient = object : WebViewClient() {
            override fun shouldOverrideUrlLoading(view: WebView, req: WebResourceRequest): Boolean {
                if (sameOrigin(req.url, origin)) return false
                startActivity(Intent(Intent.ACTION_VIEW, req.url))
                return true
            }

            override fun onReceivedError(view: WebView, req: WebResourceRequest, error: android.webkit.WebResourceError) {
                if (req.isForMainFrame) showSetup("Couldn't reach the server: ${error.description}")
            }

            override fun onPageFinished(view: WebView, url: String?) {
                launch?.getStringExtra(EXTRA_CHANNEL)?.let { openChannel(it) }
            }
        }

        w.webChromeClient = object : WebChromeClient() {
            override fun onPermissionRequest(request: PermissionRequest) {
                if (!sameOrigin(request.origin, origin)) {
                    request.deny()
                    return
                }
                val need = mutableListOf<String>()
                if (PermissionRequest.RESOURCE_AUDIO_CAPTURE in request.resources) need += Manifest.permission.RECORD_AUDIO
                if (PermissionRequest.RESOURCE_VIDEO_CAPTURE in request.resources) need += Manifest.permission.CAMERA
                val missing = need.filter { checkSelfPermission(it) != PackageManager.PERMISSION_GRANTED }
                if (missing.isEmpty()) {
                    request.grant(request.resources)
                } else {
                    pendingPermission = request
                    requestPermissions(missing.toTypedArray(), REQ_MEDIA)
                }
            }

            override fun onShowFileChooser(view: WebView, callback: ValueCallback<Array<Uri>>, params: FileChooserParams): Boolean {
                fileCallback?.onReceiveValue(null)
                fileCallback = callback
                val pick = Intent(Intent.ACTION_GET_CONTENT).apply {
                    addCategory(Intent.CATEGORY_OPENABLE)
                    type = params.acceptTypes.firstOrNull { it.isNotEmpty() } ?: "*/*"
                    putExtra(Intent.EXTRA_ALLOW_MULTIPLE, params.mode == FileChooserParams.MODE_OPEN_MULTIPLE)
                }
                return try {
                    startActivityForResult(Intent.createChooser(pick, "Choose files"), REQ_FILES)
                    true
                } catch (e: Exception) {
                    fileCallback = null
                    false
                }
            }
        }

        w.setDownloadListener { dl, userAgent, disposition, mime, _ ->
            val name = URLUtil.guessFileName(dl, disposition, mime)
            val req = DownloadManager.Request(Uri.parse(dl))
                .addRequestHeader("Cookie", CookieManager.getInstance().getCookie(dl))
                .addRequestHeader("User-Agent", userAgent)
                .setTitle(name)
                .setNotificationVisibility(DownloadManager.Request.VISIBILITY_VISIBLE_NOTIFY_COMPLETED)
                .setDestinationInExternalPublicDir(Environment.DIRECTORY_DOWNLOADS, name)
            (getSystemService(Context.DOWNLOAD_SERVICE) as DownloadManager).enqueue(req)
            Toast.makeText(this, "Downloading $name", Toast.LENGTH_SHORT).show()
        }

        setContentView(w)
        w.loadUrl(url)
    }

    /**
     * Whether a URL is the configured server: same scheme, host and port.
     * A prefix check would let chat.example.org.evil.com pass as the server.
     */
    private fun sameOrigin(u: Uri, origin: String): Boolean {
        val o = Uri.parse(origin)
        val port = { x: Uri -> if (x.port != -1) x.port else if (x.scheme == "https") 443 else 80 }
        return u.scheme.equals(o.scheme, true) && u.host.equals(o.host, true) && port(u) == port(o)
    }

    @Deprecated("Activity result API needs AndroidX; this app stays dependency-light")
    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        super.onActivityResult(requestCode, resultCode, data)
        if (requestCode != REQ_FILES) return
        val cb = fileCallback ?: return
        fileCallback = null
        if (resultCode != RESULT_OK || data == null) {
            cb.onReceiveValue(null)
            return
        }
        val uris = mutableListOf<Uri>()
        data.clipData?.let { clip -> for (i in 0 until clip.itemCount) uris += clip.getItemAt(i).uri }
        if (uris.isEmpty()) data.data?.let { uris += it }
        cb.onReceiveValue(uris.toTypedArray())
    }

    override fun onRequestPermissionsResult(requestCode: Int, permissions: Array<out String>, results: IntArray) {
        super.onRequestPermissionsResult(requestCode, permissions, results)
        if (requestCode != REQ_MEDIA) return
        val req = pendingPermission ?: return
        pendingPermission = null
        val granted = mutableListOf<String>()
        for (r in req.resources) {
            val perm = when (r) {
                PermissionRequest.RESOURCE_AUDIO_CAPTURE -> Manifest.permission.RECORD_AUDIO
                PermissionRequest.RESOURCE_VIDEO_CAPTURE -> Manifest.permission.CAMERA
                else -> null
            }
            if (perm != null && checkSelfPermission(perm) == PackageManager.PERMISSION_GRANTED) granted += r
        }
        if (granted.isEmpty()) req.deny() else req.grant(granted.toTypedArray())
    }

    @Deprecated("Matches the platform Activity API")
    override fun onBackPressed() {
        val w = web
        if (w != null && w.canGoBack()) w.goBack() else moveTaskToBack(true)
    }

    override fun onDestroy() {
        stopService(Intent(this, VoiceService::class.java))
        web?.destroy()
        super.onDestroy()
    }

    /** Methods the web app calls as window.VictorCharlieAndroid. */
    inner class Bridge {
        @JavascriptInterface
        fun notify(title: String, body: String, tag: String) {
            if (Build.VERSION.SDK_INT >= 33 && checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED) return
            val nm = getSystemService(NotificationManager::class.java)
            if (nm.getNotificationChannel(MESSAGES) == null) {
                nm.createNotificationChannel(NotificationChannel(MESSAGES, "Messages", NotificationManager.IMPORTANCE_HIGH))
            }
            val open = PendingIntent.getActivity(
                this@MainActivity, tag.hashCode(),
                Intent(this@MainActivity, MainActivity::class.java).putExtra(EXTRA_CHANNEL, tag).addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP),
                PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
            )
            val n = Notification.Builder(this@MainActivity, MESSAGES)
                .setSmallIcon(android.R.drawable.stat_notify_chat)
                .setContentTitle(title)
                .setContentText(body)
                .setStyle(Notification.BigTextStyle().bigText(body))
                .setContentIntent(open)
                .setAutoCancel(true)
                .build()
            nm.notify(tag.hashCode(), n)
        }

        @JavascriptInterface
        fun setBadge(n: Int) {
            if (n == 0) getSystemService(NotificationManager::class.java).cancelAll()
        }

        @JavascriptInterface
        fun voice(on: Boolean, label: String) {
            runOnUiThread {
                val am = getSystemService(Context.AUDIO_SERVICE) as AudioManager
                if (on) {
                    am.mode = AudioManager.MODE_IN_COMMUNICATION
                    useSpeakerUnlessHeadset(am)
                    if (checkSelfPermission(Manifest.permission.RECORD_AUDIO) == PackageManager.PERMISSION_GRANTED) {
                        startForegroundService(Intent(this@MainActivity, VoiceService::class.java).putExtra(VoiceService.EXTRA_LABEL, label))
                    }
                } else {
                    am.mode = AudioManager.MODE_NORMAL
                    stopService(Intent(this@MainActivity, VoiceService::class.java))
                }
            }
        }

        // Communication mode defaults to the earpiece; a group call wants the
        // loudspeaker unless headphones are plugged in or paired.
        private fun useSpeakerUnlessHeadset(am: AudioManager) {
            val headsets = setOf(
                AudioDeviceInfo.TYPE_WIRED_HEADSET, AudioDeviceInfo.TYPE_WIRED_HEADPHONES, AudioDeviceInfo.TYPE_BLUETOOTH_SCO,
                AudioDeviceInfo.TYPE_BLUETOOTH_A2DP, AudioDeviceInfo.TYPE_USB_HEADSET,
            )
            val outs = am.getDevices(AudioManager.GET_DEVICES_OUTPUTS)
            val headset = outs.any { it.type in headsets }
            if (Build.VERSION.SDK_INT >= 31) {
                val want = am.availableCommunicationDevices.firstOrNull {
                    if (headset) it.type in headsets else it.type == AudioDeviceInfo.TYPE_BUILTIN_SPEAKER
                }
                if (want != null) am.setCommunicationDevice(want)
            } else {
                @Suppress("DEPRECATION")
                am.isSpeakerphoneOn = !headset
            }
        }

        @JavascriptInterface
        fun changeServer() {
            runOnUiThread { showSetup(null) }
        }
    }

    companion object {
        private const val REQ_MEDIA = 1
        private const val REQ_FILES = 2
        private const val REQ_NOTIFY = 3
        private const val MESSAGES = "messages"
        const val EXTRA_CHANNEL = "channel"
    }
}

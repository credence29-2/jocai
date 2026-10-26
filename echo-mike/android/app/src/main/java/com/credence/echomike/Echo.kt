package com.credence.echomike

import android.app.Activity
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Bundle
import android.util.Log
import java.lang.ref.WeakReference
import mobile.Host

/**
 * The Android side of the Go mail engine: opens links and sign-in pages, and
 * shows new-mail notifications. One per process, shared by the window and the
 * background mail check.
 */
object Echo : Host {
    private const val CHANNEL = "mail"
    private lateinit var ctx: Context
    @Volatile private var window: WeakReference<MainActivity>? = null
    private var nextId = 100

    fun init(context: Context) {
        if (::ctx.isInitialized) return
        ctx = context.applicationContext
        val nm = ctx.getSystemService(NotificationManager::class.java)
        nm.createNotificationChannel(
            NotificationChannel(CHANNEL, "New mail", NotificationManager.IMPORTANCE_DEFAULT).apply {
                description = "A notification for each new email"
            }
        )
    }

    /** Where the engine keeps settings, aliases and sign-ins. */
    fun dataDir(context: Context): String = context.filesDir.resolve("mail").absolutePath

    fun attach(activity: MainActivity?) {
        window = activity?.let { WeakReference(it) }
    }

    override fun openURL(url: String?) {
        if (url.isNullOrBlank()) return
        val uri = Uri.parse(url)
        val host = uri.host.orEmpty()
        val signIn = host == "accounts.google.com" || host.startsWith("login.microsoftonline.")
        val activity: Activity? = window?.get()
        if (signIn) SignInService.start(ctx)
        val intent = Intent(Intent.ACTION_VIEW, uri)
        if (signIn) {
            // Open sign-in as a Custom Tab so it sits on top of the app and
            // the back button returns here. Google refuses sign-in inside a
            // WebView. Asking for a Custom Tab is just these extras.
            intent.putExtras(Bundle().apply { putBinder("android.support.customtabs.extra.SESSION", null) })
        }
        if (activity == null) intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        try {
            (activity ?: ctx).startActivity(intent)
        } catch (e: ActivityNotFoundException) {
            Log.w("EchoMike", "no app to open $url", e)
        }
    }

    override fun showNotification(title: String?, body: String?) {
        val open = PendingIntent.getActivity(
            ctx, 0,
            Intent(ctx, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )
        val n = android.app.Notification.Builder(ctx, CHANNEL)
            .setSmallIcon(R.drawable.ic_notify)
            .setColor(ctx.getColor(R.color.accent))
            .setContentTitle(title)
            .setContentText(body?.substringBefore('\n'))
            .setStyle(android.app.Notification.BigTextStyle().bigText(body))
            .setContentIntent(open)
            .setAutoCancel(true)
            .setCategory(android.app.Notification.CATEGORY_EMAIL)
            .build()
        // Without the notification permission this does nothing, quietly.
        ctx.getSystemService(NotificationManager::class.java).notify(nextId++, n)
    }

    override fun refresh() {
        window?.get()?.refreshMail()
    }
}

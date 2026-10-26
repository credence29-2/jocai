package com.credence.echomike

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.util.Log

/**
 * Keeps the app running while you sign in in the browser. Google sends the
 * browser back to the app's own address on this phone, and Android freezes
 * apps that sit in the background, which would leave sign-in hanging.
 * Stops on its own when you come back, or after 3 minutes.
 */
class SignInService : Service() {

    private val stopper = Handler(Looper.getMainLooper())

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        val nm = getSystemService(NotificationManager::class.java)
        nm.createNotificationChannel(NotificationChannel(CHANNEL, "Signing in", NotificationManager.IMPORTANCE_LOW))
        val n = Notification.Builder(this, CHANNEL)
            .setSmallIcon(R.drawable.ic_notify)
            .setContentTitle("Finish signing in")
            .setContentText("Then come back to Project Echo Mike")
            .build()
        try {
            if (Build.VERSION.SDK_INT >= 34) {
                startForeground(ID, n, ServiceInfo.FOREGROUND_SERVICE_TYPE_SHORT_SERVICE)
            } else {
                startForeground(ID, n)
            }
        } catch (e: Exception) {
            Log.w("EchoMike", "couldn't stay awake for sign-in", e)
            stopSelf()
            return START_NOT_STICKY
        }
        stopper.removeCallbacksAndMessages(null)
        stopper.postDelayed({ stopSelf() }, 3 * 60 * 1000L)
        return START_NOT_STICKY
    }

    override fun onTimeout(startId: Int) {
        stopSelf()
    }

    override fun onBind(intent: Intent?): IBinder? = null

    companion object {
        private const val CHANNEL = "signin"
        private const val ID = 2

        fun start(context: Context) {
            try {
                context.startForegroundService(Intent(context, SignInService::class.java))
            } catch (e: Exception) {
                Log.w("EchoMike", "couldn't start sign-in service", e)
            }
        }

        fun stop(context: Context) {
            context.stopService(Intent(context, SignInService::class.java))
        }
    }
}

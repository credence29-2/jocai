package com.credence.cpc.core

import android.app.usage.StorageStatsManager
import android.app.usage.UsageStatsManager
import android.content.Context
import android.content.Intent
import android.content.pm.ApplicationInfo
import android.graphics.drawable.Drawable
import android.os.Process
import android.os.storage.StorageManager

/** One installed app with its storage use. */
class AppEntry(
    val packageName: String,
    val label: String,
    val icon: Drawable?,
    val cacheBytes: Long,
    val appBytes: Long,
    val dataBytes: Long,
    /** Last time the app was opened, or 0 when Android has no record. */
    val lastUsedMs: Long,
    val isSystem: Boolean,
) {
    val totalBytes: Long get() = appBytes + dataBytes + cacheBytes
}

/** Reads installed apps and how much space each one uses. Needs usage access. */
object AppScanner {
    private const val DAY_MS = 24L * 60 * 60 * 1000

    fun load(ctx: Context): List<AppEntry> {
        val pm = ctx.packageManager
        val launcher = Intent(Intent.ACTION_MAIN).addCategory(Intent.CATEGORY_LAUNCHER)
        val packages = pm.queryIntentActivities(launcher, 0)
            .map { it.activityInfo.packageName }
            .filter { it != ctx.packageName }
            .distinct()

        val stats = ctx.getSystemService(StorageStatsManager::class.java)!!
        val usage = ctx.getSystemService(UsageStatsManager::class.java)!!
        val now = System.currentTimeMillis()
        val lastUsed: Map<String, Long> = try {
            usage.queryAndAggregateUsageStats(now - 365 * DAY_MS, now).mapValues { it.value.lastTimeUsed }
        } catch (e: Exception) {
            emptyMap()
        }
        val user = Process.myUserHandle()

        return packages.mapNotNull { pkg ->
            try {
                val info = pm.getApplicationInfo(pkg, 0)
                val s = stats.queryStatsForPackage(StorageManager.UUID_DEFAULT, pkg, user)
                AppEntry(
                    packageName = pkg,
                    label = pm.getApplicationLabel(info).toString(),
                    icon = try { pm.getApplicationIcon(info) } catch (e: Exception) { null },
                    cacheBytes = s.cacheBytes,
                    appBytes = s.appBytes,
                    dataBytes = s.dataBytes - s.cacheBytes,
                    lastUsedMs = lastUsed[pkg] ?: 0L,
                    isSystem = info.flags and ApplicationInfo.FLAG_SYSTEM != 0,
                )
            } catch (e: Exception) {
                null
            }
        }
    }
}

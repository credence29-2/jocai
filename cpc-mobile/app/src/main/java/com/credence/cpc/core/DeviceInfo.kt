package com.credence.cpc.core

import android.app.ActivityManager
import android.content.Context
import android.os.Build
import android.os.Environment
import android.os.StatFs
import java.time.LocalDate
import java.time.format.DateTimeFormatter
import java.time.temporal.ChronoUnit
import java.util.Locale

/** Snapshot of the phone's storage, memory and update status. */
data class DeviceInfo(
    val storageTotal: Long,
    val storageFree: Long,
    val memTotal: Long,
    val memAvailable: Long,
    val androidVersion: String,
    val model: String,
    val patchDate: LocalDate?,
) {
    val storageUsed: Long get() = storageTotal - storageFree
    val memUsed: Long get() = memTotal - memAvailable

    /** Days since the security patch, or null when unknown. */
    fun patchAgeDays(today: LocalDate = LocalDate.now()): Long? =
        patchDate?.let { ChronoUnit.DAYS.between(it, today) }

    fun patchText(): String =
        patchDate?.format(DateTimeFormatter.ofPattern("MMMM d, yyyy", Locale.US)) ?: "Unknown"

    companion object {
        /** Patches older than this are flagged as out of date. */
        const val PATCH_WARN_DAYS = 90L

        fun read(ctx: Context): DeviceInfo {
            val stat = StatFs(Environment.getDataDirectory().path)
            val am = ctx.getSystemService(ActivityManager::class.java)!!
            val mem = ActivityManager.MemoryInfo().also { am.getMemoryInfo(it) }
            val patch = try {
                LocalDate.parse(Build.VERSION.SECURITY_PATCH)
            } catch (e: Exception) {
                null
            }
            val model = listOf(Build.MANUFACTURER, Build.MODEL)
                .filter { it.isNotBlank() }
                .joinToString(" ")
                .replaceFirstChar { it.uppercase() }
            return DeviceInfo(
                storageTotal = stat.totalBytes,
                storageFree = stat.availableBytes,
                memTotal = mem.totalMem,
                memAvailable = mem.availMem,
                androidVersion = Build.VERSION.RELEASE,
                model = model,
                patchDate = patch,
            )
        }
    }
}

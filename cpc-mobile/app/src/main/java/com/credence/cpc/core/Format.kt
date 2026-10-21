package com.credence.cpc.core

import java.util.Locale

/** Formatting helpers shared by every screen. */
object Format {
    fun bytes(n: Long): String {
        if (n < 1024) return "$n B"
        val units = "KMGTPE"
        var value = n.toDouble() / 1024
        var i = 0
        while (value >= 1024 && i < units.length - 1) {
            value /= 1024
            i++
        }
        return String.format(Locale.US, "%.1f %sB", value, units[i])
    }

    fun files(n: Int): String = if (n == 1) "1 file" else "$n files"

    fun daysAgo(days: Long): String = when {
        days <= 0 -> "today"
        days == 1L -> "yesterday"
        days < 60 -> "$days days ago"
        days < 730 -> "${days / 30} months ago"
        else -> "${days / 365} years ago"
    }
}

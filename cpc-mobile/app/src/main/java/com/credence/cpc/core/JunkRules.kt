package com.credence.cpc.core

/**
 * The kinds of junk Kempt looks for in shared storage. Items that are safe for
 * anyone start ticked. Anything that might be wanted starts unticked so the
 * person reviews it first.
 */
enum class JunkCategory(val title: String, val description: String, val defaultChecked: Boolean) {
    TRASH(
        "Gallery trash",
        "Photos and videos you already deleted that are waiting in the trash. Removing them is permanent.",
        true,
    ),
    THUMBNAILS(
        "Thumbnail cache",
        "Preview images your gallery rebuilds on its own.",
        true,
    ),
    APK(
        "Installer files (APK)",
        "App installers left behind after installing. The installed apps don't need them.",
        true,
    ),
    LOGS(
        "Log and temp files",
        "Leftover .log and .tmp files apps wrote to shared storage.",
        true,
    ),
    EMPTY_FOLDERS(
        "Empty folders",
        "Folders with nothing in them. Standard folders like Download and DCIM are never removed.",
        true,
    ),
    OLD_DOWNLOADS(
        "Old downloads",
        "Files in Download older than 90 days. Off by default, review before deleting.",
        false,
    ),
    LARGE_FILES(
        "Large files",
        "Single files over 100 MB, often videos. Off by default, review before deleting.",
        false,
    ),
}

object JunkRules {
    const val OLD_DOWNLOAD_DAYS = 90L
    const val LARGE_FILE_BYTES = 100L * 1024 * 1024
    private const val DAY_MS = 24L * 60 * 60 * 1000

    /** Top level folders that are skipped entirely. Android/ holds app data. */
    val skippedTopLevel = setOf("android")

    /** Standard folders that must exist even when empty. */
    val protectedFolders = setOf(
        "alarms", "android", "audiobooks", "dcim", "documents", "download", "movies", "music",
        "notifications", "pictures", "podcasts", "recordings", "ringtones",
    )

    private val logExtensions = setOf("log", "tmp", "temp")
    private val apkExtensions = setOf("apk", "apks", "xapk", "apkm")
    private val junkNames = setOf("thumbs.db", ".ds_store", "desktop.ini")

    /**
     * Decides which category a file belongs to, or null to leave it alone.
     * [relPath] is relative to the storage root and uses '/' separators.
     */
    fun classify(relPath: String, size: Long, modifiedMs: Long, nowMs: Long): JunkCategory? {
        val parts = relPath.split('/').filter { it.isNotEmpty() }
        if (parts.isEmpty()) return null
        if (parts[0].lowercase() in skippedTopLevel) return null
        val name = parts.last()
        val lower = name.lowercase()
        val ext = lower.substringAfterLast('.', "")

        if (lower.startsWith(".trashed-")) return JunkCategory.TRASH
        if (parts.dropLast(1).any { it.equals(".thumbnails", ignoreCase = true) }) return JunkCategory.THUMBNAILS
        if (ext in apkExtensions) return JunkCategory.APK
        if (ext in logExtensions || lower in junkNames) return JunkCategory.LOGS
        val ageDays = (nowMs - modifiedMs) / DAY_MS
        if (parts.size >= 2 && parts[0].equals("download", ignoreCase = true) && ageDays >= OLD_DOWNLOAD_DAYS) {
            return JunkCategory.OLD_DOWNLOADS
        }
        if (size >= LARGE_FILE_BYTES) return JunkCategory.LARGE_FILES
        return null
    }

    /** True when an empty folder may be removed. */
    fun emptyFolderRemovable(relPath: String): Boolean {
        val parts = relPath.split('/').filter { it.isNotEmpty() }
        if (parts.isEmpty()) return false
        if (parts[0].lowercase() in skippedTopLevel) return false
        if (parts.size == 1 && parts[0].lowercase() in protectedFolders) return false
        return true
    }
}

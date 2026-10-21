package com.credence.cpc.core

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.io.File
import java.nio.file.Files

class JunkTest {
    @get:Rule val tmp = TemporaryFolder()

    private val day = 24L * 60 * 60 * 1000
    private val now = 1_800_000_000_000L

    @Test fun classifyRules() {
        assertEquals(JunkCategory.TRASH, JunkRules.classify("DCIM/Camera/.trashed-1700000000-IMG_1.jpg", 10, now, now))
        assertEquals(JunkCategory.THUMBNAILS, JunkRules.classify("DCIM/.thumbnails/123.jpg", 10, now, now))
        assertEquals(JunkCategory.APK, JunkRules.classify("Download/app.APK", 10, now, now))
        assertEquals(JunkCategory.LOGS, JunkRules.classify("Documents/app/debug.log", 10, now, now))
        assertEquals(JunkCategory.LOGS, JunkRules.classify("Music/Thumbs.db", 10, now, now))
        assertEquals(JunkCategory.OLD_DOWNLOADS, JunkRules.classify("Download/menu.pdf", 10, now - 120 * day, now))
        assertNull(JunkRules.classify("Download/menu.pdf", 10, now - 10 * day, now))
        assertEquals(JunkCategory.LARGE_FILES, JunkRules.classify("Movies/clip.mp4", 200L shl 20, now, now))
        assertNull(JunkRules.classify("DCIM/Camera/IMG_1.jpg", 4_000_000, now - 900 * day, now))
        assertNull("app data is off limits", JunkRules.classify("Android/data/x/cache/a.log", 10, now, now))
        assertNull("old file at root of storage is not a download", JunkRules.classify("notes.txt", 10, now - 900 * day, now))
    }

    @Test fun emptyFolderRules() {
        assertFalse(JunkRules.emptyFolderRemovable("Download"))
        assertFalse(JunkRules.emptyFolderRemovable("DCIM"))
        assertFalse(JunkRules.emptyFolderRemovable("Android/media/x"))
        assertTrue(JunkRules.emptyFolderRemovable("Download/Old stuff"))
        assertTrue(JunkRules.emptyFolderRemovable("SomeAppFolder"))
    }

    private fun file(path: String, size: Int, ageDays: Long = 0): File {
        val f = File(tmp.root, path)
        f.parentFile!!.mkdirs()
        f.writeBytes(ByteArray(size))
        f.setLastModified(now - ageDays * day)
        return f
    }

    @Test fun scanAndClean() {
        file("Download/old.apk", 50)
        file("Download/recent.pdf", 5)
        file("Download/ancient.pdf", 7, ageDays = 200)
        file("DCIM/.thumbnails/1.jpg", 20)
        file("DCIM/Camera/photo.jpg", 30)
        file("Android/data/com.x/cache/junk.log", 99)
        File(tmp.root, "Download/EmptyDir").mkdirs()
        File(tmp.root, "Music").mkdirs()

        val scanner = JunkScanner(tmp.root) { now }
        val groups = scanner.scan()
        assertEquals(1, groups.getValue(JunkCategory.APK).items.size)
        assertEquals(1, groups.getValue(JunkCategory.THUMBNAILS).items.size)
        assertEquals(1, groups.getValue(JunkCategory.OLD_DOWNLOADS).items.size)
        assertFalse(groups.getValue(JunkCategory.OLD_DOWNLOADS).items[0].checked)
        assertEquals(0, groups.getValue(JunkCategory.LOGS).items.size)
        val empties = groups.getValue(JunkCategory.EMPTY_FOLDERS).items.map { it.relPath }
        assertEquals(listOf("Download/EmptyDir"), empties)

        val result = scanner.clean(groups.values)
        assertEquals(70L, result.bytes) // apk + thumbnail
        assertEquals(3, result.deleted) // 2 files + 1 folder
        assertFalse(File(tmp.root, "Download/old.apk").exists())
        assertFalse(File(tmp.root, "Download/EmptyDir").exists())
        assertTrue("unticked review items stay", File(tmp.root, "Download/ancient.pdf").exists())
        assertTrue(File(tmp.root, "DCIM/Camera/photo.jpg").exists())
        assertTrue(File(tmp.root, "Android/data/com.x/cache/junk.log").exists())
        assertTrue(File(tmp.root, "Music").exists())
    }

    @Test fun symlinksNotFollowed() {
        val outside = tmp.newFolder("outside")
        File(outside, "keep.log").writeText("x")
        val root = File(tmp.root, "storage").apply { mkdirs() }
        Files.createSymbolicLink(File(root, "link").toPath(), outside.toPath())
        val groups = JunkScanner(root) { now }.scan()
        assertEquals(0, groups.getValue(JunkCategory.LOGS).items.size)
    }

    @Test fun folderThatGainedFilesIsKept() {
        File(tmp.root, "Stuff/Empty").mkdirs()
        val scanner = JunkScanner(tmp.root) { now }
        val groups = scanner.scan()
        File(tmp.root, "Stuff/Empty/new.txt").writeText("hi")
        scanner.clean(groups.values)
        assertTrue(File(tmp.root, "Stuff/Empty/new.txt").exists())
    }

    @Test fun formatting() {
        assertEquals("0 B", Format.bytes(0))
        assertEquals("1.5 KB", Format.bytes(1536))
        assertEquals("2.0 GB", Format.bytes(2L shl 30))
        assertEquals("1 file", Format.files(1))
        assertEquals("3 days ago", Format.daysAgo(3))
        assertEquals("4 months ago", Format.daysAgo(125))
    }
}

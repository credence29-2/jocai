package com.credence.cpc.core

import java.io.File
import java.io.IOException
import java.nio.file.FileVisitResult
import java.nio.file.Files
import java.nio.file.Path
import java.nio.file.SimpleFileVisitor
import java.nio.file.attribute.BasicFileAttributes
import java.util.EnumMap
import java.util.EnumSet

/** One file or folder the scanner found. */
class JunkItem(val file: File, val relPath: String, val size: Long, val isFolder: Boolean) {
    var checked: Boolean = true
}

/** Everything found in one category. */
class JunkGroup(val category: JunkCategory) {
    val items = mutableListOf<JunkItem>()
    val totalBytes: Long get() = items.sumOf { it.size }
    val checkedBytes: Long get() = items.filter { it.checked }.sumOf { it.size }
    val checkedCount: Int get() = items.count { it.checked }
}

class CleanResult(val deleted: Int, val bytes: Long, val failed: Int, val deletedPaths: List<String>)

/**
 * Walks shared storage looking for junk. Pure JVM code so it can be unit
 * tested without a phone. Symbolic links are never followed.
 */
class JunkScanner(private val root: File, private val now: () -> Long = System::currentTimeMillis) {

    @Volatile var cancelled = false

    fun scan(progress: (String) -> Unit = {}): Map<JunkCategory, JunkGroup> {
        val groups = EnumMap<JunkCategory, JunkGroup>(JunkCategory::class.java)
        JunkCategory.values().forEach { groups[it] = JunkGroup(it) }
        val rootPath = root.toPath()
        if (!Files.isDirectory(rootPath)) return groups
        val nowMs = now()
        // Number of entries seen in each folder, to find empty ones.
        val counts = ArrayDeque<IntArray>()
        var lastProgress = 0L

        fun rel(p: Path): String = rootPath.relativize(p).toString().replace(File.separatorChar, '/')

        Files.walkFileTree(rootPath, EnumSet.noneOf(java.nio.file.FileVisitOption::class.java), 24,
            object : SimpleFileVisitor<Path>() {
                override fun preVisitDirectory(dir: Path, attrs: BasicFileAttributes): FileVisitResult {
                    if (cancelled) return FileVisitResult.TERMINATE
                    if (dir != rootPath) {
                        counts.lastOrNull()?.let { it[0]++ }
                        val r = rel(dir)
                        if (attrs.isSymbolicLink) return FileVisitResult.SKIP_SUBTREE
                        if (!r.contains('/') && r.lowercase() in JunkRules.skippedTopLevel) {
                            return FileVisitResult.SKIP_SUBTREE
                        }
                    }
                    counts.addLast(IntArray(1))
                    return FileVisitResult.CONTINUE
                }

                override fun visitFile(file: Path, attrs: BasicFileAttributes): FileVisitResult {
                    if (cancelled) return FileVisitResult.TERMINATE
                    counts.lastOrNull()?.let { it[0]++ }
                    if (!attrs.isRegularFile) return FileVisitResult.CONTINUE
                    val r = rel(file)
                    val t = now()
                    if (t - lastProgress > 100) {
                        lastProgress = t
                        progress(r)
                    }
                    val cat = JunkRules.classify(r, attrs.size(), attrs.lastModifiedTime().toMillis(), nowMs)
                    if (cat != null) {
                        groups.getValue(cat).items += JunkItem(file.toFile(), r, attrs.size(), false).also {
                            it.checked = cat.defaultChecked
                        }
                    }
                    return FileVisitResult.CONTINUE
                }

                override fun visitFileFailed(file: Path, exc: IOException): FileVisitResult {
                    // Unreadable entry: count it so its folder is never treated as empty.
                    counts.lastOrNull()?.let { it[0]++ }
                    return FileVisitResult.CONTINUE
                }

                override fun postVisitDirectory(dir: Path, exc: IOException?): FileVisitResult {
                    val c = counts.removeLastOrNull()
                    if (dir != rootPath && exc == null && c != null && c[0] == 0) {
                        val r = rel(dir)
                        if (JunkRules.emptyFolderRemovable(r)) {
                            groups.getValue(JunkCategory.EMPTY_FOLDERS).items +=
                                JunkItem(dir.toFile(), r, 0, true).also { it.checked = JunkCategory.EMPTY_FOLDERS.defaultChecked }
                        }
                    }
                    return if (cancelled) FileVisitResult.TERMINATE else FileVisitResult.CONTINUE
                }
            })
        return groups
    }

    /**
     * Deletes every checked item in the given groups. Files first, then empty
     * folders deepest first. A folder is only removed if it is still empty.
     */
    fun clean(groups: Collection<JunkGroup>, progress: (String) -> Unit = {}): CleanResult {
        val chosen = groups.flatMap { g -> g.items.filter { it.checked } }
        var deleted = 0
        var bytes = 0L
        var failed = 0
        val paths = mutableListOf<String>()
        val rootCanon = root.canonicalPath
        for (item in chosen.filter { !it.isFolder }) {
            if (cancelled) break
            if (!isInside(item.file, rootCanon) || Files.isSymbolicLink(item.file.toPath())) {
                failed++
                continue
            }
            progress(item.relPath)
            if (item.file.delete()) {
                deleted++
                bytes += item.size
                paths += item.file.path
            } else if (item.file.exists()) {
                failed++
            }
        }
        for (item in chosen.filter { it.isFolder }.sortedByDescending { it.relPath.length }) {
            if (cancelled) break
            if (!isInside(item.file, rootCanon)) continue
            val children = item.file.list()
            if (children != null && children.isEmpty() && item.file.delete()) deleted++
        }
        return CleanResult(deleted, bytes, failed, paths)
    }

    private fun isInside(f: File, rootCanon: String): Boolean {
        val c = try { f.canonicalPath } catch (e: IOException) { return false }
        return c.startsWith(rootCanon + File.separator)
    }
}

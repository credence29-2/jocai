package com.credence.cpc.ui

import android.app.AlertDialog
import android.media.MediaScannerConnection
import android.os.Environment
import android.view.View
import android.widget.CheckBox
import android.widget.LinearLayout
import android.widget.TextView
import com.credence.cpc.MainActivity
import com.credence.cpc.core.Access
import com.credence.cpc.core.CleanResult
import com.credence.cpc.core.Format
import com.credence.cpc.core.JunkCategory
import com.credence.cpc.core.JunkGroup
import com.credence.cpc.core.JunkScanner

class JunkScreen(act: MainActivity) : Screen(act) {
    private enum class State { IDLE, SCANNING, RESULTS, CLEANING }

    private val body: LinearLayout = ui.column(16)
    override val root: View = ui.scroll(body)

    private var state = State.IDLE
    private var scanner: JunkScanner? = null
    private var groups: Map<JunkCategory, JunkGroup> = emptyMap()
    private var lastResult: CleanResult? = null
    private var progressText: TextView? = null
    private var pendingScan = false

    override fun onShow() {
        if (pendingScan && Access.hasFiles(act)) {
            pendingScan = false
            startScan()
            return
        }
        render()
    }

    /** Used by the Home screen's "Scan for junk files" button. */
    fun startScanIfReady() {
        if (state == State.SCANNING || state == State.CLEANING) return
        if (Access.hasFiles(act)) startScan() else pendingScan = true
    }

    private fun render() {
        body.removeAllViews()
        body.addView(ui.title("Junk files"))
        body.addView(ui.caption("Finds clutter in your phone's shared storage. You see everything before anything is deleted."), ui.spaced(2))
        body.addView(View(act), ui.spaced(8))

        if (!Access.hasFiles(act)) {
            body.addView(
                ui.card(
                    ui.heading("Allow file access"),
                    ui.body("To find junk files, Kempt needs \"All files access\". It only looks at shared storage like Download and DCIM, never inside other apps, and nothing leaves your phone."),
                    ui.primary("Allow access") { pendingScan = true; act.requestFileAccess() },
                ),
            )
            return
        }

        when (state) {
            State.IDLE -> {
                lastResult?.let { r ->
                    body.addView(ui.card(ui.heading("Done"), ui.status(resultText(r), true)))
                }
                body.addView(ui.primary("Scan") { startScan() })
            }
            State.SCANNING, State.CLEANING -> {
                val label = if (state == State.SCANNING) "Scanning..." else "Deleting..."
                val pt = ui.caption("")
                progressText = pt
                body.addView(ui.card(ui.heading(label), ui.spinner(), pt))
                if (state == State.SCANNING) body.addView(ui.secondary("Stop") { scanner?.cancelled = true })
            }
            State.RESULTS -> renderResults()
        }
    }

    private fun renderResults() {
        val found = groups.values.filter { it.items.isNotEmpty() }
        if (found.isEmpty()) {
            body.addView(ui.card(ui.heading("Nothing to clean"), ui.body("Your shared storage is tidy.")))
            body.addView(ui.secondary("Scan again") { startScan() })
            return
        }
        val summary = ui.heading("")
        body.addView(ui.card(summary, ui.caption("Tap a group to pick individual files.")))
        val deleteBtn = ui.primary("Delete selected") { confirmClean() }

        fun refreshSummary() {
            val bytes = found.sumOf { it.checkedBytes }
            val count = found.sumOf { it.checkedCount }
            summary.text = "${Format.bytes(bytes)} selected (${Format.files(count)})"
            deleteBtn.isEnabled = count > 0
            deleteBtn.alpha = if (count > 0) 1f else 0.5f
        }

        for (g in found) {
            val check = CheckBox(act).apply {
                isChecked = g.checkedCount > 0
                buttonTintList = android.content.res.ColorStateList.valueOf(ui.p.accent)
            }
            val detail = ui.caption("")
            fun refreshRow() {
                detail.text = "${Format.files(g.items.size)}, ${Format.bytes(g.totalBytes)}" +
                    (if (g.checkedCount in 1 until g.items.size) ". ${g.checkedCount} selected." else "") +
                    "\n" + g.category.description
                check.isChecked = g.checkedCount > 0
            }
            refreshRow()
            check.setOnClickListener {
                g.items.forEach { it.checked = check.isChecked }
                refreshRow()
                refreshSummary()
            }
            val texts = ui.column().apply {
                addView(ui.body(g.category.title))
                addView(detail)
            }
            val row = ui.row().apply {
                addView(check)
                addView(texts, LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f))
                background = ui.rounded(ui.p.card)
                setPadding(ui.dp(8), ui.dp(12), ui.dp(16), ui.dp(12))
                setOnClickListener { review(g) { refreshRow(); refreshSummary() } }
            }
            body.addView(row, ui.spaced(0).apply { bottomMargin = ui.dp(10) })
        }
        refreshSummary()
        body.addView(deleteBtn, ui.spaced(6))
        body.addView(ui.secondary("Scan again") { startScan() }, ui.spaced(10))
    }

    private fun review(g: JunkGroup, changed: () -> Unit) {
        val limit = 2000
        val shown = g.items.take(limit)
        val labels = shown.map { "${it.relPath}\n${if (it.isFolder) "folder" else Format.bytes(it.size)}" }.toTypedArray()
        val checked = shown.map { it.checked }.toBooleanArray()
        AlertDialog.Builder(act)
            .setTitle(g.category.title + if (g.items.size > limit) " (first $limit)" else "")
            .setMultiChoiceItems(labels, checked) { _, which, isChecked -> shown[which].checked = isChecked }
            .setPositiveButton("Done") { _, _ -> changed() }
            .setOnDismissListener { changed() }
            .show()
    }

    private fun startScan() {
        val s = JunkScanner(Environment.getExternalStorageDirectory())
        scanner = s
        lastResult = null
        state = State.SCANNING
        render()
        act.background({
            s.scan { path -> act.runOnUiThread { progressText?.text = path } }
        }) { r ->
            r.onSuccess { groups = it; state = if (s.cancelled) State.IDLE else State.RESULTS }
                .onFailure { state = State.IDLE; act.toast("Scan failed: ${it.message}") }
            render()
        }
    }

    private fun confirmClean() {
        val chosen = groups.values.filter { it.checkedCount > 0 }
        val bytes = chosen.sumOf { it.checkedBytes }
        val count = chosen.sumOf { it.checkedCount }
        AlertDialog.Builder(act)
            .setTitle("Delete ${Format.files(count)}?")
            .setMessage("This frees about ${Format.bytes(bytes)}. Deleted files can't be recovered.")
            .setPositiveButton("Delete") { _, _ -> clean(chosen) }
            .setNegativeButton("Cancel", null)
            .show()
    }

    private fun clean(chosen: List<JunkGroup>) {
        val s = scanner ?: return
        s.cancelled = false
        state = State.CLEANING
        render()
        act.background({
            s.clean(chosen) { path -> act.runOnUiThread { progressText?.text = path } }
        }) { r ->
            r.onSuccess { res ->
                lastResult = res
                // Let the gallery and file apps know these files are gone.
                if (res.deletedPaths.isNotEmpty()) {
                    MediaScannerConnection.scanFile(act.applicationContext, res.deletedPaths.toTypedArray(), null, null)
                }
            }.onFailure { act.toast("Cleaning stopped: ${it.message}") }
            groups = emptyMap()
            state = State.IDLE
            render()
        }
    }

    private fun resultText(r: CleanResult): String {
        var s = "Freed ${Format.bytes(r.bytes)} (${Format.files(r.deleted)})."
        if (r.failed > 0) s += " ${Format.files(r.failed)} couldn't be deleted."
        return s
    }
}

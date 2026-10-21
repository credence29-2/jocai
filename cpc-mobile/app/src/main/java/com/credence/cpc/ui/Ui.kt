package com.credence.cpc.ui

import android.content.Context
import android.content.res.ColorStateList
import android.content.res.Configuration
import android.graphics.Color
import android.graphics.Typeface
import android.graphics.drawable.GradientDrawable
import android.graphics.drawable.RippleDrawable
import android.util.TypedValue
import android.view.Gravity
import android.view.View
import android.view.ViewGroup
import android.widget.Button
import android.widget.LinearLayout
import android.widget.ProgressBar
import android.widget.ScrollView
import android.widget.TextView

/** Colors for light and dark mode. */
class Palette(val night: Boolean) {
    val bg = if (night) 0xFF0F1419.toInt() else 0xFFF3F5F9.toInt()
    val card = if (night) 0xFF1A2129.toInt() else Color.WHITE
    val text = if (night) 0xFFE8ECF1.toInt() else 0xFF15202B.toInt()
    val subtext = if (night) 0xFF9AA6B2.toInt() else 0xFF5B6773.toInt()
    val accent = if (night) 0xFF6EA8FF.toInt() else 0xFF1E5FB4.toInt()
    val onAccent = if (night) 0xFF0F1419.toInt() else Color.WHITE
    val good = if (night) 0xFF4CC27A.toInt() else 0xFF1B873F.toInt()
    val warn = if (night) 0xFFF0B44C.toInt() else 0xFFB45309.toInt()
    val track = if (night) 0xFF2B3440.toInt() else 0xFFE1E6ED.toInt()
    val nav = if (night) 0xFF151B22.toInt() else Color.WHITE

    companion object {
        fun of(ctx: Context): Palette =
            Palette(ctx.resources.configuration.uiMode and Configuration.UI_MODE_NIGHT_MASK == Configuration.UI_MODE_NIGHT_YES)
    }
}

/** Small view builders so screens read top to bottom. */
class Ui(val ctx: Context, val p: Palette) {

    fun dp(v: Int): Int = TypedValue.applyDimension(TypedValue.COMPLEX_UNIT_DIP, v.toFloat(), ctx.resources.displayMetrics).toInt()

    fun column(padding: Int = 0): LinearLayout = LinearLayout(ctx).apply {
        orientation = LinearLayout.VERTICAL
        setPadding(dp(padding), dp(padding), dp(padding), dp(padding))
    }

    fun row(): LinearLayout = LinearLayout(ctx).apply {
        orientation = LinearLayout.HORIZONTAL
        gravity = Gravity.CENTER_VERTICAL
    }

    fun scroll(content: View): ScrollView = ScrollView(ctx).apply {
        isFillViewport = true
        addView(content)
    }

    private fun text(s: CharSequence, sizeSp: Float, color: Int, bold: Boolean = false): TextView = TextView(ctx).apply {
        text = s
        setTextColor(color)
        setTextSize(TypedValue.COMPLEX_UNIT_SP, sizeSp)
        if (bold) typeface = Typeface.create("sans-serif-medium", Typeface.NORMAL)
        setLineSpacing(0f, 1.15f)
    }

    fun title(s: CharSequence) = text(s, 22f, p.text, bold = true)
    fun heading(s: CharSequence) = text(s, 17f, p.text, bold = true)
    fun body(s: CharSequence) = text(s, 15f, p.text)
    fun caption(s: CharSequence) = text(s, 13f, p.subtext)
    fun status(s: CharSequence, ok: Boolean) = text(s, 15f, if (ok) p.good else p.warn, bold = true)

    fun rounded(color: Int, radiusDp: Int = 16): GradientDrawable = GradientDrawable().apply {
        setColor(color)
        cornerRadius = dp(radiusDp).toFloat()
    }

    /** A rounded card that stacks its children with a little spacing. */
    fun card(vararg children: View): LinearLayout = column(16).apply {
        background = rounded(p.card)
        children.forEachIndexed { i, v -> addView(v, spaced(if (i == 0) 0 else 6)) }
        layoutParams = LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT)
            .apply { bottomMargin = dp(12) }
    }

    fun spaced(topDp: Int): LinearLayout.LayoutParams =
        LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT).apply { topMargin = dp(topDp) }

    private fun button(s: String, bg: Int, fg: Int, onClick: () -> Unit): Button = Button(ctx).apply {
        text = s
        isAllCaps = false
        setTextColor(fg)
        setTextSize(TypedValue.COMPLEX_UNIT_SP, 15f)
        typeface = Typeface.create("sans-serif-medium", Typeface.NORMAL)
        stateListAnimator = null
        background = RippleDrawable(ColorStateList.valueOf(0x33808080), rounded(bg, 24), null)
        minHeight = dp(48)
        setPadding(dp(20), 0, dp(20), 0)
        setOnClickListener { onClick() }
    }

    fun primary(s: String, onClick: () -> Unit) = button(s, p.accent, p.onAccent, onClick)
    fun secondary(s: String, onClick: () -> Unit) = button(s, p.track, p.text, onClick)

    fun bar(fraction: Double, warn: Boolean = false): ProgressBar =
        ProgressBar(ctx, null, android.R.attr.progressBarStyleHorizontal).apply {
            max = 1000
            progress = (fraction.coerceIn(0.0, 1.0) * 1000).toInt()
            progressTintList = ColorStateList.valueOf(if (warn) p.warn else p.accent)
            progressBackgroundTintList = ColorStateList.valueOf(p.track)
            layoutParams = LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, dp(10))
        }

    fun spinner(): ProgressBar = ProgressBar(ctx).apply {
        isIndeterminate = true
        indeterminateTintList = ColorStateList.valueOf(p.accent)
    }
}

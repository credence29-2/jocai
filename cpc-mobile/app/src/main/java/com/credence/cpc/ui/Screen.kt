package com.credence.cpc.ui

import android.view.View
import com.credence.cpc.MainActivity

/** One tab of the app. */
abstract class Screen(val act: MainActivity) {
    val ui: Ui get() = act.ui
    abstract val root: View

    /** Called whenever the tab becomes visible or the app returns to the front. */
    open fun onShow() {}
}

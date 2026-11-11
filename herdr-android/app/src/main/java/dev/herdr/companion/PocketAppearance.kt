package dev.herdr.companion

import android.app.Activity
import android.content.Context
import androidx.core.view.WindowCompat

internal data class PocketPalette(
    val background: Int,
    val panel: Int,
    val raised: Int,
    val ink: Int,
    val muted: Int,
    val accent: Int,
    val onAccent: Int,
    val border: Int,
    val selected: Int,
    val warning: Int,
    val error: Int,
)

internal enum class PocketTheme(val id: String, val title: String, val description: String, val dark: Boolean, val style: Int, val colors: PocketPalette) {
    MINT("mint", "薄荷深色", "当前默认 · 柔和薄荷绿", true, R.style.Theme_HerdrPocket, PocketPalette(
        0xff111417.toInt(), 0xff1b2025.toInt(), 0xff252c32.toInt(), 0xffedf0f3.toInt(), 0xffa5afb9.toInt(), 0xffa6dbc9.toInt(), 0xff112b23.toInt(), 0xff354049.toInt(), 0xff263b34.toInt(), 0xffebc18c.toInt(), 0xffffb0ac.toInt()
    )),
    MIDNIGHT("midnight", "午夜蓝", "深蓝背景 · 清晰冷蓝", true, R.style.Theme_HerdrPocket_Midnight, PocketPalette(
        0xff101826.toInt(), 0xff172235.toInt(), 0xff21304a.toInt(), 0xffe7eef9.toInt(), 0xffacbdd5.toInt(), 0xff9cc7ff.toInt(), 0xff102542.toInt(), 0xff405674.toInt(), 0xff243b58.toInt(), 0xfff1c68e.toInt(), 0xffffb4ab.toInt()
    )),
    PLUM("plum", "石墨紫", "石墨灰底 · 柔和薰衣草紫", true, R.style.Theme_HerdrPocket_Plum, PocketPalette(
        0xff191620.toInt(), 0xff231f2d.toInt(), 0xff302938.toInt(), 0xfff1eaf7.toInt(), 0xffbcb0c9.toInt(), 0xffd4b7f5.toInt(), 0xff2f194a.toInt(), 0xff50445c.toInt(), 0xff3b2e49.toInt(), 0xffefc58e.toInt(), 0xffffb2bb.toInt()
    )),
    CLOUD("cloud", "云白蓝", "明亮浅色 · 经典蓝色", false, R.style.Theme_HerdrPocket_Cloud, PocketPalette(
        0xfff4f7fb.toInt(), 0xffffffff.toInt(), 0xffe9eef5.toInt(), 0xff1c293a.toInt(), 0xff4d6075.toInt(), 0xff19569f.toInt(), 0xffffffff.toInt(), 0xffa4b3c5.toInt(), 0xffdbe9fa.toInt(), 0xff80520d.toInt(), 0xffae3038.toInt()
    )),
    PAPER("paper", "暖纸棕", "暖白纸色 · 沉稳棕色", false, R.style.Theme_HerdrPocket_Paper, PocketPalette(
        0xfff8f4ed.toInt(), 0xfffffdf7.toInt(), 0xffeee7dc.toInt(), 0xff30291f.toInt(), 0xff655848.toInt(), 0xff775026.toInt(), 0xffffffff.toInt(), 0xffb9a891.toInt(), 0xffe8ddcc.toInt(), 0xff78500b.toInt(), 0xffa83432.toInt()
    ));

    companion object {
        fun fromId(id: String?) = entries.firstOrNull { it.id == id } ?: MINT
    }
}

/** Local display settings only. Never recreate an Activity or touch Relay/credential state. */
internal object PocketAppearance {
    private const val PREFERENCES = "pocket-appearance"
    private const val THEME = "theme"
    var current: PocketTheme = PocketTheme.MINT
        private set

    fun load(context: Context): PocketTheme {
        val saved = runCatching { context.getSharedPreferences(PREFERENCES, Context.MODE_PRIVATE).getString(THEME, null) }.getOrNull()
        current = PocketTheme.fromId(saved)
        return current
    }

    fun save(context: Context, choice: PocketTheme): Boolean {
        if (!context.getSharedPreferences(PREFERENCES, Context.MODE_PRIVATE).edit().putString(THEME, choice.id).commit()) return false
        current = choice
        return true
    }

    fun applyWindow(activity: Activity) {
        activity.setTheme(current.style)
        activity.window.statusBarColor = current.colors.background
        activity.window.navigationBarColor = current.colors.background
        WindowCompat.getInsetsController(activity.window, activity.window.decorView).apply {
            isAppearanceLightStatusBars = !current.dark
            isAppearanceLightNavigationBars = !current.dark
        }
    }
}

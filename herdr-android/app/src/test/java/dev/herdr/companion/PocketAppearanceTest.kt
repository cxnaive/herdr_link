package dev.herdr.companion

import android.app.AlertDialog
import android.content.Context
import android.graphics.drawable.ColorDrawable
import android.view.ContextThemeWrapper
import android.view.View
import android.view.ViewGroup
import android.widget.RadioButton
import androidx.core.graphics.ColorUtils
import org.junit.After
import org.junit.Assert.*
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [28, 35])
class PocketAppearanceTest {
    private val app get() = RuntimeEnvironment.getApplication() as Context
    @Before fun setup() { app.getSharedPreferences("pocket-appearance", 0).edit().clear().commit(); PocketAppearance.load(app) }
    @After fun cleanup() = setup()
    private fun views(view: View): List<View> = listOf(view) + if (view is ViewGroup) (0 until view.childCount).flatMap { views(view.getChildAt(it)) } else emptyList()
    private fun color(context: Context, attr: Int): Int = context.obtainStyledAttributes(intArrayOf(attr)).let { values -> try { values.getColor(0, 0) } finally { values.recycle() } }

    @Test fun existingDarkPaletteIsTheDefaultAndUnknownSettingsFallBackSafely() {
        assertEquals(PocketTheme.MINT, PocketAppearance.current)
        assertEquals(0xff111417.toInt(), AgentStyle.background)
        assertEquals(0xffa6dbc9.toInt(), AgentStyle.accent)
        app.getSharedPreferences("pocket-appearance", 0).edit().putString("theme", "removed-palette").commit()
        assertEquals(PocketTheme.MINT, PocketAppearance.load(app))
        app.getSharedPreferences("pocket-appearance", 0).edit().putInt("theme", 7).commit()
        assertEquals(PocketTheme.MINT, PocketAppearance.load(app))
    }

    @Test fun everyThemePersistsAndMatchesItsNativeControlsAndDialogPalette() {
        for (theme in PocketTheme.entries) {
            assertTrue(PocketAppearance.save(app, theme))
            assertEquals(theme, PocketAppearance.load(app))
            assertEquals(theme.colors.background, AgentStyle.background)
            assertEquals(theme.colors.ink, AgentStyle.ink)
            val context = ContextThemeWrapper(app, theme.style)
            for (pair in listOf(android.R.attr.colorBackground to theme.colors.background, android.R.attr.textColorPrimary to theme.colors.ink,
                android.R.attr.textColorSecondary to theme.colors.muted, android.R.attr.colorAccent to theme.colors.accent)) {
                assertEquals("${theme.id} attr ${pair.first}", pair.second, color(context, pair.first))
            }
            val dialog = AlertDialog.Builder(context).setTitle("Synthetic dialog").create()
            assertEquals(theme.colors.panel, color(dialog.context, android.R.attr.colorBackground))
            assertEquals(theme.colors.ink, color(dialog.context, android.R.attr.textColorPrimary))
            assertEquals(theme.colors.accent, color(dialog.context, android.R.attr.colorAccent))
            val terminal = NativeTerminalView(context)
            assertEquals("ANSI canvas must remain dark", AgentStyle.terminalBackground, (terminal.background as ColorDrawable).color)
        }
    }

    @Test fun everyPaletteHasReadableTextAndStatusContrast() {
        for (theme in PocketTheme.entries) {
            val p = theme.colors
            for (background in listOf(p.background, p.panel, p.raised, p.selected)) {
                for (text in listOf(p.ink, p.muted, p.accent, p.warning, p.error)) {
                    assertTrue("${theme.id}: contrast ${ColorUtils.calculateContrast(text, background)}", ColorUtils.calculateContrast(text, background) >= 4.5)
                }
            }
            assertTrue("${theme.id}: primary button text", ColorUtils.calculateContrast(p.onAccent, p.accent) >= 4.5)
        }
    }

    @Test fun previewAndCancelDoNotSaveOrChangeTheLiveTheme() {
        var applied: PocketTheme? = null
        val context = ContextThemeWrapper(app, PocketTheme.MINT.style)
        val dialog = AppearanceDialog.show(context) { applied = it }
        try {
            for (theme in PocketTheme.entries) {
                views(dialog.window!!.decorView).filterIsInstance<RadioButton>().single { it.tag == theme.id }.performClick()
                assertEquals(PocketTheme.MINT, PocketAppearance.current)
                assertTrue(views(dialog.window!!.decorView).any { it.contentDescription == "${theme.title}配色预览" })
            }
            dialog.getButton(AlertDialog.BUTTON_NEGATIVE).performClick()
            assertNull(applied)
            assertEquals(PocketTheme.MINT, PocketAppearance.load(app))
        } finally { dialog.dismiss() }
    }
}

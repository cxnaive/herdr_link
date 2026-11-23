package dev.herdr.companion

import android.app.Activity
import android.app.AlertDialog
import android.content.ClipboardManager
import android.content.Context
import android.graphics.Bitmap
import android.graphics.Canvas
import android.os.Build
import android.os.Looper
import android.text.Spanned
import android.text.style.ClickableSpan
import android.view.View
import android.widget.TextView
import io.noties.markwon.Markwon
import io.noties.markwon.ext.latex.JLatexAsyncDrawableSpan
import io.noties.markwon.ext.tables.TableAwareMovementMethod
import io.noties.markwon.ext.tables.TablePlugin
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
import org.robolectric.RobolectricTestRunner
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import org.robolectric.shadows.ShadowAlertDialog
import java.io.File
import java.time.Duration

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [28, 35], qualifiers = "w360dp-h800dp-xhdpi")
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class PocketMathUiTest {
    private fun layout(text: TextView) {
        text.measure(View.MeasureSpec.makeMeasureSpec(720, View.MeasureSpec.EXACTLY), View.MeasureSpec.makeMeasureSpec(0, View.MeasureSpec.UNSPECIFIED))
        text.layout(0, 0, 720, text.measuredHeight)
        text.draw(Canvas(Bitmap.createBitmap(720, text.height.coerceAtLeast(1), Bitmap.Config.ARGB_8888)))
    }
    private fun spans(text: TextView): List<JLatexAsyncDrawableSpan> {
        val s = text.text as Spanned
        return s.getSpans(0, s.length, JLatexAsyncDrawableSpan::class.java).toList()
    }
    private fun settle(text: TextView) {
        val end = System.nanoTime() + 15_000_000_000L
        while (System.nanoTime() < end) {
            shadowOf(Looper.getMainLooper()).idleFor(Duration.ofMillis(30)); layout(text)
            if (spans(text).all { it.drawable.hasResult() }) return
            Thread.sleep(10)
        }
        fail("Formulas did not render: " + spans(text).filter { !it.drawable.hasResult() }.map { it.drawable.destination })
    }
    @Test fun bayesFormulasRenderNativelyAcrossThemesAndCanBeEnlargedAndCopied() {
        val controller = Robolectric.buildActivity(Activity::class.java).setup()
        val activity = controller.get()
        val previous = PocketAppearance.current
        try {
            val source = File("../../testdata/client-math/bayes.md").readText()
            for (theme in PocketTheme.entries) {
                PocketAppearance.save(activity, theme); activity.setTheme(theme.style)
                val text = TextView(activity).apply {
                    textSize = 16f; setTextColor(AgentStyle.ink); setBackgroundColor(AgentStyle.background)
                    setPadding(24, 24, 24, 24); setTextIsSelectable(true)
                }
                activity.setContentView(text)
                val md = Markwon.builder(activity).usePlugin(PocketMarkdown(activity)).usePlugin(CodexFileCitations.plugin())
                    .usePlugin(PocketMath(activity, text.textSize)).usePlugin(TablePlugin.create(activity)).build()
                md.setMarkdown(text, source); text.movementMethod = TableAwareMovementMethod.create()
                assertEquals(11, spans(text).size)
                settle(text)
                for (span in spans(text)) {
                    assertTrue(span.drawable.bounds.width() > 0)
                    assertTrue(span.drawable.bounds.width() <= 720)
                }
                val bitmap = Bitmap.createBitmap(720, text.height, Bitmap.Config.ARGB_8888)
                text.draw(Canvas(bitmap))
                val output = File("build/reports/math/sdk-${Build.VERSION.SDK_INT}/${theme.id}.png").apply { parentFile!!.mkdirs() }
                output.outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
                val s = text.text as Spanned
                val formula = spans(text).last()
                val click = s.getSpans(s.getSpanStart(formula), s.getSpanEnd(formula), ClickableSpan::class.java).single()
                val oldBounds = android.graphics.Rect(formula.drawable.result.bounds)
                click.onClick(text)
                val dialog = ShadowAlertDialog.getLatestAlertDialog()
                assertTrue(dialog.isShowing)
                dialog.getButton(AlertDialog.BUTTON_POSITIVE).performClick()
                shadowOf(Looper.getMainLooper()).idle()
                val clipboard = activity.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
                assertEquals("\\[\n${formula.drawable.destination}\n\\]", clipboard.primaryClip!!.getItemAt(0).text.toString())
                assertEquals(oldBounds, formula.drawable.result.bounds)
            }
        } finally { PocketAppearance.save(activity, previous); controller.pause().stop().destroy() }
    }
    @Test fun streamClosureTablesAndUnknownCommandsPreserveText() {
        val controller = Robolectric.buildActivity(Activity::class.java).setup(); val activity = controller.get()
        try {
            val text = TextView(activity).apply { textSize = 16f; setTextIsSelectable(true) }
            activity.setContentView(text)
            val md = Markwon.builder(activity).usePlugin(PocketMarkdown(activity)).usePlugin(CodexFileCitations.plugin())
                .usePlugin(PocketMath(activity)).usePlugin(TablePlugin.create(activity)).build()
            md.setMarkdown(text, "\\[\nx_i")
            assertTrue(spans(text).isEmpty()); assertTrue(text.text.toString().contains("\\[\nx_i"))
            md.setMarkdown(text, "\\[\nx_i\n\\]")
            assertEquals(1, spans(text).size); settle(text)
            md.setMarkdown(text, "\\(\\unknown{x}\\) 后续正文")
            layout(text); shadowOf(Looper.getMainLooper()).idleFor(Duration.ofMillis(100))
            assertTrue(text.text.toString().contains("\\unknown{x}")); assertTrue(text.text.toString().contains("后续正文"))
            val parsed = md.parse("| 名称 | 公式 |\n| --- | --- |\n| 分数 | \\(\\frac{1}{2}\\) |")
            // TablePlugin owns cell layout; the math node must survive its parser.
            var count = 0
            parsed.accept(object : org.commonmark.node.AbstractVisitor() { override fun visit(n: org.commonmark.node.CustomNode) {
                if (n is io.noties.markwon.ext.latex.JLatexMathNode) count++; super.visit(n)
            } })
            assertEquals(1, count)
        } finally { controller.pause().stop().destroy() }
    }
}

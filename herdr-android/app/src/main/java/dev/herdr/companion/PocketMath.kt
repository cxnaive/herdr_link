package dev.herdr.companion

import android.app.AlertDialog
import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.graphics.Canvas
import android.graphics.Rect
import android.graphics.drawable.Drawable
import android.text.Spannable
import android.text.Spanned
import android.text.TextPaint
import android.text.style.ClickableSpan
import android.util.TypedValue
import android.view.View
import android.widget.HorizontalScrollView
import android.widget.ScrollView
import android.widget.TextView
import io.noties.markwon.AbstractMarkwonPlugin
import io.noties.markwon.MarkwonVisitor
import io.noties.markwon.ext.latex.JLatexAsyncDrawableSpan
import io.noties.markwon.ext.latex.JLatexMathPlugin
import java.util.concurrent.Executors
import org.commonmark.node.Node
import org.commonmark.parser.Parser
import ru.noties.jlatexmath.JLatexMathAndroid

/** Reuse Markwon's asynchronous drawables, not its conflicting inline parser. */
internal class PocketMath(context: Context, textSize: Float = TypedValue.applyDimension(TypedValue.COMPLEX_UNIT_SP, 16f, context.resources.displayMetrics)) : AbstractMarkwonPlugin() {
    private val renderer: JLatexMathPlugin
    init {
        JLatexMathAndroid.init(context)
        renderer = JLatexMathPlugin.create(textSize) { builder ->
            builder.inlinesEnabled(true).executorService(executor)
            builder.theme().textColor(AgentStyle.ink)
            // Unsupported TeX stays readable as source; never replace it by an empty image.
            builder.errorHandler { _, _ -> null }
        }
    }
    override fun configureParser(builder: Parser.Builder) {
        CodexFileCitations.configure(builder, math = true)
        builder.customBlockParserFactory(PocketMathSyntax.BlockFactory())
    }
    override fun beforeRender(node: Node) = PocketMathSyntax.finish(node)
    override fun configureVisitor(builder: MarkwonVisitor.Builder) = renderer.configureVisitor(builder)
    override fun beforeSetText(textView: TextView, markdown: Spanned) = renderer.beforeSetText(textView, markdown)
    override fun afterSetText(textView: TextView) {
        val text = textView.text as? Spannable
        text?.getSpans(0, text.length, JLatexAsyncDrawableSpan::class.java)?.forEach { span ->
            val start = text.getSpanStart(span); val end = text.getSpanEnd(span)
            text.setSpan(object : ClickableSpan() {
                override fun updateDrawState(paint: TextPaint) { paint.isUnderlineText = false }
                override fun onClick(widget: View) {
                    val drawable = span.drawable
                    val image = drawable.result ?: return
                    showFormula(widget.context, drawable.destination, image)
                }
            }, start, end, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
        }
        renderer.afterSetText(textView)
    }
    private fun showFormula(context: Context, tex: String, image: Drawable) {
        val padding = AgentStyle.dp(context, 20)
        val width = (image.intrinsicWidth * 1.5f).toInt().coerceAtLeast(1)
        val height = (image.intrinsicHeight * 1.5f).toInt().coerceAtLeast(1)
        val formula = object : View(context) {
            init { contentDescription = tex; setBackgroundColor(AgentStyle.panel) }
            override fun onMeasure(w: Int, h: Int) { setMeasuredDimension(width + padding * 2, height + padding * 2) }
            override fun onDraw(canvas: Canvas) {
                val old = Rect(image.bounds)
                val save = canvas.save()
                try {
                    canvas.translate(padding.toFloat(), padding.toFloat())
                    canvas.scale(1.5f, 1.5f)
                    image.setBounds(0, 0, image.intrinsicWidth, image.intrinsicHeight); image.draw(canvas)
                } finally { image.bounds = old; canvas.restoreToCount(save) }
            }
        }
        val horizontal = HorizontalScrollView(context).apply { addView(formula) }
        val scroll = ScrollView(context).apply { addView(horizontal) }
        AlertDialog.Builder(context).setTitle("公式 · 可横向滚动").setView(scroll)
            .setPositiveButton("复制 TeX") { _, _ ->
                (context.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager).setPrimaryClip(ClipData.newPlainText("TeX", "\\[\n$tex\n\\]"))
            }.setNegativeButton("关闭", null).show()
    }
    companion object {
        // JLaTeXMath has shared font/parser state, so use one worker, not its
        // default unbounded cached thread pool. Syntax limits reject macros.
        private val executor = Executors.newSingleThreadExecutor { task -> Thread(task, "pocket-math").apply { isDaemon = true } }
    }
}

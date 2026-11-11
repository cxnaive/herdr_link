package dev.herdr.companion

import android.app.AlertDialog
import android.content.Context
import android.graphics.Color
import android.os.Handler
import android.os.Looper
import android.text.InputType
import android.view.Gravity
import android.widget.*
import java.util.concurrent.Executors

/** One page at a time, off the UI thread; zoom re-renders within a bounded pixel budget. */
class PdfPreview(context: Context, bytes: ByteArray, initialPage: Int = 1, private val openPages: (ByteArray) -> PdfPages = { AndroidPdfPages(context, it) }) : LinearLayout(context) {
    private val worker = Executors.newSingleThreadExecutor()
    private val main = Handler(Looper.getMainLooper())
    private var pages: PdfPages? = null
    private var pageIndex = -1
    private var pageCount = 0
    @Volatile private var generation = 0
    private var renderedWidth = 0
    @Volatile private var closed = false
    val picture = ZoomImageView(context)
    private val status = TextView(context).apply { text = "正在打开 PDF…"; setTextColor(AgentStyle.ink); textSize = 13f; gravity = Gravity.CENTER }
    private val previous = action("‹", "上一页") { render(pageIndex - 1, true) }
    private val next = action("›", "下一页") { render(pageIndex + 1, true) }
    private val zoom = action("适合", "还原 PDF 缩放") { picture.fitScreen() }
    private fun dp(n: Int) = (n * resources.displayMetrics.density).toInt()
    private fun action(label: String, description: String, click: () -> Unit) = Button(context).apply { text = label; textSize = 12f; isAllCaps = false; setTextColor(AgentStyle.ink); background = android.graphics.drawable.InsetDrawable(AgentStyle.shape(context, AgentStyle.raised, 12), dp(3), dp(4), dp(3), dp(4)); minWidth = 0; minimumWidth = 0; setPadding(0, 0, 0, 0); contentDescription = description; setOnClickListener { click() } }
    init {
        orientation = VERTICAL
        addView(LinearLayout(context).apply {
            gravity = Gravity.CENTER_VERTICAL
            addView(previous, LayoutParams(dp(48), dp(48))); addView(status, LayoutParams(0, dp(48), 1f)); addView(next, LayoutParams(dp(48), dp(48)))
            addView(action("−", "缩小 PDF") { picture.changeZoom(picture.viewport.zoom / 1.5f) }, LayoutParams(dp(48), dp(48)))
            addView(zoom, LayoutParams(dp(64), dp(48)))
            addView(action("+", "放大 PDF") { picture.changeZoom(picture.viewport.zoom * 1.5f) }, LayoutParams(dp(48), dp(48)))
        })
        addView(picture, LayoutParams(-1, 0, 1f))
        previous.isEnabled = false; next.isEnabled = false
        status.contentDescription = "PDF 页码，点按跳页"
        status.setOnClickListener {
            if (pageCount <= 0 || closed) return@setOnClickListener
            val input = EditText(context).apply { inputType = InputType.TYPE_CLASS_NUMBER; hint = "1–$pageCount"; setText((pageIndex + 1).toString()) }
            AlertDialog.Builder(context).setTitle("跳转到页码").setView(input).setPositiveButton("打开") { _, _ ->
                val page = input.text.toString().toIntOrNull()
                if (page != null && page in 1..pageCount) render(page - 1, true) else Toast.makeText(context, "请输入 1–$pageCount", Toast.LENGTH_SHORT).show()
            }.setNegativeButton("取消", null).show()
        }
        picture.onZoom = { zoom.text = if (it <= 1.01f) "适合" else "${(it * 100).toInt()}%" }
        picture.onSettled = { if (pageIndex >= 0) render(pageIndex, false) }
        worker.execute {
            try {
                require(bytes.size <= ReviewDecoding.MAX_BYTES)
                pages = openPages(bytes)
                val count = pages!!.count
                require(count > 0)
                main.post { if (!closed) { pageCount = count; render((initialPage - 1).coerceIn(0, (count - 1).coerceAtLeast(0)), true) } }
            } catch (_: Exception) { main.post { if (!closed) status.text = "PDF 无法打开：可能损坏或需要密码" } }
        }
    }
    private fun render(index: Int, reset: Boolean) {
        if (closed || index !in 0 until pageCount) return
        val width = (picture.width.coerceAtLeast(720) * (if (reset) 1f else picture.viewport.zoom)).toInt().coerceIn(1, 16384)
        if (!reset && width <= renderedWidth * 1.2) return
        val owner = ++generation
        previous.isEnabled = false; next.isEnabled = false
        status.text = if (reset) "正在绘制…" else "${index + 1} / $pageCount · 清晰化…"
        worker.execute {
            try {
                if (closed || owner != generation) return@execute
                val rendered = pages!!.render(index, width)
                main.post {
                    if (closed || owner != generation) { rendered.recycle(); return@post }
                    pageIndex = index; renderedWidth = width
                    picture.setBitmap(rendered, reset)
                    status.text = "${index + 1} / $pageCount"
                    previous.isEnabled = index > 0; next.isEnabled = index + 1 < pageCount
                }
            } catch (_: Exception) { main.post { if (!closed && owner == generation) { status.text = "此页无法显示"; previous.isEnabled = index > 0; next.isEnabled = index + 1 < pageCount } } }
        }
    }
    fun close() {
        if (closed) return
        closed = true; generation++; picture.onSettled = null; picture.setBitmap(null)
        worker.execute { runCatching { pages?.close() }; pages = null }; worker.shutdown()
    }
}

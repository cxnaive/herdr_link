package dev.herdr.companion

import android.content.Context
import android.graphics.Canvas
import android.view.View
import android.util.TypedValue
import androidx.core.content.res.ResourcesCompat
import com.termux.terminal.TerminalEmulator
import com.termux.terminal.TerminalOutput
import com.termux.terminal.TerminalSessionClient
import com.termux.view.TerminalRenderer
import java.lang.reflect.Proxy
import kotlin.math.ceil
import kotlin.math.roundToInt

/** Relay snapshots are laid out locally. Changing phone typography never resizes the desktop PTY. */
class NativeTerminalView(context: Context) : View(context) {
    private val preferences = context.getSharedPreferences("terminal-appearance", Context.MODE_PRIVATE)
    private var size = preferences.getFloat("font-sp", 14f).coerceIn(10f, 22f)
    private val terminalFont = ResourcesCompat.getFont(context, R.font.inconsolata)!!
    private var renderer = TerminalRenderer(px(), terminalFont)
    private var columns = 80
    private var sourceColumns = 80
    private var viewportWidth = 0
    private var raw = ""
    var wrapToPhone: Boolean = preferences.getBoolean("phone-wrap", true); private set
    private var emulator = emulator(2)
    private val inset get() = (10 * resources.displayMetrics.density).roundToInt()
    val cellWidth get() = renderer.fontWidth
    val lineHeight get() = renderer.fontLineSpacing
    val fontSizeSp get() = size
    val displayedRows get() = emulator.rows

    private fun emulator(rows: Int): TerminalEmulator {
        val silent = Proxy.newProxyInstance(TerminalSessionClient::class.java.classLoader, arrayOf(TerminalSessionClient::class.java)) { _, method, _ ->
            when (method.name) { "getTerminalCursorStyle" -> 2; else -> null }
        } as TerminalSessionClient
        val sink = object : TerminalOutput() {
            override fun write(data: ByteArray?, offset: Int, count: Int) {}
            override fun titleChanged(oldTitle: String?, newTitle: String?) {}
            override fun onCopyTextToClipboard(text: String?) {}
            override fun onPasteTextFromClipboard() {}
            override fun onBell() {}
            override fun onColorsChanged() {}
        }
        return TerminalEmulator(sink, columns, rows, ceil(cellWidth).toInt(), lineHeight, (rows + 16).coerceAtLeast(100), silent)
    }
    init {
        setBackgroundColor(AgentStyle.terminalBackground)
        contentDescription = "原生终端输出，可横向和纵向滚动"
    }
    private fun px() = TypedValue.applyDimension(TypedValue.COMPLEX_UNIT_SP, size, resources.displayMetrics).roundToInt().coerceAtLeast(1)
    fun show(content: String, width: Int = 0) {
        if (content == raw && (width <= 0 || width == sourceColumns)) return
        val nextColumns = if (width > 0) width.coerceIn(20, 512) else TerminalLayout.sourceColumns(content)
        if (content == raw && nextColumns == sourceColumns) return
        raw = content; sourceColumns = nextColumns; rebuild()
    }
    fun setViewportWidth(width: Int) {
        if (viewportWidth == width || width <= 0) return
        viewportWidth = width
        if (wrapToPhone) rebuild() else requestLayout()
    }
    fun toggleWrap() {
        wrapToPhone = !wrapToPhone
        preferences.edit().putBoolean("phone-wrap", wrapToPhone).apply()
        rebuild()
    }
    fun fontSize(delta: Int) {
        size = (size + delta).coerceIn(10f, 22f)
        preferences.edit().putFloat("font-sp", size).apply()
        renderer = TerminalRenderer(px(), terminalFont)
        rebuild()
    }
    private fun rebuild() {
        columns = if (wrapToPhone && viewportWidth > 0) TerminalLayout.columns(viewportWidth, inset * 2, cellWidth) else sourceColumns
        val snapshot = if (wrapToPhone) TerminalLayout.phoneSnapshot(raw) else raw
        emulator = emulator(TerminalLayout.rows(snapshot, columns))
        // A read_pane snapshot has no cursor coordinate. Do not invent an extra
        // editable cursor after the last serialized row; input lives in the composer.
        val encoded = (snapshot.replace("\r\n", "\n").replace("\n", "\r\n") + "\u001b[?25l").toByteArray(Charsets.UTF_8)
        emulator.append(encoded, encoded.size)
        requestLayout(); invalidate()
    }
    fun plainText(): String = emulator.screen.transcriptText
    override fun onMeasure(widthMeasureSpec: Int, heightMeasureSpec: Int) {
        // The scroll containers need the complete document height. Use the renderer's *exact* cell metrics.
        val width = maxOf(viewportWidth, ceil(cellWidth * columns).toInt() + inset * 2)
        setMeasuredDimension(resolveSize(width, widthMeasureSpec), resolveSize(lineHeight * emulator.rows + inset * 2, heightMeasureSpec))
    }
    override fun onDraw(canvas: Canvas) {
        super.onDraw(canvas)
        canvas.save(); canvas.translate(inset.toFloat(), inset.toFloat())
        renderer.render(emulator, canvas, 0, -1, -1, -1, -1)
        canvas.restore()
    }
}

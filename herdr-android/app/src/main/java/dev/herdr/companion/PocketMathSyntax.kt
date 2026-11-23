package dev.herdr.companion

import io.noties.markwon.ext.latex.JLatexMathBlock
import io.noties.markwon.ext.latex.JLatexMathNode
import org.commonmark.node.*
import org.commonmark.parser.block.*

/** Protect TeX before CommonMark consumes backslashes, underscores or pipes. */
internal object PocketMathSyntax {
    const val MAX_SOURCE = 8192
    private data class Formula(val start: Int, val end: Int, val raw: String, val tex: String?, val display: Boolean)
    internal class RawBlock(val opener: String, val closer: String) : CustomBlock() {
        val body = StringBuilder()
        var closed = false
    }
    private val unsafe = Regex("""\\(?:def|gdef|edef|xdef|let|futurelet|csname|catcode|newcommand|renewcommand|providecommand|newenvironment|renewenvironment|input|include|includegraphics|Declare\w*|jlm\w*)\b""")
    fun safe(tex: String): Boolean {
        if (tex.isBlank() || tex.length > MAX_SOURCE || unsafe.containsMatchIn(tex)) return false
        var depth = 0; var index = 0
        while (index < tex.length) {
            when (tex[index++]) {
                '\\' -> if (index < tex.length) index++
                '{' -> if (++depth > 48) return false
                '}' -> if (--depth < 0) return false
            }
        }
        return depth == 0
    }
    private fun find(text: String): List<Formula> {
        val result = mutableListOf<Formula>()
        var i = 0; var attempts = 0
        while (i < text.length && attempts < 128) {
            if (text[i] == '\\' && text.getOrNull(i + 1) !in listOf('(', '[')) { i += 2; continue }
            if (text[i] == '`') {
                var end = i + 1
                while (end < text.length && text[end] == '`') end++
                val marker = text.substring(i, end)
                var close = text.indexOf(marker, end)
                while (close >= 0 && (text.getOrNull(close - 1) == '`' || text.getOrNull(close + marker.length) == '`')) close = text.indexOf(marker, close + marker.length)
                i = if (close >= 0) close + marker.length else end
                continue
            }
            val opener = when {
                text.startsWith("\\(", i) -> "\\("
                text.startsWith("\\[", i) -> "\\["
                text.startsWith("\$\$", i) -> "\$\$"
                text[i] == '$' && text.getOrNull(i + 1)?.let { !it.isWhitespace() && it != '$' } == true -> "\$"
                else -> ""
            }
            if (opener.isEmpty()) { i++; continue }
            val closer = when (opener) { "\\(" -> "\\)"; "\\[" -> "\\]"; else -> opener }
            val content = i + opener.length
            attempts++
            var close = -1; var cursor = content
            val limit = minOf(text.length, content + MAX_SOURCE + closer.length)
            while (cursor + closer.length <= limit) {
                if (text.startsWith(closer, cursor) && (opener != "\$" ||
                    (!text[cursor - 1].isWhitespace() && text.getOrNull(cursor - 1) != '$' && text.getOrNull(cursor + 1)?.let { it.isDigit() || it == '$' } != true))) {
                    close = cursor; break
                }
                cursor += if (text[cursor] == '\\') 2 else 1
            }
            if (close < 0) {
                // A streaming \( or \[ opener must not lose its backslash.
                if (opener.startsWith('\\')) { result += Formula(i, text.length, text.substring(i), null, false); break }
                i = content; continue
            }
            val end = close + closer.length
            val raw = text.substring(i, end)
            val tex = text.substring(content, close)
            result += Formula(i, end, raw, tex.takeIf(::safe), opener == "\\[" || opener == "\$\$")
            i = end
        }
        return result
    }
    fun parse(input: String, block: Node, delegate: (String) -> Unit) {
        val formulas = find(input)
        if (formulas.isEmpty()) { delegate(input); return }
        var prefix = "HERDRMATHTOKEN"
        while (input.contains(prefix)) prefix += "X"
        val tokens = formulas.mapIndexed { index, formula -> "${prefix}${index}END" to formula }.toMap()
        val protected = StringBuilder(); var offset = 0
        tokens.forEach { (token, formula) -> protected.append(input, offset, formula.start).append(token); offset = formula.end }
        protected.append(input, offset, input.length)
        delegate(protected.toString())
        val pattern = Regex(tokens.keys.joinToString("|") { Regex.escape(it) })
        fun original(value: String) = pattern.replace(value) { tokens.getValue(it.value).raw }
        fun inLink(node: Node): Boolean {
            var parent = node.parent
            while (parent != null) { if (parent is Link || parent is Image) return true; parent = parent.parent }
            return false
        }
        block.accept(object : AbstractVisitor() {
            override fun visit(text: Text) {
                if (inLink(text)) { text.literal = original(text.literal); return }
                val literal = text.literal; val matches = pattern.findAll(literal).toList()
                if (matches.isEmpty()) return
                var start = 0
                for (match in matches) {
                    if (match.range.first > start) text.insertBefore(Text(literal.substring(start, match.range.first)))
                    val f = tokens.getValue(match.value)
                    val node = if (f.tex == null) Text(f.raw) else if (f.display) JLatexMathBlock().apply { latex(f.tex) } else JLatexMathNode().apply { latex(f.tex) }
                    text.insertBefore(node)
                    start = match.range.last + 1
                }
                if (start < literal.length) text.insertBefore(Text(literal.substring(start)))
                text.unlink()
            }
            override fun visit(code: Code) { code.literal = original(code.literal) }
            override fun visit(html: HtmlInline) { html.literal = original(html.literal) }
            override fun visit(link: Link) { link.destination = original(link.destination); link.title = link.title?.let(::original); super.visit(link) }
            override fun visit(image: Image) { image.destination = original(image.destination); image.title = image.title?.let(::original); super.visit(image) }
        })
    }
    fun finish(root: Node) {
        root.accept(object : AbstractVisitor() {
            override fun visit(block: CustomBlock) {
                if (block !is RawBlock) { super.visit(block); return }
                val tex = block.body.toString().trimEnd('\n')
                val node = if (block.closed && safe(tex)) JLatexMathBlock().apply { latex(tex) }
                    else Paragraph().apply { appendChild(Text(block.opener + "\n" + tex + if (block.closed) "\n" + block.closer else "")) }
                block.insertBefore(node); block.unlink()
            }
        })
    }
    class BlockFactory : AbstractBlockParserFactory() {
        override fun tryStart(state: ParserState, matched: MatchedBlockParser): BlockStart? {
            if (state.indent >= 4) return BlockStart.none()
            val opener = state.line.subSequence(state.nextNonSpaceIndex, state.line.length).toString().trimEnd()
            if (opener != "\\[" && opener != "\$\$") return BlockStart.none()
            val block = RawBlock(opener, if (opener == "\\[") "\\]" else opener)
            val parser = object : AbstractBlockParser() {
                private var opening = true
                override fun getBlock(): Block = block
                override fun tryContinue(next: ParserState): BlockContinue {
                    if (next.indent < 4 && next.line.subSequence(next.nextNonSpaceIndex, next.line.length).toString().trimEnd() == block.closer) {
                        block.closed = true; return BlockContinue.finished()
                    }
                    return BlockContinue.atIndex(next.index)
                }
                override fun addLine(line: CharSequence) {
                    if (opening) opening = false else block.body.append(line).append('\n')
                }
            }
            return BlockStart.of(parser).atIndex(state.line.length + 1)
        }
    }
}

package dev.herdr.companion

import io.noties.markwon.AbstractMarkwonPlugin
import org.commonmark.internal.InlineParserImpl
import org.commonmark.node.AbstractVisitor
import org.commonmark.node.Code
import org.commonmark.node.HtmlInline
import org.commonmark.node.Image
import org.commonmark.node.Link
import org.commonmark.node.Node
import org.commonmark.node.Text
import org.commonmark.parser.InlineParser
import org.commonmark.parser.Parser

/** Codex's file directive is not standard Markdown. Preserve its raw path until
 * inline parsing finishes, then create a real Link node rather than interpolating Markdown. */
object CodexFileCitations {
    private const val prefix = ":codex-file-citation{"
    private data class Citation(val start: Int, val end: Int, val raw: String, val path: String)

    fun configure(builder: Parser.Builder) = configure(builder, false)

    internal fun configure(builder: Parser.Builder, math: Boolean) {
        builder.inlineParserFactory { context ->
            // The app pins CommonMark 0.13.0 (via Markwon 4.6.2). Delegate all normal
            // Markdown to its existing parser; only the file directive is extended.
            val delegate = InlineParserImpl(context)
            InlineParser { input, block ->
                fun parse(value: String) {
                    val input = value
                    val citations = find(input)
                    if (citations.isEmpty()) delegate.parse(input, block)
                    else {
                        var tokenPrefix = "HERDRFILECITATIONTOKEN"
                        while (input.contains(tokenPrefix)) tokenPrefix += "X"
                        val tokens = citations.mapIndexed { index, citation -> "${tokenPrefix}${index}END" to citation }.toMap()
                        val protected = StringBuilder()
                        var offset = 0
                        tokens.forEach { (token, citation) ->
                            protected.append(input, offset, citation.start).append(token); offset = citation.end
                        }
                        protected.append(input, offset, input.length)
                        delegate.parse(protected.toString(), block)
                        restore(block, tokens)
                    }
                }
                if (math) PocketMathSyntax.parse(input, block, ::parse) else parse(input)
            }
        }
    }

    fun plugin() = object : AbstractMarkwonPlugin() {
        override fun configureParser(builder: Parser.Builder) = configure(builder)
    }

    private fun find(input: String): List<Citation> {
        val result = mutableListOf<Citation>()
        var offset = 0
        var attempts = 0
        while (result.size < 64 && attempts++ < 128) {
            val start = input.indexOf(prefix, offset)
            if (start < 0) break
            offset = start + prefix.length
            var slashes = 0; var before = start - 1
            while (before >= 0 && input[before--] == '\\') slashes++
            if (slashes % 2 != 0) continue
            val citation = parse(input, start) ?: continue
            result += citation; offset = citation.end
        }
        return result
    }

    private fun parse(input: String, start: Int): Citation? {
        var offset = start + prefix.length
        val limit = minOf(input.length, start + 8192)
        val attributes = linkedMapOf<String, String>()
        while (offset < limit) {
            while (offset < limit && input[offset] in " \t") offset++
            if (offset >= limit) return null
            if (input[offset] == '}') {
                val path = attributes["path"]?.takeIf { it.isNotBlank() } ?: return null
                return Citation(start, offset + 1, input.substring(start, offset + 1), path)
            }
            val keyStart = offset
            while (offset < limit && (input[offset].isLetterOrDigit() || input[offset] in "_-")) offset++
            if (offset == keyStart || attributes.size >= 16) return null
            val key = input.substring(keyStart, offset)
            if (attributes.containsKey(key)) return null
            while (offset < limit && input[offset] in " \t") offset++
            if (offset >= limit || input[offset++] != '=') return null
            while (offset < limit && input[offset] in " \t") offset++
            if (offset >= limit || input[offset] !in "\"'") return null
            val quote = input[offset++]
            val value = StringBuilder()
            var quoted = false
            while (offset < limit) {
                val char = input[offset++]
                if (char == quote) { quoted = true; break }
                if (char.code < 32) return null
                if (char == '\\') {
                    if (offset >= limit || (input[offset] != quote && input[offset] != '\\')) return null
                    value.append(input[offset++])
                } else value.append(char)
            }
            if (!quoted || offset >= limit || input[offset] !in " \t}") return null
            attributes[key] = value.toString()
        }
        return null
    }

    private fun restore(block: Node, tokens: Map<String, Citation>) {
        val pattern = Regex(tokens.keys.joinToString("|") { Regex.escape(it) })
        fun original(value: String) = pattern.replace(value) { tokens.getValue(it.value).raw }
        fun insideLink(node: Node): Boolean {
            var parent = node.parent
            while (parent != null) { if (parent is Link || parent is Image) return true; parent = parent.parent }
            return false
        }
        block.accept(object : AbstractVisitor() {
            override fun visit(text: Text) {
                val literal = text.literal
                if (insideLink(text)) { text.literal = original(literal); return }
                val matches = pattern.findAll(literal).toList()
                if (matches.isEmpty()) return
                var offset = 0
                for (match in matches) {
                    if (match.range.first > offset) text.insertBefore(Text(literal.substring(offset, match.range.first)))
                    val citation = tokens.getValue(match.value)
                    text.insertBefore(Link(citation.path, null).apply { appendChild(Text(FileRules.safeName(citation.path))) })
                    offset = match.range.last + 1
                }
                if (offset < literal.length) text.insertBefore(Text(literal.substring(offset)))
                text.unlink()
            }
            override fun visit(code: Code) { code.literal = original(code.literal) }
            override fun visit(html: HtmlInline) { html.literal = original(html.literal) }
            override fun visit(link: Link) { link.destination = original(link.destination); link.title = link.title?.let(::original); super.visit(link) }
            override fun visit(image: Image) { image.destination = original(image.destination); image.title = image.title?.let(::original); super.visit(image) }
        })
    }
}

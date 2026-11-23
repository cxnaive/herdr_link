package dev.herdr.companion

import io.noties.markwon.ext.latex.JLatexMathBlock
import io.noties.markwon.ext.latex.JLatexMathNode
import org.commonmark.node.*
import org.commonmark.parser.Parser
import org.junit.Assert.*
import org.junit.Test
import java.io.File

class PocketMathSyntaxTest {
    private val parser = Parser.builder().also { CodexFileCitations.configure(it, true); it.customBlockParserFactory(PocketMathSyntax.BlockFactory()) }.build()
    private fun parse(source: String) = parser.parse(source).also(PocketMathSyntax::finish)
    private fun nodes(node: Node): List<Node> {
        val result = mutableListOf(node); var child = node.firstChild
        while (child != null) { result += nodes(child); child = child.next }
        return result
    }
    private fun math(source: String) = nodes(parse(source)).filter { it is JLatexMathNode || it is JLatexMathBlock }
    @Test fun acceptsBothDelimiterFamiliesAndPreservesTeX() {
        val source = """\frac{1}{\sigma_A^2} + \mathbf x"""
        for ((start, end) in listOf("\\(" to "\\)", "$" to "$", "\\[" to "\\]", "$$" to "$$")) {
            for (wrapped in listOf("前 $start$source$end 后") + if (start == "$") emptyList() else listOf("$start\n$source\n$end")) {
                val found = math(wrapped)
                assertEquals(wrapped, 1, found.size)
                val n = found.single()
                val tex = if (n is JLatexMathNode) n.latex() else (n as JLatexMathBlock).latex()
                assertEquals(wrapped, source, tex.trim())
            }
        }
    }
    @Test fun bayesFixtureContainsEightInlineAndThreeDisplayFormulas() {
        val document = parse(File("../../testdata/client-math/bayes.md").readText())
        val all = nodes(document)
        assertEquals(8, all.filterIsInstance<JLatexMathNode>().size)
        assertEquals(3, all.filterIsInstance<JLatexMathBlock>().size)
        assertTrue(all.filterIsInstance<JLatexMathBlock>().last().latex().contains("来自观测"))
    }
    @Test fun codeEscapesCurrencyAndLinkTargetsStayLiteral() {
        val examples = listOf("`\\(x_i\\)`", "``\$x\$``", "```tex\n\\[\nx^2\n\\]\n```", "    \$x\$", "\\\\(x\\\\)", "\\\$x\\\$", "价格 \$5 和 \$10。", "[link](https://example.com/\$x\$)", "![\$x\$](image.png)")
        for (source in examples) assertTrue(source, math(source).isEmpty())
        val code = nodes(parse("`\\(x_i\\)`")).filterIsInstance<Code>().single()
        assertEquals("\\(x_i\\)", code.literal)
    }
    @Test fun incompleteStreamAndUnsafeInputsDoNotCreateDrawables() {
        for (source in listOf("\\(x_i", "\\[\n\\frac{1}{2}", "\$\$\nx_i", "\\(\\frac{1}{\\)", "\\(\\def\\x{\\x}\\x\\)", "\\(" + "x".repeat(PocketMathSyntax.MAX_SOURCE + 1) + "\\)")) assertTrue(source.take(60), math(source).isEmpty())
        val raw = nodes(parse("\\[\nx_i")).filterIsInstance<Text>().joinToString("") { it.literal }
        assertEquals("\\[\nx_i", raw)
        assertEquals(1, math("\\[\nx_i\n\\]").size)
    }
    @Test fun citationsAndFormulaTokensCannotLeakOrCorruptEachOther() {
        val source = "公式 \\(x_i\\) :codex-file-citation{path=\"docs/a_\$x\$.md\"} `HERDRMATHTOKEN0END`"
        val all = nodes(parse(source))
        assertEquals(1, all.filterIsInstance<JLatexMathNode>().size)
        assertEquals("docs/a_\$x\$.md", all.filterIsInstance<Link>().single().destination)
        assertEquals("HERDRMATHTOKEN0END", all.filterIsInstance<Code>().single().literal)
        assertFalse(all.filterIsInstance<Text>().any { it.literal.contains("HERDRMATHTOKEN") })
    }
    @Test fun blankLinesAndMarkdownWithinDisplayMathDoNotEscapeTheBlock() {
        val all = nodes(parse("\\[\n\\begin{aligned}\nx &= 1\\\\\n\ny &= 2\n\\end{aligned}\n\\]\n\n**后续正文**"))
        assertEquals(1, all.filterIsInstance<JLatexMathBlock>().size)
        assertEquals(1, all.filterIsInstance<StrongEmphasis>().size)
    }
}

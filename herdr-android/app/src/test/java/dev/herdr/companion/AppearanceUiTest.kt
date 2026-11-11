package dev.herdr.companion

import android.app.Activity
import android.app.AlertDialog
import android.content.Context
import android.graphics.Bitmap
import android.graphics.Canvas
import android.graphics.drawable.ColorDrawable
import android.os.Build
import android.os.Looper
import android.view.View
import android.view.ViewGroup
import android.widget.EditText
import android.widget.FrameLayout
import android.widget.RadioButton
import android.widget.TextView
import androidx.core.view.WindowCompat
import io.noties.markwon.Markwon
import org.json.JSONArray
import org.json.JSONObject
import org.junit.After
import org.junit.Assert.*
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import org.robolectric.shadows.ShadowAlertDialog
import java.io.File
import java.time.Duration
import java.util.concurrent.CopyOnWriteArrayList
import java.util.concurrent.TimeUnit
import javax.crypto.spec.SecretKeySpec

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [28, 35], qualifiers = "w360dp-h800dp-xhdpi")
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class AppearanceUiTest {
    private val app get() = RuntimeEnvironment.getApplication() as Context
    private fun get(owner: Any, field: String) = owner.javaClass.getDeclaredField(field).apply { isAccessible = true }.get(owner)
    private fun set(owner: Any, field: String, value: Any?) = owner.javaClass.getDeclaredField(field).apply { isAccessible = true }.set(owner, value)
    private fun views(view: View): List<View> = listOf(view) + if (view is ViewGroup) (0 until view.childCount).flatMap { views(view.getChildAt(it)) } else emptyList()
    @Before fun setup() {
        SharedRelay.resetForTest()
        for (name in listOf("pocket-appearance", "private_state", "background-monitor")) app.getSharedPreferences(name, 0).edit().clear().commit()
        PocketAppearance.load(app)
    }
    @After fun cleanup() = setup()
    private fun waitUntil(condition: () -> Boolean) {
        val end = System.nanoTime() + TimeUnit.SECONDS.toNanos(12)
        while (System.nanoTime() < end) { shadowOf(Looper.getMainLooper()).idleFor(Duration.ofMillis(20)); if (condition()) return; Thread.sleep(5) }
        fail("Appearance fixture timed out")
    }
    private fun tab(activity: MainActivity, name: String) = activity.javaClass.getDeclaredMethod("changeTab", String::class.java).apply { isAccessible = true }.invoke(activity, name)
    private fun markdown(ui: NativeAgents) = ui.javaClass.getDeclaredMethod("getRenderMarkdown").apply { isAccessible = true }.invoke(ui) as Markwon
    private fun layout(root: View, height: Int = 800, wrap: Boolean = false) {
        val width = AgentStyle.dp(root.context, 360); val pixels = AgentStyle.dp(root.context, height)
        root.measure(View.MeasureSpec.makeMeasureSpec(width, View.MeasureSpec.EXACTLY), View.MeasureSpec.makeMeasureSpec(pixels, if (wrap) View.MeasureSpec.AT_MOST else View.MeasureSpec.EXACTLY))
        root.layout(0, 0, root.measuredWidth, root.measuredHeight); shadowOf(Looper.getMainLooper()).idle()
    }
    private fun capture(root: View, name: String, wrap: Boolean = false) {
        layout(root, wrap = wrap)
        val bitmap = Bitmap.createBitmap(root.width, root.height, Bitmap.Config.ARGB_8888)
        root.draw(Canvas(bitmap))
        val path = File("build/reports/appearance/sdk-${Build.VERSION.SDK_INT}/$name.png"); path.parentFile!!.mkdirs()
        path.outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }; bitmap.recycle()
    }
    private fun showPicker(activity: MainActivity): AlertDialog {
        layout(activity.window.decorView)
        views(activity.window.decorView).single { it.contentDescription == "主题配色" }.performClick()
        shadowOf(Looper.getMainLooper()).idle()
        return ShadowAlertDialog.getLatestAlertDialog()
    }

    @Test fun themeChangesKeepTheSameEncryptedConnectionAgentDraftAndPendingWrite() {
        MockTcpMonitorRelay().use { fixture ->
            fixture.role = "controller"
            val commands = CopyOnWriteArrayList<String>()
            fixture.foregroundEnvelope = { request -> commands += request.getString("type"); null }
            val shared = SharedRelay.get(app)
            set(shared.vault, "key\$delegate", lazyOf(SecretKeySpec(ByteArray(32) { 29 }, "AES")))
            set(shared.engine, "state", fixture.state()); shared.vault.write("state", fixture.state().toString())
            val controller = Robolectric.buildActivity(MainActivity::class.java).setup()
            val activity = controller.get()
            try {
                waitUntil { shared.ready("fixture") }
                val ui = get(activity, "nativeAgents") as NativeAgents
                val agent = (get(activity, "agents") as Map<*, *>)["fixture"] as org.json.JSONArray
                // Store a genuine Agent draft before navigating to the appearance settings.
                ui.open("fixture", agent.getJSONObject(0))
                (get(ui, "composerPanel") as NativeComposerView).editor.setText("主题切换不能丢失的草稿")
                tab(activity, "devices")
                waitUntil { !shared.engine.snapshot().getJSONObject("auth").getJSONObject("relays").isNull("fixture") }
                val selected = get(ui, "selected") as JSONObject
                val target = WorkspaceBinding.fromAgent("fixture", selected)
                val delivery = "delivery:" + target.storageKey()
                shared.vault.write(delivery, "unconfirmed")
                @Suppress("UNCHECKED_CAST") val pending = get(ui, "pendingMutations") as MutableSet<String>
                pending += delivery
                val epoch = get(ui, "epoch")
                val state = shared.engine.snapshot().toString()
                Thread.sleep(120); shadowOf(Looper.getMainLooper()).idle()
                val count = commands.size; val connections = fixture.connected.get()
                val previousTheme = PocketAppearance.current
                var previousMarkdown = markdown(ui)
                for (theme in PocketTheme.entries.filter { it != previousTheme }) {
                    val dialog = showPicker(activity)
                    views(dialog.window!!.decorView).filterIsInstance<RadioButton>().single { it.tag == theme.id }.performClick()
                    assertEquals("preview is not committed", shared.engine.snapshot().toString(), state)
                    dialog.getButton(AlertDialog.BUTTON_POSITIVE).performClick()
                    layout(activity.window.decorView)
                    assertEquals(theme, PocketAppearance.current)
                    assertSame(shared.engine, get(activity, "engine")); assertSame(ui, get(activity, "nativeAgents"))
                    assertEquals(epoch, get(ui, "epoch")); assertSame(selected, get(ui, "selected"))
                    assertEquals(state, shared.engine.snapshot().toString()); assertEquals(connections, fixture.connected.get())
                    assertEquals(count, commands.size)
                    assertTrue(pending.contains(delivery)); assertEquals("unconfirmed", shared.vault.read(delivery))
                    assertEquals("主题切换不能丢失的草稿", shared.vault.read("prompt:" + target.storageKey()))
                    val nextMarkdown = markdown(ui); assertNotSame(previousMarkdown, nextMarkdown); previousMarkdown = nextMarkdown
                    val title = get(activity, "heading") as TextView
                    assertEquals(theme.colors.ink, title.currentTextColor)
                    assertEquals(theme.colors.background, ((get(activity, "root") as View).background as ColorDrawable).color)
                    val bars = WindowCompat.getInsetsController(activity.window, activity.window.decorView)
                    assertEquals(!theme.dark, bars.isAppearanceLightStatusBars)
                    assertEquals(!theme.dark, bars.isAppearanceLightNavigationBars)
                    assertFalse(activity.isFinishing); assertFalse(activity.isDestroyed)
                    capture(activity.window.decorView, "${theme.id}-devices")
                    val newDialog = showPicker(activity)
                    assertEquals(theme.colors.ink, newDialog.context.obtainStyledAttributes(intArrayOf(android.R.attr.textColorPrimary)).let { a -> try { a.getColor(0, 0) } finally { a.recycle() } })
                    capture(newDialog.window!!.decorView, "${theme.id}-picker", wrap = true)
                    newDialog.getButton(AlertDialog.BUTTON_NEGATIVE).performClick()
                }
                tab(activity, "agents"); layout(activity.window.decorView)
                val editor = (get(ui, "composerPanel") as NativeComposerView).editor
                assertEquals("主题切换不能丢失的草稿", editor.text.toString())
                assertEquals(PocketAppearance.current.colors.ink, editor.currentTextColor)
                assertTrue(pending.contains(delivery)); assertEquals("unconfirmed", shared.vault.read(delivery))
                assertTrue(fixture.failures.toString(), fixture.failures.isEmpty())
            } finally { controller.pause().stop().destroy() }
        }
    }

    @Test fun everyPaletteRendersConversationToolsComposerAndManagementInputs() {
        val controller = Robolectric.buildActivity(Activity::class.java).setup()
        val activity = controller.get()
        val vault = Vault(activity)
        set(vault, "key\$delegate", lazyOf(SecretKeySpec(ByteArray(32) { 30 }, "AES")))
        val engine = RelayEngine(activity, vault) {}
        val agent = JSONObject().put("pane_id", "writer").put("terminal_id", "terminal-test").put("generation", 1)
            .put("agent_session_id", "session-test").put("cwd", "/work/project").put("agent", "codex").put("tab_label", "writer")
        val ui = NativeAgents(activity, engine, vault, { mapOf("fixture" to JSONArray().put(agent)) }, { _, _ -> }, {}, {})
        val host = FrameLayout(activity)
        activity.setContentView(host)
        try {
            set(ui, "selected", agent); set(ui, "relay", "fixture")
            val state = get(ui, "conversation") as ConversationState
            state.entries["user"] = JSONObject().put("id", "user").put("role", "user").put("text", "请检查主题配色。")
            state.entries["tools"] = JSONObject().put("id", "tools").put("role", "assistant").put("text", "")
                .put("tools", JSONArray().put(JSONObject().put("id", "tool").put("name", "exec_command").put("input", "{\"cmd\":\"echo theme\"}").put("state", "completed")))
            state.entries["reply"] = JSONObject().put("id", "reply").put("role", "assistant")
                .put("text", "**检查完成**：正文与 `代码` 都清晰可读。\n\n[查看说明](https://example.com)\n\n```kotlin\nval theme = \"local\"\n```")
            for (theme in PocketTheme.entries) {
                assertTrue(PocketAppearance.save(activity, theme)); PocketAppearance.applyWindow(activity)
                host.setBackgroundColor(theme.colors.background); ui.attach(host)
                val composer = get(ui, "composerPanel") as NativeComposerView
                composer.editor.setText("继续查看工作区文件")
                capture(host, "${theme.id}-conversation")
                assertEquals(theme.colors.ink, composer.editor.currentTextColor)
                assertEquals(theme.colors.muted, composer.editor.currentHintTextColor)
                val message = views(host).filterIsInstance<TextView>().single { it.text.contains("检查完成") }
                assertEquals(theme.colors.ink, message.currentTextColor)
                assertEquals(theme.colors.accent, message.linkTextColors.defaultColor)
                assertTrue(views(host).any { it is NativeToolGroup })
                val dialog = AgentManagementDialogs.rename(activity, "writer") { _, _ -> fail("Preview must not submit") }
                try {
                    capture(dialog.window!!.decorView, "${theme.id}-rename", wrap = true)
                    val input = views(dialog.window!!.decorView).filterIsInstance<EditText>().single()
                    assertEquals(theme.colors.ink, input.currentTextColor)
                    val explanation = views(dialog.window!!.decorView).filterIsInstance<TextView>().single { it.text.startsWith("修改 Herdr") }
                    assertEquals(theme.colors.ink, explanation.currentTextColor)
                } finally { dialog.dismiss() }
            }
        } finally { ui.destroy(); engine.destroy(); controller.pause().stop().destroy() }
    }

    @Test fun newActivityLoadsTheSavedPaletteBeforeBuildingItsViews() {
        assertTrue(PocketAppearance.save(app, PocketTheme.CLOUD))
        val shared = SharedRelay.get(app)
        set(shared.vault, "key\$delegate", lazyOf(SecretKeySpec(ByteArray(32) { 28 }, "AES")))
        val controller = Robolectric.buildActivity(MainActivity::class.java).setup()
        try {
            val activity = controller.get(); layout(activity.window.decorView)
            assertEquals(PocketTheme.CLOUD, PocketAppearance.current)
            assertEquals(PocketTheme.CLOUD.colors.background, ((get(activity, "root") as View).background as ColorDrawable).color)
            assertTrue(views(activity.window.decorView).filterIsInstance<TextView>().any { it.text.contains("主题配色 · 云白蓝") })
            assertEquals(PocketTheme.CLOUD.colors.ink, (get(activity, "heading") as TextView).currentTextColor)
        } finally { controller.pause().stop().destroy() }
    }
}

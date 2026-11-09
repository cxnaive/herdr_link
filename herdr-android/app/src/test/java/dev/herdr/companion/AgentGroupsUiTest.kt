package dev.herdr.companion

import android.app.Activity
import android.graphics.Bitmap
import android.graphics.Canvas
import android.os.Looper
import android.os.SystemClock
import android.view.MotionEvent
import android.view.View
import android.view.ViewGroup
import android.view.accessibility.AccessibilityNodeInfo
import android.widget.EditText
import android.widget.FrameLayout
import android.widget.ScrollView
import android.widget.TextView
import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
import org.robolectric.RobolectricTestRunner
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import org.robolectric.shadows.ShadowAlertDialog
import java.io.Closeable
import java.io.File

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], qualifiers = "w400dp-h850dp-xhdpi")
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class AgentGroupsUiTest {
    private fun views(view: View): List<View> = listOf(view) + if (view is ViewGroup) (0 until view.childCount).flatMap { views(view.getChildAt(it)) } else emptyList()
    private fun get(owner: Any, field: String) = owner.javaClass.getDeclaredField(field).apply { isAccessible = true }.get(owner)
    private fun computer(id: String) = AgentGroupExpansion.computerKey(id)
    private fun workspace(id: String, ws: String = "shared", server: String = "primary") =
        AgentGroupExpansion.workspaceKey(AgentPresentation.Group(id, server, ws, "", "", 0, emptyList()))
    private fun agent(name: String, ws: String = "shared", order: Int = 1) = JSONObject().put("pane_id", name).put("tab_id", name)
        .put("tab_label", name).put("tab_order", order).put("terminal_id", "term-$name").put("generation", 1)
        .put("agent_session_id", "session-$name").put("workspace_id", ws).put("server_session_id", "primary")
        .put("cwd", "/work/$ws").put("agent", "codex").put("status", "working")

    private inner class Fixture(val width: Int = 400, val height: Int = 850, scale: Float = 1f) : Closeable {
        val controller = Robolectric.buildActivity(Activity::class.java).setup()
        val activity = controller.get().apply { setTheme(R.style.Theme_HerdrPocket) }
        val root = FrameLayout(activity).apply { setBackgroundColor(AgentStyle.background) }
        val vault = Vault(activity).also {
            activity.getSharedPreferences("private_state", 0).edit().clear().commit()
            it.javaClass.getDeclaredField("key\$delegate").apply { isAccessible = true }
                .set(it, lazyOf(javax.crypto.spec.SecretKeySpec(ByteArray(32) { 42 }, "AES")))
        }
        val engine = RelayEngine(activity, vault) {}
        val inventory = linkedMapOf("a" to JSONArray().put(agent("writer")).put(agent("trainer", "experiments")), "b" to JSONArray().put(agent("writer")))
        val directory = linkedMapOf<String, JSONArray>()
        var selectedFile: WorkspaceBinding? = null
        var ui: NativeAgents
        init {
            activity.getSharedPreferences(AgentGroupExpansion.PREFERENCES, 0).edit().clear().commit()
            val config = android.content.res.Configuration(activity.resources.configuration).apply { fontScale = scale }
            @Suppress("DEPRECATION") activity.resources.updateConfiguration(config, activity.resources.displayMetrics)
            activity.setContentView(root)
            val relays = JSONArray().put(JSONObject().put("id", "a").put("label", "训练机 A"))
                .put(JSONObject().put("id", "b").put("label", "训练机 B"))
                .put(JSONObject().put("id", "offline").put("label", "离线工作站"))
            val credentials = JSONObject().put("a", JSONObject().put("role", "controller")).put("b", JSONObject().put("role", "controller"))
            engine.javaClass.getDeclaredField("state").apply { isAccessible = true }.set(engine,
                JSONObject().put("relays", relays).put("auth", JSONObject().put("relays", credentials)))
            directory["a"] = JSONArray().put(ws("shared", "3d-vla", 1)).put(ws("experiments", "模型实验", 2)).put(ws("empty", "空工作空间", 3))
            directory["b"] = JSONArray().put(ws("shared", "3d-vla", 1))
            ui = createUI()
            replay()
        }
        fun ws(id: String, label: String, number: Int) = JSONObject().put("workspace_id", id).put("label", label).put("cwd", "/work/$id").put("number", number)
        fun createUI() = NativeAgents(activity, engine, vault, { inventory }, { id, value -> selectedFile = WorkspaceBinding.fromAgent(id, value) }, {}, {})
        fun message(id: String, value: JSONObject) = ui.onEvent(JSONObject().put("type", "message").put("relayId", id).put("message", value))
        fun status(id: String, online: Boolean) = ui.onEvent(JSONObject().put("type", "status").put("relayId", id).put("status", if (online) "connected" else "closed"))
        fun replay() {
            ui.attach(root)
            for ((id, rows) in directory) {
                message(id, JSONObject().put("type", "push_config").put("protocol", 3).put("inventory", JSONObject().put("state", "ready"))
                    .put("agent_profiles", JSONArray().put(JSONObject().put("id", "codex").put("label", "Codex"))))
                message(id, JSONObject().put("type", "workspaces").put("workspaces", rows))
                status(id, true)
            }
            layout()
        }
        fun layout() {
            val w = AgentStyle.dp(activity, width); val h = AgentStyle.dp(activity, height)
            repeat(3) {
                shadowOf(Looper.getMainLooper()).idle()
                root.measure(View.MeasureSpec.makeMeasureSpec(w, View.MeasureSpec.EXACTLY), View.MeasureSpec.makeMeasureSpec(h, View.MeasureSpec.EXACTLY))
                root.layout(0, 0, w, h)
            }
        }
        fun header(key: String) = views(root).filterIsInstance<AgentGroupHeader>().single { it.groupKey == key }
        fun toggle(key: String) { header(key).performClick(); layout() }
        fun search(query: String) { views(root).filterIsInstance<EditText>().single().setText(query); layout() }
        fun section(key: String) = header(key).parent.parent as View
        fun tabs(name: String = "writer") = views(root).count { it.contentDescription?.toString() == "Tab：$name" }
        fun capture(name: String) {
            layout()
            val bitmap = Bitmap.createBitmap(root.width, root.height, Bitmap.Config.ARGB_8888)
            root.draw(Canvas(bitmap))
            val output = File("build/reports/agent-groups/$name.png"); output.parentFile?.mkdirs()
            output.outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }; bitmap.recycle()
        }
        override fun close() {
            ui.destroy(); engine.destroy()
            activity.getSharedPreferences(AgentGroupExpansion.PREFERENCES, 0).edit().clear().commit()
            controller.pause().stop().destroy()
        }
    }

    @Test fun runningCountsStayVisibleWhileFoldedAndUpdateWithoutSearchBias() = Fixture().use { f ->
        f.inventory["a"]!!.put(agent("waiting").put("status", "blocked"))
            .put(agent("shell").put("terminal_only", true))
        f.message("a", JSONObject().put("type", "agents")); f.layout()
        fun summary() = f.header(computer("a")).contentDescription.toString()
        assertTrue(summary().contains("运行中 2 · 待处理 1"))
        assertTrue(summary().contains("3 个 Agent · 1 个终端"))
        f.toggle(computer("a")); assertTrue(summary().contains("运行中 2"))
        f.search("writer"); assertTrue(summary().contains("运行中 2"))
        assertTrue(summary().contains("1/3 个 Agent"))
        f.search("")
        f.inventory["a"]!!.getJSONObject(0).put("status", "done")
        f.message("a", JSONObject().put("type", "agent_update")); f.layout()
        assertFalse(f.header(computer("a")).expanded)
        assertTrue(summary().contains("运行中 1 · 待处理 1"))
        f.capture("running-counts")
        f.status("a", false); f.layout()
        assertTrue(summary().contains("运行数待确认")); assertFalse(summary().contains("运行中 1"))
        f.status("a", true)
        f.message("a", JSONObject().put("type", "inventory_status").put("state", "error").put("stale", true)); f.layout()
        assertTrue(summary().contains("运行数待确认"))
        f.message("a", JSONObject().put("type", "inventory_status").put("state", "ready")); f.layout()
        assertTrue(summary().contains("运行中 1"))
    }

    @Test fun computersAndWorkspacesCollapseIndependentlyAndRestoreAfterRecreation() = Fixture().use { f ->
        assertEquals(2, f.tabs())
        f.toggle(workspace("a")); assertEquals(1, f.tabs())
        f.toggle(computer("a")); assertEquals(1, f.tabs())
        assertTrue(views(f.root).none { it is AgentGroupHeader && it.groupKey == workspace("a") })
        f.toggle(computer("a")); assertFalse(f.header(workspace("a")).expanded)
        f.ui.destroy(); f.ui = f.createUI(); f.replay()
        assertFalse(f.header(workspace("a")).expanded)
        assertTrue(f.header(workspace("b")).expanded)
        f.toggle(workspace("a")); assertEquals(2, f.tabs())
    }
    @Test fun refreshReconnectAndIncomingAgentsDoNotReopenFoldedComputers() = Fixture().use { f ->
        val state = f.engine.snapshot().toString()
        f.toggle(computer("a")); f.status("a", false); f.status("a", true)
        f.inventory["a"]!!.put(agent("new-agent", order = 2))
        f.message("a", JSONObject().put("type", "agents").put("agents", f.inventory["a"]))
        f.layout()
        assertFalse(f.header(computer("a")).expanded)
        assertTrue(f.header(computer("a")).contentDescription.contains("3 个 Agent"))
        assertEquals(state, f.engine.snapshot().toString())
        assertEquals(0, f.tabs("new-agent"))
    }
    @Test fun searchTemporarilyExpandsBothLevelsAndRestoresTheirOriginalState() = Fixture().use { f ->
        f.toggle(workspace("a")); f.toggle(computer("a")); f.toggle(computer("b"))
        f.search("writer")
        assertEquals(2, f.tabs()); assertTrue(f.header(workspace("a")).expanded)
        assertTrue(views(f.root).none { it is AgentGroupHeader && it.groupKey == workspace("a", "empty") })
        f.toggle(computer("a")); assertEquals(1, f.tabs())
        f.search("")
        assertFalse(f.header(computer("a")).expanded); assertFalse(f.header(computer("b")).expanded)
        f.toggle(computer("a")); assertFalse(f.header(workspace("a")).expanded)
        f.search("训练机 B"); assertTrue(f.header(computer("b")).expanded); assertEquals(1, f.tabs())
    }
    @Test fun collapsedWorkspaceStillAllowsCreationWithoutTogglingOrLosingIdentity() = Fixture().use { f ->
        val key = workspace("a", "empty")
        f.toggle(key)
        val add = views(f.section(key)).single { it.contentDescription?.toString() == "在 空工作空间 新建 Agent" }
        assertTrue(add.isEnabled); add.performClick(); f.layout()
        val dialog = ShadowAlertDialog.getLatestAlertDialog()
        assertTrue(views(dialog.window!!.decorView).filterIsInstance<TextView>().any { it.text.contains("empty") })
        assertFalse(f.header(key).expanded)
        dialog.dismiss()
    }
    @Test fun identicalTabNamesOnDifferentComputersKeepExactFileAndConversationTargets() = Fixture().use { f ->
        f.toggle(computer("a"))
        val section = f.section(workspace("b"))
        views(section).filterIsInstance<TextView>().single { it.text == "工作区" }.performClick()
        assertEquals("b", f.selectedFile!!.target.relay)
        views(section).filterIsInstance<TextView>().single { it.text == "打开对话" }.performClick(); f.layout()
        assertEquals("b", get(f.ui, "relay"))
        assertEquals("writer", (get(f.ui, "selected") as JSONObject).getString("pane_id"))
        f.ui.back(); f.layout(); assertFalse(f.header(computer("a")).expanded)
    }
    @Test @Config(sdk = [28]) fun accessibilityActionsAndTouchTargetsWorkOnMinimumAndroidVersion() = Fixture().use { f ->
        val key = computer("a")
        val header = f.header(key)
        assertTrue(header.height >= AgentStyle.dp(f.activity, 48))
        val info = header.createAccessibilityNodeInfo()
        assertTrue(info.isHeading)
        assertTrue(info.actionList.any { it.id == AccessibilityNodeInfo.ACTION_COLLAPSE })
        assertTrue(header.performAccessibilityAction(AccessibilityNodeInfo.ACTION_COLLAPSE, null)); f.layout()
        assertFalse(f.header(key).expanded)
        assertTrue(f.header(key).contentDescription.contains("已折叠"))
        val folded = f.header(key); val time = SystemClock.uptimeMillis()
        val down = MotionEvent.obtain(time, time, MotionEvent.ACTION_DOWN, folded.width / 2f, folded.height / 2f, 0)
        val up = MotionEvent.obtain(time, time + 40, MotionEvent.ACTION_UP, folded.width / 2f, folded.height / 2f, 0)
        folded.dispatchTouchEvent(down); folded.dispatchTouchEvent(up); down.recycle(); up.recycle(); f.layout()
        assertTrue(f.header(key).expanded)
    }
    @Test fun anEmptySearchResultStillResetsTemporarySearchFolds() = Fixture().use { f ->
        f.search("writer"); f.toggle(computer("a"))
        f.search("not-found"); f.search("writer")
        assertTrue(f.header(computer("a")).expanded)
        f.search(""); assertTrue(f.header(computer("a")).expanded)
    }
    @Test fun keyboardCollapseKeepsFocusOnTheReplacementHeader() = Fixture().use { f ->
        val key = computer("a")
        // A hardware key exits touch mode before assigning keyboard focus.
        @Suppress("DEPRECATION") val focused = f.header(key).requestFocusFromTouch()
        assertTrue(focused)
        f.header(key).dispatchKeyEvent(android.view.KeyEvent(android.view.KeyEvent.ACTION_DOWN, android.view.KeyEvent.KEYCODE_DPAD_CENTER))
        f.header(key).dispatchKeyEvent(android.view.KeyEvent(android.view.KeyEvent.ACTION_UP, android.view.KeyEvent.KEYCODE_DPAD_CENTER))
        f.layout()
        assertFalse(f.header(key).expanded)
        assertTrue(f.header(key).hasFocus())
    }
    @Test @Config(qualifiers = "w800dp-h600dp-xhdpi") fun landscapeGroupsRemainCollapsible() = Fixture(800, 600).use { f ->
        f.toggle(computer("a")); f.toggle(computer("b")); f.toggle(computer("offline"))
        f.capture("landscape")
        assertEquals(3, views(f.root).filterIsInstance<AgentGroupHeader>().size)
        f.toggle(computer("a")); assertTrue(f.header(workspace("a")).expanded)
    }
    @Test fun staleHeaderCannotToggleARebuiltList() = Fixture().use { f ->
        val stale = f.header(computer("a"))
        f.status("a", false); f.layout()
        stale.performClick(); f.layout()
        assertTrue(f.header(computer("a")).expanded)
    }
    @Test fun foldingKeepsTheTouchedWorkspaceAtItsReadingPosition() = Fixture().use { f ->
        val rows = JSONArray()
        for (ws in listOf("shared", "experiments", "empty")) for (i in 1..10) rows.put(agent("$ws-$i", ws, i))
        f.inventory["a"] = rows
        f.message("a", JSONObject().put("type", "agents").put("agents", rows)); f.layout()
        val key = workspace("a", "experiments")
        val scroll = views(f.root).filterIsInstance<ScrollView>().single()
        fun y(view: View) = IntArray(2).also { view.getLocationOnScreen(it) }[1]
        scroll.scrollBy(0, y(f.header(key)) - y(scroll) - AgentStyle.dp(f.activity, 30)); f.layout()
        val before = y(f.header(key)) - y(scroll)
        f.toggle(key)
        assertEquals(before, y(f.header(key)) - y(scroll))
    }
    @Test fun offlineAndEmptyComputersRemainVisibleAndScreensRender() = Fixture().use { f ->
        assertTrue(f.header(computer("offline")).contentDescription.contains("0 个工作空间"))
        f.toggle(workspace("a", "experiments")); f.toggle(workspace("a", "empty")); f.toggle(computer("b")); f.toggle(computer("offline"))
        f.capture("overview")
        f.toggle(computer("a")); f.capture("collapsed")
        f.search("writer"); f.capture("search")
        f.search("not-found"); assertTrue(views(f.root).filterIsInstance<TextView>().any { it.text == "没有匹配的电脑、工作空间或 Agent" })
    }
    @Test @Config(qualifiers = "w360dp-h800dp-xhdpi") fun narrowLargeTextKeepsHeadersAndCreateButtonsSeparate() = Fixture(360, 800, 1.3f).use { f ->
        f.toggle(computer("b")); f.toggle(computer("offline")); f.toggle(workspace("a", "experiments")); f.toggle(workspace("a", "empty"))
        f.capture("narrow-large-text")
        for (header in views(f.root).filterIsInstance<AgentGroupHeader>()) {
            assertTrue(header.width >= AgentStyle.dp(f.activity, 120))
            assertTrue(header.height >= AgentStyle.dp(f.activity, 48))
        }
        for (button in views(f.root).filter { it.contentDescription?.toString()?.startsWith("在 ") == true }) {
            assertTrue(button.height >= AgentStyle.dp(f.activity, 48))
            assertTrue(button.width >= AgentStyle.dp(f.activity, 48))
        }
    }
}

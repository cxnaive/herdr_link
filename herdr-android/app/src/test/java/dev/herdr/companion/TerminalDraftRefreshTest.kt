package dev.herdr.companion

import android.app.Activity
import android.content.Context
import android.os.Looper
import android.text.InputType
import android.view.View
import android.view.inputmethod.BaseInputConnection
import android.view.inputmethod.EditorInfo
import android.view.inputmethod.InputMethodManager
import android.widget.EditText
import android.widget.FrameLayout
import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
import org.robolectric.RobolectricTestRunner
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config
import org.robolectric.annotation.Implementation
import org.robolectric.annotation.Implements
import org.robolectric.annotation.Resetter
import org.robolectric.shadows.ShadowInputMethodManager
import javax.crypto.spec.SecretKeySpec

/** Observe actual TextView input reconfiguration; no simulated IME text deletion. */
@Implements(InputMethodManager::class)
class DraftInputMethodManager : ShadowInputMethodManager() {
    companion object {
        val restarts = mutableListOf<View>()
        @JvmStatic @Resetter fun clearRestarts() = restarts.clear()
    }
    @Implementation override fun restartInput(view: View) {
        restarts += view
        super.restartInput(view)
    }
}

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [28, 35], qualifiers = "w400dp-h850dp-xhdpi", shadows = [DraftInputMethodManager::class])
class TerminalDraftRefreshTest {
    private fun get(owner: Any, field: String): Any? = owner.javaClass.getDeclaredField(field).apply { isAccessible = true }.get(owner)
    private fun set(owner: Any, field: String, value: Any?) = owner.javaClass.getDeclaredField(field).apply { isAccessible = true }.set(owner, value)
    private fun call(ui: NativeAgents, method: String) = ui.javaClass.getDeclaredMethod(method).apply { isAccessible = true }.invoke(ui)

    private class Harness(val ui: NativeAgents, val agent: JSONObject, val vault: Vault, val root: FrameLayout) {
        val editor get() = ui.javaClass.getDeclaredField("composer").apply { isAccessible = true }.get(ui) as EditText
        fun frame(value: JSONObject) {
            value.put("pane_id", agent.optString("pane_id")).put("target", AgentTarget.fromAgent("fixture", agent).json())
            ui.onEvent(JSONObject().put("type", "message").put("relayId", "fixture").put("message", value))
        }
        fun inventory() = ui.onEvent(JSONObject().put("type", "message").put("relayId", "fixture").put("message", JSONObject().put("type", "agents")))
        fun layout() {
            root.measure(View.MeasureSpec.makeMeasureSpec(800, View.MeasureSpec.EXACTLY), View.MeasureSpec.makeMeasureSpec(1700, View.MeasureSpec.EXACTLY))
            root.layout(0, 0, 800, 1700)
            shadowOf(Looper.getMainLooper()).idle()
        }
    }

    private fun withTerminal(binding: String = "verified", block: Harness.() -> Unit) {
        val controller = Robolectric.buildActivity(Activity::class.java).setup()
        val activity = controller.get()
        activity.getSystemService(Context.INPUT_METHOD_SERVICE)
        activity.getSharedPreferences("private_state", Context.MODE_PRIVATE).edit().clear().commit()
        val vault = Vault(activity)
        set(vault, "key\$delegate", lazyOf(SecretKeySpec(ByteArray(32) { 27 }, "AES")))
        val engine = RelayEngine(activity, vault) {}
        val agent = JSONObject().put("pane_id", "terminal-test").put("terminal_id", "terminal-instance")
            .put("agent", "codex").put("name", "Synthetic terminal").put("cwd", "/synthetic/project")
            .put("generation", 1).put("agent_session_id", if (binding == "verified") "synthetic-session" else "")
            .put("binding_epoch", 7).put("binding_state", binding).put("status", "idle")
        val ui = NativeAgents(activity, engine, vault, { mapOf("fixture" to JSONArray().put(agent)) }, { _, _ -> }, {}, {})
        val root = FrameLayout(activity); activity.setContentView(root)
        try {
            ui.attach(root); ui.open("fixture", agent)
            set(ui, "mode", "terminal"); call(ui, "render")
            val harness = Harness(ui, agent, vault, root); harness.layout()
            harness.block()
        } finally { ui.destroy(); controller.pause().stop().destroy() }
    }

    @Test fun agentExitKeepsTheTerminalAndSeparatesDrafts() = withTerminal {
        editor.setText("old agent draft must not reach shell")
        agent.put("terminal_only", true).put("agent", "").put("agent_session_id", "")
            .put("generation", 2).put("binding_state", "").put("binding_epoch", 0)
        inventory(); layout()
        assertEquals("terminal", get(ui, "mode"))
        assertEquals("", editor.text.toString())
        assertTrue((get(ui, "selected") as JSONObject).getBoolean("terminal_only"))
        assertEquals("terminal-instance", (get(ui, "selected") as JSONObject).getString("terminal_id"))
        assertTrue(ui.back())
        ui.open("fixture", agent); layout()
        assertEquals("terminal", get(ui, "mode")) // Also works without remembering the former agent.
        editor.setText("shell draft must not reach new agent")
        agent.put("terminal_only", false).put("agent", "codex").put("agent_session_id", "new-session")
            .put("generation", 3).put("binding_state", "verified").put("binding_epoch", 1)
        inventory(); layout()
        assertEquals("conversation", get(ui, "mode"))
        assertEquals("", editor.text.toString())
    }

    @Test fun snapshotsAndInventoryNeverRestartAnActiveChineseComposition() = withTerminal("ambiguous") {
        val input = editor
        input.requestFocus(); layout()
        val connection = input.onCreateInputConnection(EditorInfo())!!
        assertTrue(connection.setComposingText("nihao", 1))
        val editable = input.editableText
        val start = BaseInputConnection.getComposingSpanStart(editable)
        val end = BaseInputConnection.getComposingSpanEnd(editable)
        assertTrue(start >= 0 && end > start)
        DraftInputMethodManager.restarts.clear()
        repeat(6) { index ->
            frame(JSONObject().put("type", "pane_content").put("content", "terminal output $index").put("no_echo", false))
            inventory(); layout()
            assertSame(input, editor)
            assertSame(editable, input.editableText)
            assertEquals("nihao", input.text.toString())
            assertEquals(start, BaseInputConnection.getComposingSpanStart(input.text))
            assertEquals(end, BaseInputConnection.getComposingSpanEnd(input.text))
            assertEquals(5, input.selectionStart); assertTrue(input.hasFocus())
        }
        assertEquals("terminal refresh must not restart the input method", 0, DraftInputMethodManager.restarts.count { it === input })
        assertTrue(connection.commitText("你好", 1))
        assertEquals("你好", input.text.toString())
        assertFalse("keeping a local draft must not grant send authority", PrimarySessionBinding.writable(agent))
    }

    @Test fun deltaRefreshAndLeaseChangeKeepTheEditableCursorAndDraft() = withTerminal {
        frame(JSONObject().put("type", "pane_content").put("content", "old").put("no_echo", false).put("content_fingerprint", "f0"))
        val input = editor; input.requestFocus(); layout(); input.onCreateInputConnection(EditorInfo())!!; input.setText("before 界😀 after"); input.setSelection(7, 9)
        val editable = input.editableText
        DraftInputMethodManager.restarts.clear()
        frame(JSONObject().put("type", "pane_delta").put("base_fingerprint", "f0").put("content_fingerprint", "f1")
            .put("segments", JSONArray().put(JSONObject().put("text", "new output"))))
        agent.put("binding_epoch", 8); inventory(); layout()
        assertEquals("new output", get(ui, "content"))
        assertSame(input, editor); assertSame(editable, editor.editableText)
        assertEquals("before 界😀 after", editor.text.toString())
        assertEquals(7, editor.selectionStart); assertEquals(9, editor.selectionEnd)
        assertEquals(0, DraftInputMethodManager.restarts.count { it === input })
        assertEquals(input.text.toString(), vault.read("prompt:" + WorkspaceBinding.fromAgent("fixture", agent).storageKey()))
    }

    @Test fun noEchoChangesInputModeOnceAndNeverPersistsSecretDraft() = withTerminal {
        val input = editor
        frame(JSONObject().put("type", "pane_content").put("content", "Password:").put("no_echo", true))
        assertEquals(InputType.TYPE_TEXT_VARIATION_PASSWORD, input.inputType and InputType.TYPE_MASK_VARIATION)
        input.requestFocus(); layout(); input.onCreateInputConnection(EditorInfo())!!; input.setText("synthetic secret"); input.setSelection(4)
        DraftInputMethodManager.restarts.clear()
        repeat(4) {
            frame(JSONObject().put("type", "pane_content").put("content", "Password: waiting"))
            inventory()
        }
        assertEquals("synthetic secret", input.text.toString()); assertEquals(4, input.selectionStart)
        assertEquals(0, DraftInputMethodManager.restarts.count { it === input })
        call(ui, "saveDraft")
        assertFalse(vault.read("prompt:" + WorkspaceBinding.fromAgent("fixture", agent).storageKey()) == "synthetic secret")
        frame(JSONObject().put("type", "pane_content").put("content", "Prompt:").put("no_echo", false))
        assertEquals(InputType.TYPE_TEXT_FLAG_MULTI_LINE, input.inputType and InputType.TYPE_TEXT_FLAG_MULTI_LINE)
        val transitionRestarts = DraftInputMethodManager.restarts.count { it === input }
        // Older Android can restart again while changing the password transformation.
        assertTrue(transitionRestarts > 0)
        assertEquals(4, input.selectionStart)
        frame(JSONObject().put("type", "pane_content").put("content", "Prompt: still waiting").put("no_echo", false))
        assertEquals(transitionRestarts, DraftInputMethodManager.restarts.count { it === input })
    }

    @Test fun ignoredAndUnchangedFramesCannotCancelEditingOrChangePrivacy() = withTerminal {
        val input = editor; input.requestFocus(); layout()
        val connection = input.onCreateInputConnection(EditorInfo())!!
        connection.setComposingText("zhongwen", 1)
        DraftInputMethodManager.restarts.clear()
        frame(JSONObject().put("type", "pane_unchanged"))
        frame(JSONObject().put("type", "pane_content").put("error", "Synthetic unavailable snapshot").put("no_echo", true))
        val stale = JSONObject().put("type", "pane_content").put("content", "foreign output").put("no_echo", true)
            .put("pane_id", agent.optString("pane_id"))
            .put("target", AgentTarget.fromAgent("fixture", agent).json().put("binding_epoch", 6))
        ui.onEvent(JSONObject().put("type", "message").put("relayId", "fixture").put("message", stale))
        ui.onEvent(JSONObject().put("type", "status").put("relayId", "fixture").put("status", "disconnected"))
        assertSame(input, editor); assertEquals("zhongwen", input.text.toString())
        assertEquals(0, BaseInputConnection.getComposingSpanStart(input.text))
        assertEquals(8, BaseInputConnection.getComposingSpanEnd(input.text))
        assertEquals(InputType.TYPE_TEXT_VARIATION_NORMAL, input.inputType and InputType.TYPE_MASK_VARIATION)
        assertEquals(0, DraftInputMethodManager.restarts.count { it === input })
        assertEquals("", get(ui, "content"))
    }

    @Test fun rerenderKeepsSecretModeBeforeTheNextOutputFrame() = withTerminal {
        frame(JSONObject().put("type", "pane_content").put("content", "Password:").put("no_echo", true))
        editor.setText("synthetic private draft")
        call(ui, "render"); layout()
        assertEquals(InputType.TYPE_TEXT_VARIATION_PASSWORD, editor.inputType and InputType.TYPE_MASK_VARIATION)
        assertEquals("synthetic private draft", editor.text.toString())
        call(ui, "saveDraft")
        assertFalse(vault.read("prompt:" + WorkspaceBinding.fromAgent("fixture", agent).storageKey()) == "synthetic private draft")
        val fresh = editor; DraftInputMethodManager.restarts.clear()
        frame(JSONObject().put("type", "pane_content").put("content", "Password: waiting"))
        assertEquals(0, DraftInputMethodManager.restarts.count { it === fresh })
    }

    @Test fun replacingTheSessionDoesNotCarryItsDraftIntoTheNewComposer() = withTerminal {
        editor.setText("draft for original session")
        val originalKey = "prompt:" + WorkspaceBinding.fromAgent("fixture", agent).storageKey()
        agent.put("agent_session_id", "replacement-session").put("generation", 2).put("binding_epoch", 8)
        inventory(); layout()
        assertEquals("", editor.text.toString())
        assertEquals("draft for original session", vault.read(originalKey))
        assertFalse(editor.inputType and InputType.TYPE_MASK_VARIATION == InputType.TYPE_TEXT_VARIATION_PASSWORD)
    }

}

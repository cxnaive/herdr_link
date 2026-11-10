package dev.herdr.companion

import android.app.AlertDialog
import android.app.Activity
import android.content.Context
import android.os.Looper
import android.widget.FrameLayout
import android.widget.CheckBox
import android.widget.EditText
import android.view.View
import android.view.ViewGroup
import org.robolectric.shadows.ShadowAlertDialog
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
import java.time.Duration
import java.util.concurrent.CopyOnWriteArrayList
import java.util.concurrent.TimeUnit
import javax.crypto.spec.SecretKeySpec

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [28])
class PiPromptTcpUiTest {
    private val context get() = RuntimeEnvironment.getApplication() as Context
    @Before fun setup() {
        SharedRelay.resetForTest()
        for (name in listOf("private_state", "background-monitor")) context.getSharedPreferences(name, 0).edit().clear().commit()
    }
    @After fun cleanup() { SharedRelay.resetForTest() }
    private fun get(owner: Any, field: String) = owner.javaClass.getDeclaredField(field).apply { isAccessible = true }.get(owner)
    private fun set(owner: Any, field: String, value: Any?) = owner.javaClass.getDeclaredField(field).apply { isAccessible = true }.set(owner, value)
    private fun invoke(ui: NativeAgents, method: String) = ui.javaClass.getDeclaredMethod(method).apply { isAccessible = true }.invoke(ui)

    private class Harness(val ui: NativeAgents, val shared: SharedRelay, val fixture: MockTcpMonitorRelay) {
        val commands = CopyOnWriteArrayList<JSONObject>()
        var agent = JSONObject()
        @Volatile var outcome = "completed"
        val composer get() = ui.javaClass.getDeclaredField("composerPanel").apply { isAccessible = true }.get(ui) as NativeComposerView
        fun waitUntil(condition: () -> Boolean) {
            val end = System.nanoTime() + TimeUnit.SECONDS.toNanos(12)
            while (System.nanoTime() < end) {
                shadowOf(Looper.getMainLooper()).idleFor(Duration.ofMillis(25))
                if (condition()) return
                Thread.sleep(5)
            }
            fail("Pi prompt fixture timed out: commands=$commands, errors=${fixture.failures}, toast=${org.robolectric.shadows.ShadowToast.getTextOfLatestToast()}")
        }
        fun result(command: JSONObject, data: JSONObject = JSONObject(), phase: String = "completed", ok: Boolean = true) =
            JSONObject().put("type", "command_result").put("request_id", command.optString("request_id"))
                .put("ok", ok).put("phase", phase).put("data", data)
        fun respond(command: JSONObject): JSONObject? = when (command.optString("type")) {
            "get_conversation_history" -> result(command, JSONObject().put("available", true).put("state", "ready")
                .put("source_revision", "fixture-revision").put("entries", JSONArray()).put("next_cursor", ""))
            "get_conversation_updates" -> result(command, JSONObject().put("kind", "heartbeat").put("sequence", 1).put("source_revision", "fixture-revision"))
            "answer_question", "submit_prompt", "get_pi_prompt_capability", "send_pi_prompt_unconfirmed", "send_terminal_text", "send_keys" -> {
                commands += JSONObject(command.toString())
                if (command.optString("type") == "get_pi_prompt_capability") {
                    result(command, ok = false).put("error", "Pi prompt capability is unavailable for this target")
                } else if (outcome == "reject") {
                    result(command, phase = "not_started", ok = false).put("error", "Agent is not ready")
                } else result(command, phase = outcome)
            }
            "unwatch_pane", "unwatch_conversation", "release_pane_size", "refresh_agents" -> null
            else -> error("unexpected request: ${command.optString("type")}")
        }
    }

    private fun withPi(provider: String, status: String, bridgeAdvertised: Boolean = true, block: Harness.() -> Unit) {
        setup()
        MockTcpMonitorRelay().use { fixture ->
            fixture.role = "controller"
            val shared = SharedRelay.get(context)
            set(shared.vault, "key\$delegate", lazyOf(SecretKeySpec(ByteArray(32) { 31 }, "AES")))
            set(shared.engine, "state", fixture.state())
            val controller = Robolectric.buildActivity(Activity::class.java).setup()
            val activity = controller.get(); val root = FrameLayout(activity); activity.setContentView(root)
            val ui = NativeAgents(activity, shared.engine, shared.vault, { shared.agents }, { _, _ -> }, {}, {})
            val harness = Harness(ui, shared, fixture)
            fixture.foregroundEnvelope = harness::respond
            try {
                ui.attach(root); shared.attachUI(ui, ui::onEvent); harness.waitUntil { shared.ready("fixture") }
                @Suppress("UNCHECKED_CAST")
                val configs = get(ui, "configs") as MutableMap<String, JSONObject>
                configs.getValue("fixture").put("capabilities", if (bridgeAdvertised) JSONArray().put("pi_prompt_unconfirmed") else JSONArray())
                harness.agent = shared.agents.getValue("fixture").getJSONObject(0)
                    .put("agent", provider).put("name", "Synthetic $provider").put("status", status)
                ui.open("fixture", harness.agent)
                harness.block()
                assertTrue(fixture.failures.toString(), fixture.failures.isEmpty())
            } finally {
                ui.destroy(); shared.detachUI(ui); controller.pause().stop().destroy(); SharedRelay.resetForTest()
            }
        }
    }

    @Test fun normalPiAndOmpUseTheOrdinaryPromptRouteWithoutBridgeLeases() {
        for (provider in listOf("pi", "omp")) for (status in listOf("done", "idle", "working")) {
            withPi(provider, status) {
                val text = "请检查执行结果\n保留这行说明"
                composer.editor.setText(text)
                assertTrue("$provider/$status", composer.sendButton.isEnabled)
                composer.sendButton.performClick()
                waitUntil { commands.isNotEmpty() }
                assertEquals("$provider/$status", "submit_prompt", commands.single().getString("type"))
                assertEquals(text, commands.single().getString("text"))
                assertEquals(AgentTarget.fromAgent("fixture", agent).json().toString(), commands.single().getJSONObject("target").toString())
                assertFalse(commands.single().has("pi_prompt_lease"))
                waitUntil { composer.editor.text.isEmpty() }
            }
        }
    }

    @Test fun ordinaryPiAndOmpDoNotRequireTheOptionalBridgeCapability() {
        for (provider in listOf("pi", "omp")) withPi(provider, "done", bridgeAdvertised = false) {
            composer.editor.setText("no bridge needed"); composer.sendButton.performClick()
            waitUntil { commands.isNotEmpty() }
            assertEquals("submit_prompt", commands.single().getString("type"))
            waitUntil { composer.editor.text.isEmpty() }
        }
    }

    @Test fun blockedPiAndOmpKeepDraftsAndNeverBypassApprovalThroughTheBridge() {
        for (provider in listOf("pi", "omp")) withPi(provider, "blocked") {
            composer.editor.setText("do not send while awaiting approval")
            invoke(ui, "sendText")
            shadowOf(Looper.getMainLooper()).idleFor(Duration.ofMillis(100))
            assertTrue(commands.isEmpty())
            assertEquals("do not send while awaiting approval", composer.editor.text.toString())
        }
    }

    @Test fun failedPromptPreservesDraftAndNeverFallsBackToTerminalOrBridge() = withPi("omp", "done") {
        outcome = "reject"
        composer.editor.setText("keep the rejected draft"); composer.sendButton.performClick()
        waitUntil { commands.isNotEmpty() && (get(ui, "pendingMutations") as Set<*>).isEmpty() }
        assertEquals("submit_prompt", commands.single().getString("type"))
        assertEquals("keep the rejected draft", composer.editor.text.toString())
        assertTrue(composer.sendButton.isEnabled)
    }

    @Test fun uncertainSendAndReaderPermissionsStillPreventDuplicateWrites() = withPi("pi", "done") {
        outcome = "dispatched_unknown"
        composer.editor.setText("keep the uncertain draft"); composer.sendButton.performClick()
        waitUntil { commands.isNotEmpty() && (get(ui, "pendingMutations") as Set<*>).isEmpty() }
        assertEquals("submit_prompt", commands.single().getString("type"))
        assertEquals("keep the uncertain draft", composer.editor.text.toString())
        assertFalse(composer.sendButton.isEnabled)
        invoke(ui, "sendText"); shadowOf(Looper.getMainLooper()).idleFor(Duration.ofMillis(100))
        assertEquals(1, commands.size)
        ui.back(); ui.open("fixture", agent)
        assertFalse(composer.sendButton.isEnabled)
        assertEquals("keep the uncertain draft", composer.editor.text.toString())
        val delivery = "delivery:" + WorkspaceBinding.fromAgent("fixture", agent).storageKey()
        shared.vault.remove(delivery)
        (get(shared.engine, "state") as JSONObject).getJSONObject("auth").getJSONObject("relays").getJSONObject("fixture").put("role", "reader")
        invoke(ui, "updateSendState")
        assertFalse(composer.sendButton.isEnabled)
        invoke(ui, "sendText"); shadowOf(Looper.getMainLooper()).idleFor(Duration.ofMillis(100))
        assertEquals(1, commands.size)
    }
    private fun interaction(name: String) = JSONObject(requireNotNull(javaClass.classLoader!!.getResourceAsStream("pi-omp/$name.json")).bufferedReader().use { it.readText() })
    private fun views(view: View): List<View> = listOf(view) + if (view is ViewGroup) (0 until view.childCount).flatMap { views(view.getChildAt(it)) } else emptyList()
    private fun showQuestion(ui: NativeAgents, value: JSONObject): AlertDialog {
        ui.javaClass.getDeclaredMethod("answerQuestion", JSONObject::class.java).apply { isAccessible = true }.invoke(ui, value)
        shadowOf(Looper.getMainLooper()).idle()
        return ShadowAlertDialog.getLatestAlertDialog()
    }

    @Test fun currentPiOmpStructuredActionsTravelThroughEncryptedExactTargetRequests() {
        for ((provider, name, choice) in listOf(Triple("pi", "pi-confirm", "No"), Triple("omp", "omp-approval", "Deny"), Triple("omp", "omp-html-form", "下一字段（不提交）"))) {
            withPi(provider, "blocked") {
                val value = interaction(name); agent.put("interaction", value)
                val dialog = showQuestion(ui, JSONObject(value.toString()))
                views(dialog.window!!.decorView).filterIsInstance<CheckBox>().single { it.text.toString().startsWith(choice + "\n") }.isChecked = true
                dialog.getButton(AlertDialog.BUTTON_POSITIVE).performClick()
                waitUntil { commands.isNotEmpty() && (get(ui, "pendingMutations") as Set<*>).isEmpty() }
                val command = commands.single()
                assertEquals("answer_question", command.getString("type"))
                assertEquals(value.getString("id"), command.getString("interaction_id"))
                val selected = command.getJSONArray("selected_indices").getInt(0)
                assertEquals(choice, value.getJSONArray("options").getJSONObject(selected).getString("label"))
                assertEquals(AgentTarget.fromAgent("fixture", agent).json().toString(), command.getJSONObject("target").toString())
            }
        }
    }

    @Test fun currentMultiSelectCanSubmitAnEmptySetAndTextAnswersExcludeChoices() {
        withPi("omp", "blocked") {
            val value = interaction("omp-ask-multi"); agent.put("interaction", value)
            val dialog = showQuestion(ui, value)
            dialog.getButton(AlertDialog.BUTTON_POSITIVE).performClick()
            waitUntil { commands.isNotEmpty() }
            assertEquals(0, commands.single().getJSONArray("selected_indices").length())
        }
        withPi("pi", "blocked") {
            val value = interaction("pi-input"); agent.put("interaction", value)
            val dialog = showQuestion(ui, value)
            val controls = views(dialog.window!!.decorView)
            controls.filterIsInstance<CheckBox>().first().isChecked = true // Cancel must be deselected by the text answer.
            controls.filterIsInstance<CheckBox>().single { it.text == value.getJSONObject("other").getString("label") }.isChecked = true
            controls.filterIsInstance<EditText>().single().setText("中文答案")
            dialog.getButton(AlertDialog.BUTTON_POSITIVE).performClick()
            waitUntil { commands.isNotEmpty() }
            val command = commands.single()
            assertEquals("中文答案", command.getString("other_text"))
            assertTrue(command.getBoolean("other_selected"))
            assertEquals(0, command.getJSONArray("selected_indices").length())
        }
    }

    @Test fun oldQuestionDialogCannotRetargetAnIdenticalQuestionOnAnotherAgent() = withPi("pi", "blocked") {
        val value = interaction("pi-confirm"); agent.put("interaction", value)
        val dialog = showQuestion(ui, value)
        views(dialog.window!!.decorView).filterIsInstance<CheckBox>().first().isChecked = true
        val next = shared.agents.getValue("fixture").getJSONObject(1).put("agent", "pi").put("status", "blocked").put("interaction", value)
        ui.open("fixture", next)
        dialog.getButton(AlertDialog.BUTTON_POSITIVE).performClick()
        shadowOf(Looper.getMainLooper()).idleFor(Duration.ofMillis(100))
        assertTrue(commands.isEmpty())
    }

}

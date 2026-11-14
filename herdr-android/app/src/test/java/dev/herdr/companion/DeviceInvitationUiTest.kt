package dev.herdr.companion

import android.app.AlertDialog
import android.content.Context
import android.graphics.Bitmap
import android.graphics.Canvas
import android.os.Build
import android.view.ContextThemeWrapper
import android.os.Looper
import android.view.View
import android.view.ViewGroup
import android.widget.EditText
import android.widget.Spinner
import android.widget.TextView
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
import java.time.Instant
import java.util.concurrent.CopyOnWriteArrayList
import java.util.concurrent.TimeUnit
import javax.crypto.spec.SecretKeySpec

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [28, 35], qualifiers = "w360dp-h800dp-xhdpi")
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class DeviceInvitationUiTest {
    private val app get() = RuntimeEnvironment.getApplication() as Context
    private fun get(owner: Any, field: String) = owner.javaClass.getDeclaredField(field).apply { isAccessible = true }.get(owner)
    private fun set(owner: Any, field: String, value: Any?) = owner.javaClass.getDeclaredField(field).apply { isAccessible = true }.set(owner, value)
    private fun views(view: View): List<View> = listOf(view) + if (view is ViewGroup) (0 until view.childCount).flatMap { views(view.getChildAt(it)) } else emptyList()
    @Before fun setup() {
        SharedRelay.resetForTest()
        for (name in listOf("private_state", "background-monitor", "pocket-appearance")) app.getSharedPreferences(name, 0).edit().clear().commit()
        PocketAppearance.load(app)
    }
    @After fun cleanup() = setup()
    private fun waitUntil(condition: () -> Boolean) {
        val end = System.nanoTime() + TimeUnit.SECONDS.toNanos(12)
        while (System.nanoTime() < end) {
            shadowOf(Looper.getMainLooper()).idleFor(Duration.ofMillis(20))
            if (condition()) return
            Thread.sleep(5)
        }
        fail("Invitation fixture timed out")
    }
    private fun openInvite(ui: NativeAgents): AlertDialog {
        ui.manageRelay("fixture")
        val menu = ShadowAlertDialog.getLatestAlertDialog()
        assertEquals("邀请新设备", menu.listView.adapter.getItem(1))
        shadowOf(menu).clickOnItem(1)
        shadowOf(Looper.getMainLooper()).idle()
        return ShadowAlertDialog.getLatestAlertDialog()
    }

    private fun capture(dialog: AlertDialog, theme: PocketTheme) {
        val root = dialog.window!!.decorView
        val width = AgentStyle.dp(root.context, 360); val height = AgentStyle.dp(root.context, 800)
        root.measure(View.MeasureSpec.makeMeasureSpec(width, View.MeasureSpec.EXACTLY), View.MeasureSpec.makeMeasureSpec(height, View.MeasureSpec.AT_MOST))
        root.layout(0, 0, root.measuredWidth, root.measuredHeight)
        val bitmap = Bitmap.createBitmap(root.width, root.height, Bitmap.Config.ARGB_8888)
        root.draw(Canvas(bitmap))
        val path = File("build/reports/device-invitation/sdk-${Build.VERSION.SDK_INT}/${theme.id}.png")
        path.parentFile!!.mkdirs()
        path.outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        bitmap.recycle()
    }

    @Test fun roleIsAThemedDropdownAndOnlyExplicitConfirmationSubmits() {
        for (theme in PocketTheme.entries) {
            assertTrue(PocketAppearance.save(app, theme))
            val context = ContextThemeWrapper(app, theme.style)
            val submitted = mutableListOf<Pair<String, String>>()
            val dialog = DeviceInvitationDialog.show(context) { name, role -> submitted += name to role }
            try {
                val selector = views(dialog.window!!.decorView).filterIsInstance<Spinner>().single()
                assertEquals("只读（reader）", selector.selectedItem.toString())
                assertEquals("控制者（controller）", selector.getItemAtPosition(1).toString())
                selector.setSelection(1)
                shadowOf(Looper.getMainLooper()).idle()
                capture(dialog, theme)
                assertEquals(theme.colors.ink, (selector.selectedView as TextView).currentTextColor)
                assertTrue(submitted.isEmpty())
                dialog.getButton(AlertDialog.BUTTON_NEGATIVE).performClick()
                assertTrue(submitted.isEmpty())
            } finally { dialog.dismiss() }
            val reopened = DeviceInvitationDialog.show(context) { name, role -> submitted += name to role }
            shadowOf(Looper.getMainLooper()).idle()
            try {
                assertEquals(0, views(reopened.window!!.decorView).filterIsInstance<Spinner>().single().selectedItemPosition)
                reopened.getButton(AlertDialog.BUTTON_POSITIVE).performClick()
                reopened.getButton(AlertDialog.BUTTON_POSITIVE).performClick()
                assertEquals(listOf("Android" to "reader"), submitted)
            } finally { reopened.dismiss() }
        }
    }

    @Test fun invalidNamesStayEditableWithoutSendingAnInvitation() {
        var sent = false
        val dialog = DeviceInvitationDialog.show(ContextThemeWrapper(app, PocketTheme.MINT.style)) { _, _ -> sent = true }
        shadowOf(Looper.getMainLooper()).idle()
        try {
            val name = views(dialog.window!!.decorView).filterIsInstance<EditText>().single()
            for (invalid in listOf("   ", "a".repeat(81), "中".repeat(27), "device\u0001name")) {
                name.setText(invalid)
                dialog.getButton(AlertDialog.BUTTON_POSITIVE).performClick()
                assertTrue(dialog.isShowing); assertNotNull(name.error); assertFalse(sent)
            }
            name.setText("我的手机")
            dialog.getButton(AlertDialog.BUTTON_POSITIVE).performClick()
            assertTrue(sent); assertFalse(dialog.isShowing)
        } finally { dialog.dismiss() }
    }

    @Test fun dropdownSendsTheSelectedRoleToTheRelayWithoutChangingCurrentCredentials() {
        MockTcpMonitorRelay().use { fixture ->
            fixture.role = "controller"
            val invitations = CopyOnWriteArrayList<JSONObject>()
            fixture.foregroundCommand = { request ->
                if (request.optString("type") == "create_device_invitation") {
                    invitations += JSONObject(request.toString())
                    JSONObject().put("invitation", JSONObject().put("secret", "synthetic-invitation-secret")
                        .put("invitation_id", "synthetic-invitation").put("version", 1)
                        .put("expires_at", Instant.now().plusSeconds(600).toString()))
                } else null
            }
            val shared = SharedRelay.get(app)
            set(shared.vault, "key\$delegate", lazyOf(SecretKeySpec(ByteArray(32) { 32 }, "AES")))
            set(shared.engine, "state", fixture.state()); shared.vault.write("state", fixture.state().toString())
            val controller = Robolectric.buildActivity(MainActivity::class.java).setup()
            try {
                waitUntil { shared.ready("fixture") }
                val ui = get(controller.get(), "nativeAgents") as NativeAgents
                val credentials = shared.engine.snapshot().getJSONObject("auth").toString()
                for ((index, role) in listOf("reader", "controller").withIndex()) {
                    val dialog = openInvite(ui)
                    val controls = views(dialog.window!!.decorView)
                    assertEquals("only the name is typed; permissions must use a dropdown", 1, controls.filterIsInstance<EditText>().size)
                    val selector = controls.filterIsInstance<Spinner>().single()
                    assertEquals("邀请权限", selector.contentDescription)
                    assertEquals(2, selector.count); assertEquals(0, selector.selectedItemPosition)
                    selector.setSelection(index)
                    controls.filterIsInstance<EditText>().single().setText("  测试设备 $role  ")
                    dialog.getButton(AlertDialog.BUTTON_POSITIVE).performClick()
                    dialog.getButton(AlertDialog.BUTTON_POSITIVE).performClick()
                    waitUntil { invitations.size == index + 1 }
                    assertEquals(role, invitations[index].getString("role"))
                    assertEquals("测试设备 $role", invitations[index].getString("name"))
                    waitUntil { views(ShadowAlertDialog.getLatestAlertDialog().window!!.decorView).filterIsInstance<TextView>().any { it.text.contains("herdr-tcp://") } }
                    ShadowAlertDialog.getLatestAlertDialog().dismiss()
                    assertEquals(credentials, shared.engine.snapshot().getJSONObject("auth").toString())
                }
                val cancelled = openInvite(ui)
                assertEquals(0, views(cancelled.window!!.decorView).filterIsInstance<Spinner>().single().selectedItemPosition)
                cancelled.getButton(AlertDialog.BUTTON_NEGATIVE).performClick()
                shadowOf(Looper.getMainLooper()).idle()
                assertEquals(2, invitations.size)
                // A previously opened form must still re-check the inviter's own role.
                val denied = openInvite(ui)
                views(denied.window!!.decorView).filterIsInstance<Spinner>().single().setSelection(1)
                val state = shared.engine.snapshot()
                state.getJSONObject("auth").getJSONObject("relays").getJSONObject("fixture").put("role", "reader")
                set(shared.engine, "state", state)
                denied.getButton(AlertDialog.BUTTON_POSITIVE).performClick()
                shadowOf(Looper.getMainLooper()).idle()
                @Suppress("UNCHECKED_CAST") val pending = get(shared.engine, "pending") as Map<String, *>
                assertTrue("reader must not enqueue an invitation request", pending.isEmpty())
                assertEquals(2, invitations.size)
                assertTrue(fixture.failures.toString(), fixture.failures.isEmpty())
            } finally { controller.pause().stop().destroy() }
        }
    }
}

package dev.herdr.companion

import android.app.AlertDialog
import android.content.ClipData
import android.content.Intent
import android.graphics.Color
import android.graphics.Typeface
import android.graphics.BitmapFactory
import android.graphics.drawable.GradientDrawable
import android.net.Uri
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.text.Editable
import android.text.TextWatcher
import android.text.InputType
import android.util.Base64
import android.view.*
import android.widget.*
import androidx.activity.ComponentActivity
import androidx.activity.OnBackPressedCallback
import androidx.activity.result.contract.ActivityResultContracts
import androidx.core.content.FileProvider
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsCompat
import io.noties.markwon.Markwon
import io.noties.markwon.ext.tables.TablePlugin
import io.noties.markwon.ext.strikethrough.StrikethroughPlugin
import org.json.JSONArray
import org.json.JSONObject
import java.io.File
import java.util.UUID

class MainActivity : ComponentActivity() {
    private val background get() = AgentStyle.background
    private val surface get() = AgentStyle.panel
    private val accent get() = AgentStyle.accent
    private val ink get() = AgentStyle.ink
    private val muted get() = AgentStyle.muted
    private lateinit var vault: Vault
    private lateinit var engine: RelayEngine
    private lateinit var sharedRelay: SharedRelay
    private var monitorRoute: JSONObject? = null
    private var monitorSettingsPending = false
    private val monitorPermission = registerForActivityResult(ActivityResultContracts.RequestPermission()) { allowed ->
        if (allowed) startMonitoring() else toast("未允许通知，常驻监控尚未开启。可在系统设置中允许后再试。")
    }
    private fun monitorDialog() = MonitorSettingsUi.show(this, ::enableMonitoring) { monitorControlsChanged() }
    private fun monitorControlsChanged() { if (tab == "devices") renderDevices() else nativeAgents.updateMonitorControls() }
    private fun enableMonitoring() {
        if (relays.length() == 0) { toast("请先配对至少一台电脑"); return }
        if (android.os.Build.VERSION.SDK_INT >= 33 && androidx.core.content.ContextCompat.checkSelfPermission(this, android.Manifest.permission.POST_NOTIFICATIONS) != android.content.pm.PackageManager.PERMISSION_GRANTED) {
            monitorPermission.launch(android.Manifest.permission.POST_NOTIFICATIONS)
        } else startMonitoring()
    }
    private fun startMonitoring() {
        runCatching { AgentMonitorService.start(this) }.onSuccess { toast("已开启：默认关注全部 Agent；请留意电池后台限制") }.onFailure { toast(it.message ?: "监控未启动，请检查通知权限") }
        monitorControlsChanged()
    }
    private fun acceptMonitorIntent(value: Intent) {
        if (value.action == MonitorNotifications.OPEN_AGENT) {
            monitorRoute = runCatching {
                val raw = value.getStringExtra(MonitorNotifications.EXTRA_ROUTE).orEmpty(); require(raw.length <= 1024)
                JSONObject(raw).also { require(it.getString("relay").length in 1..256 && it.getString("key").matches(Regex("[a-f0-9]{64}"))) }
            }.getOrNull()
        }
        monitorSettingsPending = value.action == "dev.herdr.companion.MONITOR_SETTINGS"
    }
    private fun openMonitorRoute() {
        val route = monitorRoute ?: return
        val id = route.optString("relay")
        if (!sharedRelay.ready(id)) return
        val list = agents[id] ?: return
        val agent = (0 until list.length()).map { list.getJSONObject(it) }.find { MonitorIdentity.matchesRoute(id, it, route) }
        fun navigate() {
            monitorRoute = null; relayId = id; tab = "agents"; render()
            if (agent == null) toast("该通知对应的会话已结束或替换，请从列表选择") else nativeAgents.open(id, agent)
        }
        if (file != null && tab == "files") leaveFile(::navigate) else navigate()
    }
    private lateinit var root: LinearLayout
    private lateinit var content: FrameLayout
    private lateinit var nav: LinearLayout
    private lateinit var header: LinearLayout
    private var keyboardVisible = false
    private var agentDetailVisible = false
    private lateinit var heading: TextView
    private lateinit var subtitle: TextView
    private lateinit var stateDot: TextView
    private lateinit var nativeAgents: NativeAgents
    private var relays = JSONArray()
    private val statuses = mutableMapOf<String, String>()
    private val capabilities = mutableMapOf<String, Set<String>>()
    private val agents = mutableMapOf<String, JSONArray>()
    private var tab = "files"
    private var relayId = ""
    private var selectedAgent: JSONObject? = null
    private var entries = JSONArray()
    private var directory = ""
    private var treeRoot = ""
    private var treeTruncated = false
    private var directoryPage: WorkspaceDirectoryPage? = null
    private var treeError = ""
    private var treeRequest = 0
    private var treePending = false
    private var query = ""
    private var searchField: EditText? = null
    private var fileList: ListView? = null
    private var file: JSONObject? = null
    private var originalText = ""
    private var draft = ""
    private var editing = false
    private var saving = false
    private var busy = false
    private var fileGeneration = 0
    private var pdf: PdfPreview? = null
    private var fileReviewer: DocumentReviewer? = null
    private val documentDownloads = DocumentDownloads(this)
    private val unrestrictedFiles = mutableSetOf<String>()
    private var editor: EditText? = null
    private var fileBody: FrameLayout? = null
    private var editToggle: Button? = null
    private var saveButton: Button? = null
    private var editStatus: TextView? = null
    private var pendingExport: Pair<String, ByteArray>? = null
    private var sharedLink = ""
    private val main = Handler(Looper.getMainLooper())
    private var transientToast: Toast? = null
    private val persistDraft = Runnable { keepDraft() }
    private val picker = registerForActivityResult(SaveDocument()) { uri ->
        val export = pendingExport; pendingExport = null
        if (uri != null && export != null) {
            Thread {
                val result = runCatching { SavedDocument.write(this, uri, export.first) { it.write(export.second) } }
                runOnUiThread {
                    result.fold({ SavedDocument.report(this, it) }, { toast("保存失败，请检查存储空间；所选位置可能留有不完整文件") })
                }
            }.start()
        }
    }
    private val attachmentPicker = registerForActivityResult(ActivityResultContracts.OpenMultipleDocuments()) { uris -> nativeAgents.upload(uris) }
    private val scanner = registerForActivityResult(ActivityResultContracts.StartActivityForResult()) { result ->
        if (result.resultCode == RESULT_OK) {
            if (result.data?.getBooleanExtra(QrScannerActivity.EXTRA_MANUAL, false) == true) pairDialog()
            else result.data?.getStringExtra(QrScannerActivity.EXTRA_CONTENT)?.let { confirmPair(it) }
        }
    }

    override fun onCreate(state: Bundle?) {
        setTheme(PocketAppearance.load(this).style)
        super.onCreate(state)
        PocketAppearance.applyWindow(this)
        sharedRelay = SharedRelay.get(this)
        vault = sharedRelay.vault
        readRelays()
        relayId = state?.getString("relay") ?: relays.optJSONObject(0)?.optString("id").orEmpty()
        tab = state?.getString("tab") ?: if (relays.length() == 0) "devices" else "agents"
        root = LinearLayout(this).apply { orientation = LinearLayout.VERTICAL; setBackgroundColor(this@MainActivity.background) }
        header = LinearLayout(this).apply { orientation = LinearLayout.HORIZONTAL; gravity = Gravity.CENTER_VERTICAL; setPadding(dp(20), dp(12), dp(20), dp(12)) }
        val titles = column()
        heading = label("工作文件", 24, ink, true)
        subtitle = label("把工作现场，装进口袋。", 12, muted)
        titles.addView(heading); titles.addView(subtitle)
        header.addView(titles, LinearLayout.LayoutParams(0, -2, 1f))
        stateDot = label("", 12, muted); header.addView(stateDot, LinearLayout.LayoutParams(dp(24), dp(24)))
        root.addView(header)
        content = FrameLayout(this)
        val expanded = resources.configuration.screenWidthDp >= 600
        nav = LinearLayout(this).apply { orientation = if (expanded) LinearLayout.VERTICAL else LinearLayout.HORIZONTAL; setPadding(dp(8), dp(6), dp(8), dp(6)); setBackgroundColor(surface) }
        if (expanded) {
            root.addView(LinearLayout(this).apply { addView(nav, LinearLayout.LayoutParams(dp(88), -1)); addView(content, LinearLayout.LayoutParams(0, -1, 1f)) }, LinearLayout.LayoutParams(-1, 0, 1f))
        } else { root.addView(content, LinearLayout.LayoutParams(-1, 0, 1f)); root.addView(nav) }
        engine = sharedRelay.engine
        nativeAgents = NativeAgents(this, engine, vault, { agents }, ::selectWorkspace,
            { attachmentPicker.launch(arrayOf("*/*")) }, { detail -> agentDetailVisible = detail; updateChrome() }, { monitorDialog() }, documentDownloads, { it in unrestrictedFiles })
        setContentView(root)
        ViewCompat.setOnApplyWindowInsetsListener(root) { view, insets ->
            val bars = insets.getInsets(WindowInsetsCompat.Type.systemBars() or WindowInsetsCompat.Type.ime())
            keyboardVisible = insets.isVisible(WindowInsetsCompat.Type.ime()); updateChrome()
            view.setPadding(bars.left, bars.top, bars.right, bars.bottom); insets
        }
        onBackPressedDispatcher.addCallback(this, object : OnBackPressedCallback(true) {
            override fun handleOnBackPressed() {
                when {
                    file != null && tab == "files" -> leaveFile { render() }
                    tab == "files" && directory.isNotEmpty() -> navigateDirectory(FileRules.parent(directory))
                    tab == "agents" && nativeAgents.back() -> Unit
                    tab == "files" && selectedAgent != null -> returnToSelectedAgent()
                    tab != "agents" -> changeTab("agents")
                    else -> moveTaskToBack(true)
                }
            }
        })
        File(cacheDir, "shared").listFiles()?.filter { System.currentTimeMillis() - it.lastModified() > 86400000 }?.forEach { it.deleteRecursively() }
        sharedLink = intent.getStringExtra(Intent.EXTRA_TEXT).orEmpty()
        acceptMonitorIntent(intent)
        render()
    }
    override fun onNewIntent(intent: Intent) { super.onNewIntent(intent); setIntent(intent); intent.getStringExtra(Intent.EXTRA_TEXT)?.let { confirmPair(it) }; acceptMonitorIntent(intent); openMonitorRoute(); if (monitorSettingsPending) { monitorSettingsPending = false; monitorDialog() } }
    override fun onSaveInstanceState(outState: Bundle) { outState.putString("relay", relayId); outState.putString("tab", tab); super.onSaveInstanceState(outState) }
    override fun onStart() {
        super.onStart()
        if (::sharedRelay.isInitialized) sharedRelay.attachUI(this, ::onEvent)
        if (::nativeAgents.isInitialized && tab == "agents") nativeAgents.attach(content)
        if (MonitorPreferences(this).enabled && !AgentMonitorService.active && MonitorNotifications.allowed(this)) runCatching { AgentMonitorService.start(this) }
        openMonitorRoute()
        if (monitorSettingsPending) { monitorSettingsPending = false; monitorDialog() }
        if (sharedLink.isNotBlank() && engine.ready) { val link = sharedLink; sharedLink = ""; confirmPair(link) }
        monitorControlsChanged()
    }
    override fun onStop() { keepDraft(); nativeAgents.pause(); sharedRelay.detachUI(this); super.onStop() }
    override fun onDestroy() { documentDownloads.close(); transientToast?.cancel(); main.removeCallbacks(persistDraft); pdf?.close(); fileReviewer?.close(); nativeAgents.destroy(); sharedRelay.detachUI(this); super.onDestroy() }

    private fun readRelays() { relays = runCatching { JSONObject(vault.read("state") ?: "{}").optJSONArray("relays") ?: JSONArray() }.getOrElse { JSONArray() } }
    private fun updateChrome() {
        val hide = keyboardVisible || agentDetailVisible
        header.visibility = if (hide) View.GONE else View.VISIBLE
        nav.visibility = if (hide) View.GONE else View.VISIBLE
    }
    private fun onEvent(event: JSONObject) {
        when (event.optString("type")) {
            "monitor_state" -> monitorControlsChanged()
            "ready" -> if (sharedLink.isNotBlank()) { confirmPair(sharedLink); sharedLink = "" }
            "persist" -> { readRelays(); if (relayId.isEmpty()) relayId = relays.optJSONObject(0)?.optString("id").orEmpty() }
            "paired" -> { relayId = event.getJSONObject("relay").getString("id"); render(); toast("已添加，正在进行加密配对…") }
            "enrolled" -> { toast("设备配对成功"); if (tab == "devices") render() }
            "removed" -> { readRelays(); relayId = relays.optJSONObject(0)?.optString("id").orEmpty(); selectedAgent = null; render() }
            "renamed" -> { readRelays(); updateHeader(); if (tab == "devices") { content.removeAllViews(); renderDevices() } }
            "status" -> {
                val id = event.getString("relayId"); statuses[id] = event.getString("status"); updateHeader()
                if (tab == "devices") render()
                if (event.optJSONObject("detail")?.optBoolean("fatal") == true) toast("配对凭据已失效或连接被拒绝，请检查设备邀请")
            }
            "message" -> {
                val id = event.getString("relayId"); val message = event.getJSONObject("message")
                when (message.optString("type")) {
                    "push_config" -> {
                        val list = message.optJSONArray("capabilities") ?: JSONArray()
                        capabilities[id] = (0 until list.length()).map { list.getString(it) }.toSet()
                        updateFileAccess(id, message.optBoolean("filesystem_read"))
                    }
                    "blocked", "agent_update" -> {
                        val list = agents[id] ?: JSONArray()
                        val pane = message.optString("pane_id")
                        val index = (0 until list.length()).firstOrNull { list.getJSONObject(it).optString("pane_id") == pane }
                        val updated = if (index == null) JSONObject() else JSONObject(list.getJSONObject(index).toString())
                        message.keys().forEach { key -> if (key != "type") updated.put(key, message.get(key)) }
                        if (message.optString("type") == "blocked") updated.put("status", "blocked")
                        if (index == null) list.put(updated) else list.put(index, updated)
                        agents[id] = list
                    }
                    "agent_removed" -> {
                        val list = agents[id] ?: JSONArray()
                        agents[id] = JSONArray((0 until list.length()).map { list.getJSONObject(it) }.filter { it.optString("pane_id") != message.optString("pane_id") })
                    }
                    "agents" -> {
                        agents[id] = message.optJSONArray("agents") ?: JSONArray()
                        if (id == relayId && file == null && selectedAgent != null) {
                            val current = agents[id]!!
                            val replacement = (0 until current.length()).map { current.getJSONObject(it) }.find { it.optString("pane_id") == selectedAgent?.optString("pane_id") }
                            if (replacement == null || WorkspaceBinding.fromAgent(id, replacement) != selectedBinding()) {
                                fileReviewer?.close(); fileReviewer = null
                                selectedAgent = null; entries = JSONArray(); directoryPage = null; busy = false; treePending = false; treeRequest++; fileGeneration++
                            }
                            else selectedAgent = replacement
                        }
                        if (file != null) updateEditorState()
                        if (tab == "files" && selectedAgent == null && file == null) render()
                    }
                }
            }
            "failure" -> toast(event.optString("message", "操作失败"))
        }
        if (::nativeAgents.isInitialized) nativeAgents.onEvent(event)
        if (::sharedRelay.isInitialized && event.optString("type") == "message" && event.optJSONObject("message")?.optString("type") == "agents") openMonitorRoute()
    }
    private fun updateHeader() {
        heading.text = when (tab) { "agents" -> "Agent 工作台"; "devices" -> "我的设备"; else -> if (file != null) FileRules.safeName(file!!.optString("path")) else if (relayId in unrestrictedFiles) "电脑文件" else "Agent 工作区" }
        heading.maxLines = 1; heading.ellipsize = android.text.TextUtils.TruncateAt.END
        val connected = statuses[relayId] == "connected"
        stateDot.setCompoundDrawables(AgentStyle.icon(this, "dot", if (connected) accent else muted, 24), null, null, null); stateDot.contentDescription = if (connected) "已加密连接" else "等待连接"
        subtitle.text = if (relayId.isEmpty()) "从连接第一台电脑开始" else "${relayLabel()}  ·  ${if (connected) "已加密连接" else "等待连接"}"
    }
    private fun relayLabel(): String = (0 until relays.length()).map { relays.getJSONObject(it) }.find { it.optString("id") == relayId }?.optString("label") ?: "设备"
    private fun changeTab(next: String) {
        if (saving) { toast("正在保存，请稍候"); return }
        if (tab == "files" && file != null && next != tab) { leaveFile { tab = next; render() }; return }
        tab = next; render()
    }
    private fun renderNavigation() {
        updateHeader(); nav.removeAllViews()
        val expanded = resources.configuration.screenWidthDp >= 600
        listOf(Triple("agents", "Agents", "agents"), Triple("files", "工作区", "folder"), Triple("devices", "设备", "computer")).forEach { (id, title, icon) ->
            val item = LinearLayout(this).apply {
                orientation = LinearLayout.VERTICAL; gravity = Gravity.CENTER; minimumHeight = dp(64)
                background = AgentStyle.ripple(this@MainActivity); isFocusable = true; isSelected = id == tab; contentDescription = title; AgentStyle.actionSemantics(this)
                setOnClickListener { changeTab(id) }
                addView(ImageView(this@MainActivity).apply { setImageDrawable(AgentIcon(icon, if (id == tab) accent else muted)); setPadding(dp(16), dp(4), dp(16), dp(4)); background = AgentStyle.shape(this@MainActivity, if (id == tab) AgentStyle.selected else Color.TRANSPARENT, 12); importantForAccessibility = View.IMPORTANT_FOR_ACCESSIBILITY_NO }, LinearLayout.LayoutParams(dp(56), dp(30)))
                addView(label(title, 11, if (id == tab) ink else muted, id == tab).apply { gravity = Gravity.CENTER; importantForAccessibility = View.IMPORTANT_FOR_ACCESSIBILITY_NO })
            }
            nav.addView(item, if (expanded) LinearLayout.LayoutParams(-1, dp(76)) else LinearLayout.LayoutParams(0, dp(64), 1f))
        }
    }
    private fun render() {
        renderNavigation()
        if (tab != "agents") nativeAgents.pause()
        content.removeAllViews()
        when (tab) { "devices" -> renderDevices(); "agents" -> renderAgents(); else -> if (file == null) renderFiles() else renderFile() }
    }
    private fun applyAppearance(choice: PocketTheme) {
        // The picker belongs to Devices. Repaint only this page; do not pause
        // Agents, recreate the Activity, or cancel in-flight transfers/requests.
        if (tab != "devices" || isFinishing || isDestroyed) return
        if (!PocketAppearance.save(this, choice)) { toast("主题保存失败，请重试"); return }
        PocketAppearance.applyWindow(this)
        root.setBackgroundColor(background)
        nav.setBackgroundColor(surface)
        heading.setTextColor(ink); subtitle.setTextColor(muted); stateDot.setTextColor(muted)
        renderNavigation()
        content.removeAllViews(); renderDevices()
    }
    private fun renderDevices() {
        val body = scrollColumn()
        body.addView(AgentStyle.button(this, "主题配色 · ${PocketAppearance.current.title}") {
            AppearanceDialog.show(this, ::applyAppearance)
        }.apply { contentDescription = "主题配色" }.space(0, 0, 0, 12))
        body.addView(label("扫描电脑端的设备邀请，或粘贴完整配对链接。每台手机有独立凭据。", 14, muted).space(0, 8, 0, 20))
        val actions = LinearLayout(this)
        actions.addView(button("扫描二维码", true) {
            scanner.launch(Intent(this, QrScannerActivity::class.java))
        }, LinearLayout.LayoutParams(0, dp(52), 1f))
        actions.addView(button("粘贴链接") { pairDialog() }, LinearLayout.LayoutParams(0, dp(52), 1f).apply { marginStart = dp(8) })
        body.addView(actions)
        body.addView(AgentStyle.button(this, if (AgentMonitorService.active) "常驻监控：运行中 · 通知设置" else "开启常驻监控 / 完成提醒", subtle = true) { monitorDialog() }.space(0, 16, 0, 4))
        body.addView(label("默认关注全部 Agent；单个 Agent 的完成提醒可在其 ⋯ 菜单关闭。", 12, muted))
        body.addView(label("已添加设备  /  ${relays.length()}", 12, muted).space(0, 28, 0, 12))
        for (i in 0 until relays.length()) {
            val relay = relays.getJSONObject(i); val id = relay.getString("id")
            val card = card()
            val nameRow = LinearLayout(this).apply { gravity = Gravity.CENTER_VERTICAL }
            nameRow.addView(label(relay.optString("label", "我的电脑"), 18, ink, true).apply {
                maxLines = 2; ellipsize = android.text.TextUtils.TruncateAt.END
            }, LinearLayout.LayoutParams(0, -2, 1f))
            nameRow.addView(AgentStyle.button(this, "重命名", subtle = true) {
                ComputerNameDialog.show(this, "重命名电脑", relay.optString("label"), "保存") { name ->
                    engine.renameComputer(id, name); toast("名称已保存，仅在本机生效")
                }
            }.apply { contentDescription = "重命名电脑：${relay.optString("label")}" }, LinearLayout.LayoutParams(-2, dp(48)))
            card.addView(nameRow)
            card.addView(label(when (statuses[id]) { "connected" -> "已连接 · 端到端加密"; "connecting" -> "正在连接…"; else -> "未连接 · 可重新连接或重新配对" }, 12, if (statuses[id] == "connected") accent else muted).space(0, 5, 0, 12))
            val row = LinearLayout(this).apply { gravity = Gravity.CENTER_VERTICAL }
            row.addView(AgentStyle.button(this, "查看 Agents", subtle = true) { relayId = id; selectedAgent = null; tab = "agents"; render() }, LinearLayout.LayoutParams(0, -2, 1f).apply { marginEnd = dp(4); marginStart = dp(4) })
            row.addView(AgentStyle.button(this, "重连", subtle = true) { engine.call("reconnect") }, LinearLayout.LayoutParams(0, -2, 1f).apply { marginEnd = dp(4); marginStart = dp(4) })
            row.addView(AgentStyle.button(this, "移除", subtle = true) {
                AlertDialog.Builder(this).setTitle("从本机移除设备？").setMessage("这会清除本机的连接凭据。电脑端的设备授权可在 Agent 工作台的设置中撤销。")
                    .setPositiveButton("移除") { _, _ -> engine.call("remove", id) }.setNegativeButton("取消", null).show()
            }, LinearLayout.LayoutParams(0, -2, 1f).apply { marginEnd = dp(4); marginStart = dp(4) })
            card.addView(row)
            if (capabilities[id]?.contains("filesystem_read_opt_in_v1") == true) card.addView(button(if (id in unrestrictedFiles) "工作区外文件访问：已开启 ⚠" else "工作区外文件访问：关闭") { fileAccessDialog(id) })
            body.addView(card.space(0, 0, 0, 12))
        }
        body.addView(label("HERDR POCKET  ${BuildConfig.VERSION_NAME}", 12, accent).space(0, 28, 0, 8))
        body.addView(label("原生 Agent 对话、终端、审批与文件工具\n基于 Herdr Mobile Relay 0.24.0 · AGPL-3.0-or-later\n内置预览：图片/PDF 最大 12 MiB，文本最大 1 MiB。超限文件可下载/分享，分片写入手机缓存，需要足够存储空间。", 12, muted))
        body.addView(button("查看开源许可证") { AlertDialog.Builder(this).setTitle("GNU AGPL v3").setMessage(assets.open("LICENSE").bufferedReader().readText()).setPositiveButton("关闭", null).show() }.space(0, 14, 0, 0))
    }
    private fun updateFileAccess(id: String, enabled: Boolean) {
        if (::sharedRelay.isInitialized) sharedRelay.rememberFileAccess(id, enabled)
        val changed = if (enabled) unrestrictedFiles.add(id) else unrestrictedFiles.remove(id)
        if (!changed) return
        documentDownloads.cancel(); fileReviewer?.close(); fileReviewer = null
        if (id == relayId) {
            fileGeneration++; treeRequest++; treePending = false; busy = false; file = null; entries = JSONArray(); directoryPage = null; treeRoot = ""
            directory = if (enabled) selectedAgent?.optString("cwd").orEmpty().trim('/') else ""
            if (tab == "files" && selectedAgent != null) loadTree()
        }
        if (::nativeAgents.isInitialized) nativeAgents.fileAccessChanged(id)
        if (tab == "devices") renderDevices()
    }
    private fun fileAccessDialog(id: String) {
        engine.request(id, JSONObject().put("type", "file_access_get").put("protocol", 3), 15000) { data, error ->
            if (error != null || data == null) { toast(error ?: "未读取到文件权限"); return@request }
            val enabled = data.optBoolean("filesystem_read"); updateFileAccess(id, enabled)
            val role = engine.snapshot().optJSONObject("auth")?.optJSONObject("relays")?.optJSONObject(id)?.optString("role")
            if (role != "controller") { toast("仅控制者设备可以开启工作区外文件访问"); return@request }
            AlertDialog.Builder(this).setTitle(if (enabled) "关闭工作区外文件访问？" else "允许访问这台电脑的工作区外文件？")
                .setMessage(if (enabled) "关闭后恢复 Agent 工作区限制，并取消当前文件读取和下载。" else "仅为当前手机授权。开启后可以浏览、预览和下载 Relay 运行用户有权限读取的电脑文件，包括密钥、配置等私密文件。\n\n仅限只读，不允许修改或删除；不会自动上传文件。授权保存在电脑 Relay，可随时在此关闭。")
                .setNegativeButton("取消", null).setPositiveButton(if (enabled) "关闭访问" else "我了解风险，开启") { _, _ ->
                    engine.request(id, JSONObject().put("type", "file_access_set").put("protocol", 3).put("filesystem_read", !enabled), 15000) { result, failure ->
                        if (failure != null || result == null) toast(failure ?: "设置失败")
                        else { updateFileAccess(id, result.optBoolean("filesystem_read")); toast(if (result.optBoolean("filesystem_read")) "已开启只读文件访问；文件页可返回 / 根目录" else "已恢复工作区限制") }
                    }
                }.show()
        }
    }
    private fun pairDialog() {
        val input = EditText(this).apply { hint = "herdr-tcp://IP:端口/#invite=… 或 HTTPS 链接"; inputType = InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_VARIATION_URI; minLines = 3; maxLines = 6 }
        AlertDialog.Builder(this).setTitle("添加电脑").setView(input).setPositiveButton("检查链接") { _, _ -> confirmPair(input.text.toString()) }.setNegativeButton("取消", null).show()
    }
    private fun confirmPair(value: String) {
        if (!engine.ready) { sharedLink = value; toast("连接引擎正在启动"); return }
        val parsed = try { PairingLink.parse(value) } catch (_: Exception) { toast("配对链接无效或已过期，请从可信电脑重新生成邀请"); return }
        val uri = Uri.parse(value.trim())
        val params = Uri.parse("https://invitation.invalid/?" + uri.encodedFragment.orEmpty())
        val destination = (params.getQueryParameter("relay") ?: params.getQueryParameter("gateways"))
            ?.split(',')?.mapNotNull { runCatching { Uri.parse(it).host }.getOrNull() }?.joinToString("、") ?: uri.host
        // Never display or log invitation fragments: they contain the one-use secret.
        val suggestedName = if (params.getQueryParameter("label").isNullOrBlank()) "" else parsed.relay.optString("label")
        ComputerNameDialog.show(this, "连接这台电脑？", suggestedName, "配对",
            "连接地址：$destination" + (if (uri.scheme == "herdr-tcp") "\n原生 TCP · 应用层端到端加密（无 HTTPS）" else "") + "\n请确认二维码或链接来自你信任的电脑。") { name ->
            engine.call("pair", value.trim(), name)
        }
    }
    private fun renderAgents() {
        if (relays.length() == 0) { emptyState("还没有连接电脑", "先添加设备，即可查看原生对话、终端和审批。", "添加设备") { changeTab("devices") }; return }
        nativeAgents.attach(content)
    }
    private fun agentName(agent: JSONObject): String = AgentPresentation.title(agent)
    private fun selectWorkspace(relay: String, agent: JSONObject) {
        relayId = relay; selectedAgent = JSONObject(agent.toString()); directory = if (relay in unrestrictedFiles) agent.optString("cwd").trim('/') else ""; query = ""; treeRoot = ""; entries = JSONArray(); busy = false
        directoryPage = null; treeError = ""; treeTruncated = false; treePending = false; treeRequest++
        tab = "files"; fileGeneration++; loadTree()
    }
    private fun selectedBinding(): WorkspaceBinding? = selectedAgent?.let { WorkspaceBinding.fromAgent(relayId, it) }
    private fun returnToSelectedAgent() {
        selectedAgent?.let { nativeAgents.open(relayId, it) }
        tab = "agents"; render()
    }
    private fun selectedAgentIsCurrent(): Boolean {
        val selected = selectedBinding() ?: return false
        val list = agents[relayId] ?: return false
        return (0 until list.length()).any { WorkspaceBinding.fromAgent(relayId, list.getJSONObject(it)) == selected }
    }

    private fun renderFiles() {
        if (relays.length() == 0) { emptyState("工作文件，随身可用", "浏览电脑项目、修改文本、阅读 Markdown 和 PDF，再通过安卓分享给其他应用。", "连接第一台电脑") { changeTab("devices") }; return }
        val body = column().apply { setPadding(dp(20), 0, dp(20), 0) }; content.addView(body, FrameLayout.LayoutParams(AgentStyle.contentWidth(this, 760), -1, Gravity.CENTER_HORIZONTAL))
        val top = LinearLayout(this)
        if (selectedAgent == null) {
            top.addView(AgentStyle.button(this, relayLabel(), subtle = true) { chooseRelay() }.apply { setCompoundDrawables(null, null, AgentStyle.icon(this@MainActivity, "down", size = 16), null) }, LinearLayout.LayoutParams(0, dp(48), 1f))
        } else {
            top.addView(AgentStyle.button(this, if (directory.isEmpty()) "返回 Agent" else "上一级", subtle = true) {
                if (directory.isEmpty()) returnToSelectedAgent() else navigateDirectory(FileRules.parent(directory))
            }.apply { gravity = Gravity.CENTER_VERTICAL or Gravity.START; setCompoundDrawables(AgentStyle.icon(this@MainActivity, "back", size = 16), null, null, null); compoundDrawablePadding = dp(8) }, LinearLayout.LayoutParams(0, dp(48), 1f))
            top.addView(AgentStyle.iconButton(this, "computer", "选择电脑：${relayLabel()}") { chooseRelay() }, LinearLayout.LayoutParams(dp(48), dp(48)))
        }
        top.addView(AgentStyle.iconButton(this, "refresh", "刷新") { if (selectedAgent == null) engine.call("reconnect") else loadTree() }, LinearLayout.LayoutParams(dp(48), dp(48)))
        body.addView(top)
        if (selectedAgent == null) {
            body.addView(label("选择 Agent 的工作区", 20, ink, true).space(0, 24, 0, 8))
            body.addView(label("也可以在 Agent 的对话或终端页面，直接打开它的工作区。", 13, muted).space(0, 0, 0, 16))
            val list = LinearLayout(this).apply { orientation = LinearLayout.VERTICAL }
            body.addView(ScrollView(this).apply { addView(list) }, LinearLayout.LayoutParams(-1, 0, 1f))
            val all = agents[relayId] ?: JSONArray()
            if (all.length() == 0) list.addView(label("${if (statuses[relayId] == "connected") "暂无 Agent 工作目录，请先在电脑端打开一个项目。" else "连接后会显示工作目录。"}", 14, muted).space(0, 20, 0, 0))
            for (i in 0 until all.length()) {
                val agent = all.getJSONObject(i)
                if (agent.optString("cwd").isBlank()) continue
                val card = card()
                card.addView(label(agentName(agent), 17, ink, true))
                card.addView(label("${agent.optString("agent")} · ${agent.optString("project")} · ${agent.optString("pane_id")}", 12, muted))
                card.addView(label(agent.optString("cwd"), 12, muted).space(0, 5, 0, 8))
                card.addView(label("打开此 Agent 的工作区 →", 13, accent))
                card.setOnClickListener { selectWorkspace(relayId, agent) }
                list.addView(card.space(0, 0, 0, 12))
            }
            val drafts = vault.names("draft:")
            if (drafts.isNotEmpty()) body.addView(button("恢复本机草稿（${drafts.size}）", true) { chooseDraft() }.space(0, 12, 0, 12))
            return
        }
        body.addView(label(if (relayId in unrestrictedFiles) "电脑文件 · 只读访问已扩展" else "${agentName(selectedAgent!!)} 的工作区", 18, ink, true).space(0, 8, 0, 4))
        if (relayId in unrestrictedFiles) {
            val jumps = LinearLayout(this)
            jumps.addView(button("/ 根目录") { navigateDirectory("") }, LinearLayout.LayoutParams(0, dp(48), 1f).apply { marginEnd = dp(4); marginStart = dp(4) })
            jumps.addView(button("Agent 工作目录") { navigateDirectory(selectedAgent!!.optString("cwd").trim('/')) }, LinearLayout.LayoutParams(0, dp(48), 1f).apply { marginEnd = dp(4); marginStart = dp(4) })
            jumps.addView(button("输入路径") {
                val input = EditText(this).apply { setSingleLine(); hint = "/home/…"; setText("/$directory") }
                AlertDialog.Builder(this).setTitle("跳转到电脑目录").setView(input).setNegativeButton("取消", null).setPositiveButton("打开") { _, _ ->
                    val raw = input.text.toString().trim()
                    val path = runCatching { java.nio.file.Paths.get(raw).normalize() }.getOrNull()
                    if (path == null || !path.isAbsolute) toast("请输入绝对目录路径") else navigateDirectory(path.toString().trim('/'))
                }.show()
            }, LinearLayout.LayoutParams(0, dp(48), 1f).apply { marginEnd = dp(4); marginStart = dp(4) })
            body.addView(jumps)
        }
        val absoluteDirectory = (treeRoot.ifBlank { if (relayId in unrestrictedFiles) "/" else selectedAgent!!.optString("cwd") }.trimEnd('/') + if (directory.isEmpty()) "" else "/$directory").ifBlank { "/" }
        body.addView(label(absoluteDirectory, 12, muted).apply { maxLines = 3; setTextIsSelectable(true) }.space(0, 4, 0, 10))
        searchField = EditText(this).apply {
            hint = if (directoryPaging()) "筛选当前目录已加载的条目…" else "搜索已加载的工作区条目…"; setTextColor(ink); setHintTextColor(muted); textSize = 14f; setSingleLine(); setText(query)
            background = rounded(surface); setPadding(dp(14), dp(8), dp(14), dp(8))
            addTextChangedListener(watcher { query = it; renderEntries() })
        }
        body.addView(AgentStyle.search(this, searchField!!), LinearLayout.LayoutParams(-1, -2))
        body.addView(CheckBox(this).apply {
            text = "显示隐藏文件/目录"; textSize = 12f; setTextColor(muted); minimumHeight = dp(48)
            isChecked = showHiddenFiles()
            setOnCheckedChangeListener { _, checked ->
                getSharedPreferences("file-browser", MODE_PRIVATE).edit().putBoolean("show-hidden", checked).apply(); renderEntries()
            }
        })
        if (directoryPaging()) {
            val page = directoryPage
            val summary = when {
                treePending -> if (page == null) "正在读取当前目录…" else "正在加载更多…已加载 ${page.entries.length()}/${page.total} 项"
                page != null -> "当前目录已加载 ${page.entries.length()}/${page.total} 项" + if (page.skipped > 0) " · 已跳过 ${page.skipped} 个符号链接或特殊条目" else ""
                else -> "当前目录尚未加载"
            }
            body.addView(label(summary, 12, muted).space(0, 8, 0, 4))
            if (treePending) body.addView(ProgressBar(this).apply { isIndeterminate = true }, LinearLayout.LayoutParams(dp(24), dp(24)))
            if (page?.cursor?.isNotBlank() == true) body.addView(button("加载更多目录项（${page.entries.length()}/${page.total}）") { loadTree(true) }.apply { isEnabled = !busy })
        } else if (treeTruncated) body.addView(label("旧 Relay 返回的工作区树已截断，部分目录尚未列出；请升级 Relay 以逐层浏览。", 12, muted).space(0, 8, 0, 8))
        if (treeError.isNotBlank()) body.addView(label("读取失败：$treeError · 点顶部刷新重试", 12, accent).space(0, 6, 0, 6))
        if (capabilities[relayId]?.contains("workspace_mobile_files_v1") != true) body.addView(label(
            if (capabilities[relayId]?.contains("workspace_file_read_v1") == true) "只读工作区 · 支持图片、PDF、Markdown 查看与下载；暂不支持保存修改。"
            else "只读工作区 · 此 Relay 仅支持基础图片/文本预览。", 12, muted).space(0, 8, 0, 8))
        fileList = ListView(this).apply { divider = android.graphics.drawable.ColorDrawable(surface); dividerHeight = dp(1) }
        body.addView(fileList, LinearLayout.LayoutParams(-1, 0, 1f))
        renderEntries()
    }
    private fun renderEntries() {
        val list = fileList ?: return
        if (busy && entries.length() == 0) { list.adapter = ArrayAdapter(this, android.R.layout.simple_list_item_1, listOf("正在读取…")); return }
        val shown = (0 until entries.length()).map { entries.getJSONObject(it) }
            .filter { showHiddenFiles() || !it.optString("name").startsWith('.') }
            .filter { if (query.isBlank()) FileRules.parent(it.getString("path")) == directory else it.getString("path").contains(query, true) }
            .sortedWith(compareBy<JSONObject> { it.optString("kind") != "directory" }.thenBy { it.optString("name").lowercase() })
        if (shown.isEmpty()) { list.adapter = ArrayAdapter(this, android.R.layout.simple_list_item_1, listOf(when {
            treeError.isNotBlank() -> "目录读取失败，尚不能判断是否为空"
            directoryPaging() && directoryPage == null -> "目录尚未加载，请刷新"
            !directoryPaging() && treeTruncated -> "此目录未完整加载，请升级 Relay"
            query.isNotBlank() -> "已加载条目中没有匹配项" + if (directoryPage?.cursor?.isNotBlank() == true) "，可继续加载更多" else ""
            !showHiddenFiles() && (0 until entries.length()).any { FileRules.parent(entries.getJSONObject(it).getString("path")) == directory } -> "已加载条目均为隐藏项，可开启“显示隐藏文件/目录”"
            else -> "这个目录是空的"
        })); return }
        list.adapter = object : BaseAdapter() {
            override fun getCount() = shown.size
            override fun getItem(position: Int) = shown[position]
            override fun getItemId(position: Int) = position.toLong()
            override fun getView(position: Int, convertView: View?, parent: ViewGroup?): View {
            val entry = shown[position]
            val path = entry.getString("path"); val folder = entry.optString("kind") == "directory"
            val row = LinearLayout(this@MainActivity).apply { gravity = Gravity.CENTER_VERTICAL; setPadding(dp(2), dp(10), dp(2), dp(10)); minimumHeight = dp(64) }
            val type = if (folder) "DIR" else path.substringAfterLast('.', "TXT").uppercase().take(4)
            row.addView(ImageView(this@MainActivity).apply { setImageDrawable(AgentIcon(if (folder) "folder" else "file", if (folder) accent else muted)); setPadding(dp(4), dp(4), dp(4), dp(4)); contentDescription = if (folder) "文件夹" else type }, LinearLayout.LayoutParams(dp(32), dp(32)))
            val texts = column().apply { setPadding(dp(14), 0, dp(8), 0) }
            texts.addView(label(entry.optString("name", FileRules.safeName(path)), 15, ink).apply { maxLines = 1; ellipsize = android.text.TextUtils.TruncateAt.END })
            texts.addView(label(if (query.isNotBlank()) path else if (folder) "文件夹" else FileRules.size(entry.optLong("size")), 11, muted).space(0, 3, 0, 0))
            row.addView(texts, LinearLayout.LayoutParams(0, -2, 1f)); row.addView(ImageView(this@MainActivity).apply { setImageDrawable(AgentIcon("chevron", muted)); importantForAccessibility = View.IMPORTANT_FOR_ACCESSIBILITY_NO }, LinearLayout.LayoutParams(dp(16), dp(16)))
            row.setOnClickListener { if (folder) navigateDirectory(path) else openFile(path) }
            return row
            }
        }
    }
    private fun chooseRelay() {
        AlertDialog.Builder(this).setTitle("选择电脑").setItems((0 until relays.length()).map { relays.getJSONObject(it).optString("label") }.toTypedArray()) { _, i ->
            relayId = relays.getJSONObject(i).getString("id"); selectedAgent = null; query = ""; render()
        }.show()
    }
    private fun command(type: String) = JSONObject().put("type", type).apply {
        FileReadScope.apply(this, relayId in unrestrictedFiles)
        selectedAgent?.let { a ->
            put("pane_id", a.optString("pane_id")); put("cwd", a.optString("cwd"))
            put("target", JSONObject().put("server_session_id", a.optString("server_session_id").ifBlank { "primary" })
                .put("pane_id", a.optString("pane_id")).put("terminal_id", a.optString("terminal_id"))
                .put("generation", a.optLong("generation")).put("agent_session_id", a.optString("agent_session_id").ifBlank { a.optString("session_id") }))
        }
    }
    private fun showHiddenFiles() = getSharedPreferences("file-browser", MODE_PRIVATE).getBoolean("show-hidden", false)
    private fun directoryPaging() = capabilities[relayId]?.contains("workspace_directory_v1") == true
    private fun navigateDirectory(path: String) {
        treeRequest++; fileGeneration++; treePending = false; busy = false
        directory = path; query = ""; treeError = ""
        if (directoryPaging()) { entries = JSONArray(); directoryPage = null; loadTree() } else render()
    }
    private fun loadTree(more: Boolean = false) {
        if (treePending) return
        if (!selectedAgentIsCurrent()) { treeError = "Agent 会话已变化，请重新选择它的工作区"; render(); return }
        val paged = directoryPaging(); val previous = directoryPage.takeIf { more }
        if (more && (!paged || previous?.cursor.isNullOrBlank())) return
        busy = true; treePending = true; treeError = ""
        val binding = selectedBinding(); val generation = fileGeneration; val path = directory; val owner = ++treeRequest
        render()
        val request = command(if (paged) "workspace_list" else "workspace_tree")
        if (paged) request.put("path", path).put("cursor", previous?.cursor.orEmpty()).put("limit", 128)
        engine.request(relayId, request, 30000) { data, error ->
            if (selectedBinding() != binding || fileGeneration != generation || treeRequest != owner || directory != path) return@request
            busy = false; treePending = false
            if (!selectedAgentIsCurrent()) treeError = "Agent 会话已变化，请重新打开工作区"
            else if (error != null || data == null) treeError = error ?: "目录响应为空"
            else if (paged) {
                try {
                    val page = WorkspaceDirectoryPage.accept(data, path, previous)
                    directoryPage = page; entries = page.entries; treeRoot = page.root; treeTruncated = false
                } catch (_: Exception) { treeError = "目录已变化或分页无效，请刷新当前目录" }
            } else {
                entries = data.optJSONArray("entries") ?: JSONArray(); treeRoot = data.optString("root"); treeTruncated = data.optBoolean("truncated")
            }
            if (tab == "files" && file == null) render()
        }
    }
    private fun openFile(path: String) {
        if (busy) return
        if (!selectedAgentIsCurrent()) { toast("Agent 会话已变化，请重新打开它的工作区"); return }
        val extended = capabilities[relayId]?.any { it in setOf("workspace_mobile_files_v1", "workspace_file_read_v1") } == true
        busy = true; val binding = selectedBinding(); val generation = fileGeneration; renderEntries()
        val receive: (JSONObject?, String?) -> Unit = receive@ { data, error ->
            if (selectedBinding() != binding || fileGeneration != generation) return@receive
            busy = false
            if (error != null || data == null) { toast(error ?: "文件内容为空"); renderEntries(); return@receive }
            if (!extended) {
                val raw = if (data.optString("kind") == "image") data.optString("data_url").substringAfter(',') else Base64.encodeToString(data.optString("text").toByteArray(), Base64.NO_WRAP)
                data.put("data", raw)
            }
            file = data; fileGeneration++; originalText = if (data.optString("kind") == "text" && data.optBoolean("preview_available", true)) bytes().toString(Charsets.UTF_8) else ""
            draft = originalText; editing = false; render()
        }
        if (capabilities[relayId]?.contains("workspace_file_download_v1") == true) {
            DocumentTransfer.preview(path, { selectedBinding() == binding && fileGeneration == generation && selectedAgentIsCurrent() },
                { type, payload, callback -> engine.request(relayId, command(type).apply { payload.keys().forEach { put(it, payload.get(it)) } }, 30000, callback) }, receive)
        } else if (capabilities[relayId]?.contains("workspace_file_read_v1") == true) {
            DocumentTransfer.read(path, { selectedBinding() == binding && fileGeneration == generation },
                { payload, callback -> engine.request(relayId, command("workspace_file_read").put("path", path).apply { payload.keys().forEach { put(it, payload.get(it)) } }, 30000, callback) }, receive)
        } else engine.request(relayId, command(if (extended) "workspace_file_read" else "workspace_file").put("path", path), callback = receive)
    }
    private fun openFileReviewer(path: String, reference: ReviewReference = ReviewReference(path)) {
        val binding = selectedBinding() ?: return
        if (!selectedAgentIsCurrent()) return
        fileReviewer?.close()
        val download: ((String, Boolean) -> Unit)? = if (capabilities[relayId]?.contains("workspace_file_download_v1") == true) { path, share -> downloadOriginal(path, share, binding, true) } else null
        fileReviewer = DocumentReviewer(this, binding.cwd, { selectedBinding() == binding && selectedAgentIsCurrent() }, download, relayId in unrestrictedFiles) { requested, done ->
            val extended = capabilities[relayId]?.any { it in setOf("workspace_mobile_files_v1", "workspace_file_read_v1") } == true
            if (capabilities[relayId]?.contains("workspace_file_download_v1") == true) {
                val view = fileReviewer; val version = view?.requestVersion
                DocumentTransfer.preview(requested, { fileReviewer === view && view?.isOpen == true && view.requestVersion == version && selectedBinding() == binding && selectedAgentIsCurrent() },
                    { type, payload, callback -> engine.request(relayId, command(type).apply { payload.keys().forEach { put(it, payload.get(it)) } }, 30000, callback) }, done)
            } else if (capabilities[relayId]?.contains("workspace_file_read_v1") == true) {
                val view = fileReviewer; val version = view?.requestVersion
                DocumentTransfer.read(requested, { fileReviewer === view && view?.isOpen == true && view.requestVersion == version && selectedBinding() == binding && selectedAgentIsCurrent() },
                    { payload, callback -> engine.request(relayId, command("workspace_file_read").apply { payload.keys().forEach { put(it, payload.get(it)) } }, 30000, callback) }, done)
            } else engine.request(relayId, command(if (extended) "workspace_file_read" else "workspace_file").put("path", requested), 30000) { data, error ->
                if (selectedBinding() == binding && selectedAgentIsCurrent()) done(data, error)
            }
        }
        fileReviewer?.open(reference)
    }
    private fun renderFile() {
        val doc = file ?: return
        val body = column().apply { setPadding(dp(18), 0, dp(18), dp(8)) }; content.addView(body, FrameLayout.LayoutParams(AgentStyle.contentWidth(this), -1, Gravity.CENTER_HORIZONTAL))
        body.addView(button("‹  返回文件列表") { leaveFile { render() } })
        selectedAgent?.let { body.addView(label("${agentName(it)} · ${it.optString("cwd")}", 12, accent).apply { maxLines = 2 }) }
        body.addView(label("${doc.optString("path")}  ·  ${FileRules.size(doc.optLong("size"))}", 12, muted).space(0, 5, 0, 12))
        val actions = LinearLayout(this)
        if (doc.optString("kind") == "text" && doc.optBoolean("preview_available", true)) {
            editToggle = button(if (editing) "预览" else "编辑", true) { editing = !editing; showFileBody(); updateEditorState() }
            editToggle!!.isEnabled = canEdit()
            actions.addView(editToggle, LinearLayout.LayoutParams(0, dp(48), 1f).apply { marginEnd = dp(4); marginStart = dp(4) })
            saveButton = button("保存") { saveFile() }
            actions.addView(saveButton, LinearLayout.LayoutParams(0, dp(48), 1f).apply { marginEnd = dp(4); marginStart = dp(4) })
        }
        if (doc.optBoolean("preview_available", true) && (doc.optString("kind") in listOf("pdf", "image") || FileRules.isMarkdown(doc.optString("path")))) {
            body.addView(button("全屏查看 · 缩放 / 阅读") { openFileReviewer(doc.optString("path")) })
        }
        actions.addView(button("下载") { val docNow = file ?: return@button; if (capabilities[relayId]?.contains("workspace_file_download_v1") == true && draft == originalText) { selectedBinding()?.let { downloadOriginal(docNow.optString("path"), false, it) }; return@button }; pendingExport = FileRules.safeName(docNow.optString("path")) to currentBytes(); picker.launch(pendingExport!!.first) }, LinearLayout.LayoutParams(0, dp(48), 1f).apply { marginEnd = dp(4); marginStart = dp(4) })
        actions.addView(button("分享") { shareFile() }, LinearLayout.LayoutParams(0, dp(48), 1f).apply { marginEnd = dp(4); marginStart = dp(4) })
        body.addView(actions)
        editStatus = label("", 12, muted); body.addView(editStatus!!.space(0, 8, 0, 8))
        fileBody = FrameLayout(this); body.addView(fileBody, LinearLayout.LayoutParams(-1, 0, 1f))
        showFileBody(); updateEditorState()
    }
    private fun showFileBody() {
        pdf?.close(); pdf = null; val holder = fileBody ?: return; holder.removeAllViews(); editor = null
        val doc = file ?: return
        if (!doc.optBoolean("preview_available", true)) {
            holder.addView(label(doc.optString("preview_reason").ifBlank { "此文件不支持预览；可以下载或分享原文件" }, 15, muted).space(10, 24, 10, 0)); return
        }
        when (doc.optString("kind")) {
            "text" -> if (editing) {
                editor = EditText(this).apply {
                    setTextColor(ink); setBackgroundColor(surface); textSize = 14f; typeface = Typeface.MONOSPACE
                    gravity = Gravity.TOP or Gravity.START; setPadding(dp(12), dp(12), dp(12), dp(12))
                    inputType = InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_FLAG_MULTI_LINE or InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS
                    setHorizontallyScrolling(false); setText(draft); isEnabled = !saving
                    addTextChangedListener(watcher { draft = it; updateEditorState(); main.removeCallbacks(persistDraft); main.postDelayed(persistDraft, 500) })
                }; holder.addView(editor, FrameLayout.LayoutParams(-1, -1))
            } else {
                val text = label(draft, 15, ink).apply { setLineSpacing(0f, 1f); setTextIsSelectable(true); setPadding(dp(12), dp(12), dp(12), dp(24)) }
                if (FileRules.isMarkdown(doc.optString("path"))) Markwon.builder(this).usePlugin(PocketMarkdown(this)).usePlugin(CodexFileCitations.plugin()).usePlugin(TablePlugin.create(this)).usePlugin(StrikethroughPlugin.create())
                    .usePlugin(object : io.noties.markwon.AbstractMarkwonPlugin() {
                        override fun configureConfiguration(builder: io.noties.markwon.MarkwonConfiguration.Builder) { builder.linkResolver { _, link ->
                            val ref = ReviewReferences.local(link, selectedBinding()?.cwd.orEmpty(), doc.optString("path"), unrestricted = relayId in unrestrictedFiles)
                            if (ref != null) openFileReviewer(ref.path, ref) else DocumentReviewer.confirmExternal(this@MainActivity, link)
                        } }
                    }).build().setMarkdown(text, draft)
                else text.typeface = Typeface.MONOSPACE
                holder.addView(ScrollView(this).apply { addView(text) })
            }
            "pdf" -> { pdf = PdfPreview(this, bytes()); holder.addView(pdf, FrameLayout.LayoutParams(-1, -1)) }
            "image" -> {
                val imageBytes = bytes(); val options = BitmapFactory.Options().apply { inJustDecodeBounds = true }
                BitmapFactory.decodeByteArray(imageBytes, 0, imageBytes.size, options)
                options.inSampleSize = (maxOf(options.outWidth, options.outHeight) / 2048).coerceAtLeast(1); options.inJustDecodeBounds = false
                val bitmap = BitmapFactory.decodeByteArray(imageBytes, 0, imageBytes.size, options)
                holder.addView(ImageView(this).apply { adjustViewBounds = true; setImageBitmap(bitmap); contentDescription = doc.optString("path") }, FrameLayout.LayoutParams(-1, -1))
            }
            else -> holder.addView(label("此文件暂不支持预览。\n可以下载到手机，或分享给其他应用打开。", 15, muted).space(10, 24, 10, 0))
        }
    }
    private fun canEdit(): Boolean {
        val state = runCatching { JSONObject(vault.read("state") ?: "{}") }.getOrNull()
        val role = state?.optJSONObject("auth")?.optJSONObject("relays")?.optJSONObject(relayId)?.optString("role")
        return role == "controller" && selectedAgentIsCurrent() && file?.optString("revision").orEmpty().isNotBlank() && file?.optString("kind") == "text"
    }
    private fun updateEditorState() {
        saveButton?.isEnabled = canEdit() && draft != originalText && !saving && draft.toByteArray().size <= 1024 * 1024
        saveButton?.text = if (saving) "保存中…" else "保存"
        editToggle?.text = if (editing) "预览" else "编辑"
        editStatus?.text = when {
            saving -> "正在保存到电脑…"
            !selectedAgentIsCurrent() -> "Agent 会话已变化或尚未同步 · 草稿保留，请重新打开工作区后核对"
            file?.optString("kind") != "text" -> "下载和分享通过安卓系统完成"
            !canEdit() -> "只读预览 · 编辑需要控制者权限和 relay 文件扩展"
            draft.toByteArray().size > 1024 * 1024 -> "文本超过 1 MiB，无法保存到 relay"
            draft != originalText -> "未保存到电脑 · 草稿自动保留在本机"
            else -> "与电脑文件一致"
        }
    }
    private fun saveFile() {
        if (!canEdit() || saving) return
        val doc = file ?: return; val generation = fileGeneration; val toSave = draft
        saving = true; editor?.isEnabled = false; updateEditorState(); keepDraft()
        engine.request(relayId, command("workspace_file_write").put("path", doc.optString("path")).put("text", toSave).put("expected_revision", doc.optString("revision"))) { data, error ->
            if (generation != fileGeneration) return@request
            saving = false; editor?.isEnabled = true
            if (error != null || data == null) {
                keepDraft(); AlertDialog.Builder(this).setTitle("尚未确认保存").setMessage(error ?: "没有收到保存结果。草稿仍保留在本机。")
                    .setPositiveButton("保留草稿", null).setNeutralButton("分享草稿") { _, _ -> shareFile() }.show()
            } else { file = data; originalText = toSave; draft = toSave; vault.remove(draftKey()); toast("已保存到电脑") }
            updateEditorState()
        }
    }
    private fun leaveFile(then: () -> Unit) {
        if (saving) { toast("正在保存，请稍候"); return }
        val leave = { fileReviewer?.close(); fileReviewer = null; pdf?.close(); pdf = null; editor = null; file = null; fileGeneration++; then() }
        if (draft != originalText && file?.optString("kind") == "text") AlertDialog.Builder(this).setTitle("保留未保存的修改？").setMessage("可保留本机草稿，稍后从工作目录页面恢复。")
            .setPositiveButton("保留并返回") { _, _ -> keepDraft(); leave() }.setNegativeButton("继续编辑", null)
            .setNeutralButton("放弃修改") { _, _ -> vault.remove(draftKey()); leave() }.show()
        else leave()
    }
    private fun keepDraft() {
        if (!::vault.isInitialized || file?.optString("kind") != "text") return
        runCatching {
            if (draft == originalText) vault.remove(draftKey())
            else vault.write(draftKey(), JSONObject().put("file", file).put("text", draft).put("relayId", relayId).put("agent", selectedAgent).toString())
        }
            .onFailure { toast("草稿保存失败，请先分享或复制内容") }
    }
    private fun draftKey(): String {
        val identity = "$relayId\u0000${selectedAgent?.optString("cwd")}\u0000${file?.optString("path")}"
        return "draft:" + java.security.MessageDigest.getInstance("SHA-256").digest(identity.toByteArray()).joinToString("") { "%02x".format(it) }
    }
    private fun chooseDraft() {
        val values = vault.names("draft:").mapNotNull { runCatching { vault.read(it) }.getOrNull() }
        val labels = values.map { runCatching { JSONObject(it).getJSONObject("file").getString("path") }.getOrDefault("草稿") }
        AlertDialog.Builder(this).setTitle("恢复未保存的修改").setItems(labels.toTypedArray()) { _, i -> restoreDraft(values[i]) }.setNegativeButton("取消", null).show()
    }
    private fun restoreDraft(value: String) {
        runCatching {
            val saved = JSONObject(value); relayId = saved.getString("relayId"); selectedAgent = saved.getJSONObject("agent"); file = saved.getJSONObject("file")
            originalText = bytes().toString(Charsets.UTF_8); draft = saved.getString("text"); editing = true; fileGeneration++; render()
        }.onFailure { toast("草稿无法恢复") }
    }
    private fun bytes() = Base64.decode(file?.optString("data").orEmpty(), Base64.DEFAULT)
    private fun currentBytes() = if (file?.optString("kind") == "text") draft.toByteArray(Charsets.UTF_8) else bytes()
    private fun downloadOriginal(path: String, share: Boolean, binding: WorkspaceBinding, fromReviewer: Boolean = false) {
        val captured = selectedAgent?.let { JSONObject(it.toString()) } ?: return
        val id = relayId; val unrestricted = id in unrestrictedFiles; val view = fileReviewer; val version = view?.requestVersion; val generation = fileGeneration
        documentDownloads.start(path, share, { (id in unrestrictedFiles) == unrestricted && selectedBinding() == binding && selectedAgentIsCurrent() &&
            if (fromReviewer) fileReviewer === view && view?.isOpen == true && view.requestVersion == version else fileGeneration == generation }) { payload, callback ->
            engine.request(id, FileReadScope.apply(AgentProtocol.command(id, captured, "workspace_file_download"), unrestricted).apply { payload.keys().forEach { put(it, payload.get(it)) } }, 60000, callback)
        }
    }
    private fun shareFile() {
        val doc = file ?: return
        if (capabilities[relayId]?.contains("workspace_file_download_v1") == true && draft == originalText) {
            selectedBinding()?.let { downloadOriginal(doc.optString("path"), true, it) }; return
        }
        runCatching {
            val folder = File(cacheDir, "shared/${UUID.randomUUID()}").apply { mkdirs() }
            val output = File(folder, FileRules.safeName(doc.optString("path"))).apply { writeBytes(currentBytes()) }
            val uri = FileProvider.getUriForFile(this, "$packageName.files", output)
            val send = Intent(Intent.ACTION_SEND).apply { type = DocumentMime.forName(output.name, doc.optString("media_type").ifBlank { "application/octet-stream" }); putExtra(Intent.EXTRA_STREAM, uri); clipData = ClipData.newRawUri(output.name, uri); addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION) }
            startActivity(Intent.createChooser(send, "分享 ${output.name}"))
        }.onFailure { toast("暂时无法分享，请检查存储空间或先下载文件") }
    }
    private fun emptyState(title: String, detail: String, action: String, run: () -> Unit) {
        val body = scrollColumn(); body.addView(ImageView(this).apply { setImageDrawable(AgentIcon("folder", muted)); contentDescription = null }, LinearLayout.LayoutParams(dp(48), dp(48)).apply { topMargin = dp(48); bottomMargin = dp(24) }); body.addView(label(title, 23, ink, true))
        body.addView(label(detail, 15, muted).space(0, 14, 0, 28)); body.addView(button(action, true, run))
    }
    private fun dp(value: Int) = (value * resources.displayMetrics.density).toInt()
    private fun label(value: String, size: Int, color: Int, bold: Boolean = false) = TextView(this).apply { text = value; textSize = size.toFloat(); setTextColor(color); if (bold) typeface = AgentStyle.medium; setLineSpacing(0f, 1.15f) }
    private fun column() = LinearLayout(this).apply { orientation = LinearLayout.VERTICAL }
    private fun scrollColumn(): LinearLayout {
        val body = column().apply { setPadding(dp(22), dp(10), dp(22), dp(28)) }
        content.addView(ScrollView(this).apply { isFillViewport = true; addView(body) }, FrameLayout.LayoutParams(AgentStyle.contentWidth(this, 680), -1, Gravity.CENTER_HORIZONTAL))
        return body
    }
    private fun card() = column().apply { setPadding(dp(17), dp(17), dp(17), dp(17)); background = rounded(surface) }
    private fun rounded(color: Int) = GradientDrawable().apply { setColor(color); cornerRadius = dp(12).toFloat() }
    private fun button(title: String, primary: Boolean = false, action: () -> Unit) = AgentStyle.button(this, title, primary, action = action)
    private fun <T : View> T.space(left: Int, top: Int, right: Int, bottom: Int): T = apply { layoutParams = LinearLayout.LayoutParams(-1, -2).apply { setMargins(dp(left), dp(top), dp(right), dp(bottom)) } }
    private fun watcher(action: (String) -> Unit) = object : TextWatcher { override fun beforeTextChanged(s: CharSequence?, start: Int, count: Int, after: Int) {}; override fun onTextChanged(s: CharSequence?, start: Int, before: Int, count: Int) {}; override fun afterTextChanged(s: Editable?) { action(s.toString()) } }
    private fun toast(value: String) { transientToast?.cancel(); transientToast = Toast.makeText(this, value, Toast.LENGTH_LONG).also { it.show() } }
}

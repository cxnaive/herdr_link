package dev.herdr.companion

import android.app.AlertDialog
import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.graphics.Color
import android.graphics.Typeface
import android.net.Uri
import android.os.Handler
import android.os.Looper
import android.os.SystemClock
import android.provider.OpenableColumns
import android.view.Gravity
import android.view.View
import android.view.ViewGroup
import android.widget.*
import androidx.core.view.doOnLayout
import io.noties.markwon.Markwon
import io.noties.markwon.ext.tables.TableAwareMovementMethod
import io.noties.markwon.ext.tables.TablePlugin
import io.noties.markwon.ext.strikethrough.StrikethroughPlugin
import org.json.JSONArray
import org.json.JSONObject
import java.security.MessageDigest
import java.util.Base64
import java.util.UUID
import java.util.concurrent.Executors

/** Native Agent screens share the file browser's controls, typography and Android input methods. */
class NativeAgents(private val context: Context, private val engine: RelayEngine, private val vault: Vault,
    private val inventory: () -> Map<String, JSONArray>, private val workspace: (String, JSONObject) -> Unit,
    private val pickAttachment: () -> Unit, private val detailVisibility: (Boolean) -> Unit, private val monitoring: () -> Unit = {}, private val downloads: DocumentDownloads? = null, private val unrestrictedFiles: (String) -> Boolean = { false }) {
    private val main = Handler(Looper.getMainLooper())
    private val io = Executors.newSingleThreadExecutor()
    private val configs = mutableMapOf<String, JSONObject>()
    private val online = mutableSetOf<String>()
    private val workspaces = mutableMapOf<String, JSONArray>()
    private var host: FrameLayout? = null
    private var agentListRows: LinearLayout? = null
    private val groupExpansion = AgentGroupExpansion(context)
    private var monitorButton: Button? = null
    fun updateMonitorControls() { monitorButton?.text = when { AgentMonitorService.active -> "常驻监控运行中 · 通知设置"; MonitorPreferences(context).enabled -> "监控待恢复 · 检查通知权限"; else -> "开启常驻监控 / 完成提醒" } }
    private var active = false
    private var epoch = 0
    private var selected: JSONObject? = null
    private var relay = ""
    private var mode = "conversation"
    private var composer: EditText? = null
    private var composerPanel: NativeComposerView? = null
    private var body: LinearLayout? = null
    private var conversationBody: LinearLayout? = null
    private var conversationScroll: ScrollView? = null
    private var interactionBody: LinearLayout? = null
    private var stateLabel: TextView? = null
    private var bindingLabel: TextView? = null
    private var runStatusView: AgentRunStatusView? = null
    private val runPresentation = AgentRunPresentation()
    private var runPending = false
    private var runRequest = 0
    private var runSignal = 0
    private var runRefreshAt = 0L
    private val runTicker = object : Runnable {
        override fun run() {
            if (!active || selected == null || runStatusView == null) return
            updateRunStatus()
            if (relay in online && inventoryReady() && capability("agent_run_state_v1") && !runPending && SystemClock.elapsedRealtime() >= runRefreshAt) readRunStatus()
            main.postDelayed(this, 1000)
        }
    }
    private var identityTitle: TextView? = null
    private var identitySubtitle: TextView? = null
    private var sendButton: View? = null
    private var terminal: NativeTerminalView? = null
    private var terminalScroll: TerminalViewport? = null
    private var terminalKeys: TerminalExtraKeys? = null
    private var terminalOlderButton: Button? = null
    private var terminalHistorySpinner: ProgressBar? = null
    private var terminalHistoryMessage = ""
    private var terminalHasMore = true
    private var terminalRequest = 0
    private var terminalSavedY = 0
    private var terminalFollowing = true
    private var terminalHistoryLines = 500
    private var loadingOlderTerminal = false
    private var terminalReadPending = false
    private val expandedToolGroups = mutableSetOf<String>()
    private val expandedToolItems = mutableSetOf<String>()
    private val conversationImages = ConversationImages(context)
    private var reviewer: DocumentReviewer? = null
    private var search = ""
    private var pendingActivityRelay: String? = null
    private var speech: android.speech.tts.TextToSpeech? = null
    private var transientToast: Toast? = null
    private var conversation = ConversationState()
    private var conversationPoll = 0
    private var historyPending = false
    private var historyOlder = false
    private var historyRequest = 0
    private var historyMessage = ""
    private var historyProgress: JSONObject? = null
    private var historyControl: HistoryLoadView? = null
    private var resumeOlderAfterReconnect = false
    private val conversationCache = ConversationMemoryCache()
    private var content = ""
    private var fingerprint = ""
    private var frame = JSONObject()
    private var frameObservedAt = 0L
    private val pendingMutations = mutableSetOf<String>()
    private val inputPending get() = deliveryKey() in pendingMutations
    private val uncertain get() = vault.read(deliveryKey()) != null
    private var attachments = JSONArray()
    private var uploading = false
    private var uploadId = ""
    private var uiDraft = ""
    private val textInk get() = AgentStyle.ink
    private val muted get() = AgentStyle.muted
    private val surface get() = AgentStyle.panel
    private val accent get() = AgentStyle.accent
    private var markdownTheme: PocketTheme? = null
    private var markdownRenderer: Markwon? = null
    private val renderMarkdown: Markwon
        get() {
            val existing = markdownRenderer
            if (existing != null && markdownTheme == PocketAppearance.current) return existing
            val built = Markwon.builder(context).usePlugin(PocketMarkdown(context))
                .usePlugin(CodexFileCitations.plugin())
                .usePlugin(TablePlugin.create(context))
                .usePlugin(StrikethroughPlugin.create())
                .usePlugin(object : io.noties.markwon.AbstractMarkwonPlugin() {
                    override fun configureConfiguration(builder: io.noties.markwon.MarkwonConfiguration.Builder) { builder.linkResolver { _, link -> openReviewLink(link) } }
                }).build()
            markdownTheme = PocketAppearance.current
            markdownRenderer = built
            return built
        }

    fun attach(container: FrameLayout) { host = container; active = true; render() }
    fun pause() { rememberConversation(); saveDraft(); active = false; updateImageAvailability(); stopStreams(); detailVisibility(false) }
    fun fileAccessChanged(id: String) { if (relay == id) { reviewer?.close(); reviewer = null; conversationImages.clear(); if (active) render() } }
    fun destroy() { pause(); transientToast?.cancel(); speech?.stop(); speech?.shutdown(); conversationImages.close(); conversationCache.clear(); io.shutdown() }
    fun back(): Boolean { if (selected == null) return false; rememberConversation(); saveDraft(); stopStreams(); selected = null; render(); return true }
    fun open(relayId: String, agent: JSONObject) { rememberConversation(); saveDraft(); stopStreams(); composer = null; relay = relayId; selected = JSONObject(agent.toString()); runPresentation.clear(); terminalHistoryLines = 500; terminalHasMore = true; terminalHistoryMessage = ""; terminalSavedY = 0; terminalFollowing = true; terminalScroll = null; loadingOlderTerminal = false; terminalReadPending = false; expandedToolGroups.clear(); expandedToolItems.clear(); conversationImages.clear(); mode = if (AgentPresentation.terminalOnly(agent)) "terminal" else "conversation"; conversation = if (agent.optString("binding_state") == "conflict") ConversationState() else conversationCache.get(WorkspaceBinding.fromAgent(relayId, agent)) ?: ConversationState(); resumeOlderAfterReconnect = false; historyMessage = ""; attachments = JSONArray(); content = ""; fingerprint = ""; frame = JSONObject(); uploading = false; uiDraft = vault.read(draftKey()).orEmpty(); if (active) render() }
    private fun name(agent: JSONObject) = AgentPresentation.title(agent)
    private fun current(): JSONObject? { val captured = selected ?: return null; val rows = inventory()[relay] ?: return null; return (0 until rows.length()).map { rows.getJSONObject(it) }.find { WorkspaceBinding.fromAgent(relay, it) == WorkspaceBinding.fromAgent(relay, captured) } }
    private fun role(id: String = relay) = engine.snapshot().optJSONObject("auth")?.optJSONObject("relays")?.optJSONObject(id)?.optString("role") ?: "reader"
    private fun writable() = PrimarySessionBinding.writable(current()) && managementReady(relay)
    private fun terminalWritable() = PrimarySessionBinding.terminalWritable(current()) && managementReady(relay)
    private fun operationWritable(type: String) = PrimarySessionBinding.operationWritable(current(), type, mode == "terminal") && managementReady(relay)
    private fun draftKey() = "prompt:" + selected?.let { WorkspaceBinding.fromAgent(relay, it).storageKey() }.orEmpty()
    private fun deliveryKey() = "delivery:" + selected?.let { WorkspaceBinding.fromAgent(relay, it).storageKey() }.orEmpty()
    private fun saveDraft() { if (selected != null) { uiDraft = composer?.text?.toString() ?: uiDraft; if (!frame.optBoolean("no_echo")) vault.write(draftKey(), uiDraft) } }
    private fun stopStreams() {
        terminalScroll?.let { terminalSavedY = it.scrollY; terminalFollowing = it.followsOutput }
        terminalRequest++; terminalKeys?.setAvailable(false)
        reviewer?.close(); reviewer = null
        main.removeCallbacks(runTicker); runRequest++; runPending = false; runPresentation.invalidate(); updateRunStatus()
        epoch++; historyRequest++; historyPending = false; historyProgress = null; updateHistoryControl(); terminalReadPending = false; loadingOlderTerminal = false; updateTerminalControls()
        selected?.let { engine.send(relay, AgentProtocol.command(relay, it, "unwatch_pane")); if (conversation.subscription.isNotBlank()) engine.send(relay, AgentProtocol.command(relay, it, "unwatch_conversation").put("subscription_id", conversation.subscription)); engine.send(relay, AgentProtocol.command(relay, it, "release_pane_size").put("immediate", true)) }
        conversation.prepareRefresh()
    }
    fun onEvent(event: JSONObject) {
        val id = event.optString("relayId")
        if (event.optString("type") == "monitor_state") { updateMonitorControls(); return }
        if (event.optString("type") == "renamed") {
            if (active && selected == null) renderList() else if (id == relay) updateIdentity()
            return
        }
        if (event.optString("type") == "status") {
            if (event.optString("status") == "connected") online += id else online -= id
            if (id == relay && selected != null) {
                updateImageAvailability()
                if (id !in online) {
                    resumeOlderAfterReconnect = resumeOlderAfterReconnect || (historyOlder && historyPending)
                    rememberConversation(); stopStreams(); historyMessage = "连接中断，已读历史保留；重连后继续"; updateHistoryControl(); stateLabel?.text = "连接中断 · 草稿和已读历史保留"
                }
                else if (active) startStreams()
                updateRunStatus()
            }
            if (active && selected == null) renderList()
            return
        }
        if (event.optString("type") != "message") return
        val value = event.getJSONObject("message")
        when (value.optString("type")) {
            "push_config" -> { configs[id] = value; if (active && selected == null) renderList() else if (id == relay && active) startStreams() }
            "workspaces" -> { workspaces[id] = value.optJSONArray("workspaces") ?: JSONArray(); if (active && selected == null) renderList() else if (id == relay) updateIdentity() }
            "inventory_status" -> { configs[id]?.put("inventory", value); if (active && selected == null) renderList() else if (id == relay) { updateImageAvailability(); updateSendState(); updateRunStatus() } }
            "activity_history" -> if (pendingActivityRelay == id) { pendingActivityRelay = null; showText("活动记录", value.optJSONArray("activities")?.toString(2).orEmpty()) }
            "agents", "blocked", "agent_update", "agent_removed" -> {
                if (!active) return
                if (selected == null) renderList() else if (id == relay) {
                    val captured = selected!!
                    val rows = inventory()[relay] ?: JSONArray()
                    val updated = (0 until rows.length()).map { rows.getJSONObject(it) }.singleOrNull { PrimarySessionBinding.sameIdentity(relay, captured, it) }
                    if (updated == null) {
                        val replacement = (0 until rows.length()).map { rows.getJSONObject(it) }.singleOrNull { PrimarySessionBinding.relayReplacement(relay, captured, it) || PrimarySessionBinding.terminalTransition(relay, captured, it) }
                        if (replacement != null) { open(relay, replacement); return }
                        rememberConversation(); saveDraft(); stopStreams(); conversation = ConversationState(); renderConversation()
                        stateLabel?.text = "此 Agent 会话已被替换。请返回列表重新打开。"; sendButton?.isEnabled = false
                    }
                    else {
                        val leaseChanged = captured.optLong("binding_epoch") != updated.optLong("binding_epoch")
                        if (leaseChanged) { rememberConversation(); saveDraft(); stopStreams() }
                        if (selected?.optString("status") != updated.optString("status") || selected?.optString("attention_kind") != updated.optString("attention_kind")) {
                            runPresentation.invalidate(); runSignal++; runRefreshAt = 0
                        }
                        selected = JSONObject(updated.toString())
                        if (updated.optString("binding_state") == "conflict") { conversation = ConversationState(); renderConversation() }
                        updateIdentity(); updateInteraction(); updateSendState()
                        if (leaseChanged) startStreams()
                    }
                    updateImageAvailability(); updateRunStatus()
                }
            }
            "pane_content", "pane_delta", "pane_resync", "pane_unchanged" -> {
                val agent = selected ?: return
                if (!active || mode != "terminal" || id != relay || !AgentProtocol.targetMatches(id, agent, value)) return
                // Requested snapshots are applied by their generation-checked callback.
                if (value.optString("request_id").isNotBlank() || terminalReadPending) return
                acceptFrame(value)
            }
        }
    }
    private fun render() {
        if (!active) return
        saveDraft(); terminalKeys?.setAvailable(false); terminalScroll?.let { terminalSavedY = it.scrollY; terminalFollowing = it.followsOutput }; main.removeCallbacks(runTicker); runStatusView = null; host?.removeAllViews(); agentListRows = null; composer = null; composerPanel = null; identityTitle = null; identitySubtitle = null; bindingLabel = null; terminal = null; terminalScroll = null; terminalKeys = null; terminalOlderButton = null; terminalHistorySpinner = null; conversationScroll = null; conversationBody = null; historyControl = null
        updateImageAvailability(); detailVisibility(selected != null)
        if (selected == null) renderList() else renderAgent()
    }
    private fun renderList() {
        if (!active || selected != null) return
        val target = host ?: return
        agentListRows?.takeIf { it.isAttachedToWindow }?.let { fillAgentRows(it); return }
        target.removeAllViews()
        val layout = column(16); target.addView(layout, FrameLayout.LayoutParams(AgentStyle.contentWidth(context, 720), -1, Gravity.CENTER_HORIZONTAL))
        val controls = row()
        val expanded = context.resources.configuration.screenWidthDp >= 600
        controls.addView(AgentStyle.button(context, "新建 Agent", primary = true) { launchAgent() }, LinearLayout.LayoutParams(if (expanded) dp(160) else 0, dp(48), if (expanded) 0f else 1f).apply { marginEnd = dp(8) })
        if (expanded) controls.addView(View(context), LinearLayout.LayoutParams(0, 1, 1f))
        controls.addView(AgentStyle.button(context, "电脑管理", subtle = true) { chooseRelay(::manageRelay) }, LinearLayout.LayoutParams(-2, dp(48)))
        controls.addView(AgentStyle.iconButton(context, "refresh", "刷新") { inventory().keys.forEach { engine.send(it, JSONObject().put("type", "refresh_agents")) } }, LinearLayout.LayoutParams(dp(48), dp(48)))
        layout.addView(controls)
        monitorButton = AgentStyle.button(context, "常驻监控 / 完成提醒", subtle = true, action = monitoring).also { layout.addView(it, LinearLayout.LayoutParams(-1, dp(48))) }; updateMonitorControls()
        val filter = EditText(context).apply { hint = "搜索电脑、工作空间或 Agent"; contentDescription = "搜索工作空间、Tab、Agent 或电脑"; setTextColor(textInk); setHintTextColor(muted); setSingleLine(); setText(search) }
        val rows = column(0).also { agentListRows = it }
        filter.addTextChangedListener(simpleWatcher { search = it; fillAgentRows(rows) }); layout.addView(AgentStyle.search(context, filter), LinearLayout.LayoutParams(-1, -2).apply { topMargin = dp(4); bottomMargin = dp(20) })
        layout.addView(ScrollView(context).apply { isVerticalScrollBarEnabled = false; addView(rows) }, LinearLayout.LayoutParams(-1, 0, 1f)); fillAgentRows(rows)
    }
    private fun workspaceTitle(id: String, agent: JSONObject): String =
        AgentPresentation.workspaceTitle(id, agent, workspaces)
    private fun fillAgentRows(rows: LinearLayout) {
        groupExpansion.updateSearch(search)
        rows.removeAllViews()
        val computers = AgentListPresentation.computers(engine.snapshot().optJSONArray("relays") ?: JSONArray(), inventory(), workspaces, search)
        val searching = search.isNotBlank()
        for (computer in computers) {
            val key = AgentGroupExpansion.computerKey(computer.id)
            val expanded = groupExpansion.expanded(key, search)
            val status = if (computer.id in online) "已连接" else "未连接"
            val counts = (if (searching) "匹配 ${computer.workspaces.size}/${computer.totalWorkspaces} 个工作空间 · ${computer.matchingAgents}/${computer.totalAgents} 个 Agent"
                else "${computer.totalWorkspaces} 个工作空间 · ${computer.totalAgents} 个 Agent") +
                if (computer.activity.terminals > 0) " · " + (if (searching) "${computer.matchingTerminals}/" else "") + "${computer.activity.terminals} 个终端" else ""
            val inventoryStatus = configs[computer.id]?.optJSONObject("inventory")
            val activity = computer.activity.summary(computer.id in online, inventoryStatus?.optString("state") == "ready" && !inventoryStatus.optBoolean("stale"))
            val section = column(0)
            section.addView(AgentGroupHeader(context, key, computer.label, "$status · $activity\n$counts", expanded, true) { toggleListGroup(rows, it) })
            if (expanded) {
                val children = column(0).apply { setPadding(dp(12), dp(6), 0, 0) }
                for (workspace in computer.workspaces) children.addView(workspaceSection(rows, workspace), LinearLayout.LayoutParams(-1, -2).apply { bottomMargin = dp(12) })
                if (computer.workspaces.isEmpty()) children.addView(label(if (computer.id in online) "这台电脑尚未提供工作空间。" else "连接后显示这台电脑的工作空间与 Agent。", 13, muted).apply { setPadding(dp(4), dp(12), dp(4), dp(12)) })
                section.addView(children)
            }
            rows.addView(section, LinearLayout.LayoutParams(-1, -2).apply { bottomMargin = dp(16) })
        }
        if (computers.isEmpty()) rows.addView(label(if (searching) "没有匹配的电脑、工作空间或 Agent" else "连接电脑后，Agent 会按电脑、工作空间和 Tab 显示。", 15, muted))
    }
    private fun toggleListGroup(rows: LinearLayout, header: AgentGroupHeader) {
        if (!active || selected != null || agentListRows !== rows || rows.findViewWithTag<View>(header.groupKey) !== header) return
        val scroll = rows.parent as? ScrollView
        fun top(view: View): Int {
            val bounds = android.graphics.Rect(0, 0, view.width, view.height)
            rows.offsetDescendantRectToMyCoords(view, bounds)
            return bounds.top
        }
        val offset = top(header) - (scroll?.scrollY ?: 0)
        val focused = header.hasFocus()
        groupExpansion.toggle(header.groupKey, search)
        fillAgentRows(rows)
        rows.doOnLayout {
            if (!active || selected != null || agentListRows !== rows) return@doOnLayout
            val replacement = rows.findViewWithTag<AgentGroupHeader>(header.groupKey) ?: return@doOnLayout
            if (focused) replacement.requestFocus()
            scroll?.scrollTo(0, (top(replacement) - offset).coerceAtLeast(0))
        }
    }
    private fun workspaceSection(rows: LinearLayout, workspace: AgentListPresentation.Workspace): LinearLayout {
        val group = workspace.group
        val members = workspace.agents
        val key = AgentGroupExpansion.workspaceKey(group)
        val expanded = groupExpansion.expanded(key, search)
        val section = column(0)
        val workspaceHeader = row()
        val count = if (search.isBlank()) "${members.size} 个 Agent" else "匹配 ${members.size}/${group.agents.size} 个 Agent"
        val server = if (group.server == "primary") "" else " · ${group.server}"
        workspaceHeader.addView(AgentGroupHeader(context, key, group.title, count + server, expanded, false, group.path) { toggleListGroup(rows, it) }, weight())
        workspaceHeader.addView(AgentStyle.button(context, "＋ Agent", subtle = true) { launchInWorkspace(group.relay, group.id, group.server) }.apply {
            contentDescription = "在 ${group.title} 新建 Agent"
            isEnabled = group.server == "primary" && !group.id.startsWith("cwd:") && managementReady(group.relay)
        })
        section.addView(workspaceHeader)
        if (!expanded) return section
        val agentRows = column(0).apply { setPadding(dp(28), 0, 0, 0) }
        section.addView(agentRows)
        if (members.isEmpty()) agentRows.addView(label("这个工作空间还没有 Agent，可点击上方 ＋ Agent 新建。", 13, muted))
        for ((_, agents) in members.groupBy { it.optString("tab_id").ifBlank { it.optString("pane_id") } }) {
            val first = agents.first()
            if (agents.size > 1) agentRows.addView(label("${first.optInt("tab_number").takeIf { it > 0 }?.let { "$it  " }.orEmpty()}${name(first)}", 14, muted)
                .apply { setPadding(dp(4), dp(14), 0, dp(8)); contentDescription = "Tab：${name(first)}" })
            for (agent in agents) {
                val card = column(0).apply { setPadding(dp(2), dp(12), dp(2), dp(4)) }
                val cardHeader = row()
                cardHeader.addView(label(name(agent), 17, textInk).apply { typeface = AgentStyle.medium; maxLines = 2; ellipsize = android.text.TextUtils.TruncateAt.END; contentDescription = "Tab：${name(agent)}"; setOnClickListener { open(group.relay, agent) }; isFocusable = true; minimumHeight = dp(48); gravity = Gravity.CENTER_VERTICAL }, weight())
                cardHeader.addView(AgentStyle.iconButton(context, "more", "管理 ${name(agent)}") {
                    val captured = JSONObject(agent.toString())
                    AlertDialog.Builder(context).setTitle(name(captured)).setItems(arrayOf("重命名 Agent", "模型 / 思考强度", "打开管理详情", MonitorSettingsUi.muteLabel(context, group.relay, captured))) { _, choice -> when (choice) {
                        0 -> renameAgent(group.relay, captured)
                        1 -> modelSettings(group.relay, captured)
                        2 -> { open(group.relay, captured); manageAgent() }
                        3 -> { MonitorSettingsUi.toggle(context, group.relay, captured); if (active && selected == null) renderList() }
                    } }.show()
                }, LinearLayout.LayoutParams(dp(48), dp(48)))
                card.addView(cardHeader)
                val status = when (agent.optString("status")) { "blocked" -> "需要处理"; "working" -> "运行中"; "done" -> "已完成"; else -> "空闲" }
                val statusColor = when (agent.optString("status")) { "working", "done" -> accent; "blocked" -> AgentStyle.warning; else -> muted }
                card.addView(label("${AgentPresentation.provider(agent)} · $status" + if (agents.size > 1) " · ${agent.optString("pane_id")}" else "", 12, statusColor).apply { setPadding(0, 0, 0, dp(6)) })
                if (runCatching { MonitorPreferences(context).muted(MonitorIdentity.key(group.relay, agent)) }.getOrDefault(false)) card.addView(label("完成提醒已关闭", 11, muted))
                if (agent.optString("cwd") != group.path) card.addView(label(agent.optString("cwd"), 11, muted))
                if (agent.optString("status") == "blocked") card.addView(label(agent.optString("prompt").take(220), 13, textInk))
                val actions = row()
                if (!AgentPresentation.terminalOnly(agent)) actions.addView(AgentStyle.button(context, "打开对话", subtle = true) { open(group.relay, agent) }, weight())
                actions.addView(AgentStyle.button(context, "终端", subtle = true) { open(group.relay, agent); mode = "terminal"; render() }, weight())
                actions.addView(AgentStyle.button(context, "工作区", subtle = true) { workspace(group.relay, agent) }, weight())
                card.addView(actions)
                agentRows.addView(card, LinearLayout.LayoutParams(-1, -2))
                agentRows.addView(View(context).apply { setBackgroundColor(AgentStyle.border); importantForAccessibility = View.IMPORTANT_FOR_ACCESSIBILITY_NO }, LinearLayout.LayoutParams(-1, dp(1)))
            }
        }
        return section
    }
    private fun updateIdentity() {
        val agent = selected ?: return
        identityTitle?.text = name(agent)
        bindingLabel?.text = if (mode == "terminal") PrimarySessionBinding.terminalLabel(agent) else PrimarySessionBinding.label(agent)
        bindingLabel?.visibility = if (agent.optString("binding_state").isBlank() && !AgentPresentation.terminalOnly(agent)) View.GONE else View.VISIBLE
        identitySubtitle?.text = "${relayName(relay)} · ${workspaceTitle(relay, agent)} · ${agent.optString("cwd")}"
    }
    private fun renderAgent() {
        val agent = selected ?: return
        val layout = object : LinearLayout(context) {
            override fun onMeasure(widthMeasureSpec: Int, heightMeasureSpec: Int) {
                terminalKeys?.setCompact(View.MeasureSpec.getSize(heightMeasureSpec)<dp(580))
                super.onMeasure(widthMeasureSpec,heightMeasureSpec)
            }
        }.apply { orientation=LinearLayout.VERTICAL; setPadding(dp(16), 0, dp(16), dp(8)) }; body = layout; host?.addView(layout, FrameLayout.LayoutParams(AgentStyle.contentWidth(context), -1, Gravity.CENTER_HORIZONTAL))
        val header = row()
        header.addView(AgentStyle.iconButton(context, "back", "返回 Agents") { back() }, LinearLayout.LayoutParams(dp(48), dp(56)))
        val identity = column(0)
        identity.addView(label(name(agent), 18, textInk).apply { identityTitle = this; typeface = AgentStyle.medium; maxLines = 1; ellipsize = android.text.TextUtils.TruncateAt.END; setPadding(0, 0, 0, dp(2)) })
        identity.addView(label("${relayName(relay)} · ${workspaceTitle(relay, agent)} · ${agent.optString("cwd")}", 11, muted).apply { identitySubtitle = this; maxLines = 1; ellipsize = android.text.TextUtils.TruncateAt.MIDDLE; setPadding(0, 0, 0, 0) })
        header.addView(identity, weight())
        header.addView(AgentStyle.iconButton(context, "more", "对话选项") { conversationOptions() }, LinearLayout.LayoutParams(dp(48), dp(56)))
        layout.addView(header, LinearLayout.LayoutParams(-1, dp(64)))
        val tabs = row().apply { setPadding(0, dp(2), 0, dp(2)) }
        for ((id, title) in listOf("conversation" to "对话", "terminal" to "终端", "workspace" to "工作区")) {
            if (id == "conversation" && AgentPresentation.terminalOnly(agent)) continue
            tabs.addView(TextView(context).apply {
                text = title; textSize = 13f; gravity = Gravity.CENTER; isFocusable = true
                setTextColor(if (id == mode) accent else muted); setTypeface(typeface, if (id == mode) Typeface.BOLD else Typeface.NORMAL)
                background = AgentStyle.ripple(context, if (id == mode) AgentStyle.selected else Color.TRANSPARENT, 8)
                contentDescription = title; isSelected = id == mode; AgentStyle.actionSemantics(this)
                setOnClickListener { if (id == "workspace") current()?.let { workspace(relay, it) } else if (mode != id) { saveDraft(); stopStreams(); mode = id; render() } }
            }, LinearLayout.LayoutParams(0, dp(48), 1f).apply { marginStart = dp(3); marginEnd = dp(3) })
        }; layout.addView(tabs)
        bindingLabel = label(if (mode == "terminal") PrimarySessionBinding.terminalLabel(agent) else PrimarySessionBinding.label(agent), 12, muted).apply {
            setPadding(dp(2), dp(6), dp(2), dp(6))
            visibility = if (agent.optString("binding_state").isBlank() && !AgentPresentation.terminalOnly(agent)) View.GONE else View.VISIBLE
            accessibilityLiveRegion = View.ACCESSIBILITY_LIVE_REGION_POLITE
        }.also { layout.addView(it) }
        runStatusView = AgentRunStatusView(context) {
            if (SystemClock.elapsedRealtime() >= runRefreshAt && !runPending) { readRunStatus(); updateRunStatus() }
        }.also { layout.addView(it, LinearLayout.LayoutParams(-1, -2).apply { topMargin = dp(4) }) }
        updateRunStatus()
        stateLabel = label(if (mode == "conversation" && conversation.entries.isNotEmpty()) "已恢复 ${conversation.entries.size} 条内存缓存，正在同步…" else "正在同步…", 11, muted).apply { setPadding(dp(2), dp(4), dp(2), dp(4)) }; layout.addView(stateLabel)
        if (mode == "conversation") {
            conversationBody = column(0).apply { setPadding(0, dp(8), 0, dp(20)) }
            conversationScroll = ScrollView(context).apply { addView(conversationBody); isFillViewport = true; isVerticalScrollBarEnabled = false }
            layout.addView(conversationScroll, LinearLayout.LayoutParams(-1, 0, 1f)); renderConversation()
        } else {
            val output = NativeTerminalView(context); terminal = output
            val controls = row()
            terminalHistorySpinner = ProgressBar(context, null, android.R.attr.progressBarStyleSmall).apply { contentDescription="正在加载终端历史"; visibility=View.GONE; indeterminateTintList=android.content.res.ColorStateList.valueOf(accent) }
            controls.addView(terminalHistorySpinner,LinearLayout.LayoutParams(dp(18),dp(18)))
            terminalOlderButton = button("更早输出") { if(terminalHasMore) readPane(true) else noMoreTerminalHistory() }.also { controls.addView(it,weight()) }
            controls.addView(button(if (output.wrapToPhone) "手机换行" else "原始列宽") { output.toggleWrap(); renderTerminalWrapLabel(controls, output) }, weight())
            controls.addView(button("回到底部") { terminalScroll?.jumpToBottom() }, weight())
            layout.addView(controls)
            terminalScroll = TerminalViewport(context,output).apply {
                onReadingHistory = { terminalFollowing = !it; updateTerminalStatus() }
                onReachHistoryTop = { if (terminalHasMore && !terminalReadPending && active && mode=="terminal") readPane(true, true) }
            }
            layout.addView(terminalScroll, LinearLayout.LayoutParams(-1,0,1f))
            output.show(content,frame.optInt("columns",0)); terminalScroll?.restorePosition(terminalSavedY,terminalFollowing)
            terminalKeys = TerminalExtraKeys(context,::sendKeys).also { keys ->
                layout.addView(keys,LinearLayout.LayoutParams(-1,-2))
            }
        }
        interactionBody = column(0); layout.addView(interactionBody); updateInteraction()
        val panel = NativeComposerView(context, mode == "terminal", uiDraft,
            { uiDraft = it; updateSendState() }, { if (writable()) pickAttachment() else toast("当前设备只读或未连接") },
            ::slashCommands, ::inputMenu, ::sendText)
        panel.setSecretInput(mode == "terminal" && frame.optBoolean("no_echo"))
        composerPanel = panel; composer = panel.editor; sendButton = panel.sendButton
        layout.addView(panel, LinearLayout.LayoutParams(-1, -2).apply { topMargin = dp(8) })
        updateAttachments()
        updateSendState(); startStreams()
    }
    private fun renderTerminalWrapLabel(controls: LinearLayout, output: NativeTerminalView) {
        for(i in 0 until controls.childCount) (controls.getChildAt(i) as? Button)?.takeIf { it.text in listOf("手机换行","原始列宽") }?.text = if(output.wrapToPhone) "手机换行" else "原始列宽"
    }

    private fun conversationOptions() {
        val items = if (mode == "conversation") arrayOf("重命名 Agent", "模型 / 思考强度", "在此工作空间新建 Agent", "更早消息", "搜索 / 复制", "刷新", "管理 Agent") else arrayOf("重命名 Agent", "模型 / 思考强度", "在此工作空间新建 Agent", "字号 ＋", "字号 −", "搜索 / 复制", "刷新", "管理 Agent")
        val choices = items + arrayOf(selected?.let { MonitorSettingsUi.muteLabel(context, relay, it) }.orEmpty(), "常驻监控设置") +
            if (selected?.optString("agent") == "codex") arrayOf("会话绑定诊断") else emptyArray()
        AlertDialog.Builder(context).setItems(choices) { _, index -> when (choices[index]) {
            "关闭此 Agent 完成提醒", "开启此 Agent 完成提醒" -> selected?.let { MonitorSettingsUi.toggle(context, relay, it) }
            "常驻监控设置" -> monitoring()
            "会话绑定诊断" -> showBindingDiagnostics()
            "重命名 Agent" -> selected?.let { renameAgent(relay, JSONObject(it.toString())) }
            "模型 / 思考强度" -> selected?.let { modelSettings(relay, JSONObject(it.toString())) }
            "在此工作空间新建 Agent" -> selected?.let { launchInWorkspace(relay, it.optString("workspace_id"), AgentPresentation.server(it)) }
            "更早消息" -> loadHistory(true); "搜索 / 复制" -> showSearch(); "刷新" -> if (mode == "terminal") readPane() else loadHistory()
            "字号 ＋" -> terminal?.fontSize(1); "字号 −" -> terminal?.fontSize(-1); "管理 Agent" -> manageAgent()
        } }.show()
    }
    private fun showBindingDiagnostics() {
        val agent = current() ?: run { toast("会话已变化，请刷新后重试"); return }
        if (!capability(SessionBindingDiagnostics.CAPABILITY)) {
            showText("会话绑定诊断", "此 Relay 未提供诊断接口，请升级 Relay 后重试。")
            return
        }
        val owner = epoch
        request(SessionBindingDiagnostics.ACTION, timeout = 8000) { data, error ->
            if (!active || owner != epoch) return@request
            if (error != null || data == null) {
                showText("会话绑定诊断", error ?: "诊断数据暂不可用，请稍后重试。")
            } else {
                val text = runCatching { SessionBindingDiagnostics.render(relay, agent, data) }
                    .getOrElse { "诊断响应与当前会话不匹配或格式无效，请刷新后重试。" }
                showText("会话绑定诊断", text)
            }
        }
    }

    private fun updateSendState() {
        updateTerminalControls()
        val available = (if (mode == "terminal") terminalWritable() else writable()) && !inputPending && !uploading && !uncertain && !loadingOlderTerminal
        terminalKeys?.setAvailable(active && mode == "terminal" && available)
        composerPanel?.setAvailability(available && (uiDraft.isNotBlank() || attachments.length() > 0), available && writable() && !frame.optBoolean("no_echo") && selected?.optString("agent") !in listOf("pi", "omp"))
        if (uncertain) stateLabel?.text = "上次操作结果未确认。刷新终端并核对后，在输入方式中解除。"
    }
    private fun startStreams() {
        updateImageAvailability()
        if (!active || selected == null || relay !in online) return
        stopStreams(); startRunStatus(); val owner = epoch
        if (mode == "conversation") loadHistory() else {
            readPane()
            fun fallback() { if (active && epoch == owner && mode == "terminal") { if (!capability("pane_realtime_delta")) readPane(); main.postDelayed({ fallback() }, 1200) } }
            main.postDelayed({ fallback() }, 1200)
        }
    }
    private fun updateImageAvailability() {
        conversationImages.setAvailability(active && selected != null && mode == "conversation", relay in online && current() != null && inventoryReady())
    }
    private fun inventoryReady(): Boolean = configs[relay]?.optJSONObject("inventory")?.optString("state") == "ready"
    private fun updateRunStatus() {
        runStatusView?.show(runPresentation.display(current(), relay in online, inventoryReady(), capability("agent_run_state_v1"), SystemClock.elapsedRealtime()))
    }
    private fun startRunStatus() {
        main.removeCallbacks(runTicker); runRefreshAt = 0
        runTicker.run()
    }
    private fun readRunStatus() {
        if (!active || selected == null || current() == null || relay !in online || !inventoryReady() || runPending || !capability("agent_run_state_v1")) return
        val owner = epoch; val signal = runSignal; val requestId = ++runRequest; val target = WorkspaceBinding.fromAgent(relay, requireNotNull(selected))
        runPending = true; runRefreshAt = SystemClock.elapsedRealtime() + 5000
        request("get_agent_run_state", timeout = 8000, trackAfterNavigation = true) { data, error ->
            if (owner != epoch || requestId != runRequest || target != selected?.let { WorkspaceBinding.fromAgent(relay, it) }) return@request
            runPending = false
            if (signal != runSignal) { runRefreshAt = 0; updateRunStatus(); return@request }
            if (error == null && data != null && relay in online && current() != null) {
                try { runPresentation.accept(data, SystemClock.elapsedRealtime()) } catch (_: IllegalArgumentException) { runPresentation.invalidate(); runPresentation.failed() }
            } else { runPresentation.failed() }
            updateRunStatus()
        }
    }
    private fun request(type: String, payload: JSONObject = JSONObject(), timeout: Long = 20000, trackAfterNavigation: Boolean = false, callback: (JSONObject?, String?) -> Unit) {
        val agent = current() ?: run { callback(null, "Agent 会话已变化"); return }
        val command = AgentProtocol.command(relay, agent, type)
        payload.keys().forEach { command.put(it, payload.get(it)) }
        FileReadScope.apply(command, unrestrictedFiles(relay))
        val target = WorkspaceBinding.fromAgent(relay, agent)
        engine.request(relay, command, timeout) { data, error ->
            if (trackAfterNavigation || target == selected?.let { WorkspaceBinding.fromAgent(relay, it) }) callback(data, error)
        }
    }
    private fun rememberConversation() {
        selected?.let { conversationCache.put(WorkspaceBinding.fromAgent(relay, it), conversation) }
    }
    private fun updateHistoryControl() { historyControl?.update(historyPending, historyOlder, conversation.next.isNotBlank(), historyMessage, historyProgress) }
    private fun cancelHistoryWait() {
        historyRequest++; historyPending = false; resumeOlderAfterReconnect = false; historyProgress = null
        historyMessage = "已取消等待，已读历史保留"; updateHistoryControl()
    }
    private fun loadHistory(older: Boolean = false) {
        if (historyPending || !active || mode != "conversation") return
        if (!PrimarySessionBinding.readable(current())) {
            historyMessage = selected?.let { PrimarySessionBinding.label(it) }.orEmpty()
            stateLabel?.text = historyMessage; updateHistoryControl(); return
        }
        if (older && conversation.next.isEmpty()) { toast("已到最早消息"); return }
        if (!older) conversationPoll++
        historyPending = true; historyOlder = older; historyMessage = ""; historyProgress = null
        updateHistoryControl(); stateLabel?.text = if (older) "正在读取更早消息…已读内容保留" else "正在同步对话…"
        val owner = epoch; val requestOwner = ++historyRequest
        var preparations = 0
        val recovery = if (!older) ConversationRecentRecovery(conversation) else null
        val recoveryStarted = SystemClock.elapsedRealtime()
        var recoveryIncomplete = false
        val initial = ConversationHistoryRequest(older, if (older) conversation.next else "", !older && capability("conversation_activity") && PrimarySessionBinding.live(current()))
        fun readPage(pageRequest: ConversationHistoryRequest) {
            request("get_conversation_history", pageRequest.payload(), 90000) { response, error ->
                var data = response
                if (owner != epoch || requestOwner != historyRequest || !active || mode != "conversation") return@request
                if (error != null || data == null) {
                    if (older) resumeOlderAfterReconnect = true
                    historyPending = false; historyMessage = "读取失败，已读历史保留；可重试"; updateHistoryControl()
                    stateLabel?.text = error ?: "对话不可用"; return@request
                }
                if (data.optString("state") == "preparing") {
                    // Keep both the older-page intent and the server-issued preparation cursor.
                    // Restarting loadHistory() here loses the cursor and loops on the recent tail.
                    if (++preparations > 90) { historyPending = false; historyMessage = "历史仍在整理，可稍后重试"; updateHistoryControl(); return@request }
                    historyProgress = data.optJSONObject("progress"); historyMessage = "正在整理历史…"; updateHistoryControl()
                    stateLabel?.text = if (pageRequest.older) "正在整理更早历史…当前对话会保留" else "正在整理历史…"
                    val preparedRequest = pageRequest.afterPreparing(data)
                    main.postDelayed({ if (owner == epoch && requestOwner == historyRequest && active) readPage(preparedRequest) }, 800)
                    return@request
                }
                if (recovery != null && data.optBoolean("available") && data.optString("state") != "failed") {
                    try { recovery.accept(data) } catch (_: IllegalArgumentException) {
                        historyPending = false; historyMessage = "补齐对话时版本发生变化，请重试"; updateHistoryControl(); return@request
                    }
                    if (recovery.needsMore && SystemClock.elapsedRealtime() - recoveryStarted < 20000) {
                        historyMessage = "正在补齐中途回复与图片…已读取 ${recovery.size} 条"; updateHistoryControl()
                        stateLabel?.text = historyMessage
                        main.post { if (owner == epoch && requestOwner == historyRequest && active) readPage(ConversationHistoryRequest(true, recovery.next, false)) }
                        return@request
                    }
                    recoveryIncomplete = recovery.incomplete
                    data = recovery.page()
                }
                historyPending = false; historyProgress = null
                if (!conversation.page(data, older, preserveLoaded = true)) {
                    historyMessage = if (older) "历史版本已变化，请刷新后重试" else "暂未读到会话，可重试"; updateHistoryControl()
                    val code = data.optString("reason_code").ifBlank { data.optJSONObject("error")?.optString("code").orEmpty() }
                    if (older && code in listOf("cursor_expired", "invalid_cursor", "source_changed", "snapshot_expired")) {
                        conversation.resetCursor(); resumeOlderAfterReconnect = false
                        historyMessage = "历史游标已过期，点重试重新同步"; updateHistoryControl()
                    }
                    stateLabel?.text = when (code) {
                        "invalid_session" -> if (selected?.optString("agent_session_id").isNullOrBlank())
                            "Herdr 未提供此 Agent 的会话 ID，暂可查看终端；不会按目录猜测其他会话"
                            else "电脑端未定位到此会话日志，可先查看终端；不是手机连接失败"
                        else -> data.optJSONObject("error")?.optString("message").orEmpty()
                            .ifBlank { data.optString("reason").ifBlank { "此 Agent 暂无结构化历史，可切换终端" } }
                    }
                    return@request
                }
                historyMessage = if (recoveryIncomplete) "最近对话尚未完整补齐，可加载更早消息继续" else ""; if (older) resumeOlderAfterReconnect = false
                rememberConversation(); updateHistoryControl()
                stateLabel?.text = when {
                    recoveryIncomplete -> "最近对话未完整补齐 · 已读内容保留"
                    data.optString("live_state") == "incomplete" -> "历史可能不完整"
                    conversation.next.isNotBlank() -> "已同步 ${conversation.entries.size} 条 · 可加载更早消息"
                    else -> "已同步 ${conversation.entries.size} 条对话"
                }
                renderConversation(older); historyOlder = false
                if (!older) pollConversation(owner)
                if (!older && resumeOlderAfterReconnect && conversation.next.isNotBlank()) {
                    resumeOlderAfterReconnect = false; main.post { if (owner == epoch) loadHistory(true) }; return@request
                }

            }
        }
        readPage(initial)
    }
    private fun pollConversation(owner: Int, poll: Int = conversationPoll) {
        if (owner != epoch || poll != conversationPoll || !active || mode != "conversation") return
        if (conversation.subscription.isEmpty()) { main.postDelayed({ if (owner == epoch && poll == conversationPoll) loadHistory() }, 4000); return }
        request("get_conversation_updates", JSONObject().put("source_revision", conversation.revision).put("after", conversation.sequence).put("subscription_id", conversation.subscription), 18000) { data, error ->
            if (owner != epoch || poll != conversationPoll || !active) return@request
            if (error != null || data == null || !conversation.update(data)) {
                fun refreshAfterHistory() {
                    if (owner != epoch || poll != conversationPoll || !active) return
                    if (historyPending) main.postDelayed({ refreshAfterHistory() }, 500) else loadHistory()
                }
                main.postDelayed({ refreshAfterHistory() }, 1000); return@request
            }
            if (data.optString("kind") != "heartbeat") {
                if (!historyPending) stateLabel?.text = "已同步 ${conversation.entries.size} 条" + if (conversation.next.isNotBlank()) " · 可加载更早消息" else "对话"
                renderConversation()
            }
            main.postDelayed({ pollConversation(owner, poll) }, 100)
        }
    }
    private fun renderConversation(prepended: Boolean = false) {
        val container = conversationBody ?: return
        val scroll = conversationScroll ?: return
        val previousHeight = container.height; val previousY = scroll.scrollY
        val follow = !prepended && !historyOlder && (container.childCount == 0 || previousHeight - previousY - scroll.height < dp(80))
        container.removeAllViews()
        historyControl = HistoryLoadView(context, { loadHistory(conversation.next.isNotBlank()) }, ::cancelHistoryWait)
        container.addView(historyControl, LinearLayout.LayoutParams(-1, -2).apply { bottomMargin = dp(12) })
        updateHistoryControl()
        for (block in ConversationTimeline.blocks(conversation.entries.values)) {
            when (block) {
                is ConversationTimeline.Tools -> {
                    container.addView(NativeToolGroup(context, block, expandedToolGroups, expandedToolItems, ::showText),
                        LinearLayout.LayoutParams(-1, -2).apply { bottomMargin = dp(10) })
                    // Images remain visible even while both levels of tool details are collapsed.
                    val shown = mutableSetOf<String>()
                    for (entry in block.entries) renderConversationImages(container, entry, shown)
                }
                is ConversationTimeline.Message -> {
                    val entry = block.entry
                    val isUser = entry.optString("role") == "user"
                    val box = column(if (isUser) 12 else 2).apply {
                        if (isUser) background = AgentStyle.shape(context, AgentStyle.panel, 12)
                    }
                    if (!isUser && entry.optString("text").isNotBlank()) {
                        val author = row()
                        author.addView(label(selected?.let(::name).orEmpty(), 12, muted).apply { typeface = AgentStyle.medium; maxLines = 1; ellipsize = android.text.TextUtils.TruncateAt.END }, weight())
                        box.addView(author, LinearLayout.LayoutParams(-1, -2).apply { bottomMargin = dp(8) })
                    }
                    if (entry.optString("text").isNotBlank()) {
                        val text = label("", 16, textInk).apply { setTextIsSelectable(true); setLineSpacing(0f, 1.22f); setLinkTextColor(accent) }
                        renderMarkdown.setMarkdown(text, entry.optString("text")); text.movementMethod = TableAwareMovementMethod.create(); box.addView(text)
                    }
                    if (entry.optBoolean("truncated")) box.addView(label("此条内容已截断", 11, muted))
                    container.addView(box, LinearLayout.LayoutParams(-1, -2).apply { bottomMargin = dp(18); if (isUser) marginStart = dp(36) })
                    if ((entry.optJSONArray("tools")?.length() ?: 0) == 0) renderConversationImages(container, entry, mutableSetOf())
                }
            }
        }
        scroll.post { if (conversationBody === container) { if (follow) scroll.scrollTo(0, (container.height - scroll.height).coerceAtLeast(0)) else if (prepended) scroll.scrollTo(0, previousY + container.height - previousHeight) else scroll.scrollTo(0, previousY) } }
    }
    private fun renderConversationImages(container: LinearLayout, entry: JSONObject, shown: MutableSet<String>) {
        val binding = selected?.let { WorkspaceBinding.fromAgent(relay, it) } ?: return
        val cwd = binding.cwd
        for (image in ConversationPresentation.images(entry, cwd, unrestrictedFiles(relay))) {
            val fingerprint = sha(image.source.toByteArray(Charsets.UTF_8))
            if (!shown.add(image.workspacePath ?: fingerprint)) continue
            val key = deliveryKey() + ":" + entry.getString("id") + ":" + fingerprint
            container.addView(conversationImages.view(image, key, {
                openReview(ReviewReference(image.workspacePath.orEmpty(), image.label, inlineImage = image.source.takeIf { it.startsWith("data:image/") }))
            }) { path, done ->
                if (!active || current()?.let { WorkspaceBinding.fromAgent(relay, it) } != binding) done(null, "Agent 会话已变化或读取暂停")
                else request("workspace_file", JSONObject().put("path", path), 15000) { data, error ->
                    if (error != null || data?.optString("kind") != "image") done(null, error ?: "未返回图像")
                    else done(data.optString("data_url"), null)
                }
            }, LinearLayout.LayoutParams(-1, -2).apply { topMargin = dp(6); bottomMargin = dp(12) })
        }
        for (ref in ReviewReferences.documents(entry, cwd, unrestrictedFiles(relay))) {
            if (!shown.add("document:${ref.path}#${ref.anchor}:${ref.line}")) continue
            val card = AgentStyle.button(context, "${when (ref.kind) { "pdf" -> "PDF"; "markdown" -> "Markdown"; else -> "文件" }}  ·  ${ref.label}", subtle = true) { openReview(ref) }
            card.contentDescription = "查看文档：${ref.path}"
            card.isAllCaps = false; card.gravity = Gravity.START or Gravity.CENTER_VERTICAL; card.setCompoundDrawables(AgentStyle.icon(context, "file"), null, AgentStyle.icon(context, "chevron", size = 16), null); card.compoundDrawablePadding = dp(10)
            container.addView(card, LinearLayout.LayoutParams(-1, -2).apply { topMargin = dp(4); bottomMargin = dp(8) })
        }
    }
    private fun openReviewLink(link: String) {
        val cwd = current()?.optString("cwd").orEmpty()
        val ref = ReviewReferences.local(link, cwd, unrestricted = unrestrictedFiles(relay))
        if (ref != null) openReview(ref) else DocumentReviewer.confirmExternal(context, link)
    }
    private fun openReview(ref: ReviewReference) {
        val agent = current() ?: return
        val binding = WorkspaceBinding.fromAgent(relay, agent)
        val unrestricted = unrestrictedFiles(relay)
        reviewer?.close()
        val download: ((String, Boolean) -> Unit)? = if (capability("workspace_file_download_v1") && downloads != null) { path, share ->
            val view = reviewer; val version = view?.requestVersion; val id = relay
            downloads.start(path, share, { active && unrestrictedFiles(relay) == unrestricted && reviewer === view && view?.isOpen == true && view.requestVersion == version && current()?.let { WorkspaceBinding.fromAgent(relay, it) } == binding }) { payload, callback ->
                engine.request(id, FileReadScope.apply(AgentProtocol.command(id, agent, "workspace_file_download"), unrestricted).apply { payload.keys().forEach { put(it, payload.get(it)) } }, 60000, callback)
            }
        } else null
        reviewer = DocumentReviewer(context, binding.cwd, { active && unrestrictedFiles(relay) == unrestricted && current()?.let { WorkspaceBinding.fromAgent(relay, it) } == binding }, download, unrestricted) { path, done ->
            val extended = capability("workspace_file_read_v1") || capability("workspace_mobile_files_v1")
            val view = reviewer; val version = view?.requestVersion
            if (capability("workspace_file_download_v1")) DocumentTransfer.preview(path,
                { active && unrestrictedFiles(relay) == unrestricted && reviewer === view && view?.isOpen == true && view.requestVersion == version && current()?.let { WorkspaceBinding.fromAgent(relay, it) } == binding },
                { type, payload, callback -> request(type, payload, 30000, callback = callback) }, done)
            else if (capability("workspace_file_read_v1")) DocumentTransfer.read(path,
                { active && unrestrictedFiles(relay) == unrestricted && reviewer === view && view?.isOpen == true && view.requestVersion == version && current()?.let { WorkspaceBinding.fromAgent(relay, it) } == binding },
                { payload, callback -> request("workspace_file_read", payload, 30000, callback = callback) }, done)
            else if (path.endsWith(".pdf", true) && !extended) done(null, "此 Relay 未提供 PDF 读取能力，请升级 Relay 后重试")
            else request(if (extended) "workspace_file_read" else "workspace_file", JSONObject().put("path", path), 30000, callback = done)
        }
        reviewer?.open(ref)
    }
    private fun updateTerminalControls() {
        terminalOlderButton?.text = if (loadingOlderTerminal) "读取中…" else "更早输出"
        terminalOlderButton?.isEnabled = active && !terminalReadPending && !inputPending && !uncertain && !uploading && relay in online && current()!=null
        terminalHistorySpinner?.visibility = if (loadingOlderTerminal) View.VISIBLE else View.GONE
    }
    private fun updateTerminalStatus() {
        if(mode!="terminal" || frame.optBoolean("no_echo") || uncertain) return
        stateLabel?.text = when {
            terminalHistoryMessage.isNotBlank() -> terminalHistoryMessage
            terminalScroll?.followsOutput == false -> "正在查看已加载历史 · 新输出不会拉回底部"
            else -> "终端 ${content.count { it=='\n' }+1} 行 · 向下拖动看更早内容" + if(frame.optBoolean("truncated")) "（电脑端有截断）" else ""
        }
    }
    private fun noMoreTerminalHistory() {
        val binding = current()?.let { WorkspaceBinding.fromAgent(relay,it) } ?: return
        val options = arrayOf("远端 PgUp 翻页（会改变电脑视图）", "切到对话看历史", "重新请求更早输出", "保持当前画面")
        AlertDialog.Builder(context).setTitle("电脑没有返回更多终端历史")
            .setItems(options) { _,i -> if (!active || mode!="terminal" || current()?.let { WorkspaceBinding.fromAgent(relay,it) } != binding) return@setItems
                when(i) {
                0 -> sendKeys("PPage")
                1 -> { saveDraft(); stopStreams(); mode="conversation"; render() }
                2 -> readPane(terminalHistoryLines<10000)
            } }.show()
    }
    private fun readPane(older: Boolean = false, automatic: Boolean = false) {
        if (terminalReadPending || !active || mode!="terminal" || current()==null || relay !in online) return
        if (older && (inputPending || uncertain || uploading)) return
        if (older && terminalHistoryLines>=10000) { terminalHistoryMessage="已请求最多 10,000 行；程序内部历史可用 PgUp/PgDn 或对话页查看"; updateTerminalStatus(); if(!automatic) noMoreTerminalHistory(); return }
        val requested = if(older) (terminalHistoryLines+1000).coerceAtMost(10000) else terminalHistoryLines
        val previousRows = content.count { it=='\n' } + if(content.isEmpty()) 0 else 1
        if (older) {
            loadingOlderTerminal=true; terminalHistoryMessage="正在请求更早输出…已加载 $previousRows 行"
            selected?.let { engine.send(relay, AgentProtocol.command(relay,it,"unwatch_pane")) }
        }
        terminalReadPending=true; updateTerminalControls(); updateTerminalStatus(); updateSendState()
        val owner=epoch; val requestOwner=++terminalRequest
        request("read_pane",JSONObject().put("lines",requested).put("format","ansi"),30000) { data,error ->
            if(owner!=epoch || requestOwner!=terminalRequest) return@request
            terminalReadPending=false
            val frameError = data?.optString("error")?.takeIf { it.isNotBlank() && it!="null" }
            if(error!=null || data==null || frameError!=null) {
                loadingOlderTerminal=false; terminalHistoryMessage=(error ?: frameError ?: "终端未返回画面")+"；已读历史保留"
                updateTerminalControls(); updateTerminalStatus(); updateSendState(); watchTerminal(); return@request
            }
            if (!AgentProtocol.targetMatches(relay, current() ?: return@request, data) || data.optString("type") !in listOf("pane_content", "pane_unchanged")) {
                loadingOlderTerminal=false; terminalHistoryMessage="终端响应身份或类型不匹配；已读内容保留"; updateTerminalControls(); updateTerminalStatus(); updateSendState(); watchTerminal(); return@request
            }
            if(older) {
                terminalHistoryLines=requested
                val returned = if(data.optString("type")=="pane_unchanged") content else data.optString("content"); val count = returned.count { it=='\n' } + if(returned.isEmpty()) 0 else 1
                terminalHasMore=count>previousRows && requested<10000
                terminalHistoryMessage=if(count<=previousRows) "电脑只返回 $count 行；PgUp/PgDn 可翻程序内部历史（影响电脑视图）" else "已加载 $count 行 · 可继续向下拖动查看更早内容"
            } else if(!terminalHistoryMessage.contains("只返回") && !terminalHistoryMessage.contains("远端翻页")) terminalHistoryMessage=""
            updateTerminalControls()
            acceptFrame(data)
        }
    }
    private fun watchTerminal() {
        val agent = current() ?: return
        if (!active || mode!="terminal" || relay !in online || !capability("pane_realtime_delta")) return
        engine.send(relay,AgentProtocol.command(relay,agent,"watch_pane").put("lines",terminalHistoryLines).put("format","ansi").put("interval_ms",500).put("content_fingerprint",fingerprint))
    }
    private fun acceptFrame(value: JSONObject) {
        val type=value.optString("type")
        if(type=="pane_resync") { readPane(); return }
        if(value.optString("error").let { it.isNotBlank() && it!="null" }) {
            loadingOlderTerminal=false; terminalHistoryMessage=value.optString("error")+"；已读终端保留"; updateTerminalControls(); updateTerminalStatus(); updateSendState(); return
        }
        if(type=="pane_unchanged") { loadingOlderTerminal=false; updateTerminalControls(); updateTerminalStatus(); updateSendState(); watchTerminal(); return }
        if(type=="pane_delta") content=AgentProtocol.applyDelta(content,value,fingerprint) ?: run {readPane();return}
        else if(type=="pane_content") content=value.optString("content")
        fingerprint=value.optString("content_fingerprint",fingerprint)
        // Deltas do not necessarily repeat privacy and width metadata.
        frame=JSONObject(frame.toString()).apply { value.keys().forEach { put(it,value.get(it)) } }; frameObservedAt=System.currentTimeMillis()
        val older=loadingOlderTerminal && type=="pane_content"
        terminalScroll?.update(content,frame.optInt("columns",0),older)
        loadingOlderTerminal=false; updateTerminalControls(); updateTerminalStatus()
        val agent=selected ?: return
        if(value.has("interaction")) { selected!!.put("interaction",value.opt("interaction"));current()?.put("interaction",value.opt("interaction"));updateInteraction() }
        if(fingerprint.isNotBlank()) {
            engine.send(relay,AgentProtocol.command(relay,agent,"pane_applied").put("content_fingerprint",fingerprint))
            if(type=="pane_content") watchTerminal()
        }
        composerPanel?.setSecretInput(frame.optBoolean("no_echo"))
        if(frame.optBoolean("no_echo")) stateLabel?.text="终端要求隐私输入，内容不会保存为草稿"
        updateSendState()
    }
    private fun updateInteraction() {
        val container = interactionBody ?: return; container.removeAllViews()
        val agent = current() ?: return
        val question = agent.optJSONObject("interaction")
        if (question != null && question.optString("id").isNotBlank()) {
            container.addView(label(question.optString("question"), 15, textInk))
            container.addView(button("回答问题…") { answerQuestion(JSONObject(question.toString())) }.apply { isEnabled = writable() && !inputPending }); return
        }
        if (agent.optString("status") != "blocked") return
        val options = agent.optJSONArray("options") ?: return
        if (agent.optString("approval_fingerprint").isBlank()) { container.addView(label("需要处理：请在终端核对当前界面", 13, accent)); return }
        val snapshot = JSONObject(agent.toString())
        container.addView(label(agent.optString("prompt").take(500), 14, textInk))
        val actions = row()
        for (i in 0 until options.length()) actions.addView(button(options.getString(i)) {
            val live = current() ?: return@button; val payload = AgentProtocol.approval(live, snapshot, i)
            if (payload == null) toast("审批已经变化，请处理最新请求") else mutate("respond", payload)
        }.apply { isEnabled = writable() && !inputPending })
        container.addView(HorizontalScrollView(context).apply { addView(actions) })
    }
    private fun answerQuestion(captured: JSONObject) {
        val target = current()?.let { WorkspaceBinding.fromAgent(relay, it) } ?: return
        val ownerEpoch = epoch
        val stillCurrent = { ownerEpoch == epoch && current()?.let { WorkspaceBinding.fromAgent(relay, it) } == target }
        val stepwise = captured.optBoolean("stepwise")
        val form = column(12); form.addView(label(captured.optString("question"), 16, textInk))
        val options = captured.optJSONArray("options") ?: JSONArray(); val checked = mutableSetOf<Int>(); val buttons = mutableListOf<CheckBox>()
        for (i in 0 until options.length()) {
            val option = options.getJSONObject(i); val index = option.optInt("index", i)
            val box = CheckBox(context).apply { text = option.optString("label") + "\n" + option.optString("description"); setTextColor(textInk); isChecked = option.optBoolean("selected"); if (isChecked) checked += index }
            box.setOnCheckedChangeListener { _, enabled -> if (enabled) { if (captured.optString("kind") == "single_select") { checked.clear(); buttons.filter { it !== box }.forEach { it.isChecked = false } }; checked += index } else checked -= index }
            buttons += box; form.addView(box)
        }
        val other = captured.optJSONObject("other")
        val otherEnabled = CheckBox(context).apply { text = other?.optString("label").orEmpty().ifBlank { "其他" }; setTextColor(textInk) }
        otherEnabled.setOnCheckedChangeListener { _, enabled ->
            if (enabled && captured.optString("kind") == "single_select") { checked.clear(); buttons.forEach { it.isChecked = false } }
        }
        if (captured.optString("kind") == "single_select") buttons.forEach { box -> box.setOnCheckedChangeListener { _, enabled ->
            if (enabled) { checked.clear(); buttons.filter { it !== box }.forEach { it.isChecked = false }; otherEnabled.isChecked = false }
            val index = options.getJSONObject(buttons.indexOf(box)).optInt("index", buttons.indexOf(box))
            if (enabled) checked += index else checked -= index
        } }
        val text = EditText(context).apply { hint = other?.optString("placeholder"); setTextColor(textInk) }
        if (other != null && !other.optBoolean("hidden")) { form.addView(otherEnabled); form.addView(text) }
        val dialog = AlertDialog.Builder(context).setTitle(if (stepwise) "操作当前交互" else "回答 Agent").setView(ScrollView(context).apply { addView(form) }).setPositiveButton(captured.optString("submit_label", "提交"), null).setNegativeButton("取消", null)
        if (captured.optBoolean("can_go_back")) dialog.setNeutralButton("上一题") { _, _ ->
            if (stillCurrent()) questionAction("navigate_question", captured, JSONObject().put("direction", "previous")) else toast("会话已变化，请重新打开当前问题")
        }
        val shown = dialog.create(); shown.setOnShowListener { shown.getButton(AlertDialog.BUTTON_POSITIVE).setOnClickListener {
            if (!stillCurrent()) { toast("会话已变化，请重新打开当前问题"); shown.dismiss(); return@setOnClickListener }
            if (checked.isEmpty() && !otherEnabled.isChecked && !(stepwise && captured.optString("kind") == "multi_select")) { toast("请选择答案"); return@setOnClickListener }
            if (otherEnabled.isChecked && text.text.isBlank() && other?.optBoolean("allow_empty") != true) { toast("请填写其他答案"); return@setOnClickListener }
            questionAction("answer_question", captured, JSONObject().put("selected_indices", JSONArray(checked.sorted())).put("other_selected", otherEnabled.isChecked).put("other_text", if (otherEnabled.isChecked) text.text.toString() else "")); shown.dismiss()
        } }; shown.show()
    }
    private fun questionAction(type: String, captured: JSONObject, payload: JSONObject) {
        if (current()?.optJSONObject("interaction")?.optString("id") != captured.optString("id")) { toast("问题已变化，请重新打开"); return }
        mutate(type, payload.put("interaction_id", captured.getString("id")))
    }
    private fun sendText() {
        val text = composer?.text?.toString().orEmpty(); if (text.isBlank() && attachments.length() == 0) return
        if (frame.optBoolean("no_echo")) { if (System.currentTimeMillis() - frameObservedAt > 15000) { readPane(); toast("请在最新终端画面确认隐私输入"); return }; mutate("send_secret", JSONObject().put("text", text), true); return }
        if (mode == "terminal") {
            if (text.any { it.code < 32 || it.code in 127..159 } || text.toByteArray().size > 65536) { toast("终端文本需要单行、无控制字符；Enter 单独发送"); return }
            mutate("send_terminal_text", JSONObject().put("text", text), true)
        } else {
            if (current()?.optString("status") == "blocked") { toast("请先回答审批或问题，或在终端使用手动输入"); return }
            val attachmentText = (0 until attachments.length()).joinToString("\n") { "Attachment: ${attachments.getJSONObject(it).getString("ref")}" }
            mutate("submit_prompt", JSONObject().put("text", if (attachmentText.isEmpty()) text else "$text\n$attachmentText\n"), true)
        }
    }
    private fun sendKeys(key: String) {
        if (!active || mode!="terminal") return
        if(loadingOlderTerminal) { toast("正在读取终端历史，请稍候"); return }
        if(key in listOf("PPage","NPage")) terminalHistoryMessage="远端翻页：会改变电脑端程序视图；不会发送手机输入框草稿"
        val command = TerminalKeyCommand.from(key) ?: run { toast("不支持的终端按键"); return }
        mutate(command.type, command.payload)
    }
    private fun mutate(type: String, payload: JSONObject = JSONObject(), clear: Boolean = false) {
        if (!operationWritable(type) || inputPending || uncertain) { toast("当前不可发送：检查连接、权限和上次操作结果"); return }
        val delivery = deliveryKey(); val owner = selected?.let { WorkspaceBinding.fromAgent(relay, it) }; val sent = composer?.text?.toString()
        // Persist before dispatch: process death and navigation cannot silently permit a duplicate.
        saveDraft(); vault.write(delivery, "unconfirmed"); pendingMutations += delivery
        updateSendState(); updateInteraction()
        request(type, payload, if (type == "agent_start") 60000 else 25000, trackAfterNavigation = true) { data, error ->
            pendingMutations -= delivery
            val unknown = error?.let { it.contains("未确认") || it.contains("超时") }
                ?: (data?.optString("_phase") in listOf("unconfirmed", "dispatched_unknown", "awaiting_evidence"))
            if (!unknown) vault.remove(delivery)
            if (owner != selected?.let { WorkspaceBinding.fromAgent(relay, it) }) return@request
            if (error != null) { toast(error) }
            else {
                if (clear && !uncertain && composer?.text?.toString() == sent) { composer?.setText(""); uiDraft = ""; vault.remove(draftKey()); attachments = JSONArray(); updateAttachments() }
                if (uncertain) toast("已投递，执行结果尚未确认，请核对终端")
            }
            updateSendState(); updateInteraction(); if (active) { if (mode == "terminal") readPane() else loadHistory() }
        }
    }
    private fun inputMenu() {
        val binding = current()?.let { WorkspaceBinding.fromAgent(relay,it) } ?: return
        fun selectedCurrent() = active && mode=="terminal" && current()?.let { WorkspaceBinding.fromAgent(relay,it) } == binding
        val keys = listOf("退格 · 删除远端光标前字符" to "BSpace", "Delete · 删除远端光标后字符" to "DC", "Home · 远端行首" to "Home", "End · 远端行尾" to "End", "Page Up · 翻远端历史" to "PPage", "Page Down · 翻远端历史" to "NPage", "← · 光标向左" to "Left", "↑ · 上方向键" to "Up", "↓ · 下方向键" to "Down", "→ · 光标向右" to "Right", "Ctrl-C · 中断" to "C-c", "Ctrl-D · EOF / 退出" to "C-d", "Ctrl-L · 清屏" to "C-l", "Ctrl-U · 删除到行首" to "C-u", "Ctrl-W · 删除前一个词" to "C-w")
        val actions = listOf("核对后解除未确认状态", "调整电脑终端尺寸", "问题改为聊天", "功能键 F1–F12", "Ctrl / Alt / Shift 组合键")
        AlertDialog.Builder(context).setTitle("远端终端按键（不修改手机草稿）").setItems((keys.map { it.first } + actions).toTypedArray()) { _, index ->
            if(!selectedCurrent()) return@setItems
            if(index < keys.size) { sendKeys(keys[index].second); return@setItems }
            when(index-keys.size) {
                0 -> { val delivery = deliveryKey(); AlertDialog.Builder(context).setMessage("请先刷新并确认上次输入是否已经执行。解除后不会自动重发。 ").setPositiveButton("我已核对") { _, _ -> if (!selectedCurrent()) return@setPositiveButton; if (delivery in pendingMutations) toast("请求仍在等待响应，请稍后核对") else { vault.remove(delivery); updateSendState(); updateInteraction() } }.setNegativeButton("取消", null).show() }
                1 -> form("终端尺寸", listOf("列数（40–240）" to "100", "行数（10–120）" to "32")) { values -> if (!selectedCurrent()) return@form; val cols = values[0].toIntOrNull(); val rows = values[1].toIntOrNull(); if (cols !in 40..240 || rows !in 10..120) toast("尺寸超出范围") else mutate("lease_pane_size", JSONObject().put("columns", cols).put("rows", rows).put("opt_in", true)) }
                2 -> current()?.optJSONObject("interaction")?.takeIf { it.optBoolean("can_chat") }?.let { questionAction("clarify_question", it, JSONObject()) }
                3 -> AlertDialog.Builder(context).setTitle("功能键").setItems((1..12).map { "F$it" }.toTypedArray()) { _, n -> if(selectedCurrent()) sendKeys("f${n + 1}") }.show()
                4 -> form("组合键", listOf("例如 C-a、M-x、S-Tab" to "C-a")) { values -> if (selectedCurrent() && values[0].matches(Regex("(?:(?:C|M|S)-){1,3}(?:[A-Za-z0-9]|Tab|Enter|Left|Right|Up|Down|Home|End)"))) sendKeys(values[0]) else toast("组合键格式无效") }
            }
        }.show()
    }
    private fun slashCommands() { request("list_slash_commands") { data, error -> if (error != null) toast(error) else { val commands = data?.optJSONArray("commands") ?: JSONArray(); AlertDialog.Builder(context).setTitle("Agent 指令").setItems((0 until commands.length()).map { commands.getJSONObject(it).optString("command") + "  " + commands.getJSONObject(it).optString("description") }.toTypedArray()) { _, i -> composer?.setText(commands.getJSONObject(i).getString("command") + " ") }.show() } } }
    private fun showSearch() {
        val text = if (mode == "terminal") terminal?.plainText().orEmpty() else conversation.entries.values.joinToString("\n\n") { it.optString("text") }
        showText("搜索 / 复制", text)
    }
    private fun manageAgent() {
        val captured = current()?.let { WorkspaceBinding.fromAgent(relay, it) } ?: return
        AlertDialog.Builder(context).setTitle("管理 ${selected?.let(::name)}").setItems(arrayOf("重命名", "停止 Agent", "重启 Agent", "清空会话", "复制最新回复", "Git 状态", "活动记录", "朗读最新回复", "移动标签顺序")) { _, i ->
            when (i) {
                0 -> selected?.let { renameAgent(relay, JSONObject(it.toString())) }
                1, 2, 3 -> AlertDialog.Builder(context).setMessage("确认${arrayOf("", "停止", "重启", "清空")[i]}这个 Agent？").setPositiveButton("确认") { _, _ ->
                    if (!active || current()?.let { WorkspaceBinding.fromAgent(relay, it) } != captured) toast("目标终端已变化，请重新打开管理菜单")
                    else mutate(arrayOf("", "agent_stop", "agent_restart", "agent_clear")[i])
                }.setNegativeButton("取消", null).show()
                4 -> request("copy_agent_response") { data, error -> if (error != null) toast(error) else copy(data?.optString("text").orEmpty()) }
                5 -> gitStatus()
                6 -> activity(relay)
                7 -> request("copy_agent_response") { data, error -> if (error != null) toast(error) else speak(data?.optString("text").orEmpty()) }
                8 -> form("移动标签", listOf("目标序号（从 0 起）" to "0")) { values -> values[0].toIntOrNull()?.let { mutate("tab_reorder", JSONObject().put("insert_index", it)) } }
            }
        }.show()
    }
    private fun gitStatus() { request("workspace_git_status") { data, error ->
        if (error != null) { toast(error); return@request }; val files = data?.optJSONArray("files") ?: JSONArray()
        AlertDialog.Builder(context).setTitle("Git · ${data?.optString("branch")}").setItems((0 until files.length()).map { files.getJSONObject(it).optString("status") + " " + files.getJSONObject(it).optString("path") }.toTypedArray()) { _, i -> val path = files.getJSONObject(i).getString("path"); request("workspace_git_diff", JSONObject().put("path", path)) { diff, e -> if (e != null) toast(e) else showText(path, diff?.optString("diff").orEmpty()) } }.show()
    } }
    private fun managementReady(id: String): Boolean = id in online && role(id) == "controller" && configs[id]?.optInt("protocol") == 3 && configs[id]?.optJSONObject("inventory")?.optString("state") == "ready"
    private fun exactAgent(id: String, captured: JSONObject): JSONObject? {
        val rows = inventory()[id] ?: return null
        val target = WorkspaceBinding.fromAgent(id, captured)
        return (0 until rows.length()).map { rows.getJSONObject(it) }.find { WorkspaceBinding.fromAgent(id, it) == target }
    }
    private fun managedWrite(id: String, captured: JSONObject, type: String, payload: JSONObject, done: ManagementReply) {
        val agent = exactAgent(id, captured)
        val key = "delivery:" + WorkspaceBinding.fromAgent(id, captured).storageKey()
        if (agent == null || !managementReady(id) || !PrimarySessionBinding.operationWritable(agent, type) || key in pendingMutations || vault.read(key) != null) { done(null, "当前不可修改：请检查连接、控制者权限、会话身份和待核对的操作"); return }
        val command = AgentProtocol.command(id, agent, type)
        payload.keys().forEach { command.put(it, payload.get(it)) }
        FileReadScope.apply(command, unrestrictedFiles(relay))
        vault.write(key, "unconfirmed:$type"); pendingMutations += key; updateSendState()
        engine.request(id, command, 25000) { data, error ->
            pendingMutations -= key
            if (!AgentManagement.unknown(data, error)) vault.remove(key)
            updateSendState(); done(data, error)
            if (error == null) engine.send(id, JSONObject().put("type", "refresh_agents"))
        }
    }
    private fun renameAgent(id: String, captured: JSONObject) {
        if (!managementReady(id)) { toast("重命名需要已连接的控制者设备"); return }
        AgentManagementDialogs.rename(context, name(captured)) { payload, done -> managedWrite(id, captured, "agent_rename", payload, done) }
    }
    private fun modelSettings(id: String, captured: JSONObject) {
        if (captured.optString("agent") != "codex") { toast("当前模型设置先支持 Codex，其他 Agent 尚未接入"); return }
        if (configs[id]?.optJSONArray("capabilities")?.let { list -> (0 until list.length()).any { list.optString(it) == "agent_model_settings_v1" } } != true) { toast("请先更新电脑 Relay，当前版本未提供模型设置接口"); return }
        val key = "delivery:" + WorkspaceBinding.fromAgent(id, captured).storageKey()
        AgentManagementDialogs.settings(context, role(id) == "controller", { done ->
            val agent = exactAgent(id, captured)
            if (agent == null || id !in online) done(null, "Agent 已断开或会话已变化")
            else engine.request(id, AgentProtocol.command(id, agent, "get_agent_settings"), 20000) { data, error ->
                if (error == null && data?.optBoolean("available") == true && vault.read(key) == "unconfirmed:set_agent_settings" && key !in pendingMutations) { vault.remove(key); updateSendState() }
                done(data, error)
            }
        }, { payload, done -> managedWrite(id, captured, "set_agent_settings", payload, done) })
    }
    private fun workspaceMetadata(id: String, workspaceId: String, server: String): JSONObject? {
        val rows = workspaces[id] ?: return null
        return (0 until rows.length()).map { rows.getJSONObject(it) }.find { it.optString("workspace_id") == workspaceId && AgentPresentation.server(it) == server }?.let { JSONObject(it.toString()) }
    }
    private fun launchInWorkspace(id: String, workspaceId: String, server: String = "primary") {
        if (!managementReady(id)) { toast("新建 Agent 需要已连接、已同步的控制者设备"); return }
        val ws = workspaceMetadata(id, workspaceId, server)
        if (ws == null || server != "primary" || ws.optString("cwd").isBlank()) { toast("工作空间身份或目录不可用，请先刷新"); return }
        val profiles = configs[id]?.optJSONArray("agent_profiles") ?: JSONArray()
        if (profiles.length() == 0) { toast("此电脑尚未提供 Agent 配置"); return }
        val key = "launch:$id:$server:$workspaceId"
        if (vault.read(key) != null) {
            AlertDialog.Builder(context).setTitle("先核对上次创建结果").setMessage("上次在此工作空间创建 Agent 的结果未确认。请先查看列表，避免重复创建。")
                .setPositiveButton("已核对，清除待确认状态") { _, _ -> if (key !in pendingMutations) vault.remove(key) else toast("请求仍在处理中") }.setNegativeButton("返回", null).show(); return
        }
        val launchDefaults = configs[id]?.optJSONObject("agent_launch_defaults")?.let { JSONObject(it.toString()) }
        AgentManagementDialogs.launch(context, ws, profiles, AgentLaunchArguments.supported(configs[id]), launchDefaults) { payload, done ->
            val current = workspaceMetadata(id, workspaceId, server)
            if (!managementReady(id) || current == null || current.optString("cwd") != ws.optString("cwd") || key in pendingMutations || vault.read(key) != null) done(null, "工作空间或连接已变化，请刷新后再创建")
            else if (AgentLaunchArguments.defaultArgs(launchDefaults, payload.optString("profile_id")) != AgentLaunchArguments.defaultArgs(configs[id]?.optJSONObject("agent_launch_defaults"), payload.optString("profile_id"))) done(null, "电脑端默认启动参数已变化，请关闭后重新创建")
            else if (payload.has("agent_args") && !AgentLaunchArguments.supported(configs[id])) done(null, "此电脑 Relay 的启动参数能力已变化，请升级并刷新后重试")
            else {
                vault.write(key, "unconfirmed"); pendingMutations += key
                engine.request(id, payload, 75000) { data, error ->
                    pendingMutations -= key
                    if (!AgentManagement.unknown(data, error)) vault.remove(key)
                    done(data, error); engine.send(id, JSONObject().put("type", "refresh_agents"))
                    if (error == null && !AgentManagement.unknown(data, null)) toast("Agent 已创建在 ${ws.optString("label")}")
                }
            }
        }
    }
    private fun launchAgent() { chooseRelay { id ->
        val rows = workspaces[id] ?: JSONArray()
        val choices = (0 until rows.length()).map { rows.getJSONObject(it) }.filter { AgentPresentation.server(it) == "primary" }
        if (choices.isEmpty()) { toast("没有可用工作空间，请先刷新或在电脑管理中创建工作空间"); return@chooseRelay }
        AlertDialog.Builder(context).setTitle("选择目标工作空间").setItems(choices.map { "${it.optString("label")} [${it.optString("workspace_id")}] · ${it.optString("cwd")}" }.toTypedArray()) { _, index -> launchInWorkspace(id, choices[index].getString("workspace_id")) }.show()
    } }
    private fun chooseRelay(action: (String) -> Unit) { val list = engine.snapshot().optJSONArray("relays") ?: JSONArray(); AlertDialog.Builder(context).setTitle("选择电脑").setItems((0 until list.length()).map { list.getJSONObject(it).optString("label") }.toTypedArray()) { _, i -> action(list.getJSONObject(i).getString("id")) }.show() }
    fun manageRelay(id: String) {
        AlertDialog.Builder(context).setTitle(relayName(id)).setItems(arrayOf("已配对设备", "邀请新设备", "工作区 / Worktree", "创建工作区", "活动记录", "Pi 集成", "检查 relay 更新", "版本与能力")) { _, i -> when (i) {
            0 -> devices(id)
            1 -> DeviceInvitationDialog.show(context) { name, role -> createInvitation(id, name, role) }
            2 -> workspaceManager(id)
            3 -> form("创建工作区", listOf("绝对路径" to "", "名称" to "")) { relayCommand(id, "workspace_create", JSONObject().put("cwd", it[0]).put("label", it[1])) }
            4 -> activity(id)
            5 -> engine.request(id, JSONObject().put("type", "get_pi_integration")) { state, error ->
                if (error != null) toast(error) else AlertDialog.Builder(context).setTitle("Pi 集成").setMessage(state?.toString(2)).setPositiveButton("开启") { _, _ -> relayCommand(id, "set_pi_integration", JSONObject().put("instance", state?.optString("instance")).put("enabled", true), show = true) }.setNeutralButton("关闭") { _, _ -> relayCommand(id, "set_pi_integration", JSONObject().put("instance", state?.optString("instance")).put("enabled", false), show = true) }.setNegativeButton("返回", null).show()
            }
            6 -> relayCommand(id, "check_update", show = true)
            7 -> showText("版本与能力", configs[id]?.toString(2) ?: "尚未连接")
        } }.show()
    }
    private fun relayCommand(id: String, type: String, payload: JSONObject = JSONObject(), show: Boolean = false) {
        if (type !in listOf("device_list", "get_pi_integration", "check_update", "get_activity", "worktree_list") && role(id) != "controller") { toast("此操作需要控制者权限"); return }
        engine.request(id, payload.put("type", type), 75000) { data, error -> if (error != null) toast(error) else if (show) showText("操作结果", data?.toString(2).orEmpty()) else toast("操作已确认") }
    }
    private fun speak(text: String) {
        speech?.stop(); speech?.shutdown()
        speech = android.speech.tts.TextToSpeech(context) { status ->
            if (status != android.speech.tts.TextToSpeech.SUCCESS) toast("本机朗读服务不可用") else {
                speech?.language = java.util.Locale.getDefault()
                speech?.speak(text.take(android.speech.tts.TextToSpeech.getMaxSpeechInputLength()), android.speech.tts.TextToSpeech.QUEUE_FLUSH, null, "herdr-reply")
                AlertDialog.Builder(context).setMessage("正在使用手机语音服务朗读").setPositiveButton("停止") { _, _ -> speech?.stop() }.setNegativeButton("后台朗读", null).show()
            }
        }
    }
    private fun activity(id: String) { pendingActivityRelay = id; engine.send(id, JSONObject().put("type", "get_activity").put("protocol", 3).put("limit", 100)); main.postDelayed({ if (pendingActivityRelay == id) { pendingActivityRelay = null; toast("活动记录尚未返回，请检查连接") } }, 10000) }
    private fun devices(id: String) { engine.request(id, JSONObject().put("type", "device_list")) { data, error ->
        if (error != null) { toast(error); return@request }; val devices = data?.optJSONArray("devices") ?: JSONArray()
        AlertDialog.Builder(context).setTitle("配对设备").setItems((0 until devices.length()).map { val d = devices.getJSONObject(it); "${d.optString("name")} · ${d.optString("role")}" }.toTypedArray()) { _, i ->
            val device = devices.getJSONObject(i)
            AlertDialog.Builder(context).setTitle(device.optString("name")).setItems(arrayOf("重命名", "撤销授权")) { _, choice ->
                if (choice == 0) form("设备名称", listOf("名称" to device.optString("name"))) { relayCommand(id, "rename_device", JSONObject().put("device_id", device.getString("device_id")).put("name", it[0])) }
                else AlertDialog.Builder(context).setMessage("撤销此设备的连接权限？").setPositiveButton("撤销") { _, _ -> relayCommand(id, "revoke_device", JSONObject().put("device_id", device.getString("device_id"))) }.setNegativeButton("取消", null).show()
            }.show()
        }.show()
    } }
    private fun createInvitation(id: String, name: String, role: String) {
        if (this.role(id) != "controller") { toast("需要控制者权限"); return }
        engine.request(id, JSONObject().put("type", "create_device_invitation").put("name", name).put("role", role)) { data, error ->
            if (error != null) { toast(error); return@request }
            runCatching {
                val invite = data!!.getJSONObject("invitation"); val list = engine.snapshot().getJSONArray("relays"); val config = (0 until list.length()).map { list.getJSONObject(it) }.first { it.getString("id") == id }
                val parameters = linkedMapOf("setup" to invite.getString("secret"), "invite" to invite.getString("invitation_id"), "invite_version" to invite.getLong("version").toString(), "invite_expires" to java.time.Instant.parse(invite.getString("expires_at")).toEpochMilli().toString(), "label" to config.optString("label"))
                if (config.optString("url").isNotBlank()) parameters["relay"] = config.getString("url") else { val gateways = config.optJSONArray("gatewayUrls") ?: JSONArray().put(config.optString("gatewayUrl")); parameters["gateways"] = (0 until gateways.length()).joinToString(",") { gateways.getString(it) }; parameters["relay_id"] = config.getString("gatewayRelayId"); parameters["rendezvous"] = config.getString("rendezvousKey") }
                val nativeGateway = config.optString("gatewayUrl").takeIf { it.startsWith("tcp://") }
                if (nativeGateway != null) parameters.remove("gateways")
                val base = if (nativeGateway != null) "herdr-tcp://" + java.net.URI(nativeGateway).rawAuthority + "/#" else "https://herdr-pocket.invalid/#"
                val link = base + parameters.entries.joinToString("&") { java.net.URLEncoder.encode(it.key, "UTF-8") + "=" + java.net.URLEncoder.encode(it.value, "UTF-8") }
                showText("一次性邀请（请勿公开）", link)
            }.onFailure { toast("邀请格式异常，请重试") }
        }
    }
    private fun workspaceManager(id: String) {
        val rows = workspaces[id] ?: JSONArray()
        AlertDialog.Builder(context).setTitle("工作区").setItems((0 until rows.length()).map { rows.getJSONObject(it).optString("label") + " · " + rows.getJSONObject(it).optString("cwd") }.toTypedArray()) { _, i ->
            val ws = rows.getJSONObject(i); val wsId = ws.getString("workspace_id")
            AlertDialog.Builder(context).setTitle(ws.optString("label")).setItems(arrayOf("重命名", "关闭工作区", "列出 Worktree", "创建 Worktree", "移动顺序", "移除此 Worktree", "在此新建 Agent")) { _, choice -> when (choice) {
                6 -> launchInWorkspace(id, wsId, AgentPresentation.server(ws))
                0 -> form("工作区名称", listOf("名称" to ws.optString("label"))) { relayCommand(id, "workspace_rename", JSONObject().put("workspace_id", wsId).put("label", it[0])) }
                1 -> AlertDialog.Builder(context).setMessage("关闭工作区和其中的会话？").setPositiveButton("关闭") { _, _ -> relayCommand(id, "workspace_close", JSONObject().put("workspace_id", wsId)) }.setNegativeButton("取消", null).show()
                2 -> engine.request(id, JSONObject().put("type", "worktree_list").put("workspace_id", wsId)) { data, e -> if (e != null) toast(e) else {
                    val trees = data?.optJSONArray("worktrees") ?: JSONArray()
                    AlertDialog.Builder(context).setTitle("Worktree").setItems((0 until trees.length()).map { trees.getJSONObject(it).optString("path") }.toTypedArray()) { _, n -> relayCommand(id, "worktree_open", JSONObject().put("workspace_id", wsId).put("path", trees.getJSONObject(n).getString("path"))) }.show()
                } }
                3 -> form("创建 Worktree", listOf("分支" to "", "基础分支（可空）" to "", "名称（可空）" to "")) { relayCommand(id, "worktree_create", JSONObject().put("workspace_id", wsId).put("branch", it[0]).put("base", it[1]).put("label", it[2])) }
                4 -> form("工作区排序", listOf("序号（从 0 起）" to "0")) { values -> values[0].toIntOrNull()?.let { relayCommand(id, "workspace_reorder", JSONObject().put("workspace_id", wsId).put("insert_index", it)) } }
                5 -> AlertDialog.Builder(context).setTitle("移除此 Worktree？").setMessage("目标：${ws.optString("cwd")}\n此操作会删除对应检出目录。存在未提交更改时由服务器拒绝；不会强制删除。")
                    .setPositiveButton("移除") { _, _ -> relayCommand(id, "worktree_remove", JSONObject().put("workspace_id", wsId).put("force", false)) }.setNegativeButton("取消", null).show()
            } }.show()
        }.show()
    }
    fun upload(uris: List<Uri>) {
        if (uploading || !writable() || uris.isEmpty()) return
        if (selected?.optString("agent") in listOf("pi", "omp") || frame.optBoolean("no_echo")) { toast("此输入方式暂不支持附件，请切换到支持附件的 Agent"); return }
        if (uris.size + attachments.length() > 8) { toast("每次最多 8 个附件"); return }
        uploading = true; updateSendState(); updateAttachments(); composerPanel?.uploadStatus("正在读取附件…"); val target = selected?.let { WorkspaceBinding.fromAgent(relay, it) } ?: return
        io.execute {
            val files = runCatching { uris.map { uri ->
                val name = context.contentResolver.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME), null, null, null)?.use { if (it.moveToFirst()) it.getString(0) else "attachment" } ?: "attachment"
                val bytes = context.contentResolver.openInputStream(uri)!!.use { stream -> val output = java.io.ByteArrayOutputStream(); val buffer = ByteArray(65536); while (true) { val count = stream.read(buffer); if (count < 0) break; require(output.size() + count <= 20 * 1024 * 1024) { "单个附件最大 20 MiB" }; output.write(buffer, 0, count) }; output.toByteArray() }
                require(bytes.isNotEmpty()); Triple(name, context.contentResolver.getType(uri) ?: "application/octet-stream", bytes)
            }.also { require(it.sumOf { file -> file.third.size } <= 50 * 1024 * 1024) { "附件总量最大 50 MiB" } } }
            main.post {
                if (target != selected?.let { WorkspaceBinding.fromAgent(relay, it) } || !active) { uploading = false; return@post }
                if (files.isFailure) { finishUpload(files.exceptionOrNull()?.message ?: "附件无法读取"); return@post }
                uploadFiles(files.getOrThrow(), target)
            }
        }
    }
    private fun uploadFiles(files: List<Triple<String, String, ByteArray>>, target: WorkspaceBinding) {
        val descriptors = JSONArray(files.map { JSONObject().put("name", it.first).put("media_type", it.second).put("bytes", it.third.size) })
        request("upload_begin", JSONObject().put("files", descriptors)) { begin, error ->
            if (error != null || begin == null) { finishUpload(error ?: "上传失败"); return@request }
            uploadId = begin.optString("upload_id"); val chunkSize = begin.optInt("chunk_bytes", 262144).coerceIn(1, 262144)
            val hashes = JSONArray(); fun nextFile(index: Int) {
                if (target != selected?.let { WorkspaceBinding.fromAgent(relay, it) } || !active) { engine.send(target.target.relay, JSONObject().put("type", "upload_cancel").put("protocol", 3).put("target", target.target.json()).put("upload_id", uploadId)); finishUpload("Agent 已切换，上传已取消"); return }
                if (index == files.size) {
                    request("upload_finish", JSONObject().put("upload_id", uploadId).put("files", hashes)) { result, e ->
                        if (e == null) { val refs = result?.optJSONArray("attachments") ?: JSONArray(); for (i in 0 until refs.length()) { val ref = refs.getJSONObject(i); files.getOrNull(i)?.let { file -> if (ref.optString("name").isBlank()) ref.put("name", file.first); ref.put("bytes", file.third.size) }; attachments.put(ref) } }
                        finishUpload(e)
                    }; return
                }
                val file = files[index]; val digest = sha(file.third); hashes.put(JSONObject().put("file_index", index).put("sha256", digest))
                fun chunk(offset: Int, sequence: Int) {
                    if (offset >= file.third.size) { nextFile(index + 1); return }
                    val part = file.third.copyOfRange(offset, minOf(offset + chunkSize, file.third.size))
                    composerPanel?.uploadStatus("上传 ${file.first} · ${offset * 100L / file.third.size}%")
                    request("upload_chunk", JSONObject().put("upload_id", uploadId).put("file_index", index).put("sequence", sequence).put("data", Base64.getEncoder().encodeToString(part)).put("sha256", sha(part))) { result, e ->
                        if (e != null || result?.optInt("next_sequence") != sequence + 1) { finishUpload(e ?: "上传块确认异常"); return@request }
                        if (target != selected?.let { WorkspaceBinding.fromAgent(relay, it) } || !active) nextFile(index) else chunk(offset + part.size, sequence + 1)
                    }
                }; chunk(0, 0)
            }; nextFile(0)
        }
    }
    private fun finishUpload(error: String?) { uploading = false; if (error != null) { if (uploadId.isNotBlank()) selected?.let { engine.send(relay, AgentProtocol.command(relay, it, "upload_cancel").put("upload_id", uploadId)) }; toast(error) }; uploadId = ""; composerPanel?.uploadStatus(null); updateAttachments(); updateSendState() }
    private fun updateAttachments() {
        composerPanel?.attachments((0 until attachments.length()).map { val file = attachments.getJSONObject(it); NativeComposerView.Attachment(file.optString("name", "附件"), file.optLong("bytes")) }, !uploading && !inputPending) { index ->
            if (!uploading && !inputPending) { attachments.remove(index); updateAttachments(); updateSendState() }
        }
    }
    private fun sha(bytes: ByteArray) = MessageDigest.getInstance("SHA-256").digest(bytes).joinToString("") { "%02x".format(it) }
    private fun capability(name: String) = configs[relay]?.optJSONArray("capabilities")?.let { list -> (0 until list.length()).any { list.optString(it) == name } } == true
    private fun relayName(id: String): String { val rows = engine.snapshot().optJSONArray("relays") ?: return id; return (0 until rows.length()).map { rows.getJSONObject(it) }.find { it.optString("id") == id }?.optString("label") ?: id }
    private fun showText(title: String, text: String) { val body = column(12); val query = EditText(context).apply { hint = "查找文本"; setSingleLine() }; val value = label(text, 14, textInk).apply { setTextIsSelectable(true); typeface = Typeface.MONOSPACE }; body.addView(query); body.addView(ScrollView(context).apply { addView(value) }, LinearLayout.LayoutParams(-1, dp(380))); query.addTextChangedListener(simpleWatcher { q -> value.text = if (q.isBlank()) text else text.lines().filter { it.contains(q, true) }.joinToString("\n") }); AlertDialog.Builder(context).setTitle(title).setView(body).setPositiveButton("关闭", null).setNeutralButton("复制全部") { _, _ -> copy(text) }.show() }
    private fun copy(value: String) { (context.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager).setPrimaryClip(ClipData.newPlainText("Herdr", value)); toast("已复制") }
    private fun form(title: String, fields: List<Pair<String, String>>, submit: (List<String>) -> Unit) { val body = column(16); val inputs = fields.map { (hint, value) -> EditText(context).apply { this.hint = hint; setText(value); setTextColor(textInk); body.addView(this) } }; AlertDialog.Builder(context).setTitle(title).setView(ScrollView(context).apply { addView(body) }).setPositiveButton("确定") { _, _ -> submit(inputs.map { it.text.toString().trim() }) }.setNegativeButton("取消", null).show() }
    private fun dp(value: Int) = (value * context.resources.displayMetrics.density).toInt()
    private fun column(padding: Int) = LinearLayout(context).apply { orientation = LinearLayout.VERTICAL; setPadding(dp(padding), dp(padding), dp(padding), dp(padding)) }
    private fun row() = LinearLayout(context).apply { gravity = Gravity.CENTER_VERTICAL }
    private fun weight() = LinearLayout.LayoutParams(0, -2, 1f)
    private fun label(value: String, size: Int, color: Int) = TextView(context).apply { text = value; textSize = size.toFloat(); setTextColor(color); setPadding(0, dp(2), 0, dp(2)) }
    private fun button(value: String, action: () -> Unit) = AgentStyle.button(context, value, action = action)
    private fun toast(value: String) { transientToast?.cancel(); transientToast = Toast.makeText(context, value, Toast.LENGTH_LONG).also { it.show() } }
    private fun simpleWatcher(change: (String) -> Unit) = object : android.text.TextWatcher { override fun beforeTextChanged(s: CharSequence?, start: Int, count: Int, after: Int) {}; override fun onTextChanged(s: CharSequence?, start: Int, before: Int, count: Int) {}; override fun afterTextChanged(s: android.text.Editable?) { change(s.toString()) } }
}

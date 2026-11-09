package dev.herdr.companion

import org.json.JSONObject

/** Session identity survives temporary loss of proof; permission to interact does not. */
object PrimarySessionBinding {
    fun writable(agent: JSONObject?): Boolean = agent != null && !AgentPresentation.terminalOnly(agent) && (agent.optString("agent") != "codex" || agent.optString("binding_state") in setOf("", "verified"))
    private val terminalActions = setOf("send_terminal_text", "send_text", "send_keys", "send_input", "send_filter_text", "send_secret", "lease_pane_size")
    private val lifecycleActions = setOf("agent_stop", "agent_restart", "agent_clear", "agent_rename")
    fun terminalWritable(agent: JSONObject?): Boolean = agent != null && agent.optString("pane_id").isNotBlank() && agent.optString("terminal_id").isNotBlank()
    fun operationWritable(agent: JSONObject?, operation: String, terminalView: Boolean = false): Boolean = when {
        operation in lifecycleActions || terminalView && operation in terminalActions -> terminalWritable(agent)
        else -> writable(agent)
    }
    fun terminalLabel(agent: JSONObject): String = if (agent.optString("agent") == "codex" && !writable(agent))
        "主对话未确认：历史、按对话发送和审批不可用；可直接操作当前终端，也可停止或重启 Agent。" else label(agent)
    fun readable(agent: JSONObject?): Boolean = writable(agent)
    fun live(agent: JSONObject?): Boolean = writable(agent)
    fun label(agent: JSONObject): String {
        if (AgentPresentation.terminalOnly(agent)) return "当前是终端，没有运行中的 Agent；可以直接输入命令。"
        if (agent.optString("agent").let { it.isNotBlank() && it != "codex" }) return ""
        when (agent.optString("binding_reason")) {
            "standalone_runtime_unavailable" -> return "Codex 内嵌/独立运行时的会话写锁不可读，请检查版本与进程权限"
            "standalone_metadata_unavailable" -> return "独立会话元数据尚不可读 · 新会话请在电脑端开始首轮输入后刷新"
            "standalone_ambiguous" -> return "独立 Codex 仍持有多个主会话 · 旧 hook 上报不足以确认，请退出原 TUI 后在独立终端恢复目标会话"
            "standalone_ancestry_unavailable" -> return "独立 Codex 的主子会话关系无法完整确认 · 不会猜选"
            "standalone_metadata_conflict" -> return "独立 Codex 的写锁与会话元数据冲突 · 不会改读其他会话"
            "terminal_title_missing", "terminal_title_invalid" -> return "已配置线程 ID 标题，但尚未读到有效 ID · 等待终端刷新，不按名称猜选"
            "terminal_title_ambiguous" -> return "标题 ID 前缀匹配多个会话 · 暂不能唯一确认"
            "terminal_title_conflict" -> return "标题 ID 与本终端会话不匹配 · 可能正在切换或查看子代理"
            "runtime_unavailable" -> return "终端进程已找到，但会话运行时关联不可用 · 需检查电脑 Relay，不代表启动参数未附加"
            "runtime_changed" -> return "终端或主会话已变化 · 正在重新确认，旧请求已失效"
            "runtime_metadata_conflict", "process_project_conflict" -> return "Codex 运行时与会话信息冲突 · 请检查终端和工作目录"
            "runtime_platform_unsupported" -> return "此平台暂不支持 Codex 终端运行时确认"
        }
        return when (agent.optString("binding_state")) {
            "verified" -> {
                val id = agent.optString("agent_session_id")
                "终端对应的 Codex 主会话已确认" + if (id.matches(Regex("[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}"))) "\nSession ID：$id" else ""
            }
            "verifying", "stale", "ambiguous", "conflict" -> "无法唯一确认主会话 · 可尝试 codex --no-daemon 并检查 Herdr hook，不保证修复；已读内容仅为旧缓存"
            else -> ""
        }
    }
    /** Follow a final ID from Relay inventory; never derive or confirm one locally. */
    fun relayReplacement(relay: String, previous: JSONObject, next: JSONObject): Boolean {
        val before = WorkspaceBinding.fromAgent(relay, previous)
        val after = WorkspaceBinding.fromAgent(relay, next)
        return previous.optString("agent") == "codex" && next.optString("agent") == "codex"
            && next.optString("binding_state").isNotBlank()
            && before.target.session != after.target.session && after.target.generation > before.target.generation
            && before.cwd == after.cwd && before.target.server == after.target.server
            && before.target.pane == after.target.pane && before.target.terminal.isNotBlank()
            && before.target.terminal == after.target.terminal
    }
    fun terminalTransition(relay: String, previous: JSONObject, next: JSONObject): Boolean {
        val before = WorkspaceBinding.fromAgent(relay, previous)
        val after = WorkspaceBinding.fromAgent(relay, next)
        return AgentPresentation.terminalOnly(previous) != AgentPresentation.terminalOnly(next)
            && after.target.generation > before.target.generation
            && before.target.server == after.target.server && before.target.pane == after.target.pane
            && before.target.terminal.isNotBlank() && before.target.terminal == after.target.terminal
            && previous.optString("workspace_id") == next.optString("workspace_id")
            && previous.optString("tab_id") == next.optString("tab_id")
    }
    fun sameIdentity(relay: String, previous: JSONObject, next: JSONObject): Boolean =
        WorkspaceBinding.fromAgent(relay, previous).identity() == WorkspaceBinding.fromAgent(relay, next).identity()
}

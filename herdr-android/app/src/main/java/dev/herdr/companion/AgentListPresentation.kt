package dev.herdr.companion

import org.json.JSONArray
import org.json.JSONObject

internal object AgentListPresentation {
    data class Workspace(val group: AgentPresentation.Group, val agents: List<JSONObject>)
    data class Computer(val id: String, val label: String, val workspaces: List<Workspace>,
        val totalWorkspaces: Int, val activity: Activity) {
        val totalAgents get() = activity.agents
        val matchingAgents get() = workspaces.sumOf { it.agents.count { agent -> !AgentPresentation.terminalOnly(agent) } }
        val matchingTerminals get() = workspaces.sumOf { it.agents.count(AgentPresentation::terminalOnly) }
    }

    data class Activity(val agents: Int, val terminals: Int, val running: Int, val waiting: Int, val unknown: Int) {
        fun summary(online: Boolean, ready: Boolean): String {
            if (!online || !ready) return "运行数待确认"
            return "运行中 $running" + (if (waiting > 0) " · 待处理 $waiting" else "") +
                (if (unknown > 0) " · 待确认 $unknown" else "")
        }
    }

    // Count the full Relay inventory, never the search result or terminal output.
    fun activity(rows: List<JSONObject>): Activity {
        var agents = 0; var terminals = 0; var running = 0; var waiting = 0; var unknown = 0
        for (agent in rows) {
            if (AgentPresentation.terminalOnly(agent)) { terminals++; continue }
            agents++
            if (agent.optString("agent") == "codex" && agent.optString("binding_state") !in setOf("", "verified")) {
                unknown++; continue
            }
            when (agent.optString("status").trim().lowercase()) {
                "working", "running", "busy" -> running++
                "blocked", "waiting", "needs_input" -> waiting++
                "idle", "ready", "done", "completed", "finished", "interrupted", "failed", "error", "stopped", "exited", "ended" -> Unit
                else -> unknown++
            }
        }
        return Activity(agents, terminals, running, waiting, unknown)
    }

    fun computers(relays: JSONArray, inventory: Map<String, JSONArray>, workspaces: Map<String, JSONArray>, search: String): List<Computer> {
        val groups = AgentPresentation.groups(inventory, workspaces).groupBy { it.relay }
        val labels = linkedMapOf<String, String>()
        for (index in 0 until relays.length()) {
            val relay = relays.optJSONObject(index) ?: continue
            val id = relay.optString("id")
            if (id.isNotBlank()) labels[id] = relay.optString("label").ifBlank { "电脑" }
        }
        for (id in groups.keys) labels.putIfAbsent(id, "电脑")
        val query = search.trim()
        return labels.mapNotNull { (id, label) ->
            val all = groups[id].orEmpty()
            val deviceMatches = label.contains(query, true)
            val visible = all.mapNotNull { group ->
                val workspaceMatches = query.isEmpty() || deviceMatches || group.title.contains(query, true) ||
                    group.path.contains(query, true) || group.server.contains(query, true)
                val agents = if (workspaceMatches) group.agents else group.agents.filter { it.toString().contains(query, true) }
                if (workspaceMatches || agents.isNotEmpty()) Workspace(group, agents) else null
            }
            if (query.isNotEmpty() && !deviceMatches && visible.isEmpty()) null
            else Computer(id, label, visible, all.size, activity(all.flatMap { it.agents }))
        }
    }
}

package dev.herdr.companion

import org.json.JSONArray
import org.json.JSONObject

object AgentPresentation {
    fun terminalOnly(agent: JSONObject): Boolean = agent.optBoolean("terminal_only")
    fun provider(agent: JSONObject): String = if (terminalOnly(agent)) "终端" else agent.optString("agent").ifBlank { "Agent" }
    data class Group(val relay: String, val server: String, val id: String, val title: String, val path: String, val number: Int, val agents: List<JSONObject>)
    fun title(agent: JSONObject): String = agent.optString("tab_label").ifBlank { agent.optString("tab_name") }
        .ifBlank { agent.optString("name") }.ifBlank { provider(agent) }
    fun server(agent: JSONObject) = agent.optString("server_session_id").ifBlank { "primary" }
    fun workspaceTitle(relay: String, agent: JSONObject, workspaces: Map<String, JSONArray>): String =
        workspaceTitle(agent, workspaceMetadata(relay, agent, workspaces))
    private fun workspaceMetadata(relay: String, agent: JSONObject, workspaces: Map<String, JSONArray>): JSONObject? {
        val id = agent.optString("workspace_id")
        if (id.isBlank()) return null
        val rows = workspaces[relay] ?: return null
        val server = server(agent)
        for (i in 0 until rows.length()) {
            val ws = rows.optJSONObject(i) ?: continue
            if (server(ws) == server && ws.optString("workspace_id") == id) return ws
        }
        return null
    }
    private fun workspacePath(agent: JSONObject, metadata: JSONObject?): String =
        metadata?.optString("cwd").orEmpty().ifBlank { agent.optString("cwd") }
    private fun workspaceTitle(agent: JSONObject, metadata: JSONObject?): String =
        metadata?.optString("label").orEmpty().ifBlank { agent.optString("workspace_name") }
            .ifBlank { workspacePath(agent, metadata).trimEnd('/').substringAfterLast('/').ifBlank { "未命名工作空间" } }
    fun groups(inventory: Map<String, JSONArray>, workspaces: Map<String, JSONArray>): List<Group> {
        val result = mutableListOf<Group>()
        for ((relay, entries) in inventory) {
            val agents = (0 until entries.length()).mapNotNull { entries.optJSONObject(it) }
            for ((key, members) in agents.groupBy { server(it) to it.optString("workspace_id").ifBlank { "cwd:" + it.optString("cwd") } }) {
                val metadata = workspaceMetadata(relay, members.first(), workspaces)
                val path = workspacePath(members.first(), metadata)
                val title = workspaceTitle(members.first(), metadata)
                val sorted = members.sortedWith(compareBy<JSONObject> { it.optInt("tab_order", it.optInt("tab_number", Int.MAX_VALUE)) }
                    .thenBy { it.optString("tab_id") }.thenBy { it.optString("pane_id") })
                result += Group(relay, key.first, key.second, title, path, metadata?.optInt("number", Int.MAX_VALUE) ?: Int.MAX_VALUE, sorted)
            }
        }
        for ((relay, entries) in workspaces) for (i in 0 until entries.length()) {
            val ws = entries.optJSONObject(i) ?: continue
            val id = ws.optString("workspace_id"); val server = server(ws)
            if (id.isBlank() || result.any { it.relay == relay && it.server == server && it.id == id }) continue
            val path = ws.optString("cwd")
            result += Group(relay, server, id, ws.optString("label").ifBlank { path.trimEnd('/').substringAfterLast('/').ifBlank { "未命名工作空间" } }, path, ws.optInt("number", Int.MAX_VALUE), emptyList())
        }
        return result.sortedWith(compareBy<Group> { it.relay }.thenBy { it.server }.thenBy { it.number }.thenBy { it.title }.thenBy { it.id })
    }
}

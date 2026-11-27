package dev.herdr.companion

import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test

class AgentManagementTest {
    private fun workspace(id: String) = JSONObject().put("workspace_id", id).put("cwd", "/work/shared").put("label", "同名空间")
    @Test fun launchAlwaysBindsChosenWorkspaceEvenWithIdenticalPaths() {
        val first = AgentManagement.launchPayload(workspace("one"), "codex", "writer-2", "任务")
        val second = AgentManagement.launchPayload(workspace("two"), "codex", "writer-2", "任务")
        assertEquals("one", first.getString("workspace_id")); assertEquals("two", second.getString("workspace_id"))
        assertEquals(first.getString("cwd"), second.getString("cwd")); assertEquals("primary", second.getString("server_session_id"))
    }
    @Test fun launchRefusesMissingIdentityOtherServerAndInvalidNameBeforeDispatch() {
        for (bad in listOf(workspace(""), workspace("cwd:/work/shared"), workspace("one").put("cwd", "relative"), workspace("one").put("server_session_id", "other"))) {
            assertThrows(IllegalArgumentException::class.java) { AgentManagement.launchPayload(bad, "codex", "writer-2", "") }
        }
        for (name in listOf("", "中文名称", "Writer", "-flag", "bad name", "x".repeat(33))) assertThrows(IllegalArgumentException::class.java) { AgentManagement.launchPayload(workspace("one"), "codex", name, "") }
    }
    @Test fun remoteProjectPathsUseTheRelaysSyntaxAndAreSentUnchanged() {
        fun launch(path: String) = AgentManagement.launchPayload(workspace("project").put("cwd", path), "codex", "test", "")
        for (path in listOf("D:\\platform", "d:/项目 A", "/mnt/projects", "/Volumes/Projects")) {
            assertEquals(path, launch(path).getString("cwd"))
        }
        for (path in listOf("D:platform", "\\platform", "\\\\server\\share", "//server/share", "D:\\a\nb")) {
            assertThrows(IllegalArgumentException::class.java) { launch(path) }
        }
    }
    @Test fun renameAllowsChineseTabLabelsButRejectsControls() {
        assertEquals("论文调研", AgentManagement.renamePayload(" 论文调研 ").getString("name"))
        for (name in listOf("", "a\nb", "a\u001bb", "x".repeat(129))) assertThrows(IllegalArgumentException::class.java) { AgentManagement.renamePayload(name) }
    }
    @Test fun emptyWorkspacesRemainVisibleAndSeparateAcrossComputersAndServers() {
        val rows = JSONArray().put(workspace("one")).put(workspace("one").put("server_session_id", "other")).put(workspace("two"))
        val groups = AgentPresentation.groups(emptyMap(), mapOf("a" to rows, "b" to rows))
        assertEquals(6, groups.size); assertTrue(groups.all { it.agents.isEmpty() })
    }
    @Test fun modelAndEffortComeFromCatalogAndUseOptimisticRevision() {
        val settings = settingsFixture()
        val payload = AgentManagement.settingsPayload(settings, "runtime-model-b", "maximum-custom")
        assertEquals("revision-a", payload.getString("settings_revision")); assertEquals(3, payload.length())
        assertThrows(IllegalArgumentException::class.java) { AgentManagement.settingsPayload(settings, "invented", "high") }
        assertThrows(IllegalArgumentException::class.java) { AgentManagement.settingsPayload(settings, "runtime-model-b", "medium") }
        assertThrows(IllegalArgumentException::class.java) { AgentManagement.settingsPayload(JSONObject(settings.toString()).put("available", false), "runtime-model-b", "high") }
    }
    companion object {
        fun settingsFixture(): JSONObject = JSONObject().put("available", true).put("model", "runtime-model-a").put("effort", "medium").put("settings_revision", "revision-a").put("next_turn", true)
            .put("models", JSONArray().put(model("runtime-model-a", "Model A", "medium", "medium", "high")).put(model("runtime-model-b", "Model B", "high", "high", "maximum-custom")))
        private fun model(id: String, label: String, default: String, vararg efforts: String) = JSONObject().put("model", id).put("displayName", label).put("defaultReasoningEffort", default)
            .put("supportedReasoningEfforts", JSONArray(efforts.map { JSONObject().put("reasoningEffort", it).put("description", "runtime value") }))
    }
}

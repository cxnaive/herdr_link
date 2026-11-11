package dev.herdr.companion

import android.app.AlertDialog
import android.content.Context
import android.view.View
import android.widget.*
import org.json.JSONArray
import org.json.JSONObject

typealias ManagementReply = (JSONObject?, String?) -> Unit

/** Management never types slash commands into a busy terminal. */
object AgentManagementDialogs {
    private val ink get() = AgentStyle.ink
    private fun body(context: Context) = LinearLayout(context).apply { orientation = LinearLayout.VERTICAL; val p = (20 * resources.displayMetrics.density).toInt(); setPadding(p, p / 2, p, p / 2) }
    private fun text(context: Context, value: String) = TextView(context).apply { text = value; textSize = 14f; setTextColor(ink); setPadding(0, 10, 0, 10) }
    private fun input(context: Context, hint: String, value: String = "") = EditText(context).apply { this.hint = hint; setText(value); setTextColor(ink); setSingleLine() }
    private fun choices(context: Context, spinner: Spinner, values: List<String>) { spinner.adapter = ArrayAdapter(context, android.R.layout.simple_spinner_dropdown_item, values) }
    fun rename(context: Context, currentName: String, submit: (JSONObject, ManagementReply) -> Unit): AlertDialog {
        val body = body(context); val name = input(context, "Agent / Tab 名称", currentName)
        body.addView(text(context, "修改 Herdr Tab 名称，不重启 Agent。同一 Tab 的分屏共享此名称。")); body.addView(name)
        val status = text(context, ""); body.addView(status)
        val dialog = AlertDialog.Builder(context).setTitle("重命名 Agent").setView(body).setPositiveButton("保存", null).setNegativeButton("取消", null).create()
        dialog.setOnShowListener { dialog.getButton(AlertDialog.BUTTON_POSITIVE).setOnClickListener {
            if (!dialog.getButton(AlertDialog.BUTTON_POSITIVE).isEnabled) return@setOnClickListener
            val payload = try { AgentManagement.renamePayload(name.text.toString()) } catch (e: IllegalArgumentException) { name.error = e.message; return@setOnClickListener }
            name.error = null; name.isEnabled = false
            dialog.getButton(AlertDialog.BUTTON_NEGATIVE).text = "关闭"
            val save = dialog.getButton(AlertDialog.BUTTON_POSITIVE); save.isEnabled = false; status.text = "正在保存…"
            submit(payload) { _, error -> if (!dialog.isShowing) return@submit
                if (error == null) dialog.dismiss() else { status.text = error; save.isEnabled = !AgentManagement.unknown(null, error); name.isEnabled = save.isEnabled }
            }
        } }; dialog.show(); return dialog
    }
    fun launch(context: Context, workspace: JSONObject, profiles: JSONArray, launchArgsSupported: Boolean = false, launchDefaults: JSONObject? = null, submit: (JSONObject, ManagementReply) -> Unit): AlertDialog {
        val body = body(context)
        body.addView(text(context, "工作空间：${workspace.optString("label")} [${workspace.optString("workspace_id")}]\n${workspace.optString("cwd")}"))
        val profile = Spinner(context).apply { contentDescription = "Agent 类型" }
        val options = (0 until profiles.length()).map { profiles.getJSONObject(it) }
        choices(context, profile, options.map { it.optString("label").ifBlank { it.optString("id") } }); body.addView(profile)
        val name = input(context, "名称，例如 writer-2")
        val arguments = input(context, "例如 --no-daemon").apply {
            contentDescription = "启动参数"
            inputType = android.text.InputType.TYPE_CLASS_TEXT or android.text.InputType.TYPE_TEXT_VARIATION_VISIBLE_PASSWORD or android.text.InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS
            isEnabled = launchArgsSupported
        }
        val prompt = input(context, "初始提示（可留空）").apply { setSingleLine(false); minLines = 3; maxLines = 6; gravity = android.view.Gravity.TOP }
        body.addView(name)
        val defaultsLabel = text(context, "").apply { contentDescription = "电脑端默认启动参数" }
        body.addView(defaultsLabel)
        fun showDefaults(position: Int) {
            defaultsLabel.text = AgentLaunchArguments.defaultLabel(launchDefaults, options.getOrNull(position)?.optString("id").orEmpty())
            defaultsLabel.visibility = if (defaultsLabel.text.isEmpty()) View.GONE else View.VISIBLE
        }
        profile.onItemSelectedListener = object : AdapterView.OnItemSelectedListener {
            override fun onItemSelected(parent: AdapterView<*>?, view: View?, position: Int, id: Long) { showDefaults(position) }
            override fun onNothingSelected(parent: AdapterView<*>?) { showDefaults(-1) }
        }
        showDefaults(profile.selectedItemPosition)
        body.addView(text(context, "额外启动参数（可留空）")); body.addView(arguments)
        body.addView(text(context, if (launchArgsSupported) "只填参数，不填命令名。含空格的值用引号；重启此 Agent 时保留。" else "此电脑 Relay 尚不支持启动参数，请先升级 Relay。"))
        body.addView(prompt)
        val status = text(context, "只会在上述工作空间创建，不改变现有 Agent。名称以小写字母开头，可含数字、_ 和 -。"); body.addView(status)
        val dialog = AlertDialog.Builder(context).setTitle("在此工作空间新建 Agent").setView(ScrollView(context).apply { addView(body) }).setPositiveButton("创建", null).setNegativeButton("取消", null).create()
        dialog.setOnShowListener { dialog.getButton(AlertDialog.BUTTON_POSITIVE).setOnClickListener {
            if (!dialog.getButton(AlertDialog.BUTTON_POSITIVE).isEnabled) return@setOnClickListener
            val payload = try { AgentManagement.launchPayload(workspace, options.getOrNull(profile.selectedItemPosition)?.optString("id").orEmpty(), name.text.toString().trim(), prompt.text.toString(), arguments.text.toString(), launchArgsSupported) }
                catch (e: IllegalArgumentException) { status.text = e.message; return@setOnClickListener }
            name.isEnabled = false; arguments.isEnabled = false; prompt.isEnabled = false; profile.isEnabled = false; dialog.getButton(AlertDialog.BUTTON_NEGATIVE).text = "关闭"
            val create = dialog.getButton(AlertDialog.BUTTON_POSITIVE); create.isEnabled = false; status.text = "正在创建 Agent，请勿重复提交…"
            submit(payload) { data, error -> if (!dialog.isShowing) return@submit
                if (error == null && !AgentManagement.unknown(data, null)) dialog.dismiss()
                else { status.text = error ?: "创建结果未确认，请刷新工作空间核对后再操作。"; create.isEnabled = !AgentManagement.unknown(data, error); name.isEnabled = create.isEnabled; arguments.isEnabled = create.isEnabled && launchArgsSupported; prompt.isEnabled = create.isEnabled; profile.isEnabled = create.isEnabled }
            }
        } }; dialog.show(); return dialog
    }
    fun settings(context: Context, writable: Boolean, read: (ManagementReply) -> Unit, write: (JSONObject, ManagementReply) -> Unit): AlertDialog {
        val body = body(context)
        body.addView(text(context, "仅影响此 Codex 会话的后续轮次，不中断当前任务，不修改全局默认设置。"))
        val current = text(context, "正在读取运行时设置…"); body.addView(current)
        val progress = ProgressBar(context); body.addView(progress)
        body.addView(text(context, "模型")); val model = Spinner(context).apply { contentDescription = "会话模型" }; body.addView(model)
        body.addView(text(context, "思考强度")); val effort = Spinner(context).apply { contentDescription = "思考强度" }; body.addView(effort)
        val status = text(context, if (writable) "" else "当前设备为只读，不能修改设置。"); body.addView(status)
        val dialog = AlertDialog.Builder(context).setTitle("模型 / 思考强度").setView(ScrollView(context).apply { addView(body) }).setPositiveButton("应用到下一轮", null).setNeutralButton("刷新", null).setNegativeButton("关闭", null).create()
        var settings: JSONObject? = null; var models = emptyList<JSONObject>(); var efforts = emptyList<JSONObject>(); var loading = false; var loadEpoch = 0; var mustRefresh = true
        fun selectEfforts(index: Int) {
            val option = models.getOrNull(index); val list = option?.optJSONArray("supportedReasoningEfforts") ?: JSONArray()
            efforts = (0 until list.length()).map { list.getJSONObject(it) }
            choices(context, effort, efforts.map { it.getString("reasoningEffort") })
            val selected = if (option?.optString("model") == settings?.optString("model")) settings?.optString("effort") else option?.optString("defaultReasoningEffort")
            val pos = efforts.indexOfFirst { it.optString("reasoningEffort") == selected }; if (pos >= 0) effort.setSelection(pos)
            dialog.getButton(AlertDialog.BUTTON_POSITIVE)?.isEnabled = writable && !loading && !mustRefresh && settings?.optBoolean("available") == true && option != null && efforts.isNotEmpty()
        }
        model.onItemSelectedListener = object : android.widget.AdapterView.OnItemSelectedListener {
            override fun onItemSelected(parent: android.widget.AdapterView<*>?, view: View?, position: Int, id: Long) { selectEfforts(position) }
            override fun onNothingSelected(parent: android.widget.AdapterView<*>?) { selectEfforts(-1) }
        }
        fun load() {
            val owner = ++loadEpoch; loading = true; settings = null; progress.visibility = View.VISIBLE
            model.isEnabled = false; effort.isEnabled = false; dialog.getButton(AlertDialog.BUTTON_POSITIVE).isEnabled = false; dialog.getButton(AlertDialog.BUTTON_NEUTRAL).isEnabled = false
            read { data, error -> if (!dialog.isShowing || owner != loadEpoch) return@read
                loading = false; progress.visibility = View.GONE; dialog.getButton(AlertDialog.BUTTON_NEUTRAL).isEnabled = true
                if (error != null || data?.optBoolean("available") != true) { current.text = error ?: data?.optString("reason").orEmpty().ifBlank { "此 Agent 暂不支持运行时模型设置" }; return@read }
                settings = data; mustRefresh = false; status.text = if (writable) "已从当前会话运行时读取。" else "当前设备为只读，不能修改设置。"; current.text = "当前：${data.optString("model")} · ${data.optString("effort").ifBlank { "默认" }}"
                val options = data.optJSONArray("models") ?: JSONArray(); models = (0 until options.length()).map { options.getJSONObject(it) }
                val index = models.indexOfFirst { it.optString("model") == data.optString("model") }
                if (index < 0) models = listOf(JSONObject().put("displayName", "请选择模型（当前型号不在列表）")) + models
                choices(context, model, models.map { it.optString("displayName").ifBlank { it.optString("model") } }); model.setSelection(index.coerceAtLeast(0)); selectEfforts(index.coerceAtLeast(0))
                model.isEnabled = writable; effort.isEnabled = writable
            }
        }
        dialog.setOnShowListener {
            dialog.getButton(AlertDialog.BUTTON_NEUTRAL).setOnClickListener { load() }
            dialog.getButton(AlertDialog.BUTTON_POSITIVE).setOnClickListener {
                if (!dialog.getButton(AlertDialog.BUTTON_POSITIVE).isEnabled) return@setOnClickListener
                val payload = try { AgentManagement.settingsPayload(requireNotNull(settings), models.getOrNull(model.selectedItemPosition)?.optString("model").orEmpty(), efforts.getOrNull(effort.selectedItemPosition)?.optString("reasoningEffort").orEmpty()) }
                    catch (e: IllegalArgumentException) { status.text = e.message; return@setOnClickListener }
                loading = true; mustRefresh = true; progress.visibility = View.VISIBLE; dialog.getButton(AlertDialog.BUTTON_POSITIVE).isEnabled = false; dialog.getButton(AlertDialog.BUTTON_NEUTRAL).isEnabled = false; model.isEnabled = false; effort.isEnabled = false
                status.text = "正在更新并读取回执…"
                write(payload) { data, error -> if (!dialog.isShowing) return@write
                    loading = false; progress.visibility = View.GONE; dialog.getButton(AlertDialog.BUTTON_NEUTRAL).isEnabled = true
                    if (error == null && data?.optString("model") == payload.getString("model") && data.optString("effort") == payload.getString("effort")) {
                        current.text = "当前：${data.optString("model")} · ${data.optString("effort")}"; status.text = "已确认，下一轮生效。"; settings = data
                    } else status.text = error ?: "未取得一致的设置回执，请刷新核对。"
                    // Require an explicit refresh before any subsequent write, especially after timeout.
                }
            }; load()
        }; dialog.show(); return dialog
    }
}

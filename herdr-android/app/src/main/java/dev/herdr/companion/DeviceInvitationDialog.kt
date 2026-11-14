package dev.herdr.companion

import android.app.AlertDialog
import android.content.Context
import android.view.View
import android.view.ViewGroup
import android.widget.ArrayAdapter
import android.widget.EditText
import android.widget.LinearLayout
import android.widget.ScrollView
import android.widget.Spinner
import android.widget.TextView

/** The role belongs to the new invitation, never to the inviting device. */
internal object DeviceInvitationDialog {
    fun show(context: Context, submit: (name: String, role: String) -> Unit): AlertDialog {
        val padding = AgentStyle.dp(context, 20)
        val body = LinearLayout(context).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(padding, padding / 2, padding, padding / 2)
        }
        fun label(value: String) = TextView(context).apply {
            text = value; textSize = 14f; setTextColor(AgentStyle.muted)
            setPadding(0, padding / 2, 0, padding / 2)
        }
        val name = EditText(context).apply {
            id = View.generateViewId(); contentDescription = "设备名称"
            hint = "例如：我的平板"; setSingleLine(); setText("Android")
            setTextColor(AgentStyle.ink); setHintTextColor(AgentStyle.muted)
            minimumHeight = AgentStyle.dp(context, 48)
        }
        body.addView(label("设备名称").apply { labelFor = name.id })
        body.addView(name)
        val roles = listOf("reader", "controller")
        val role = Spinner(context, Spinner.MODE_DROPDOWN).apply {
            id = View.generateViewId(); contentDescription = "邀请权限"
            val ink = AgentStyle.ink
            adapter = object : ArrayAdapter<String>(context, android.R.layout.simple_spinner_dropdown_item,
                listOf("只读（reader）", "控制者（controller）")) {
                private fun styled(view: View) = (view as TextView).apply {
                    setTextColor(ink); textSize = 14f; minHeight = AgentStyle.dp(context, 48)
                }
                override fun getView(position: Int, convertView: View?, parent: ViewGroup) = styled(super.getView(position, convertView, parent))
                override fun getDropDownView(position: Int, convertView: View?, parent: ViewGroup) = styled(super.getDropDownView(position, convertView, parent))
            }
            minimumHeight = AgentStyle.dp(context, 48)
            setSelection(0)
        }
        body.addView(label("邀请权限").apply { labelFor = role.id })
        body.addView(role, LinearLayout.LayoutParams(-1, -2))
        body.addView(label("只读：查看对话、终端和允许访问的文件。\n控制者：还可发送任务、操作终端、管理 Agent 和配对设备。"))
        body.addView(label("仅设置新邀请的权限，不改变本设备权限。请只将一次性邀请分享给目标设备。"))
        val dialog = AlertDialog.Builder(context).setTitle("邀请设备")
            .setView(ScrollView(context).apply { addView(body) })
            .setPositiveButton("创建邀请", null).setNegativeButton("取消", null).create()
        dialog.setOnShowListener {
            dialog.getButton(AlertDialog.BUTTON_POSITIVE).setOnClickListener {
                if (!dialog.isShowing) return@setOnClickListener
                val value = name.text.toString().trim()
                val error = when {
                    value.isEmpty() -> "请输入设备名称"
                    value.toByteArray(Charsets.UTF_8).size > 80 -> "设备名称过长（最多 80 字节）"
                    value.any { it.isISOControl() } -> "设备名称不能包含控制字符"
                    else -> null
                }
                if (error != null) { name.error = error; name.requestFocus(); return@setOnClickListener }
                val selectedRole = roles.getOrNull(role.selectedItemPosition) ?: return@setOnClickListener
                name.error = null
                dialog.dismiss()
                submit(value, selectedRole)
            }
        }
        dialog.show()
        return dialog
    }
}

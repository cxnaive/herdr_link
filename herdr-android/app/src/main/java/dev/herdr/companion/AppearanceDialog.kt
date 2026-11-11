package dev.herdr.companion

import android.app.AlertDialog
import android.content.Context
import android.view.ContextThemeWrapper
import android.view.Gravity
import android.view.View
import android.widget.LinearLayout
import android.widget.RadioButton
import android.widget.RadioGroup
import android.widget.ScrollView
import android.widget.TextView

/** Preview is isolated: canceling does not change the palette, saved preference or connections. */
internal object AppearanceDialog {
    fun show(context: Context, apply: (PocketTheme) -> Unit): AlertDialog {
        var choice = PocketAppearance.current
        val body = LinearLayout(context).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(dp(context, 20), dp(context, 8), dp(context, 20), 0)
        }
        body.addView(TextView(context).apply {
            text = "仅改变本机界面；终端输出保持深色。不影响配对、草稿和常驻监控。"
            textSize = 13f; setTextColor(AgentStyle.muted)
        })
        val group = RadioGroup(context)
        PocketTheme.entries.forEach { theme ->
            group.addView(RadioButton(context).apply {
                id = View.generateViewId(); tag = theme.id
                text = "${theme.title} · ${theme.description}"
                textSize = 14f; setTextColor(AgentStyle.ink)
                minHeight = dp(context, 48); setPadding(0, dp(context, 3), 0, dp(context, 3))
                isChecked = theme == choice
                contentDescription = "主题：${theme.title}"
            }, RadioGroup.LayoutParams(-1, -2))
        }
        body.addView(group)
        val preview = LinearLayout(context).apply { orientation = LinearLayout.VERTICAL }
        body.addView(preview, LinearLayout.LayoutParams(-1, -2).apply { setMargins(0, dp(context, 12), 0, dp(context, 12)) })
        fun renderPreview() {
            preview.removeAllViews()
            val themed = ContextThemeWrapper(context, choice.style)
            val colors = choice.colors
            val sample = LinearLayout(themed).apply {
                orientation = LinearLayout.VERTICAL
                setPadding(dp(context, 14), dp(context, 12), dp(context, 14), dp(context, 12))
                background = AgentStyle.shape(context, colors.background, 12, colors.border)
                contentDescription = "${choice.title}配色预览"
            }
            fun text(value: String, color: Int, size: Float) = TextView(themed).apply {
                text = value; setTextColor(color); textSize = size
            }
            sample.addView(text("配色预览", colors.ink, 17f).apply { typeface = AgentStyle.medium })
            sample.addView(text("运行中 2 · 对话和工作区", colors.muted, 12f).apply { setPadding(0, dp(context, 4), 0, dp(context, 10)) })
            sample.addView(text("任务已完成，查看结果。", colors.ink, 14f).apply {
                background = AgentStyle.shape(context, colors.panel, 8)
                setPadding(dp(context, 10), dp(context, 10), dp(context, 10), dp(context, 10))
            })
            sample.addView(text("发送消息", colors.onAccent, 13f).apply {
                gravity = Gravity.CENTER; background = AgentStyle.shape(context, colors.accent, 8)
                minHeight = dp(context, 36)
            }, LinearLayout.LayoutParams(dp(context, 100), -2).apply { gravity = Gravity.END; topMargin = dp(context, 10) })
            preview.addView(sample)
        }
        renderPreview()
        group.setOnCheckedChangeListener { _, id ->
            choice = PocketTheme.fromId(group.findViewById<RadioButton>(id)?.tag as? String)
            renderPreview()
        }
        val dialog = AlertDialog.Builder(context).setTitle("主题配色")
            .setView(ScrollView(context).apply { addView(body) })
            .setNegativeButton("取消", null).setPositiveButton("应用", null).create()
        dialog.setOnShowListener {
            dialog.getButton(AlertDialog.BUTTON_POSITIVE).setOnClickListener {
                if (choice == PocketAppearance.current) { dialog.dismiss(); return@setOnClickListener }
                dialog.dismiss()
                apply(choice)
            }
        }
        dialog.show()
        return dialog
    }
    private fun dp(context: Context, value: Int) = AgentStyle.dp(context, value)
}

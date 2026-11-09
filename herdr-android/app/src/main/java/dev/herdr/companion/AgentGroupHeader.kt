package dev.herdr.companion

import android.content.Context
import android.os.Bundle
import android.text.TextUtils
import android.view.Gravity
import android.view.View
import android.view.accessibility.AccessibilityNodeInfo
import android.widget.Button
import android.widget.ImageView
import android.widget.LinearLayout
import android.widget.TextView

internal class AgentGroupHeader(context: Context, val groupKey: String, title: String, summary: String,
    val expanded: Boolean, computer: Boolean, path: String? = null, action: (AgentGroupHeader) -> Unit) : LinearLayout(context) {
    init {
        tag = groupKey
        orientation = HORIZONTAL
        gravity = Gravity.CENTER_VERTICAL
        minimumHeight = AgentStyle.dp(context, if (computer) 72 else 64)
        setPadding(AgentStyle.dp(context, if (computer) 12 else 4), AgentStyle.dp(context, 10), AgentStyle.dp(context, 8), AgentStyle.dp(context, 10))
        background = AgentStyle.ripple(context, if (computer) AgentStyle.panel else android.graphics.Color.TRANSPARENT, 12)
        isFocusable = true
        contentDescription = "${if (computer) "电脑" else "工作空间"}：$title，${if (expanded) "已展开" else "已折叠"}，$summary" + path?.let { "，$it" }.orEmpty()
        addView(ImageView(context).apply {
            setImageDrawable(AgentIcon(if (expanded) "down" else "chevron", if (computer) AgentStyle.accent else AgentStyle.muted))
            importantForAccessibility = IMPORTANT_FOR_ACCESSIBILITY_NO
        }, LayoutParams(AgentStyle.dp(context, 18), AgentStyle.dp(context, 18)).apply { marginEnd = AgentStyle.dp(context, 10) })
        val text = LinearLayout(context).apply { orientation = VERTICAL; importantForAccessibility = IMPORTANT_FOR_ACCESSIBILITY_NO_HIDE_DESCENDANTS }
        fun label(value: String, size: Float, color: Int, lines: Int = 2) = TextView(context).apply {
            this.text = value; textSize = size; setTextColor(color); maxLines = lines
            ellipsize = if (lines == 1) TextUtils.TruncateAt.MIDDLE else TextUtils.TruncateAt.END
        }
        text.addView(label(title, if (computer) 20f else 18f, AgentStyle.ink).apply { typeface = AgentStyle.medium })
        text.addView(label(summary, 12f, AgentStyle.muted, if (computer) 4 else 2).apply { setPadding(0, AgentStyle.dp(context, 3), 0, 0) })
        if (!path.isNullOrBlank()) text.addView(label(path, 12f, AgentStyle.muted, 1))
        addView(text, LayoutParams(0, -2, 1f))
        setOnClickListener { action(this) }
        accessibilityDelegate = object : AccessibilityDelegate() {
            override fun onInitializeAccessibilityNodeInfo(host: View, info: AccessibilityNodeInfo) {
                super.onInitializeAccessibilityNodeInfo(host, info)
                info.className = Button::class.java.name
                info.isHeading = true
                info.addAction(if (expanded) AccessibilityNodeInfo.AccessibilityAction.ACTION_COLLAPSE else AccessibilityNodeInfo.AccessibilityAction.ACTION_EXPAND)
            }
            override fun performAccessibilityAction(host: View, action: Int, args: Bundle?): Boolean {
                val toggle = if (expanded) AccessibilityNodeInfo.ACTION_COLLAPSE else AccessibilityNodeInfo.ACTION_EXPAND
                return if (action == toggle) host.performClick() else super.performAccessibilityAction(host, action, args)
            }
        }
    }
}

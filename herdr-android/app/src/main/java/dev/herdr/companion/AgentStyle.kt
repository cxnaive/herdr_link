package dev.herdr.companion

import android.content.Context
import android.content.res.ColorStateList
import android.graphics.Color
import android.graphics.Typeface
import android.graphics.drawable.GradientDrawable
import android.graphics.drawable.RippleDrawable
import android.view.Gravity
import android.view.View
import android.widget.*

/** Shared palette tokens. Only explicit local appearance settings change these colors. */
internal object AgentStyle {
    val background get() = PocketAppearance.current.colors.background
    val panel get() = PocketAppearance.current.colors.panel
    val raised get() = PocketAppearance.current.colors.raised
    val ink get() = PocketAppearance.current.colors.ink
    val muted get() = PocketAppearance.current.colors.muted
    val accent get() = PocketAppearance.current.colors.accent
    val onAccent get() = PocketAppearance.current.colors.onAccent
    val border get() = PocketAppearance.current.colors.border
    val selected get() = PocketAppearance.current.colors.selected
    val warning get() = PocketAppearance.current.colors.warning
    val error get() = PocketAppearance.current.colors.error
    // Remote ANSI output is not recolored when changing the app's palette.
    val terminalBackground = 0xff111417.toInt()
    val medium = Typeface.create("sans-serif-medium", Typeface.NORMAL)
    fun dp(context: Context, value: Int) = (value * context.resources.displayMetrics.density).toInt()
    fun contentWidth(context: Context, maximum: Int = 760): Int {
        val width = context.resources.configuration.screenWidthDp
        // Leave room for the expanded navigation rail; never size a child wider than its host.
        return if (width - (if (width >= 600) 88 else 0) > maximum) dp(context, maximum) else -1
    }
    fun actionSemantics(view: View) {
        view.accessibilityDelegate = object : View.AccessibilityDelegate() {
            override fun onInitializeAccessibilityNodeInfo(host: View, info: android.view.accessibility.AccessibilityNodeInfo) {
                super.onInitializeAccessibilityNodeInfo(host, info); info.className = Button::class.java.name
            }
        }
    }
    fun shape(context: Context, color: Int, radius: Int, stroke: Int = Color.TRANSPARENT) = GradientDrawable().apply {
        setColor(color); cornerRadius = dp(context, radius).toFloat()
        if (stroke != Color.TRANSPARENT) setStroke(dp(context, 1).coerceAtLeast(1), stroke)
    }
    fun ripple(context: Context, color: Int = Color.TRANSPARENT, radius: Int = 10) = RippleDrawable(
        ColorStateList.valueOf((accent and 0x00ffffff) or 0x24000000), shape(context, color, radius), shape(context, Color.WHITE, radius))
    fun button(context: Context, text: String, primary: Boolean = false, subtle: Boolean = false, action: () -> Unit) = Button(context).apply {
        this.text = text; isAllCaps = false; textSize = 13f; typeface = medium; letterSpacing = 0f
        val foreground = if (primary) onAccent else if (subtle) accent else ink
        setTextColor(ColorStateList(arrayOf(intArrayOf(-android.R.attr.state_enabled), intArrayOf()), intArrayOf(muted, foreground)))
        background = ripple(context, if (primary) accent else if (subtle) Color.TRANSPARENT else raised, 10)
        backgroundTintList = null; stateListAnimator = null; elevation = 0f
        minimumHeight = dp(context, 48); minHeight = dp(context, 48); minimumWidth = 0; minWidth = 0
        setPadding(dp(context, 12), dp(context, 6), dp(context, 12), dp(context, 6)); setOnClickListener { action() }
    }
    fun icon(context: Context, name: String, color: Int = muted, size: Int = 20) = AgentIcon(name, color).apply { setBounds(0, 0, dp(context, size), dp(context, size)) }
    fun iconButton(context: Context, icon: String, description: String, action: () -> Unit) = ImageButton(context).apply {
        contentDescription = description; tooltipText = description
        setImageDrawable(AgentIcon(icon, ink)); scaleType = ImageView.ScaleType.FIT_CENTER
        setPadding(dp(context, 13), dp(context, 13), dp(context, 13), dp(context, 13))
        background = ripple(context); setOnClickListener { action() }
        minimumWidth = dp(context, 48); minimumHeight = dp(context, 48)
    }
    fun search(context: Context, field: EditText): LinearLayout = LinearLayout(context).apply {
        gravity = Gravity.CENTER_VERTICAL; background = shape(context, panel, 12, border); minimumHeight = dp(context, 48)
        setPadding(dp(context, 12), 0, dp(context, 8), 0)
        addView(ImageView(context).apply { setImageDrawable(AgentIcon("search", muted)); importantForAccessibility = android.view.View.IMPORTANT_FOR_ACCESSIBILITY_NO }, LinearLayout.LayoutParams(dp(context, 20), dp(context, 20)))
        field.apply { background = null; textSize = 14f; setTextColor(ink); setHintTextColor(muted); setPadding(dp(context, 10), dp(context, 10), dp(context, 6), dp(context, 10)); minimumHeight = dp(context, 48) }
        addView(field, LinearLayout.LayoutParams(0, -2, 1f))
    }
}

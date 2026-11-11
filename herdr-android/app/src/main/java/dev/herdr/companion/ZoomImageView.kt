package dev.herdr.companion

import android.content.Context
import android.graphics.Bitmap
import android.graphics.Canvas
import android.graphics.Paint
import android.view.GestureDetector
import android.view.MotionEvent
import android.view.ScaleGestureDetector
import android.view.View
import android.view.accessibility.AccessibilityNodeInfo

class ZoomImageView(context: Context) : View(context) {
    val viewport = ReviewViewport()
    private val paint = Paint(Paint.ANTI_ALIAS_FLAG or Paint.FILTER_BITMAP_FLAG)
    private var bitmap: Bitmap? = null
    var onZoom: ((Float) -> Unit)? = null
    var onSettled: (() -> Unit)? = null
    private val settle = Runnable { onSettled?.invoke() }
    private val scaleDetector = ScaleGestureDetector(context, object : ScaleGestureDetector.SimpleOnScaleGestureListener() {
        override fun onScale(detector: ScaleGestureDetector): Boolean { changeZoom(viewport.zoom * detector.scaleFactor, detector.focusX, detector.focusY); return true }
    })
    private val gestures = GestureDetector(context, object : GestureDetector.SimpleOnGestureListener() {
        override fun onDown(e: MotionEvent): Boolean = true
        override fun onSingleTapConfirmed(e: MotionEvent): Boolean { performClick(); return true }
        override fun onDoubleTap(e: MotionEvent): Boolean { changeZoom(if (viewport.zoom > 1.05f) 1f else 3f, e.x, e.y); return true }
        override fun onScroll(e1: MotionEvent?, e2: MotionEvent, dx: Float, dy: Float): Boolean {
            if (!scaleDetector.isInProgress && e2.pointerCount == 1) { viewport.pan(-dx, -dy); invalidate() }; return true
        }
    })
    init { setBackgroundColor(AgentStyle.background); isFocusable = true; contentDescription = "文档图像，双指缩放、单指拖动、双击放大或还原" }
    fun setBitmap(value: Bitmap?, reset: Boolean = true) {
        bitmap = value
        if (value != null) viewport.size(width.toFloat(), height.toFloat(), value.width.toFloat(), value.height.toFloat(), reset)
        onZoom?.invoke(viewport.zoom); invalidate()
    }
    fun changeZoom(value: Float, x: Float = width / 2f, y: Float = height / 2f) {
        viewport.zoomTo(value, x, y); onZoom?.invoke(viewport.zoom); invalidate(); removeCallbacks(settle); postDelayed(settle, 220)
    }
    fun fitScreen() { viewport.fitScreen(); onZoom?.invoke(1f); invalidate(); removeCallbacks(settle); postDelayed(settle, 220) }
    override fun onSizeChanged(w: Int, h: Int, oldw: Int, oldh: Int) { viewport.size(w.toFloat(), h.toFloat()); onSettled?.invoke() }
    override fun onDraw(canvas: Canvas) {
        super.onDraw(canvas)
        bitmap?.takeUnless { it.isRecycled }?.let { canvas.save(); canvas.translate(viewport.x, viewport.y); canvas.scale(viewport.scale, viewport.scale); canvas.drawBitmap(it, 0f, 0f, paint); canvas.restore() }
    }
    override fun onTouchEvent(event: MotionEvent): Boolean {
        parent?.requestDisallowInterceptTouchEvent(true)
        scaleDetector.onTouchEvent(event); gestures.onTouchEvent(event)
        if (event.actionMasked == MotionEvent.ACTION_UP || event.actionMasked == MotionEvent.ACTION_CANCEL) { removeCallbacks(settle); postDelayed(settle, 220) }
        return true
    }
    override fun performClick(): Boolean { super.performClick(); return true }
    override fun onInitializeAccessibilityNodeInfo(info: AccessibilityNodeInfo) {
        super.onInitializeAccessibilityNodeInfo(info)
        info.addAction(AccessibilityNodeInfo.AccessibilityAction.ACTION_SCROLL_FORWARD); info.addAction(AccessibilityNodeInfo.AccessibilityAction.ACTION_SCROLL_BACKWARD)
    }
    override fun performAccessibilityAction(action: Int, arguments: android.os.Bundle?): Boolean = when (action) {
        AccessibilityNodeInfo.ACTION_SCROLL_FORWARD -> { changeZoom(viewport.zoom * 1.5f); true }
        AccessibilityNodeInfo.ACTION_SCROLL_BACKWARD -> { changeZoom(viewport.zoom / 1.5f); true }
        else -> super.performAccessibilityAction(action, arguments)
    }
    override fun onDetachedFromWindow() { removeCallbacks(settle); super.onDetachedFromWindow() }
}

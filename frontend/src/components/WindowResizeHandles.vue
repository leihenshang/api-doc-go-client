<script setup lang="ts">
// Linux 无边框窗口：让「可见边框」可以直接拖动缩放。
//
// 为什么需要这一层：Wails 运行时的边缘检测只认**窗口外沿** 6px（runtime JS 用
// `window.outerWidth - clientX` 判断右/下边），而桌面态外壳为画外阴影让出了
// `--app-frame-gap`（32px，见 base.css）：用户拖的是**可见边框**，离窗口外沿 32px，
// 命中不到。最大化时留白归零，所以此前看起来「能用」；恢复成小窗口后就拖不动了。
//
// 这里在可见边框内侧放 6px 热区，直接发送运行时同款消息 `resize:<edge>`
// （Linux 后端把它交给 GTK 的 begin_resize_drag），不依赖 outerWidth。
// 只在 Linux 桌面壳显示（见 styles/window-resize.css），不影响 Windows/macOS 与浏览器态。
type Edge = 'n' | 's' | 'e' | 'w' | 'ne' | 'nw' | 'se' | 'sw'

const EDGES: Edge[] = ['n', 's', 'e', 'w', 'ne', 'nw', 'se', 'sw']
const WAILS_EDGE: Record<Edge, string> = {
  n: 'n-resize',
  s: 's-resize',
  e: 'e-resize',
  w: 'w-resize',
  ne: 'ne-resize',
  nw: 'nw-resize',
  se: 'se-resize',
  sw: 'sw-resize',
}

/** 按下即把缩放交回窗口管理器（GTK），并阻止冒泡，避免被运行时当成拖动窗口。 */
function onStart(edge: Edge, ev: PointerEvent): void {
  if (ev.button !== 0 || !ev.isPrimary) return
  const invoke = (window as unknown as { WailsInvoke?: (msg: string) => void }).WailsInvoke
  if (!invoke) return
  ev.preventDefault()
  ev.stopPropagation()
  invoke(`resize:${WAILS_EDGE[edge]}`)
}
</script>

<template>
  <div class="win-resize" aria-hidden="true">
    <div
      v-for="edge in EDGES"
      :key="edge"
      class="wr"
      :class="`wr-${edge}`"
      :data-testid="`window.resize.${edge}`"
      @pointerdown="onStart(edge, $event)"
    />
  </div>
</template>

<style scoped src="@/styles/window-resize.css"></style>

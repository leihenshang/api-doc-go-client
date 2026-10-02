<script setup lang="ts">
// 自绘标题栏（主窗口无边框）：拖动区靠 CSS `--wails-draggable`，
// 非桌面壳（浏览器 / devserver）下不渲染窗口控制按钮。
// 右侧常驻主题切换按钮：未打开集合时也能改配色（工具栏那时还没渲染）。
// 双击拖动区 = 最大化 / 还原（Windows 标题栏习惯）：Wails 的拖动默认延后到 mousemove
// （runtime 里 flags.deferDragToMouseMove = true），静止双击不会进入系统移动循环，
// 所以这里能正常收到 dblclick，直接调 WindowToggleMaximise 即可 —— 注意这不是「真全屏」，
// 行为与系统标题栏一致（占满工作区，不盖任务栏）。
import { NIcon } from 'naive-ui'
import { CloseOutline, CopyOutline, MoonOutline, RemoveOutline, SquareOutline, SunnyOutline } from '@vicons/ionicons5'
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { hasWailsRuntime, windowCtl } from '@/lib/ipc'

// layout 为 null 表示当前没有打开的请求（没有「请求 / 响应」可排布），此时不渲染这组按钮
const props = defineProps<{ dark: boolean; layout?: 'right' | 'bottom' | null }>()
const emit = defineEmits<{ 'toggle-theme': []; 'set-layout': [layout: 'right' | 'bottom'] }>()

const { t } = useI18n()
const customChrome = hasWailsRuntime()
const maximised = ref(false)

/** 最大化后窗口铺满屏幕，外壳圆角要归零（见 styles/base.css 的 :root[data-window='max']）。 */
const RESIZE_SYNC_MS = 180

// 提示文案说明「点了会切到哪」而不是当前状态
const themeHint = computed(() => (props.dark ? t('app.themeToLight') : t('app.themeToDark')))

/** 同步最大化状态：按钮、双击标题栏、Win+↑ 都会改变它。 */
async function syncMaximised(): Promise<void> {
  if (!customChrome) return
  maximised.value = await windowCtl.isMaximised()
  document.documentElement.dataset.window = maximised.value ? 'max' : 'normal'
}

// 最大化由窗口管理器异步完成，切换后稍等再回读，避免图标停在旧状态
async function toggleMax(): Promise<void> {
  windowCtl.toggleMaximise()
  await new Promise((resolve) => setTimeout(resolve, 80))
  await syncMaximised()
}

/** 双击拖动区 = 最大化 / 还原；落在窗口控制按钮、主题按钮上的双击不参与。 */
function onTitleDblClick(e: MouseEvent): void {
  if (!customChrome) return
  if ((e.target as HTMLElement | null)?.closest('button')) return
  void toggleMax()
}

// 拖动改变窗口大小 / 系统快捷键最大化时也会触发 resize，借它兜住状态同步
let resizeTimer: ReturnType<typeof setTimeout> | null = null
function onResize(): void {
  if (resizeTimer) clearTimeout(resizeTimer)
  resizeTimer = setTimeout(() => void syncMaximised(), RESIZE_SYNC_MS)
}

onMounted(() => {
  void syncMaximised()
  window.addEventListener('resize', onResize)
})

onBeforeUnmount(() => {
  window.removeEventListener('resize', onResize)
  if (resizeTimer) clearTimeout(resizeTimer)
})
</script>

<template>
  <header class="titlebar" data-testid="titlebar" @dblclick="onTitleDblClick">
    <span class="logo" aria-hidden="true">
      <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linejoin="round">
        <path d="M22 2 11 13" />
        <path d="M22 2 15 22l-4-9-9-4 20-7Z" />
      </svg>
    </span>
    <span class="brand">{{ t('app.brand') }}</span>
    <span class="sp" />

    <!-- 响应区排列（左：请求响应左右 / 右：上下）：原先挂在分栏中缝的胶囊里，
         挪到标题栏与主题按钮相邻，省掉中缝的视觉占位 -->
    <div v-if="layout" class="layouts" role="group" :aria-label="t('settings.responseLayout')">
      <button
        class="lay"
        :class="{ on: layout === 'right' }"
        type="button"
        data-testid="titlebar.layoutRight"
        :title="t('editor.layoutHorizontal')"
        @click="emit('set-layout', 'right')"
      >
        <svg viewBox="0 0 14 12" width="14" height="12" aria-hidden="true">
          <rect x="0.6" y="0.6" width="12.8" height="10.8" rx="1.6" fill="none" stroke="currentColor" />
          <line x1="7" y1="0.6" x2="7" y2="11.4" stroke="currentColor" />
        </svg>
      </button>
      <button
        class="lay"
        :class="{ on: layout === 'bottom' }"
        type="button"
        data-testid="titlebar.layoutBottom"
        :title="t('editor.layoutVertical')"
        @click="emit('set-layout', 'bottom')"
      >
        <svg viewBox="0 0 14 12" width="14" height="12" aria-hidden="true">
          <rect x="0.6" y="0.6" width="12.8" height="10.8" rx="1.6" fill="none" stroke="currentColor" />
          <line x1="0.6" y1="6" x2="13.4" y2="6" stroke="currentColor" />
        </svg>
      </button>
    </div>

    <button
      class="theme"
      type="button"
      data-testid="titlebar.theme"
      :title="themeHint"
      :aria-label="themeHint"
      @click="emit('toggle-theme')"
    >
      <n-icon :component="dark ? SunnyOutline : MoonOutline" :size="15" />
    </button>

    <div v-if="customChrome" class="win">
      <button class="wc" type="button" data-testid="titlebar.min" :title="t('app.minimise')" @click="windowCtl.minimise()">
        <n-icon :component="RemoveOutline" :size="15" />
      </button>
      <button class="wc" type="button" data-testid="titlebar.max" :title="t('app.maximise')" @click="toggleMax">
        <n-icon :component="maximised ? CopyOutline : SquareOutline" :size="12" />
      </button>
      <button class="wc danger" type="button" data-testid="titlebar.close" :title="t('app.close')" @click="windowCtl.quit()">
        <n-icon :component="CloseOutline" :size="15" />
      </button>
    </div>
  </header>
</template>

<style scoped>
.titlebar {
  height: var(--app-titlebar-h);
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 0 6px 0 13px;
  background: var(--app-bg);
  border-bottom: 1px solid var(--app-border);
  user-select: none;
  /* Wails 无边框拖动：属性会向下继承，交互元素需显式 no-drag */
  --wails-draggable: drag;
}

.logo {
  display: inline-flex;
  color: var(--app-accent);
}

.brand {
  font-size: 13.5px;
  font-weight: 600;
  letter-spacing: 0.2px;
  color: var(--app-text);
}

.sp {
  flex: 1 1 auto;
}

/* 主题切换：与窗口控制按钮同高，悬停才有底色，避免抢视觉 */
.theme {
  --wails-draggable: no-drag;
  width: 30px;
  height: 30px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: none;
  border-radius: 6px;
  background: none;
  color: var(--app-muted);
  cursor: pointer;
}

.theme:hover {
  background: var(--app-row-hover);
  color: var(--app-accent);
}

/* 布局切换：一个小分段控件（两侧都是 26×26），比原来中缝的胶囊（26×52 / 52×26）省地方 */
.layouts {
  --wails-draggable: no-drag;
  display: flex;
  align-items: center;
  gap: 1px;
  padding: 1px;
  border: 1px solid var(--app-border);
  border-radius: 7px;
}

.lay {
  width: 26px;
  height: 26px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: none;
  border-radius: 6px;
  background: none;
  color: var(--app-muted);
  cursor: pointer;
}

.lay:hover {
  background: var(--app-row-hover);
  color: var(--app-text-2);
}

/* 选中态用强调色淡底（比原来胶囊的实心强调色更安静，和旁边的主题按钮同量级） */
.lay.on {
  background: var(--app-active);
  color: var(--app-accent);
}

.win {
  display: flex;
  align-items: center;
}

.wc {
  --wails-draggable: no-drag;
  width: 34px;
  height: 30px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: none;
  background: none;
  color: var(--app-muted);
  cursor: pointer;
  border-radius: 6px;
}

.wc:hover {
  background: var(--app-row-hover);
  color: var(--app-text);
}

.wc.danger:hover {
  background: var(--app-danger);
  color: var(--app-on-accent);
}
</style>

<script setup lang="ts">
// 自绘标题栏（主窗口无边框）：拖动区靠 CSS `--wails-draggable`，
// 非桌面壳（浏览器 / devserver）下不渲染窗口控制按钮。
// 右侧常驻主题切换按钮：未打开集合时也能改配色（工具栏那时还没渲染）。
import { NIcon } from 'naive-ui'
import { CloseOutline, CopyOutline, MoonOutline, RemoveOutline, SquareOutline, SunnyOutline } from '@vicons/ionicons5'
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { hasWailsRuntime, windowCtl } from '@/lib/ipc'

const props = defineProps<{ dark: boolean }>()
const emit = defineEmits<{ 'toggle-theme': [] }>()

const { t } = useI18n()
const customChrome = hasWailsRuntime()
const maximised = ref(false)

// 提示文案说明「点了会切到哪」而不是当前状态
const themeHint = computed(() => (props.dark ? t('app.themeToLight') : t('app.themeToDark')))

// 最大化由窗口管理器异步完成，切换后回读一次以换成正确的图标
async function toggleMax(): Promise<void> {
  windowCtl.toggleMaximise()
  maximised.value = await windowCtl.isMaximised()
}

onMounted(async () => {
  if (customChrome) maximised.value = await windowCtl.isMaximised()
})
</script>

<template>
  <header class="titlebar">
    <span class="logo" aria-hidden="true">
      <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linejoin="round">
        <path d="M22 2 11 13" />
        <path d="M22 2 15 22l-4-9-9-4 20-7Z" />
      </svg>
    </span>
    <span class="brand">{{ t('app.brand') }}</span>
    <span class="sp" />

    <button class="theme" type="button" :title="themeHint" :aria-label="themeHint" @click="emit('toggle-theme')">
      <n-icon :component="dark ? SunnyOutline : MoonOutline" :size="15" />
    </button>

    <div v-if="customChrome" class="win">
      <button class="wc" type="button" :title="t('app.minimise')" @click="windowCtl.minimise()">
        <n-icon :component="RemoveOutline" :size="15" />
      </button>
      <button class="wc" type="button" :title="t('app.maximise')" @click="toggleMax">
        <n-icon :component="maximised ? CopyOutline : SquareOutline" :size="12" />
      </button>
      <button class="wc danger" type="button" :title="t('app.close')" @click="windowCtl.quit()">
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

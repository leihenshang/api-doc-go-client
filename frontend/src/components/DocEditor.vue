<script setup lang="ts">
// 文档条目编辑器（B13 → B19）：Markdown 编辑 + 预览。
//
// B19 起它不再是弹窗内容，而是**文档页签**的编辑区：数据由父组件（App.vue）以
// tab.doc 传入，编辑经 update:doc 回写 store（标脏），保存由父组件走
// tabs.saveDoc（带 ExpectHash 冲突检测）——本组件不自己读写门面。
// 文档改名只在左侧树上进行（树的右键/悬停菜单），编辑器内不再放标题框。
import { uploadMdImages } from '@/lib/md-image'
import { isDark } from '@/lib/theme'
import { useSettingsStore } from '@/stores/settings'
import type { DocTabDoc } from '@/stores/tabs'
import { MdEditor, MdPreview } from 'md-editor-v3'
import 'md-editor-v3/lib/preview.css'
import 'md-editor-v3/lib/style.css'
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{ doc: DocTabDoc; readonly?: boolean }>()
const emit = defineEmits<{
  'update:doc': [doc: DocTabDoc]
  save: []
}>()
const { t, locale } = useI18n()
const settings = useSettingsStore()

// 编辑 / 预览切换：初始态取设置「Markdown 编辑器默认状态」（只读预览 / 分屏编辑），
// 之后由页签内的按钮临时切换（不回写设置）。
const readonly = computed(() => props.readonly ?? false)
/** 用户在本页签里手动切过就以此为准（null = 跟随设置，设置异步加载完也能自动生效）。 */
const modeOverride = ref<boolean | null>(null)
/**
 * 只读预览：整块换成 MdPreview（纯渲染、无工具栏）。
 *
 * 为什么不用 MdEditor 的 previewOnly：v7 里它是**内部设置**（由工具栏的 preview-only 按钮切换），
 * 不是组件 prop —— 传 `:preview-only` 只会落到 attrs 上，被忽略（实测源码列仍占一半宽）。
 */
const previewOnly = computed(() => modeOverride.value ?? settings.mdEditorMode === 'readonly')
const mdLanguage = computed(() => (locale.value === 'en-US' ? 'en-US' : 'zh-CN'))

function toggleMode(): void {
  modeOverride.value = !previewOnly.value
}

/** 正文走 store：改一点就标脏，保存由页签统一调度（手动/快捷键一致）。 */
function onContent(v: string): void {
  emit('update:doc', { ...props.doc, content: v })
}

/** Markdown 工具栏自带的「保存」按钮 / 编辑器内 Ctrl+S 都走这里（与全局快捷键同一落点）。 */
function onSave(): void {
  emit('save')
}

</script>

<template>
  <div class="doc-editor">
    <!-- 查看 / 编辑切换：初始态来自设置（只读预览 / 分屏编辑），这里只做本页签的临时切换。
         集合只读授权时不显示 —— 那种情况下本来就不许编辑。 -->
    <div v-if="!readonly" class="doc-bar">
      <button class="gin" type="button" data-testid="doc.toggleMode" @click="toggleMode">
        {{ previewOnly ? t('docs.edit') : t('docs.view') }}
      </button>
    </div>
    <div :class="['body', previewOnly ? 'preview' : 'edit']">
      <md-preview
        v-if="previewOnly"
        :model-value="doc.content"
        :language="mdLanguage"
        :theme="isDark ? 'dark' : 'light'"
        :preview-theme="isDark ? 'github' : 'default'"
        class="md-preview"
      />
      <md-editor
        v-else
        :model-value="doc.content"
        :language="mdLanguage"
        :theme="isDark ? 'dark' : 'light'"
        class="md-edit"
        :readonly="readonly"
        :on-upload-img="uploadMdImages"
        @update:model-value="onContent"
        @on-save="onSave"
      />
    </div>
  </div>
</template>

<style scoped>
/* 查看 / 编辑切换条：与接口说明的 docs-bar 同一观感（12px、muted、悬停浅底） */
.doc-bar {
  display: flex;
  flex: 0 0 auto;
  align-items: center;
  gap: 6px;
  padding: 2px 8px 0;
}

.doc-bar .gin {
  border: none;
  border-radius: 5px;
  padding: 3px 10px;
  background: none;
  font-family: inherit;
  font-size: 12px;
  color: var(--app-muted);
  cursor: pointer;
}

.doc-bar .gin:hover {
  background: var(--app-row-hover);
  color: var(--app-text);
}

.doc-editor {
  display: flex;
  flex-direction: column;
  /* 铺满右侧工作区：父级是 column flex（.detail），这里再显式给宽 + 允许收缩 */
  flex: 1 1 auto;
  width: 100%;
  min-width: 0;
  min-height: 0;
  height: 100%;
}

.body {
  flex: 1 1 auto;
  width: 100%;
  height: 100%;
  min-width: 0;
  min-height: 0;
  display: flex;
}

/* 只读预览：整块换成 MdPreview，铺满正文区（不再有源码列）。
   预览态没有编辑器工具栏，正文会直接贴着容器边框，因此由外层统一给四周留白；
   MdPreview 自带 padding-inline 20px / padding-block 10px，这里归零避免双重缩进。 */
.body.preview {
  display: block;
  box-sizing: border-box;
  padding: 16px 22px;
  overflow: auto;
}

.body.preview :deep(.md-preview) {
  box-sizing: border-box;
  width: 100%;
  min-height: 100%;
}

.body.preview :deep(.md-editor-preview) {
  padding: 0;
}

/* 源码 / 预览各占一半。
   实测层级：.md-editor-content-wrapper(flex, 1176) → .md-editor-custom-scrollbar(block, 588)
   → .md-editor-input-wrapper / -preview-wrapper(block, 588)。
   md-editor-v3 本身已经是 50/50，"列"是 custom-scrollbar；两个 wrapper 是列内的普通块，
   **不是 flex 项**，对它们写 flex 属性不生效。唯一能改宽度的是 width，而 width 的百分比
   按「列宽」（588）解析，不是按整幅（1176）——上一版写 width: 44% 因此只得到 44% × 588 ≈ 259px
   （约整幅的 22%），源码区被压成一条窄栏、正文提前折行。 */
.body.edit :deep(.md-editor-input-wrapper),
.body.edit :deep(.md-editor-preview-wrapper) {
  width: auto;
  min-width: 0;
}

/* 预览首元素的 margin-top 归零。
   预览容器 `.md-editor-preview` 是 display: flow-root（独立 BFC）+ padding-top 10px，
   首元素 h2（24px，UA 默认 margin 0.83em）约 20px 的上外边距不会穿透到容器外，
   而是整段压在标题上方，标题因此比左侧源码首行低 20px（实测 top 187 vs 167），
   看起来像"预览顶部多了一块空白"。只清首元素，第二段起保留正常段间距。 */
.body.edit :deep(.md-editor-preview > :first-child) {
  margin-top: 0;
}

/* md-editor-v3 默认 height:500px（固定高），不覆盖就只占半屏 */
.body.edit :deep(.md-editor) {
  flex: 1 1 auto;
  width: 100%;
  height: 100%;
  min-width: 0;
  min-height: 0;
}

.body.edit :deep(.md-preview) {
  flex: 1 1 auto;
  width: 100%;
  height: 100%;
  min-width: 0;
  overflow: auto;
}
</style>

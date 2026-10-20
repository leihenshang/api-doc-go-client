<script setup lang="ts">
// 文档条目编辑器（B13 → B19）：Markdown 编辑 + 预览。
//
// B19 起它不再是弹窗内容，而是**文档页签**的编辑区：数据由父组件（App.vue）以
// tab.doc 传入，编辑经 update:doc 回写 store（标脏），保存由父组件走
// tabs.saveDoc（带 ExpectHash 冲突检测）——本组件不自己读写门面。
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { MdEditor, MdPreview } from 'md-editor-v3'
import 'md-editor-v3/lib/preview.css'
import 'md-editor-v3/lib/style.css'
import { isDark } from '@/lib/theme'
import type { DocTabDoc } from '@/stores/tabs'

const props = defineProps<{ doc: DocTabDoc; readonly?: boolean }>()
const emit = defineEmits<{
  'update:doc': [doc: DocTabDoc]
  save: []
  close: []
}>()
const { t, locale } = useI18n()

// 文档页签只有「编辑 / 预览」两种单栏形态（不支持左右分栏）
const mode = ref<'edit' | 'preview'>('edit')
const readonly = computed(() => props.readonly ?? false)
const mdLanguage = computed(() => (locale.value === 'en-US' ? 'en-US' : 'zh-CN'))

/** 标题与正文都走 store：改一点就标脏，保存由页签统一调度（防抖/手动一致）。 */
function onName(v: string): void {
  emit('update:doc', { ...props.doc, name: v })
}
function onContent(v: string): void {
  emit('update:doc', { ...props.doc, content: v })
}
</script>

<template>
  <div class="doc-editor">
    <div class="hd">
      <n-input
        :value="doc.name"
        size="small"
        class="name"
        :readonly="readonly"
        data-testid="doc.name"
        @update:value="onName"
      />
      <span class="sp" />
      <n-button size="tiny" :type="mode === 'edit' ? 'primary' : 'default'" data-testid="doc.modeEdit" @click="mode = 'edit'">
        {{ t('editor.edit') }}
      </n-button>
      <n-button size="tiny" :type="mode === 'preview' ? 'primary' : 'default'" data-testid="doc.modePreview" @click="mode = 'preview'">
        {{ t('editor.preview') }}
      </n-button>
      <n-button v-if="!readonly" size="tiny" type="primary" data-testid="doc.save" @click="emit('save')">
        {{ t('common.save') }}
      </n-button>
      <n-button size="tiny" quaternary data-testid="doc.close" @click="emit('close')">×</n-button>
    </div>
    <div class="body" :class="mode">
      <md-editor
        v-if="mode !== 'preview'"
        :model-value="doc.content"
        :language="mdLanguage"
        :theme="isDark ? 'dark' : 'light'"
        class="md-edit"
        :readonly="readonly"
        @update:model-value="onContent"
      />
      <md-preview v-if="mode !== 'edit'" :model-value="doc.content" class="md-preview" />
    </div>
  </div>
</template>

<style scoped>
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

.hd {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 12px;
  border-bottom: 1px solid var(--app-border);
  /* 头部不参与拉伸，也不被内容挤走 */
  flex: 0 0 auto;
  width: 100%;
  min-width: 0;
}

.name {
  /* 可收缩：按钮变宽（长语言/窄窗口）时不要把输入框挤没 */
  flex: 0 1 220px;
  min-width: 120px;
}

.sp {
  flex: 1;
}

.body {
  flex: 1 1 auto;
  width: 100%;
  height: 100%;
  min-width: 0;
  min-height: 0;
  display: flex;
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

.body.preview :deep(.md-preview) {
  flex: 1 1 auto;
  width: 100%;
  height: 100%;
  min-width: 0;
  overflow: auto;
}
</style>

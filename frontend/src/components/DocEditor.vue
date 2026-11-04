<script setup lang="ts">
// 文档条目编辑器（B13 → B19）：Markdown 编辑 + 预览。
//
// B19 起它不再是弹窗内容，而是**文档页签**的编辑区：数据由父组件（App.vue）以
// tab.doc 传入，编辑经 update:doc 回写 store（标脏），保存由父组件走
// tabs.saveDoc（带 ExpectHash 冲突检测）——本组件不自己读写门面。
// 文档改名只在左侧树上进行（树的右键/悬停菜单），编辑器内不再放标题框。
import { uploadMdImages } from '@/lib/md-image'
import { isDark } from '@/lib/theme'
import type { DocTabDoc } from '@/stores/tabs'
import { MdEditor } from 'md-editor-v3'
import 'md-editor-v3/lib/style.css'
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{ doc: DocTabDoc; readonly?: boolean }>()
const emit = defineEmits<{
  'update:doc': [doc: DocTabDoc]
  save: []
}>()
const { locale } = useI18n()

// 编辑 / 预览切换：不再自建按钮，直接用 md-editor 工具栏自带的「仅预览」开关。
// 文档页签固定以编辑态打开 —— v7 里 previewOnly 既不是受控 prop、也没进实例暴露，
// 想在挂载时「程序化进入预览」只能模拟点击，脆弱且会和重渲染打架；
// 而工具栏开关本身就是双向的，用户点一下即可，语义也更直白。
const readonly = computed(() => props.readonly ?? false)
const mdLanguage = computed(() => (locale.value === 'en-US' ? 'en-US' : 'zh-CN'))

// 仅预览时工具栏只留「切换」按钮：否则预览态下点「加粗/插入表格」等仍在改正文，
// 而用户看不到源码列，等于悄悄改内容。编辑态回到默认全量工具栏（undefined = 用组件默认值）。
const previewOnly = ref(false)
const toolbars = computed<import('md-editor-v3').ToolbarNames[] | undefined>(() =>
  previewOnly.value ? ['previewOnly'] : undefined,
)

// md-editor 不对外 emit「预览态变化」（内部只有 eventBus 常量），这里用事件委托同步：
// 点了「仅预览」按钮后，等编辑器把源码列收起，再按实际布局判定当前态。
const rootRef = ref<HTMLElement | null>(null)

function isPreviewMode(): boolean {
  const src = rootRef.value?.querySelector<HTMLElement>('.md-editor-input-wrapper')
  return !src || src.getBoundingClientRect().width === 0
}

function onEditorClick(ev: MouseEvent): void {
  const el = ev.target as HTMLElement | null
  const btn = el?.closest?.('.md-editor-toolbar-item')
  if (!btn) return
  if (!/仅预览|preview only/i.test(btn.getAttribute('title') ?? '')) return
  // md-editor 在这一次点击里切换内部状态；等 DOM 落定再读，避免读到切换前的宽度
  requestAnimationFrame(() => {
    previewOnly.value = isPreviewMode()
  })
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
  <div ref="rootRef" class="doc-editor" @click.capture="onEditorClick">
    <div class="body edit">
      <md-editor
        :model-value="doc.content"
        :language="mdLanguage"
        :theme="isDark ? 'dark' : 'light'"
        class="md-edit"
        :readonly="readonly"
        :toolbars="toolbars"
        :on-upload-img="uploadMdImages"
        @update:model-value="onContent"
        @on-save="onSave"
      />
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

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

const mode = ref<'edit' | 'split' | 'preview'>('split')
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
      <n-button size="tiny" :type="mode === 'edit' ? 'primary' : 'default'" @click="mode = 'edit'">
        {{ t('editor.edit') }}
      </n-button>
      <n-button size="tiny" :type="mode === 'split' ? 'primary' : 'default'" @click="mode = 'split'">
        {{ t('editor.split') }}
      </n-button>
      <n-button size="tiny" :type="mode === 'preview' ? 'primary' : 'default'" @click="mode = 'preview'">
        {{ t('editor.preview') }}
      </n-button>
      <n-button v-if="!readonly" size="tiny" type="primary" data-testid="doc.save" @click="emit('save')">
        {{ t('common.save') }}
      </n-button>
      <n-button size="tiny" quaternary @click="emit('close')">×</n-button>
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
  height: 100%;
  min-height: 0;
}

.hd {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 12px;
  border-bottom: 1px solid var(--app-border);
}

.name {
  width: 220px;
}

.sp {
  flex: 1;
}

.body {
  flex: 1 1 auto;
  min-height: 0;
  display: flex;
}

.body.edit :deep(.md-editor) {
  flex: 1 1 auto;
}

.body.split :deep(.md-editor),
.body.split :deep(.md-preview) {
  flex: 1 1 50%;
  min-width: 0;
}

.body.preview :deep(.md-preview) {
  flex: 1 1 auto;
  min-width: 0;
}
</style>

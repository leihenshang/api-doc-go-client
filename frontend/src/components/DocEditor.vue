<script setup lang="ts">
// 文档条目编辑器（B13）：Markdown 编辑 + 预览。
import { NButton, NInput } from 'naive-ui'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { MdEditor, MdPreview } from 'md-editor-v3'
import 'md-editor-v3/lib/preview.css'
import 'md-editor-v3/lib/style.css'
import { api } from '@/lib/ipc'
import { message } from '@/lib/notice'
import { useCollectionStore } from '@/stores/collection'
import type { DocEntry } from '@/types'

const props = defineProps<{ uid: string }>()
const emit = defineEmits<{ close: []; saved: [] }>()
const { t, locale } = useI18n()
const coll = useCollectionStore()

const doc = ref<DocEntry | null>(null)
const content = ref('')
const name = ref('')
const mode = ref<'edit' | 'preview' | 'split'>('split')
const readonly = computed(() => coll.isReadOnly)
const mdLanguage = computed(() => (locale.value === 'en-US' ? 'en-US' : 'zh-CN'))

watch(
  () => props.uid,
  async (uid) => {
    if (!uid) return
    try {
      doc.value = await api.readDoc(uid)
      content.value = doc.value?.content ?? ''
      name.value = doc.value?.name ?? ''
    } catch (e) {
      message.error(e instanceof Error ? e.message : String(e))
    }
  },
  { immediate: true },
)

async function save(): Promise<void> {
  if (!doc.value || readonly.value) return
  try {
    doc.value.content = content.value
    doc.value.name = name.value
    await api.saveDoc(doc.value)
    message.success(t('common.saved'))
    emit('saved')
  } catch (e) {
    message.error(e instanceof Error ? e.message : String(e))
  }
}
</script>

<template>
  <div class="doc-editor">
    <div class="hd">
      <n-input
        v-model:value="name"
        size="small"
        class="name"
        :readonly="readonly"
        data-testid="doc.name"
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
      <n-button v-if="!readonly" size="tiny" type="primary" data-testid="doc.save" @click="save">
        {{ t('common.save') }}
      </n-button>
      <n-button size="tiny" quaternary @click="emit('close')">×</n-button>
    </div>
    <div class="body" :class="mode">
      <md-editor
        v-if="mode !== 'preview'"
        v-model="content"
        :language="mdLanguage"
        :theme="mode === 'split' ? 'light' : 'light'"
        class="md-edit"
        :readonly="readonly"
      />
      <md-preview v-if="mode !== 'edit'" :model-value="content" class="md-preview" />
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
  flex: 1;
  min-height: 0;
  display: flex;
}

.body.edit .md-edit,
.body.preview .md-preview {
  flex: 1;
  min-width: 0;
}

.body.split .md-edit,
.body.split .md-preview {
  flex: 1;
  min-width: 0;
}

.body.split .md-edit {
  border-right: 1px solid var(--app-border);
}

.md-edit,
.md-preview {
  height: 100%;
  overflow: auto;
}
</style>

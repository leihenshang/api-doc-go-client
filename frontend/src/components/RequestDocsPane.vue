<script setup lang="ts">
// Docs 分段（从 RequestEditor 拆出）：Markdown 说明的编辑/预览、「生成接口文档」与下载。
// 默认只读预览；生成结果先经 GenDocDialog 预览/编辑，再由用户选择覆盖、追加或下载。
import GenDocDialog from '@/components/GenDocDialog.vue'
import { buildApiDoc } from '@/lib/genDocs'
import { isGrpc } from '@/lib/grpc'
import { api } from '@/lib/ipc'
import { message } from '@/lib/notice'
import { rowsFromDoc } from '@/lib/responseFields'
import { isDark } from '@/lib/theme'
import { useCollectionStore } from '@/stores/collection'
import type { Tab } from '@/stores/tabs'
import { useTabsStore } from '@/stores/tabs'
import { MdEditor, MdPreview } from 'md-editor-v3'
import 'md-editor-v3/lib/style.css'
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{ tab: Tab }>()
/** 是否处于编辑态由父级持有（页内查找按它决定是否有可查找 textarea）。 */
const editing = defineModel<boolean>('editing', { default: false })

const tabs = useTabsStore()
const coll = useCollectionStore()
const { t, locale } = useI18n()
const readonly = computed(() => coll.isReadOnly)
// gRPC 请求也允许编辑 Docs，但不提供「生成接口文档」（buildApiDoc 面向 HTTP）
const canGenerate = computed(() => !isGrpc(props.tab.request))

const docs = computed({
  get: () => props.tab.request.docs,
  set: (v: string) => {
    props.tab.request.docs = v
    touch()
  },
})

function touch(): void {
  if (readonly.value) return
  tabs.touch(props.tab.key)
}

function toggleEditing(): void {
  editing.value = !editing.value
}

const hasDoc = computed(() => (docs.value ?? '').trim().length > 0)

const showGen = ref(false)
const genText = ref('')

/**
 * 生成接口文档（不直接覆写 Docs）：请求示例取 curl 代码；响应字段取**请求模型 RespDoc.params**
 * （侧栏 Resp 节点同一份；表格为空时不输出该章节）；响应示例从已保存示例里挑最新一个 JSON 体。
 */
async function genDocs(): Promise<void> {
  try {
    const curl = await api.generateCode('curl', coll.currentEnv, props.tab.request)
    const fields = rowsFromDoc(props.tab.request.respDoc)
    const examples = await api.listResponseExamples(props.tab.uid)
    const ex = examples.find((e) => !e.response.binary && /^[{[]/.test((e.response.body ?? '').trim()))
    genText.value = buildApiDoc({
      request: props.tab.request,
      curl,
      fields,
      exampleBody: ex?.response.body ?? '',
      labels: {
        desc: t('docs.desc'),
        uri: t('docs.uri'),
        method: t('docs.method'),
        headers: t('docs.headers'),
        params: t('docs.params'),
        body: t('docs.body'),
        bodyParams: t('docs.bodyParams'),
        reqExample: t('docs.reqExample'),
        resFields: t('docs.resFields'),
        resExample: t('docs.resExample'),
        note: t('docs.note'),
        colName: t('docs.colName'),
        colValue: t('docs.colValue'),
        colType: t('docs.colType'),
        colExample: t('docs.colExample'),
        colDesc: t('docs.colDesc'),
        colRequired: t('docs.colRequired'),
        requiredYes: t('docs.requiredYes'),
        requiredNo: t('docs.requiredNo'),
      },
    })
    showGen.value = true
  } catch (err) {
    message.error(err instanceof Error ? err.message : String(err))
  }
}

/** 对话框回传的落点：覆盖/追加只改 Docs（随自动保存落盘 + 同步）；下载走原生保存对话框。 */
function onGenApply(p: { mode: 'overwrite' | 'append' | 'download'; text: string }): void {
  if (p.mode === 'overwrite') {
    docs.value = p.text
  } else if (p.mode === 'append') {
    const cur = (docs.value ?? '').trim()
    docs.value = cur ? `${cur}\n\n${p.text}` : p.text
  } else {
    void downloadText(p.text)
  }
}

/** 把指定 Markdown 经原生保存对话框落盘为 .md（复用 SaveResponseBody 的写盘能力）。 */
async function downloadText(content: string): Promise<void> {
  try {
    const safeName = (props.tab.request.name || 'request').replace(/[\\/:*?"<>|]/g, '_')
    const path = await api.saveResponseBody(`${safeName}.md`, false, content)
    if (path) message.success(t('docs.downloaded', { path }))
  } catch (err) {
    message.error(err instanceof Error ? err.message : String(err))
  }
}

async function downloadDoc(): Promise<void> {
  await downloadText(docs.value ?? '')
}

const mdLanguage = computed(() => (locale.value === 'en-US' ? 'en-US' : 'zh-CN'))
// md-editor 自带明暗两套配色；暗色下用 github 预览主题（默认主题在深底上对比度不足）
const mdTheme = computed(() => (isDark.value ? 'dark' : 'light'))
const mdPreviewTheme = computed(() => (isDark.value ? 'github' : 'default'))
</script>

<template>
  <div class="docs-pane">
    <div class="docs-bar">
      <button class="gin" type="button" data-testid="docs.edit" @click="toggleEditing">
        {{ editing ? t('docs.view') : t('docs.edit') }}
      </button>
      <template v-if="canGenerate">
        <button class="gin" type="button" data-testid="docs.gen" :disabled="readonly" @click="genDocs">
          {{ t('docs.gen') }}
        </button>
        <button class="gin" type="button" data-testid="docs.download" @click="downloadDoc">
          {{ t('docs.download') }}
        </button>
      </template>
    </div>
    <md-editor v-if="editing" v-model="docs" :language="mdLanguage" :theme="mdTheme"
      :preview-theme="mdPreviewTheme" class="md-edit" />
    <template v-else>
      <md-preview v-if="hasDoc" :model-value="docs" :language="mdLanguage" :theme="mdTheme"
        :preview-theme="mdPreviewTheme" class="md-preview" />
      <div v-else class="docs-empty">
        <p>{{ t('docs.noDoc') }}</p>
      </div>
    </template>

    <!-- 生成接口文档结果弹窗（预览/编辑 + 覆盖/追加/下载） -->
    <gen-doc-dialog v-model:show="showGen" :text="genText" @apply="onGenApply" />
  </div>
</template>

<style scoped>
/* Docs 区：顶部工具条（「生成接口文档」）+ 编辑器纵向撑满 */
.docs-pane {
  flex: 1 1 auto;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.docs-bar {
  flex: 0 0 auto;
  display: flex;
  justify-content: flex-end;
  gap: 6px;
  margin-bottom: 6px;
}

.docs-pane .md-edit {
  flex: 1 1 auto;
  min-height: 0;
}

/* 只读预览：撑满并滚动在自身容器内；给最小高度兜底，避免 flex 高度塌陷时预览整体消失 */
.docs-pane .md-preview {
  flex: 1 1 auto;
  min-height: 0;
  height: 100%;
  overflow: auto;
  border: 1px solid var(--app-border);
  border-radius: 6px;
  padding: 12px 16px;
}

/* 预览态没有内容时的空态提示 */
.docs-pane .docs-empty {
  flex: 1 1 auto;
  display: flex;
  align-items: center;
  justify-content: center;
  border: 1px dashed var(--app-border);
  border-radius: 6px;
  min-height: 160px;
}

.docs-pane .docs-empty p {
  margin: 0;
  font-size: 13px;
  color: var(--app-muted);
}

/* Docs 区（尤其左右布局区间较窄时）顶部工具栏允许换行，避免右侧按钮被截断 */
.docs-pane :deep(.md-editor-toolbar) {
  flex-wrap: wrap;
}

.gin {
  border: 1px solid var(--app-accent-tint);
  background: var(--app-accent-tint);
  color: var(--app-accent-dark);
  border-radius: 6px;
  font-size: 12px;
  font-family: inherit;
  padding: 4px 10px;
  cursor: pointer;
}

.gin:hover:not(:disabled) {
  border-color: var(--app-accent);
}

.gin:disabled {
  opacity: 0.6;
  cursor: default;
}
</style>

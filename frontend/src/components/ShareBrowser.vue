<script setup lang="ts">
// U5：只读打开分享链接。
//
// 定位：分享是「给非成员的只读分发通道」，所以这里**不建集合、不落盘、不参与同步**，
// 数据只在内存里。为了让它真的有用，支持两件事：
//   ① 一键复制 cURL（贴到终端就能跑）；
//   ② 直接发送（用客户端的发送链路，变量/脚本/断言一致），响应只在这里展示。
// 发送走 App.SendSharedRequest，不写本地历史（这条请求不属于任何集合）。
import { api } from '@/lib/ipc'
import { message } from '@/lib/notice'
import type { ShareApi, ShareBundle, ShareDoc } from '@/types'
import { CopyOutline, LinkOutline, PaperPlaneOutline } from '@vicons/ionicons5'
import { MdPreview } from 'md-editor-v3'
import 'md-editor-v3/lib/style.css'
import { NButton, NIcon, NInput, NModal, NSpin, NTag } from 'naive-ui'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{ 'update:show': [v: boolean] }>()
const { t } = useI18n()

const url = ref('')
const loading = ref(false)
const bundle = ref<ShareBundle | null>(null)
const pickedKey = ref('')
const sending = ref(false)
/** 发送结果（分享场景不落盘，只在这里展示） */
const resp = ref<{ status: number; body: string; cost: number } | null>(null)

watch(
  () => props.show,
  (v) => {
    if (!v) {
      bundle.value = null
      pickedKey.value = ''
      resp.value = null
    }
  },
)

interface Row {
  key: string
  depth: number
  kind: 'folder' | 'api' | 'doc'
  title: string
  method?: string
  api?: ShareApi
  doc?: ShareDoc
}

/** 树先序扁平化：分组按 priority 排序，同级接口/文档按 priority 排序。 */
const rows = computed<Row[]>(() => {
  const b = bundle.value
  if (!b) return []
  const out: Row[] = []
  const byGroup = (gid: number, depth: number): void => {
    const key = (x: { priority: number; id: number }) => x.priority * 1e6 + x.id
    const apis = (b.apis ?? []).filter((a) => a.group_id === gid).sort((x, y) => key(x) - key(y))
    const docs = (b.docs ?? []).filter((d) => d.group_id === gid).sort((x, y) => key(x) - key(y))
    for (const a of apis) {
      out.push({ key: `a-${a.id}`, depth, kind: 'api', title: a.api_name || a.url, method: a.http_method_type, api: a })
    }
    for (const d of docs) {
      out.push({ key: `d-${d.id}`, depth, kind: 'doc', title: d.icon ? `${d.icon} ${d.title}` : d.title, doc: d })
    }
    for (const g of (b.groups ?? []).filter((x) => x.p_id === gid).sort((x, y) => key(x) - key(y))) {
      out.push({ key: `g-${g.id}`, depth, kind: 'folder', title: g.title })
      byGroup(g.id, depth + 1)
    }
  }
  byGroup(0, 0)
  return out
})

const picked = computed(() => rows.value.find((r) => r.key === pickedKey.value) ?? null)

/** 服务端各富字段是 JSON 文本，解析失败按「未设置」处理（脆弱数据不该让面板崩掉）。 */
function parseList(raw: string): { name: string; value: string; enabled: boolean; description?: string }[] {
  try {
    const v = JSON.parse(raw || '[]')
    return Array.isArray(v) ? v : []
  } catch {
    return []
  }
}

const pickedParams = computed(() => (picked.value?.api ? parseList(picked.value.api.http_request_params) : []))
const pickedHeaders = computed(() => (picked.value?.api ? parseList(picked.value.api.http_request_header) : []))

/** 生成可粘贴执行的 cURL（与客户端「导出 cURL」同形，够用且不引入依赖）。 */
const curl = computed(() => {
  const a = picked.value?.api
  if (!a) return ''
  const quote = (s: string): string => `'${s.replace(/'/g, `'\\''`)}'`
  const parts = [`curl -X ${(a.http_method_type || 'GET').toUpperCase()} ${quote(a.url)}`]
  for (const h of pickedHeaders.value) {
    if (h.enabled && h.name) parts.push(`-H ${quote(`${h.name}: ${h.value}`)}`)
  }
  if (a.http_request_body) parts.push(`--data-raw ${quote(a.http_request_body)}`)
  return parts.join(' \\\n  ')
})

async function open(): Promise<void> {
  if (!url.value.trim()) {
    message.warning(t('share.needUrl'))
    return
  }
  loading.value = true
  try {
    bundle.value = await api.fetchShare(url.value.trim())
    pickedKey.value = ''
    resp.value = null
    // 默认选中第一条接口，省一次点击
    const first = rows.value.find((r) => r.kind === 'api')
    if (first) pickedKey.value = first.key
  } catch (e) {
    bundle.value = null
    message.error(e instanceof Error ? e.message : String(e))
  } finally {
    loading.value = false
  }
}

async function copyCurl(): Promise<void> {
  if (!curl.value) return
  await navigator.clipboard.writeText(curl.value)
  message.success(t('common.copied'))
}

async function send(): Promise<void> {
  const a = picked.value?.api
  if (!a) return
  sending.value = true
  resp.value = null
  try {
    const r = await api.sendSharedRequest(a, '')
    resp.value = { status: r.status, body: r.body, cost: r.timeMs ?? 0 }
  } catch (e) {
    message.error(e instanceof Error ? e.message : String(e))
  } finally {
    sending.value = false
  }
}

function close(): void {
  emit('update:show', false)
}
</script>

<template>
  <n-modal :show="show" preset="card" :title="t('share.title')" :style="{ width: bundle ? '1000px' : '560px' }" data-testid="share.dialog"
    @update:show="close">
    <div class="top">
      <n-input v-model:value="url" size="small" :placeholder="t('share.urlPlaceholder')" data-testid="share.url"
        @keydown.enter="open" />
      <n-button size="small" type="primary" :loading="loading" data-testid="share.open" @click="open">
        <template #icon>
          <n-icon :component="LinkOutline" :size="14" />
        </template>
        {{ t('share.open') }}
      </n-button>
    </div>

    <n-spin :show="loading">
      <div v-if="bundle" class="wrap">
        <!-- 左：只读树 -->
        <div class="tree" data-testid="share.tree">
          <div class="hd">
            <b>{{ bundle.project.title }}</b>
            <span v-if="bundle.project.version" class="ver">{{ bundle.project.version }}</span>
          </div>
          <p v-if="bundle.project.description" class="desc">{{ bundle.project.description }}</p>
          <div v-for="r in rows" :key="r.key" class="row" :class="{ on: r.key === pickedKey, folder: r.kind === 'folder' }"
            :style="{ paddingLeft: `${8 + r.depth * 12}px` }" @click="r.kind !== 'folder' && (pickedKey = r.key)">
            <template v-if="r.kind === 'folder'">
              <span class="nm">{{ r.title }}</span>
            </template>
            <template v-else>
              <span v-if="r.method" class="m">{{ r.method }}</span>
              <span class="nm">{{ r.title }}</span>
            </template>
          </div>
          <p v-if="!rows.length" class="muted">{{ t('share.empty') }}</p>
        </div>

        <!-- 右：详情 / 文档 / 响应 -->
        <div class="detail">
          <template v-if="picked?.api">
            <div class="line">
              <span class="m">{{ picked.api.http_method_type }}</span>
              <code class="u">{{ picked.api.url }}</code>
              <n-tag v-if="picked.api.status === 3" size="small" type="error">{{ t('editor.statusDeprecated') }}</n-tag>
            </div>
            <p v-if="picked.api.http_request_summary" class="sum">{{ picked.api.http_request_summary }}</p>

            <div v-if="pickedParams.filter((p) => p.enabled && p.name).length" class="sec">
              <h4>{{ t('share.params') }}</h4>
              <div v-for="(p, i) in pickedParams.filter((x) => x.enabled && x.name)" :key="i" class="kv">
                <code>{{ p.name }}</code><span class="v">{{ p.value }}</span>
                <span v-if="p.description" class="d">{{ p.description }}</span>
              </div>
            </div>
            <div v-if="pickedHeaders.filter((h) => h.enabled && h.name).length" class="sec">
              <h4>{{ t('share.headers') }}</h4>
              <div v-for="(h, i) in pickedHeaders.filter((x) => x.enabled && x.name)" :key="i" class="kv">
                <code>{{ h.name }}</code><span class="v">{{ h.value }}</span>
              </div>
            </div>
            <div v-if="picked.api.http_request_body" class="sec">
              <h4>{{ t('share.body') }}</h4>
              <pre class="body">{{ picked.api.http_request_body }}</pre>
            </div>
            <div v-if="picked.api.description" class="sec">
              <h4>{{ t('share.docs') }}</h4>
              <div class="md docs-pane">
                <md-preview :model-value="picked.api.description" language="zh-CN" class="md-preview" />
              </div>
            </div>

            <div class="acts">
              <n-button size="small" secondary :disabled="!curl" data-testid="share.copyCurl" @click="copyCurl">
                <template #icon><n-icon :component="CopyOutline" :size="14" /></template>
                {{ t('share.copyCurl') }}
              </n-button>
              <n-button size="small" type="primary" :loading="sending" data-testid="share.send" @click="send">
                <template #icon><n-icon :component="PaperPlaneOutline" :size="14" /></template>
                {{ t('share.send') }}
              </n-button>
            </div>

            <div v-if="resp" class="sec" data-testid="share.resp">
              <h4>{{ t('share.response') }} · {{ resp.status }} · {{ resp.cost }}ms</h4>
              <pre class="body">{{ resp.body }}</pre>
            </div>
          </template>

          <template v-else-if="picked?.doc">
            <div class="line"><b>{{ picked.doc.title }}</b></div>
            <div class="md docs-pane">
              <md-preview :model-value="picked.doc.content" language="zh-CN" class="md-preview" />
            </div>
          </template>

          <p v-else class="muted">{{ t('share.pickHint') }}</p>
        </div>
      </div>
      <div v-else class="empty">
        <n-icon :component="LinkOutline" :size="36" class="empty-ic" />
        <p class="empty-t">{{ t('share.emptyTitle') }}</p>
        <p class="empty-d">{{ t('share.intro') }}</p>
      </div>
    </n-spin>
  </n-modal>
</template>

<style scoped>
.top {
  display: flex;
  gap: 8px;
  margin-bottom: 12px;
}

.wrap {
  display: flex;
  gap: 14px;
  min-height: 420px;
  max-height: 62vh;
}

.tree {
  width: 280px;
  flex: 0 0 auto;
  overflow: auto;
  border-right: 1px solid var(--app-border);
  padding-right: 10px;
}

.tree .hd {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 2px 8px 6px;
  font-size: 13px;
}

.tree .ver {
  color: var(--app-muted);
  font-size: 11px;
}

.tree .desc {
  margin: 0 8px 6px;
  color: var(--app-muted);
  font-size: 12px;
}

.row {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 3px 8px;
  border-radius: 5px;
  font-size: 12px;
  cursor: pointer;
  white-space: nowrap;
  overflow: hidden;
}

.row:hover {
  background: var(--app-surface-3);
}

.row.on {
  background: var(--app-accent-tint);
}

.row.folder {
  color: var(--app-muted);
  cursor: default;
}

.row .m {
  color: var(--app-accent);
  font-size: 10px;
  font-weight: 600;
}

.row .nm {
  overflow: hidden;
  text-overflow: ellipsis;
}

.detail {
  flex: 1;
  min-width: 0;
  overflow: auto;
}

.line {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}

.line .m {
  color: var(--app-accent);
  font-size: 11px;
  font-weight: 700;
}

.line .u {
  font-size: 12px;
  word-break: break-all;
}

.sum {
  margin: 0 0 10px;
  color: var(--app-muted);
  font-size: 12px;
}

.sec {
  margin-bottom: 12px;
}

.sec h4 {
  margin: 0 0 6px;
  font-size: 12px;
  color: var(--app-muted);
  font-weight: 600;
}

.kv {
  display: flex;
  gap: 8px;
  font-size: 12px;
  padding: 2px 0;
}

.kv code {
  color: var(--app-accent);
}

.kv .v {
  color: var(--app-text);
}

.kv .d {
  color: var(--app-muted);
}

.body {
  margin: 0;
  padding: 8px;
  border-radius: 5px;
  background: var(--app-surface-3);
  font-size: 12px;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 220px;
  overflow: auto;
}

.acts {
  display: flex;
  gap: 8px;
  margin: 4px 0 12px;
}

.muted {
  color: var(--app-muted);
  font-size: 12px;
}

.empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 6px;
  padding: 34px 24px 30px;
  text-align: center;
}

.empty-ic {
  color: var(--app-accent);
  opacity: 0.55;
}

.empty-t {
  margin: 0;
  font-size: 13px;
  font-weight: 600;
}

.empty-d {
  margin: 0;
  max-width: 420px;
  color: var(--app-muted);
  font-size: 12px;
  line-height: 1.7;
}
</style>

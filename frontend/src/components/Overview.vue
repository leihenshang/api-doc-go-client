<script setup lang="ts">
// 集合概览（Collection tab 的内容）：集合概况 + 常用快捷键 + 空集合时的六大特性卡。
// 接口清单已移除：集合下的接口由左侧集合树承担展示与入口，概览页不再重复列一份。
import { NButton, NIcon } from 'naive-ui'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import FeatureCards from '@/components/FeatureCards.vue'
import { message } from '@/lib/notice'
import {
  ArchiveOutline,
  CloudDownloadOutline,
  CloudUploadOutline,
  CopyOutline,
  PlayOutline,
} from '@vicons/ionicons5'
import type { CollectionInfo, TreeNode } from '@/types'

const props = defineProps<{ info: CollectionInfo | null }>()
// 集合概要页承载的常用操作：新建请求 + 导入接口 / 导出 MD / 导出 HTML / 导出集合包 / 本地 Mock
// （原先散落在集合切换下拉里，现统一收拢到这里，入口更聚焦）。
const emit = defineEmits<{
  'new-request': []
  import: []
  export: [format: 'markdown' | 'html' | 'apidocgo']
  mock: []
}>()
const { t } = useI18n()

function countRequests(nodes: TreeNode[]): number {
  let n = 0
  for (const x of nodes) {
    if (x.type === 'request') n += 1
    else if (x.children) n += countRequests(x.children)
  }
  return n
}

const requests = computed(() => countRequests(props.info?.tree ?? []))

const envs = computed(() => props.info?.envs.length ?? 0)

const stats = computed(() => [
  { key: 'dir', label: t('overview.dir'), value: props.info?.dir ?? '-' },
  { key: 'requests', label: t('overview.requests'), value: String(requests.value) },
  { key: 'envs', label: t('overview.envs'), value: String(envs.value) },
  { key: 'files', label: t('overview.files'), value: t('overview.filesValue') },
])

// 只留四个最高频的：其余（环境/关闭页签/撤销/重做）在 Ctrl+K 面板与按钮 tooltip 里都有，
// 常驻 8 组会在首屏堆成两行、把「刚打开的集合」挤下去（UI-02）。
/** 目录是最需要「完整可读 + 可粘贴」的信息：卡片宽度有限，给一个复制入口兜住（UI-08）。 */
async function copyDir(): Promise<void> {
  const dir = props.info?.dir ?? ''
  if (!dir) return
  try {
    await navigator.clipboard.writeText(dir)
    message.success(t('overview.dirCopied'))
  } catch {
    message.error(t('overview.copyFailed'))
  }
}

const shortcuts = computed(() => [
  { keys: ['Ctrl', 'K'], label: t('overview.paletteHint') },
  { keys: ['Ctrl', 'Enter'], label: t('overview.send') },
  { keys: ['Ctrl', 'N'], label: t('overview.newRequest') },
  { keys: ['Ctrl', 'S'], label: t('overview.save') },
])
</script>

<template>
  <div class="overview">
    <section class="head">
      <h1 class="ttl">{{ info?.name ?? t('overview.title') }}</h1>
      <div class="stats">
        <div v-for="s in stats" :key="s.key" class="st">
          <span class="sl">{{ s.label }}</span>
          <span class="sv mono" :title="s.value">{{ s.value }}</span>
          <button v-if="s.key === 'dir'" class="copy" type="button" :title="t('overview.copyDir')"
            data-testid="overview.copyDir" @click="copyDir">
            <n-icon :component="CopyOutline" :size="12" />
          </button>
        </div>
      </div>
      <div class="acts">
        <n-button class="act" size="medium" type="primary" data-testid="overview.newRequest" @click="emit('new-request')">
          {{ t('overview.newRequest') }}
        </n-button>
        <n-button class="act" size="medium" secondary data-testid="overview.import" @click="emit('import')">
          <template #icon>
            <n-icon :component="CloudDownloadOutline" :size="14" />
          </template>
          {{ t('import.title') }}
        </n-button>
        <n-button class="act" size="medium" secondary data-testid="overview.exportMd" @click="emit('export', 'markdown')">
          <template #icon>
            <n-icon :component="CloudUploadOutline" :size="14" />
          </template>
          {{ t('export.markdown') }}
        </n-button>
        <n-button class="act" size="medium" secondary data-testid="overview.exportHtml" @click="emit('export', 'html')">
          <template #icon>
            <n-icon :component="CloudUploadOutline" :size="14" />
          </template>
          {{ t('export.html') }}
        </n-button>
        <!-- 集合整包：目录 + 请求 + 文档，可被另一个 api-doc-go 客户端直接导入（item 26） -->
        <n-button class="act" size="medium" secondary data-testid="overview.exportBundle" @click="emit('export', 'apidocgo')">
          <template #icon>
            <n-icon :component="ArchiveOutline" :size="14" />
          </template>
          {{ t('export.bundle') }}
        </n-button>
        <n-button class="act" size="medium" secondary data-testid="overview.mock" @click="emit('mock')">
          <template #icon>
            <n-icon :component="PlayOutline" :size="14" />
          </template>
          {{ t('mock.title') }}
        </n-button>
      </div>
      <ul v-if="shortcuts.length" class="sc">
        <li v-for="s in shortcuts" :key="s.label">
          <kbd v-for="k in s.keys" :key="k">{{ k }}</kbd>
          <span>{{ s.label }}</span>
        </li>
      </ul>
    </section>

    <!-- 空集合：展示特性卡，帮第一次打开的人认识产品；有接口时概览页只留概况/操作，接口交给左侧树 -->
    <feature-cards v-if="requests === 0" />
  </div>
</template>

<style scoped>
.overview {
  flex: 1 1 auto;
  overflow: auto;
  background: var(--app-bg);
}

.head {
  padding: 20px 24px 4px;
}

.ttl {
  margin: 0 0 14px;
  font-size: 18px;
  font-weight: 700;
  color: var(--app-text);
}

.stats {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
  max-width: 900px;
}

.st {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 10px 12px;
  border: 1px solid var(--app-border);
  border-radius: 8px;
  background: var(--app-panel);
}

.st .copy {
  position: absolute;
  top: 6px;
  right: 6px;
  display: none;
  align-items: center;
  justify-content: center;
  width: 20px;
  height: 20px;
  padding: 0;
  border: none;
  border-radius: 4px;
  background: transparent;
  color: var(--app-muted);
  cursor: pointer;
}

.st:hover .copy {
  display: flex;
}

.st .copy:hover {
  background: var(--app-hover);
  color: var(--app-text);
}

.sl {
  font-size: 11px;
  color: var(--app-muted);
}

.sv {
  font-size: 12.5px;
  color: var(--app-text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.acts {
  margin-top: 14px;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
}

/* 操作按钮放大：medium 基础高度 34px，再抬到 38px 并加宽横向内边距，使其更醒目；
   accent 绿配色与圆角沿用 client 默认（不改文案/emit/点击行为）。 */
.acts :deep(.act) {
  min-height: 38px;
  padding-left: 22px;
  padding-right: 22px;
  font-size: 14px;
  font-weight: 500;
}

.sc {
  list-style: none;
  margin: 14px 0 0;
  padding: 0;
  display: flex;
  gap: 18px;
}

.sc li {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: var(--app-muted);
}

kbd {
  border: 1px solid var(--app-border-strong);
  border-bottom-width: 2px;
  border-radius: 4px;
  padding: 0 6px;
  font-family: var(--app-mono);
  font-size: 11px;
  background: var(--app-panel);
  color: var(--app-text-2);
}
</style>

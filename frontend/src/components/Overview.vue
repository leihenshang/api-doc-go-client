<script setup lang="ts">
// 集合概览（Collection tab 的内容）：集合概况 + 常用快捷键 + 六大特性卡。
import { NButton } from 'naive-ui'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import FeatureCards from '@/components/FeatureCards.vue'
import type { CollectionInfo, TreeNode } from '@/types'

const props = defineProps<{ info: CollectionInfo | null }>()
const emit = defineEmits<{ 'new-request': [] }>()
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

const shortcuts = computed(() => [
  { keys: ['Ctrl', 'K'], label: t('overview.paletteHint') },
  { keys: ['Ctrl', 'Enter'], label: t('overview.send') },
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
        </div>
      </div>
      <div class="acts">
        <n-button size="small" type="primary" @click="emit('new-request')">
          {{ t('overview.newRequest') }}
        </n-button>
      </div>
      <ul v-if="shortcuts.length" class="sc">
        <li v-for="s in shortcuts" :key="s.label">
          <kbd v-for="k in s.keys" :key="k">{{ k }}</kbd>
          <span>{{ s.label }}</span>
        </li>
      </ul>
    </section>

    <feature-cards />
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
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 10px 12px;
  border: 1px solid var(--app-border);
  border-radius: 8px;
  background: var(--app-panel);
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

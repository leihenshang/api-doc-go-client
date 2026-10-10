<script setup lang="ts">
// 单个冲突的折叠卡片：收起时仅名称（+ rev），点击展开后才显示差异与二选一按钮。
import type { ConflictDetail } from '@/types'
import {
  ChevronDownOutline,
  ChevronForwardOutline,
} from '@vicons/ionicons5'
import { NButton, NIcon } from 'naive-ui'
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{ detail: ConflictDetail; busy: boolean }>()
const emit = defineEmits<{ resolve: [choice: 'local' | 'remote'] }>()
const { t } = useI18n()

/** 默认收起：列表里只看到名称；点开才加载差异内容 */
const open = ref(false)

function fieldLabel(f: string): string {
  return t(`sync.field_${f}`)
}

// 值统一加引号展示，形如 URL："/old" => "/new"；空值即 ""，空 / 非空一眼可辨。
function quote(v: string): string {
  return `"${v}"`
}

const createdLabel = computed(() => new Date(props.detail.createdAt).toLocaleString())
</script>

<template>
  <section class="cr" :class="{ open }" data-testid="conflict.card">
    <!-- 折叠头（整行可点） -->
    <button class="cr-hd" type="button" data-testid="conflict.toggle" :title="t('sync.expandHint')"
      @click="open = !open">
      <n-icon :component="open ? ChevronDownOutline : ChevronForwardOutline" :size="13" class="car" />
      <span class="cr-name">{{ detail.name || detail.file }}</span>
      <span class="cr-rev">rev {{ detail.serverRev }}</span>
      <span v-if="!detail.hasPayload" class="tag tag-old">{{ t('sync.tagOld') }}</span>
    </button>

    <!-- 展开内容 -->
    <div v-show="open" class="cr-body">
      <!-- 旧版本遗留副本：没有服务器快照，只能保留本地 -->
      <p v-if="!detail.hasPayload" class="cr-old" data-testid="conflict.old">
        {{ t('sync.oldConflictHint') }}
      </p>

      <!-- 有差异的字段 -->
      <ul v-else class="cr-diffs">
        <li v-for="d in detail.diffs" :key="d.field" class="dif" data-testid="conflict.field">
          <div class="dif-label">{{ fieldLabel(d.field) }}</div>
          <div class="dif-vals">
            <span class="v v-local" data-testid="conflict.value.local">{{ quote(d.local) }}</span>
            <span class="arrow">=></span>
            <span class="v v-server" data-testid="conflict.value.server">{{ quote(d.server) }}</span>
          </div>
        </li>
      </ul>

      <footer class="cr-ft">
        <span class="cr-time">{{ createdLabel }}</span>
        <span class="sp" />
        <n-button size="tiny" :loading="busy" data-testid="conflict.local" @click="emit('resolve', 'local')">
          {{ t('sync.keepLocal') }}
        </n-button>
        <n-button size="tiny" type="primary" :disabled="!detail.hasPayload" :loading="busy"
          data-testid="conflict.remote" @click="emit('resolve', 'remote')">
          {{ t('sync.keepRemote') }}
        </n-button>
      </footer>
    </div>
  </section>
</template>

<style scoped>
.cr {
  border: 1px solid var(--app-border);
  border-radius: 8px;
  background: var(--app-panel);
}

.cr.open {
  border-color: var(--app-border-strong);
}

/* ---- 折叠头 ---- */
.cr-hd {
  display: flex;
  align-items: center;
  gap: 7px;
  width: 100%;
  min-height: 30px;
  padding: 4px 10px;
  border: none;
  background: none;
  font-family: inherit;
  text-align: left;
  cursor: pointer;
}

.cr-hd:hover {
  background: var(--app-hover-soft);
}

.cr.open .cr-hd {
  border-bottom: 1px solid var(--app-border);
}

.car {
  flex: 0 0 auto;
  color: var(--app-muted);
}

.cr-name {
  font-size: 13px;
  font-weight: 600;
  color: var(--app-text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.cr-rev {
  font-family: var(--app-mono);
  font-size: 11px;
  color: var(--app-muted);
}

.tag {
  flex: 0 0 auto;
  font-size: 10px;
  line-height: 15px;
  padding: 0 6px;
  border-radius: 999px;
}

.tag-old {
  color: var(--app-warn);
  background: var(--app-warn-tint);
}

/* ---- 展开内容 ---- */
.cr-body {
  padding: 10px;
}

/* legacy 提示：独立底色块、留足上下空间，避免被压扁看不全 */
.cr-old {
  margin: 0;
  padding: 10px 12px;
  border-radius: 6px;
  background: var(--app-warn-tint);
  color: var(--app-warn);
  font-size: 12px;
  line-height: 1.7;
}

.cr-diffs {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.dif {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.dif-label {
  font-size: 11px;
  font-weight: 500;
  color: var(--app-muted);
}

.dif-vals {
  display: flex;
  align-items: flex-start;
  gap: 8px;
}

.v {
  flex: 1;
  min-width: 0;
  /* 允许 flex 子项收缩，长文本才能换行而不撑破布局 */
  padding: 5px 8px;
  border-radius: 6px;
  font-family: var(--app-mono);
  font-size: 12px;
  line-height: 1.5;
  white-space: pre-wrap;
  /* 保留 docs 里的换行 */
  word-break: break-all;
  /* 长 URL / JSON 不溢出 */
  max-height: 90px;
  /* 超长文本限高，块内滚动，不把卡片拉得过长 */
  overflow-y: auto;
}

.v-local {
  color: var(--app-danger);
  background: var(--app-danger-tint);
}

.v-server {
  color: var(--app-accent-dark);
  background: var(--app-accent-tint);
}

.arrow {
  flex-shrink: 0;
  padding-top: 5px;
  font-family: var(--app-mono);
  font-size: 12px;
  color: var(--app-muted);
}

.cr-ft {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 10px;
}

.cr-time {
  font-size: 11px;
  color: var(--app-muted);
  white-space: nowrap;
}

.sp {
  flex: 1;
}
</style>

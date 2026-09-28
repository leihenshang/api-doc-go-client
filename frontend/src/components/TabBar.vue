<script setup lang="ts">
// 请求标签栏（design-spec §2）：首个固定为 Collection 概览页，其后是各请求 tab。
// dot 表示有未落盘编辑，method 前置按语义色着色，响应状态码跟随 tab 展示。
import { NIcon } from 'naive-ui'
import { AddOutline, CloseOutline, LayersOutline } from '@vicons/ionicons5'
import { useI18n } from 'vue-i18n'
import MethodTag from '@/components/MethodTag.vue'
import type { Tab } from '@/stores/tabs'

defineProps<{ tabs: Tab[]; activeKey: string }>()
const emit = defineEmits<{
  select: [key: string]
  'select-overview': []
  close: [key: string]
  new: []
}>()
const { t } = useI18n()
</script>

<template>
  <div class="tabbar">
    <button class="tab coll" :class="{ active: !activeKey }" type="button" @click="emit('select-overview')">
      <n-icon :component="LayersOutline" :size="13" />
      <span class="tt">{{ t('tab.collection') }}</span>
    </button>

    <div class="tabs">
      <button
        v-for="tab in tabs"
        :key="tab.key"
        class="tab"
        :class="{ active: tab.key === activeKey }"
        type="button"
        :title="tab.request.path"
        @click="emit('select', tab.key)"
      >
        <method-tag :method="tab.request.method" />
        <span v-if="tab.dirty" class="dot" :title="t('tab.unsaved')" />
        <span class="tt">{{ tab.title }}</span>
        <span v-if="tab.response" class="st" :class="tab.response.status < 400 ? 'ok' : 'err'">
          {{ tab.response.status }}
        </span>
        <span class="x" :title="t('tab.closeHint')" @click.stop="emit('close', tab.key)">
          <n-icon :component="CloseOutline" :size="13" />
        </span>
      </button>
    </div>

    <button class="add" type="button" :title="t('tab.newHint')" @click="emit('new')">
      <n-icon :component="AddOutline" :size="16" />
    </button>
  </div>
</template>

<style scoped>
.tabbar {
  height: 38px;
  flex: 0 0 auto;
  display: flex;
  align-items: stretch;
  gap: 2px;
  padding: 0 8px;
  border-bottom: 1px solid var(--app-border);
  background: var(--app-panel);
}

.tabs {
  display: flex;
  align-items: stretch;
  gap: 2px;
  overflow-x: auto;
  scrollbar-width: none;
}

.tabs::-webkit-scrollbar {
  height: 0;
}

.tab {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  max-width: 230px;
  padding: 8px 10px;
  border: none;
  border-bottom: 2px solid transparent;
  background: transparent;
  font-family: inherit;
  font-size: 12.5px;
  color: var(--app-muted);
  cursor: pointer;
  white-space: nowrap;
}

.tab:hover {
  background: var(--app-row-hover);
}

.tab.active {
  color: var(--app-text);
  border-bottom-color: var(--app-accent);
}

.tab.coll {
  flex: 0 0 auto;
  border-right: 1px solid var(--app-border);
  padding-right: 14px;
}

.tab.coll .tt {
  font-weight: 500;
}

.tt {
  overflow: hidden;
  max-width: 140px;
  text-overflow: ellipsis;
}

.dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--app-accent);
  flex: 0 0 auto;
}

.st {
  font-family: var(--app-mono);
  font-size: 10px;
  padding: 0 4px;
  border-radius: 4px;
  line-height: 14px;
}

.st.ok {
  background: var(--app-accent-tint);
  color: var(--app-accent-dark);
}

.st.err {
  background: #fdecec;
  color: #d03050;
}

.x {
  color: var(--app-placeholder);
  border-radius: 3px;
  padding: 0 2px;
  line-height: 14px;
}

.x:hover {
  background: #e0e3e6;
  color: #d03050;
}

.add {
  border: none;
  background: transparent;
  font-size: 16px;
  color: var(--app-muted);
  cursor: pointer;
  padding: 0 10px;
  flex: 0 0 auto;
}

.add:hover {
  color: var(--app-accent);
}
</style>

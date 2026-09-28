<script setup lang="ts">
// 多请求标签栏：每 tab 一份草稿与响应；dot 表示有未落盘编辑，method 彩标前置。
import MethodTag from '@/components/MethodTag.vue'
import { useI18n } from 'vue-i18n'
import type { Tab } from '@/stores/tabs'

defineProps<{ tabs: Tab[]; activeKey: string }>()
const emit = defineEmits<{ select: [key: string]; close: [key: string]; new: [] }>()
const { t } = useI18n()
</script>

<template>
  <div class="tabbar">
    <div class="tabs">
      <button
        v-for="tab in tabs"
        :key="tab.key"
        class="tab"
        :class="{ active: tab.key === activeKey }"
        :title="tab.request.path"
        @click="emit('select', tab.key)"
      >
        <method-tag :method="tab.request.method" />
        <span v-if="tab.dirty" class="dot" :title="t('tab.unsaved')" />
        <span class="tt">{{ tab.title }}</span>
        <span
          v-if="tab.response"
          class="st"
          :class="tab.response.status < 400 ? 'ok' : 'err'"
        >{{ tab.response.status }}</span>
        <span class="x" :title="t('tab.closeHint')" @click.stop="emit('close', tab.key)">×</span>
      </button>
    </div>
    <button class="add" :title="t('tab.newHint')" @click="emit('new')">＋</button>
  </div>
</template>

<style scoped>
.tabbar {
  display: flex;
  align-items: stretch;
  gap: 2px;
  padding: 0 8px;
  border-bottom: 1px solid var(--app-border);
  background: var(--app-sidebar);
}

.tabs {
  display: flex;
  align-items: stretch;
  gap: 2px;
  overflow-x: auto;
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
  font-size: 12.5px;
  color: #555;
  cursor: pointer;
  white-space: nowrap;
}

.tab:hover {
  background: var(--app-row-hover);
}

.tab.active {
  background: var(--app-panel);
  border-bottom-color: var(--app-accent);
  color: var(--app-text);
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
  font-size: 10px;
  padding: 0 4px;
  border-radius: 3px;
  line-height: 14px;
}

.st.ok {
  background: #e8f6ee;
  color: #18a058;
}

.st.err {
  background: #fdecec;
  color: #d03050;
}

.x {
  color: var(--app-muted);
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
}

.add:hover {
  color: var(--app-accent);
}
</style>

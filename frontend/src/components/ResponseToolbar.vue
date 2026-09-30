<script setup lang="ts">
// 响应操作工具条（response-panel design-spec §3 第 3 排）：
// 左「视图→显示」：美化/原始 分段控件、展开/折叠全部、换行开关态胶囊；
// 右「解析→输出」：更新响应字段、竖分隔线、复制响应体（幽灵）、保存▾（唯一主按钮）。
import { NIcon } from 'naive-ui'
import { ChevronDownOutline, ChevronUpOutline, CopyOutline, RefreshOutline } from '@vicons/ionicons5'
import { useI18n } from 'vue-i18n'
import SaveDropdown from '@/components/SaveDropdown.vue'
import type { VarScope } from '@/lib/saveVars'

const view = defineModel<'pretty' | 'raw'>('view', { required: true })
const wrap = defineModel<boolean>('wrap', { required: true })

const props = defineProps<{
  isJson: boolean
  copied: boolean
  scope: VarScope
  canSaveExample: boolean
}>()
const emit = defineEmits<{
  'expand-all': []
  'collapse-all': []
  'update-fields': []
  'copy-body': []
  'update:scope': [v: VarScope]
  'save-file': []
  'save-all-vars': []
  'save-selected-var': []
  'save-example': []
}>()
const { t } = useI18n()

const showTreeOps = () => props.isJson && view.value === 'pretty'
</script>

<template>
  <div class="bar">
    <div class="grp">
      <div class="seg" role="group">
        <button
          class="seg-item"
          :class="{ on: view === 'pretty' }"
          type="button"
          data-testid="resp.pretty"
          @click="view = 'pretty'"
        >
          {{ t('resp.pretty') }}
        </button>
        <button
          class="seg-item"
          :class="{ on: view === 'raw' }"
          type="button"
          data-testid="resp.raw"
          @click="view = 'raw'"
        >
          {{ t('resp.raw') }}
        </button>
      </div>
      <template v-if="showTreeOps()">
        <button class="chip-btn" type="button" data-testid="resp.expandAll" :title="t('json.expandAll')" @click="emit('expand-all')">
          <n-icon :component="ChevronDownOutline" :size="13" />
          {{ t('json.expandAll') }}
        </button>
        <button class="chip-btn" type="button" data-testid="resp.collapseAll" :title="t('json.collapseAll')" @click="emit('collapse-all')">
          <n-icon :component="ChevronUpOutline" :size="13" />
          {{ t('json.collapseAll') }}
        </button>
      </template>
      <button
        class="chip-btn wrap-btn"
        :class="{ on: wrap }"
        type="button"
        data-testid="resp.wrap"
        :aria-pressed="wrap"
        @click="wrap = !wrap"
      >
        {{ t('resp.wrap') }}
      </button>
    </div>
    <!-- 输出组：更新响应字段 / 复制响应体 / 保存▾；行首插槽（示例下拉 + 删除）由 ResponsePanel 提供，
         固定排在本组最前面，即「复制响应体」这一排的开头；整条放不下时本组整体换到下一排 -->
    <div class="grp acts">
      <slot name="leading" />
      <button
        v-if="isJson"
        class="outline-btn"
        type="button"
        data-testid="resp.updateFields"
        :title="t('resp.updateFieldsHint')"
        @click="emit('update-fields')"
      >
        <n-icon :component="RefreshOutline" :size="13" />
        {{ t('resp.updateFields') }}
      </button>
      <span class="div" />
      <button class="ghost-btn" type="button" data-testid="resp.copyBody" @click="emit('copy-body')">
        <n-icon :component="CopyOutline" :size="13" />
        {{ copied ? t('common.copied') : t('resp.copyBody') }}
      </button>
      <save-dropdown
        :scope="props.scope"
        :can-save-example="props.canSaveExample"
        @update:scope="emit('update:scope', $event)"
        @save-file="emit('save-file')"
        @save-all-vars="emit('save-all-vars')"
        @save-selected-var="emit('save-selected-var')"
        @save-example="emit('save-example')"
      />
    </div>
  </div>
</template>

<style scoped>
/* 工具条宽度不足时整体换行（何况行首还有示例下拉），所以高度只能给 min-height：
   写死 height 会把换到第二排的按钮挤出可视区，压到边框和内容区上（视图错位）。 */
.bar {
  display: flex;
  align-items: center;
  justify-content: flex-start;
  gap: 8px 10px;
  min-height: 44px;
  padding: 5px 0;
  flex: 0 0 auto;
  border-bottom: 1px solid var(--app-border);
  flex-wrap: wrap;
}

/* 输出组靠右；换行后仍靠右，两排各自对齐不串位 */
.acts {
  margin-left: auto;
}

.grp {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

/* ① 美化/原始 分段控件：外框浅灰圆角 6，激活项白底描边圆角 4 绿字 */
.seg {
  display: inline-flex;
  align-items: center;
  gap: 2px;
  background: var(--app-chip);
  border-radius: 6px;
  padding: 2px;
  flex: 0 0 auto;
}

.seg-item {
  border: 1px solid transparent;
  background: none;
  border-radius: 4px;
  padding: 2px 12px;
  font-size: 11px;
  font-family: inherit;
  color: var(--app-muted);
  cursor: pointer;
  white-space: nowrap;
}

.seg-item.on {
  background: var(--app-panel);
  border-color: var(--app-border-strong);
  color: var(--app-accent);
  font-weight: 600;
}

/* ② 展开/折叠全部：灰底圆角 6 */
.chip-btn {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  border: none;
  background: var(--app-chip);
  border-radius: 6px;
  padding: 4px 9px;
  font-size: 11px;
  font-family: inherit;
  color: var(--app-text-2);
  cursor: pointer;
  white-space: nowrap;
  flex: 0 0 auto;
}

.chip-btn:hover {
  background: var(--app-chip-hover);
  color: var(--app-text);
}

/* ③ 换行：开关态胶囊（激活浅绿底深绿字） */
.wrap-btn.on {
  background: var(--app-accent-tint);
  color: var(--app-accent-dark);
  font-weight: 600;
}

/* ④ 更新响应字段：白底描边 */
.outline-btn {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  border: 1px solid var(--app-border);
  background: var(--app-panel);
  border-radius: 6px;
  padding: 4px 9px;
  font-size: 11px;
  font-family: inherit;
  color: var(--app-text-2);
  cursor: pointer;
  white-space: nowrap;
  flex: 0 0 auto;
}

.outline-btn:hover {
  border-color: var(--app-accent);
  color: var(--app-accent);
}

/* ⑤ 竖分隔线 1×16 */
.div {
  width: 1px;
  height: 16px;
  background: var(--app-border);
  flex: 0 0 auto;
}

/* ⑥ 复制响应体：幽灵按钮 */
.ghost-btn {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  border: none;
  background: none;
  border-radius: 6px;
  padding: 4px 7px;
  font-size: 11px;
  font-family: inherit;
  color: var(--app-text-2);
  cursor: pointer;
  white-space: nowrap;
  flex: 0 0 auto;
}

.ghost-btn:hover {
  background: var(--app-row-hover);
  color: var(--app-accent);
}
</style>

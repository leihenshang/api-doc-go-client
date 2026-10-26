<script setup lang="ts">
// 「响应字段」分段的字段表：与请求体表格同构 —— 5 列全可编辑
// （字段名 / 类型 / 必填 / 示例值 / 说明）+ 行尾删除 + 底部「新增行」。
//
// 表格真源是 respDoc.params（由「生成数据表格」/「从当前响应生成」派生，或用户手工增行）；
// 不再从「响应数据」自动派生。本组件不持有状态，所有修改只往父级 emit 补丁，
// 由父级写回 respDoc（含 required）。
import type { FieldRow } from '@/lib/responseFields'
import { NButton, NCheckbox, NIcon, NInput, NSelect } from 'naive-ui'
import { useI18n } from 'vue-i18n'

withDefaults(defineProps<{ rows: FieldRow[]; readonly?: boolean }>(), { readonly: false })
const emit = defineEmits<{
  update: [index: number, patch: Partial<FieldRow>]
  add: []
  remove: [index: number]
}>()
const { t } = useI18n()

/** 类型下拉选项：JSON 类型名（也允许手输，故 select 可过滤 + 可清空 + 可新增）。 */
const TYPE_OPTIONS = ['string', 'integer', 'number', 'boolean', 'array', 'object', 'null']
</script>

<template>
  <section class="fields">
    <div class="fh">
      <span class="ft">{{ t('resp.fields') }}<span class="num">{{ rows.length }}</span></span>
      <span class="fd">{{ t('resp.fieldsHint') }}</span>
    </div>

    <!-- 行区独立滚动：表格高度超过页签可视高度时只滚行区，表头（sticky）与底部新增按钮保持可见 -->
    <div class="rows">
      <template v-if="rows.length">
        <div class="tr th">
          <span>{{ t('body.colName') }}</span>
          <span>{{ t('body.colType') }}</span>
          <span class="c">{{ t('body.colRequired') }}</span>
          <span>{{ t('body.colExample') }}</span>
          <span>{{ t('body.colDesc') }}</span>
          <span></span>
        </div>

        <div v-for="(f, i) in rows" :key="i" class="tr" data-testid="resp.fields.row" :data-path="f.path">
          <n-input :value="f.path" size="small" data-testid="resp.fields.name" :disabled="readonly"
            :placeholder="t('body.namePh')" @update:value="emit('update', i, { path: $event })" />
          <n-select :value="f.type || null" size="small" filterable clearable tag :disabled="readonly"
            :options="TYPE_OPTIONS.map((v) => ({ label: v, value: v }))" data-testid="resp.fields.type"
            :placeholder="t('body.colType')" @update:value="emit('update', i, { type: $event ?? '' })" />
          <span class="c">
            <n-checkbox :checked="f.required" :disabled="readonly" data-testid="resp.fields.required"
              @update:checked="emit('update', i, { required: $event })" />
          </span>
          <n-input :value="f.value" size="small" data-testid="resp.fields.example" :disabled="readonly"
            :placeholder="t('body.colExample')" @update:value="emit('update', i, { value: $event })" />
          <n-input :value="f.meaning" size="small" data-testid="resp.fields.meaning" :disabled="readonly"
            :placeholder="t('common.optional')" @update:value="emit('update', i, { meaning: $event })" />
          <n-button size="tiny" quaternary type="error" data-testid="resp.fields.remove" :disabled="readonly"
            :title="t('body.removeRow')" @click="emit('remove', i)">
            <template #icon>
              <n-icon><svg viewBox="0 0 24 24" width="14" height="14"><path fill="currentColor"
                d="M9 3h6l1 2h4v2H4V5h4l1-2Zm-3 6h12l-1 12H7L6 9Z" /></svg></n-icon>
            </template>
          </n-button>
        </div>
      </template>

      <!-- 空态：表格不再从响应数据自动派生，只有点生成按钮或「新增行」才有内容 -->
      <p v-else class="fields-empty" data-testid="resp.fields.empty">{{ t('resp.fieldsEmpty') }}</p>
    </div>

    <div class="tf">
      <n-button size="tiny" data-testid="resp.fields.add" :disabled="readonly" @click="emit('add')">
        + {{ t('body.addRow') }}
      </n-button>
    </div>
  </section>
</template>

<style scoped>
.fields {
  display: flex;
  flex-direction: column;
  min-height: 0;
  border: 1px solid var(--app-border);
  border-radius: 8px;
  overflow: hidden;
}

.fh {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  padding: 8px 12px;
  border-bottom: 1px solid var(--app-border);
}

/* 行区：可用高度不足时在这里滚动（表头 sticky、底部新增按钮不滚走） */
.rows {
  flex: 1 1 auto;
  min-height: 0;
  overflow: auto;
}

.ft {
  font-size: 12.5px;
  font-weight: 600;
  color: var(--app-text);
}

.ft .num {
  margin-left: 6px;
  padding: 0 6px;
  border-radius: 999px;
  background: var(--app-chip);
  font-size: 11px;
  font-weight: 500;
  color: var(--app-muted);
}

.fd {
  font-size: 11.5px;
  color: var(--app-muted);
}

.tr {
  display: grid;
  grid-template-columns: minmax(0, 1.3fr) 108px 44px minmax(0, 1fr) minmax(0, 1.4fr) 32px;
  align-items: center;
  gap: 8px;
  padding: 5px 10px;
  border-bottom: 1px solid var(--app-border);
  font-size: 12px;
}

.tr.th {
  position: sticky;
  top: 0;
  z-index: 1;
  background: var(--app-surface-2);
  color: var(--app-muted);
  font-size: 11.5px;
}

.tr:last-child {
  border-bottom: none;
}

.c {
  display: flex;
  justify-content: center;
}

.fields-empty {
  margin: 0;
  padding: 10px 12px;
  border-bottom: 1px solid var(--app-border);
  font-size: 12px;
  color: var(--app-placeholder);
}

.tf {
  display: flex;
  justify-content: flex-start;
  padding: 6px 8px;
  border-top: 1px solid var(--app-border);
}
</style>

<script setup lang="ts">
// 「响应字段」页签的表格：字段 / 类型 / 含义 + 删除操作。
// 字段来自用户点击「更新响应字段」后的解析结果（由 ResponsePanel 持有并落 localStorage），
// 本组件只负责展示与编辑含义，不自行解析响应体。
import { NButton, NInput, NPopconfirm } from 'naive-ui'
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { FieldRow } from '@/lib/responseFields'

defineProps<{ rows: FieldRow[] }>()
const emit = defineEmits<{
  meaning: [path: string, value: string]
  remove: [path: string]
  removeMany: [paths: string[]]
  clear: []
}>()
const { t } = useI18n()

const checked = ref(new Set<string>())

function toggle(path: string): void {
  if (checked.value.has(path)) checked.value.delete(path)
  else checked.value.add(path)
  // 触发响应式更新
  checked.value = new Set(checked.value)
}

function removeOne(path: string): void {
  checked.value.delete(path)
  checked.value = new Set(checked.value)
  emit('remove', path)
}

function removeChecked(): void {
  if (!checked.value.size) return
  emit('removeMany', [...checked.value])
  checked.value = new Set()
}
</script>

<template>
  <section v-if="rows.length" class="fields">
    <div class="fh">
      <span class="ft">{{ t('resp.fields') }}<span class="num">{{ rows.length }}</span></span>
      <span class="fd">{{ t('resp.fieldsHint') }}</span>
      <span class="sp" />
      <n-popconfirm @positive-click="removeChecked">
        <template #trigger>
          <n-button
            v-if="checked.size"
            size="tiny"
            type="error"
            tertiary
            data-testid="resp.fields.removeChecked"
          >
            {{ t('resp.fieldsRemoveChecked', { n: checked.size }) }}
          </n-button>
        </template>
        {{ t('resp.fieldsRemoveConfirm', { n: checked.size }) }}
      </n-popconfirm>
      <n-popconfirm @positive-click="emit('clear')">
        <template #trigger>
          <n-button size="tiny" quaternary data-testid="resp.fields.clear">{{ t('resp.fieldsClear') }}</n-button>
        </template>
        {{ t('resp.fieldsClearConfirm') }}
      </n-popconfirm>
    </div>
    <div class="tr th">
      <span class="ckcol" />
      <span>{{ t('resp.field') }}</span>
      <span>{{ t('resp.type') }}</span>
      <span>{{ t('resp.meaning') }}</span>
      <span class="opcol" />
    </div>
    <div
      v-for="f in rows"
      :key="f.path"
      class="tr"
      data-testid="resp.fields.row"
      :data-path="f.path"
      :class="{ on: checked.has(f.path) }"
    >
      <span class="ckcol">
        <input type="checkbox" :checked="checked.has(f.path)" @change="toggle(f.path)" />
      </span>
      <span class="mono fp" data-testid="resp.fields.path" :title="f.path">{{ f.path }}</span>
      <span class="mono ftype">{{ f.type }}</span>
      <n-input
        :value="f.meaning"
        size="small"
        data-testid="resp.fields.meaning"
        :placeholder="t('common.optional')"
        @update:value="emit('meaning', f.path, $event)"
      />
      <span class="opcol">
        <button class="rm" type="button" :title="t('common.delete')" data-testid="resp.fields.remove" @click="removeOne(f.path)">
          ×
        </button>
      </span>
    </div>
  </section>

  <p v-else class="fields-empty">{{ t('resp.fieldsEmpty') }}</p>
</template>

<style scoped>
.fields {
  border: 1px solid var(--app-border);
  border-radius: 6px;
}

.fh {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 10px 6px;
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

.sp {
  flex: 1;
}

.tr {
  display: grid;
  grid-template-columns: 24px minmax(0, 1.2fr) 70px minmax(0, 1.6fr) 24px;
  align-items: center;
  gap: 8px;
  padding: 5px 8px;
  border-top: 1px solid var(--app-border);
  font-size: 12px;
}

.tr.th {
  background: var(--app-surface-2);
  color: var(--app-muted);
  font-size: 11.5px;
}

.tr.on {
  background: var(--app-info-tint);
}

.ckcol {
  display: flex;
  align-items: center;
  justify-content: center;
}

.ckcol input {
  cursor: pointer;
}

.fp {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--app-code-key);
}

.ftype {
  color: var(--app-muted);
}

.opcol {
  display: flex;
  align-items: center;
  justify-content: center;
}

.rm {
  border: none;
  background: none;
  color: var(--app-placeholder);
  cursor: pointer;
  font-size: 15px;
  line-height: 1;
  padding: 0;
}

.rm:hover {
  color: var(--app-danger, #d03050);
}

.fields-empty {
  margin: 12px 0 0;
  font-size: 12.5px;
  color: var(--app-placeholder);
}
</style>

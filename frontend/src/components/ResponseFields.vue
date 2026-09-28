<script setup lang="ts">
// 响应字段映射（design-spec 画板四）：把 JSON 响应摊平成「字段 / 类型 / 含义」表。
// 「含义」是本地标注（按请求 uid 存 localStorage），不写回集合文件，避免污染 round-trip。
import { NInput } from 'naive-ui'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{ uid: string; text: string }>()
const { t } = useI18n()

const MAX_FIELDS = 200
const MAX_DEPTH = 4

interface Field {
  path: string
  type: string
}

const meanings = ref<Record<string, string>>({})

const storeKey = computed(() => `client.fieldmap.${props.uid}`)

watch(
  storeKey,
  (k) => {
    try {
      meanings.value = JSON.parse(localStorage.getItem(k) ?? '{}') as Record<string, string>
    } catch {
      meanings.value = {}
    }
  },
  { immediate: true },
)

function persist(): void {
  localStorage.setItem(storeKey.value, JSON.stringify(meanings.value))
}

function setMeaning(path: string, value: string): void {
  meanings.value = { ...meanings.value, [path]: value }
}

function typeName(v: unknown): string {
  if (v === null) return 'null'
  if (Array.isArray(v)) return 'array'
  return typeof v
}

function childEntries(v: unknown): [string, unknown][] {
  if (Array.isArray(v)) return v.map((x, i) => [String(i), x] as [string, unknown])
  if (v && typeof v === 'object') return Object.entries(v as Record<string, unknown>)
  return []
}

const fields = computed<Field[]>(() => {
  let data: unknown
  try {
    data = JSON.parse(props.text)
  } catch {
    return []
  }
  const out: Field[] = []
  // 只列叶子字段（容器本身不占行），与设计稿的 data.userId / data.deleted 形态一致
  const walk = (v: unknown, path: string, depth: number): void => {
    if (out.length >= MAX_FIELDS) return
    const kids = depth >= MAX_DEPTH ? [] : childEntries(v)
    if (kids.length === 0) {
      out.push({ path, type: typeName(v) })
      return
    }
    for (const [k, child] of kids) walk(child, path ? `${path}.${k}` : k, depth + 1)
  }

  const root = childEntries(data)
  if (root.length === 0) return [{ path: '$', type: typeName(data) }]
  for (const [k, child] of root) walk(child, k, 1)
  return out
})
</script>

<template>
  <section v-if="fields.length" class="fields">
    <div class="fh">
      <span class="ft">{{ t('resp.fields') }}</span>
      <span class="fd">{{ t('resp.fieldsHint') }}</span>
    </div>
    <div class="tr th">
      <span>{{ t('resp.field') }}</span>
      <span>{{ t('resp.type') }}</span>
      <span>{{ t('resp.meaning') }}</span>
    </div>
    <div v-for="f in fields" :key="f.path" class="tr">
      <span class="mono fp" :title="f.path">{{ f.path }}</span>
      <span class="mono ftype">{{ f.type }}</span>
      <n-input
        :value="meanings[f.path] ?? ''"
        size="small"
        :placeholder="t('common.optional')"
        @update:value="setMeaning(f.path, $event)"
        @blur="persist"
      />
    </div>
  </section>
</template>

<style scoped>
.fields {
  margin-top: 14px;
  border-top: 1px solid var(--app-border);
  padding-top: 10px;
}

.fh {
  display: flex;
  align-items: baseline;
  gap: 8px;
  margin-bottom: 8px;
}

.ft {
  font-size: 12.5px;
  font-weight: 600;
  color: var(--app-text);
}

.fd {
  font-size: 11.5px;
  color: var(--app-muted);
}

.tr {
  display: grid;
  grid-template-columns: minmax(0, 1.2fr) 90px minmax(0, 1.6fr);
  align-items: center;
  gap: 10px;
  padding: 5px 8px;
  border: 1px solid var(--app-border);
  border-top: none;
  font-size: 12px;
}

.tr.th {
  border-top: 1px solid var(--app-border);
  border-radius: 6px 6px 0 0;
  background: #fafafa;
  color: var(--app-muted);
  font-size: 11.5px;
}

.tr:last-child {
  border-radius: 0 0 6px 6px;
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
</style>

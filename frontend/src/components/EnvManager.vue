<script setup lang="ts">
// 环境变量管理：新建/删除环境，编辑变量（敏感值明文只落 *.secrets.yml，由 Go 层拆分存储）。
import { NButton, NCheckbox, NIcon, NInput, NModal, NPopconfirm } from 'naive-ui'
import { CloseOutline } from '@vicons/ionicons5'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useCollectionStore } from '@/stores/collection'
import type { Env } from '@/types'

const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{ 'update:show': [v: boolean]; saved: [] }>()
const { t } = useI18n()
const coll = useCollectionStore()

const selected = ref('')
const working = ref<Env>({ name: '', vars: [] })

watch(
  () => props.show,
  (v) => {
    if (v) select(coll.currentEnv || coll.envNames[0] || '')
  },
)

function select(name: string): void {
  selected.value = name
  const found = coll.info?.envs.find((e) => e.name === name)
  working.value = found
    ? JSON.parse(JSON.stringify(found)) as Env
    : { name, vars: [] }
}

const canSave = computed(() => /^[A-Za-z0-9_-]{1,60}$/.test(working.value.name))

function addVar(): void {
  working.value.vars.push({ name: '', value: '', enabled: true, secret: false })
}

function delVar(i: number): void {
  working.value.vars.splice(i, 1)
}

async function save(): Promise<void> {
  working.value.vars = working.value.vars.filter((v) => v.name.trim() !== '')
  await coll.saveEnv({ ...working.value, vars: [...working.value.vars] })
  selected.value = working.value.name
}

async function addEnv(): Promise<void> {
  const name = `env-${coll.envNames.length + 1}`
  await coll.saveEnv({ name, vars: [] })
  select(name)
}

async function delEnv(): Promise<void> {
  if (!selected.value) return
  await coll.deleteEnv(selected.value)
  select(coll.envNames[0] ?? '')
}

function close(): void {
  emit('update:show', false)
}
</script>

<template>
  <n-modal :show="show" preset="card" :title="t('env.title')" style="width: 660px" @update:show="close">
    <div class="wrap">
      <div class="list">
        <button
          v-for="e in coll.envNames"
          :key="e"
          class="env"
          :class="{ on: e === selected }"
          @click="select(e)"
        >
          {{ e }}
        </button>
        <n-button text type="primary" size="small" @click="addEnv">{{ t('env.add') }}</n-button>
      </div>
      <div class="detail">
        <template v-if="selected">
          <div class="hd">
            <span class="lbl">{{ t('env.name') }}</span>
            <n-input v-model:value="working.name" size="small" class="nm" />
            <n-popconfirm @positive-click="delEnv">
              <template #trigger>
                <n-button size="tiny" type="error" tertiary>{{ t('env.deleteEnv') }}</n-button>
              </template>
              {{ t('common.confirm') }}？
            </n-popconfirm>
          </div>
          <div v-for="(v, i) in working.vars" :key="i" class="vrow">
            <n-checkbox v-model:checked="v.enabled" size="small" />
            <n-input v-model:value="v.name" size="small" placeholder="name" class="vn" />
            <n-input
              v-model:value="v.value"
              size="small"
              placeholder="value"
              class="vv"
              :type="v.secret ? 'password' : 'text'"
            />
            <n-checkbox v-model:checked="v.secret" size="small">{{ t('env.secret') }}</n-checkbox>
            <n-button text size="tiny" @click="delVar(i)">
              <n-icon :component="CloseOutline" :size="13" />
            </n-button>
          </div>
          <n-button text size="tiny" type="primary" @click="addVar">{{ t('env.addVar') }}</n-button>
          <p class="hint muted">{{ t('env.saveHint') }}</p>
          <div class="ft">
            <n-button size="small" @click="close">{{ t('common.cancel') }}</n-button>
            <n-button size="small" type="primary" :disabled="!canSave" @click="save">
              {{ t('common.save') }}
            </n-button>
          </div>
        </template>
        <div v-else class="muted none">{{ t('env.noEnvs') }}</div>
      </div>
    </div>
  </n-modal>
</template>

<style scoped>
.wrap {
  display: flex;
  gap: 14px;
  min-height: 280px;
}

.list {
  width: 140px;
  flex: 0 0 auto;
  display: flex;
  flex-direction: column;
  gap: 4px;
  border-right: 1px solid var(--app-border);
  padding-right: 12px;
}

.env {
  border: none;
  background: none;
  text-align: left;
  padding: 5px 8px;
  border-radius: 5px;
  cursor: pointer;
  font-size: 13px;
}

.env:hover {
  background: #f0f2f4;
}

.env.on {
  background: #e8f6ee;
  color: var(--app-accent);
  font-weight: 600;
}

.detail {
  flex: 1 1 auto;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.hd {
  display: flex;
  align-items: center;
  gap: 8px;
}

.nm {
  width: 180px;
}

.vrow {
  display: flex;
  align-items: center;
  gap: 8px;
}

.vn {
  width: 180px;
  flex: 0 0 auto;
}

.vv {
  flex: 1 1 auto;
}

.hint {
  font-size: 11.5px;
  margin: 4px 0 0;
}

.none {
  padding: 30px;
  text-align: center;
}

.sp {
  flex: 1 1 auto;
}

.ft {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: auto;
}
</style>

<script setup lang="ts">
// 环境变量管理：新建/删除环境，编辑变量（敏感值明文只落 *.secrets.yml，由 Go 层拆分存储）。
import { message } from '@/lib/notice'
import { useCollectionStore } from '@/stores/collection'
import type { Env } from '@/types'
import { AddOutline, CloseOutline, TrashOutline } from '@vicons/ionicons5'
import { NButton, NCheckbox, NIcon, NInput, NModal, NPopconfirm } from 'naive-ui'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

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

/** 环境名规则与共享包 ValidEnvName 保持一致：Unicode 字母/数字 + - _ ( ) .（含全角括号），不以 . 开头。 */
const NAME_RE = /^(?!\.)[\p{L}\p{N}_().（）-]{1,60}$/u
const canSave = computed(() => NAME_RE.test(working.value.name))

function addVar(): void {
  working.value.vars.push({ name: '', value: '', enabled: true, secret: false })
}

function delVar(i: number): void {
  working.value.vars.splice(i, 1)
}

async function save(): Promise<void> {
  const oldName = selected.value
  const nextName = working.value.name.trim()
  working.value.name = nextName
  working.value.vars = working.value.vars.filter((v) => v.name.trim() !== '')
  try {
    // 改过名字：先把环境改名（两个文件一起搬）再写内容，否则 SaveEnv 按新名新建、旧环境残留
    if (oldName && nextName !== oldName) await coll.renameEnv(oldName, nextName)
    await coll.saveEnv({ ...working.value, vars: [...working.value.vars] })
  } catch (e) {
    message.error(e instanceof Error ? e.message : String(e))
    return
  }
  selected.value = nextName
  // 变量值变了要重算地址栏解析，否则「替换后 / 变量提示」仍是旧值，看起来像没保存成功
  emit('saved')
  message.success(t('env.saved', { name: nextName }))
  // 保存即收工：弹窗自动关掉（结果已经由上面的 toast 说明，不用再手动关一次）
  close()
}

async function addEnv(): Promise<void> {
  const name = `env-${coll.envNames.length + 1}`
  try {
    await coll.saveEnv({ name, vars: [] })
  } catch (e) {
    message.error(e instanceof Error ? e.message : String(e))
    return
  }
  select(name)
  emit('saved')
}

async function delEnv(name: string): Promise<void> {
  if (!name) return
  try {
    await coll.deleteEnv(name)
  } catch (e) {
    message.error(e instanceof Error ? e.message : String(e))
    return
  }
  if (selected.value === name) select(coll.envNames[0] ?? '')
  emit('saved')
}

function close(): void {
  emit('update:show', false)
}
</script>

<template>
  <n-modal :show="show" preset="card" :title="t('env.title')" style="width: 720px" @update:show="close">
    <div class="wrap">
      <div class="list">
        <div v-for="e in coll.envNames" :key="e" class="erow" :class="{ on: e === selected }">
          <button class="env" type="button" @click="select(e)">{{ e }}</button>
          <n-popconfirm @positive-click="delEnv(e)">
            <template #trigger>
              <button class="del" type="button" data-testid="env.del" :title="t('env.deleteEnv')"
                :aria-label="t('env.deleteEnv')">
                <n-icon :component="TrashOutline" :size="14" />
              </button>
            </template>
            {{ t('env.deleteConfirm', { name: e }) }}
          </n-popconfirm>
        </div>
        <!-- 新建环境：与上方环境项同一左内边距对齐，虚线实心按钮 + 图标，避免看起来像一段说明文字 -->
        <n-button class="new-env" size="small" dashed block data-testid="env.add" @click="addEnv">
          <template #icon>
            <n-icon :component="AddOutline" :size="14" />
          </template>
          {{ t('env.add') }}
        </n-button>
      </div>
      <div class="detail">
        <template v-if="selected">
          <div class="hd">
            <span class="lb">{{ t('env.name') }}</span>
            <n-input v-model:value="working.name" size="small" class="nm" />
          </div>

          <!-- 敏感值存放说明：放在变量表上方，先说明再编辑 -->
          <p class="hint muted">{{ t('env.saveHint') }}</p>

          <div class="vt">
            <div class="th">
              <span class="ctr">{{ t('env.enabled') }}</span>
              <span>{{ t('env.varName') }}</span>
              <span>{{ t('env.varValue') }}</span>
              <span class="ctr">{{ t('env.secret') }}</span>
              <span />
            </div>
            <div v-for="(v, i) in working.vars" :key="i" class="row">
              <n-checkbox v-model:checked="v.enabled" size="small" class="ctr" />
              <n-input v-model:value="v.name" size="small" placeholder="name" />
              <n-input v-model:value="v.value" size="small" placeholder="value"
                :type="v.secret ? 'password' : 'text'" />
              <n-checkbox v-model:checked="v.secret" size="small" class="ctr" :title="t('env.secret')"
                :aria-label="t('env.secret')" />
              <button class="act" type="button" :title="t('common.delete')" @click="delVar(i)">
                <n-icon :component="CloseOutline" :size="13" />
              </button>
            </div>
          </div>
          <n-button text size="tiny" type="primary" class="add" @click="addVar">{{ t('env.addVar') }}</n-button>

          <div class="ft">
            <n-button size="small" @click="close">{{ t('common.cancel') }}</n-button>
            <n-button size="small" type="primary" :disabled="!canSave" data-testid="env.save" @click="save">
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

/* 左栏一行：环境名 + 悬停/选中时可见的红色删除图标 */
.erow {
  display: flex;
  align-items: center;
  gap: 2px;
  border-radius: 5px;
}

.erow:hover {
  background: var(--app-surface-3);
}

.erow.on {
  background: var(--app-accent-tint);
}

.env {
  flex: 1 1 auto;
  min-width: 0;
  border: none;
  background: none;
  text-align: left;
  padding: 5px 8px;
  border-radius: 5px;
  cursor: pointer;
  font-size: 13px;
  font-family: inherit;
  color: var(--app-text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.erow.on .env {
  color: var(--app-accent);
  font-weight: 600;
}

/* 红色垃圾桶：默认略淡，悬停变实，选中或悬停行时出现 */
.del {
  flex: 0 0 auto;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: none;
  border-radius: 4px;
  background: none;
  padding: 3px 4px;
  margin-right: 2px;
  color: var(--app-danger);
  opacity: 0.5;
  cursor: pointer;
}

.erow:hover .del,
.erow.on .del {
  opacity: 1;
}

.del:hover {
  background: var(--app-danger-tint);
}

/* 新建环境：撑满左栏并与环境项左边缘对齐（环境项內边距 8px，按钮去掉多余内边距后图标落在同一条竖线上） */
.new-env {
  width: 100%;
  margin-top: 6px;
  justify-content: flex-start;
  padding-left: 7px;
  padding-right: 7px;
}

.new-env :deep(.n-button__content) {
  justify-content: flex-start;
}

.detail {
  flex: 1 1 auto;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

/* 环境名一行：标签 + 输入 */
.hd {
  display: flex;
  align-items: center;
  gap: 8px;
}

.lb {
  flex: 0 0 auto;
  font-size: 12px;
  font-weight: 500;
  color: var(--app-text-2);
}

.nm {
  width: 200px;
  flex: 0 0 auto;
}

/* 变量表：与「参数/请求头」表同一套列宽约定，敏感值单独成列（不再是行内长 label） */
.vt {
  display: flex;
  flex-direction: column;
}

.th,
.row {
  display: grid;
  grid-template-columns: 40px minmax(0, 1.1fr) minmax(0, 1.4fr) 58px 24px;
  align-items: center;
  gap: 8px;
}

.th {
  padding: 6px 8px;
  border: 1px solid var(--app-border);
  border-radius: 6px 6px 0 0;
  background: var(--app-surface-2);
  font-size: 11.5px;
  color: var(--app-muted);
  white-space: nowrap;
}

/* 没有变量时表头是唯一一行，自己收口 */
.th:last-child {
  border-radius: 6px;
}

.row {
  padding: 5px 8px;
  border: 1px solid var(--app-border);
  border-top: none;
}

.row:last-of-type {
  border-radius: 0 0 6px 6px;
}

.row:hover {
  background: var(--app-hover-soft);
}

.ctr {
  display: flex;
  align-items: center;
  justify-content: center;
  white-space: nowrap;
}

.act {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: none;
  background: none;
  padding: 0;
  color: var(--app-placeholder);
  cursor: pointer;
}

.act:hover {
  color: var(--app-danger);
}

.add {
  align-self: flex-start;
  margin-top: 8px;
}

.hint {
  font-size: 11.5px;
  margin: 4px 0 0;
}

.none {
  padding: 30px;
  text-align: center;
}

.ft {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: auto;
}
</style>

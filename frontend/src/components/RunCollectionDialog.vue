<script setup lang="ts">
// 集合运行器（RM-02）：桌面端复用 internal/testrun，按树顺序执行并展示逐请求结果。
// 运行读取磁盘上的集合文件（未保存的编辑器草稿不会进入本次运行）。
import { api } from '@/lib/ipc'
import { message } from '@/lib/notice'
import type { Env, RunCase, RunReport } from '@/types'
import { NButton, NInput, NInputNumber, NModal, NSelect, NSpin, NSwitch, NTag } from 'naive-ui'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{
  show: boolean
  envs: Env[]
  collectionName: string
}>()
const emit = defineEmits<{ (e: 'update:show', value: boolean): void }>()

const { t } = useI18n()

const open = computed({
  get: () => props.show,
  set: (v: boolean) => emit('update:show', v),
})

const envName = ref('')
const filter = ref('')
const bail = ref(false)
const timeoutSec = ref(0)
const running = ref(false)
const report = ref<RunReport | null>(null)
const runError = ref('')
const expanded = ref(-1)

const envOptions = computed(() => [
  { label: t('run.noEnv'), value: '' },
  ...props.envs.map((e) => ({ label: e.name, value: e.name })),
])

watch(
  () => props.show,
  (v) => {
    if (!v) return
    report.value = null
    runError.value = ''
    expanded.value = -1
    if (!props.envs.some((e) => e.name === envName.value)) envName.value = ''
  },
)

async function run(): Promise<void> {
  running.value = true
  runError.value = ''
  report.value = null
  expanded.value = -1
  try {
    report.value = await api.runCollection({
      env_name: envName.value,
      filter: filter.value.trim(),
      bail: bail.value,
      timeout_sec: timeoutSec.value || 0,
    })
  } catch (e) {
    runError.value = e instanceof Error ? e.message : String(e)
    message.error(runError.value)
  } finally {
    running.value = false
  }
}

async function exportJunit(): Promise<void> {
  if (!report.value) return
  try {
    const base = (props.collectionName || 'collection').replace(/[\\/:*?"<>|]/g, '-')
    const path = await api.exportRunReport('junit', `${base}.junit.xml`)
    if (path) message.success(t('run.exported', { path }))
  } catch (e) {
    message.error(e instanceof Error ? e.message : String(e))
  }
}

function toggle(i: number): void {
  expanded.value = expanded.value === i ? -1 : i
}

function caseError(c: RunCase): string {
  return c.error || c.script_error || ''
}
</script>

<template>
  <n-modal v-model:show="open" preset="card" :title="t('run.title')" style="width: 900px">
    <div class="run-form">
      <label class="field">
        <span>{{ t('run.env') }}</span>
        <n-select v-model:value="envName" :options="envOptions" size="small" data-testid="run.env" />
      </label>
      <label class="field field--wide">
        <span>{{ t('run.filter') }}</span>
        <n-input v-model:value="filter" size="small" clearable :placeholder="t('run.filterPlaceholder')"
          data-testid="run.filter" />
      </label>
      <label class="field field--narrow">
        <span>{{ t('run.timeout') }}</span>
        <n-input-number v-model:value="timeoutSec" :min="0" :max="600" size="small" data-testid="run.timeout" />
      </label>
      <label class="field field--switch">
        <span>{{ t('run.bail') }}</span>
        <n-switch v-model:value="bail" size="small" data-testid="run.bail" />
      </label>
      <n-button type="primary" size="small" :loading="running" data-testid="run.start" @click="run">
        {{ running ? t('run.running') : t('run.start') }}
      </n-button>
    </div>
    <p class="run-hint">{{ t('run.hint') }}</p>

    <n-spin :show="running">
      <p v-if="runError" class="run-error">{{ runError }}</p>

      <template v-if="report">
        <div class="run-summary">
          <n-tag type="success" round>{{ t('run.passedCount', { n: report.passed }) }}</n-tag>
          <n-tag type="error" round>{{ t('run.failedCount', { n: report.failed }) }}</n-tag>
          <span class="run-meta">{{ t('run.summary', { n: report.total, ms: report.duration_ms }) }}</span>
        </div>

        <div class="run-table-wrap">
          <table class="run-table">
            <thead>
              <tr>
                <th>{{ t('run.result') }}</th>
                <th>{{ t('run.request') }}</th>
                <th>{{ t('run.method') }}</th>
                <th>{{ t('run.status') }}</th>
                <th>{{ t('run.time') }}</th>
                <th>{{ t('run.detail') }}</th>
              </tr>
            </thead>
            <tbody>
              <template v-for="(c, i) in report.cases" :key="c.uid + i">
                <tr :class="{ 'run-row--fail': !c.passed }">
                  <td>
                    <n-tag :type="c.passed ? 'success' : 'error'" size="small" round>
                      {{ c.passed ? t('run.pass') : t('run.fail') }}
                    </n-tag>
                  </td>
                  <td>
                    <div class="run-name">{{ c.name }}</div>
                    <div class="run-path">{{ c.path || c.url }}</div>
                  </td>
                  <td class="run-mono">{{ c.method || '—' }}</td>
                  <td class="run-mono">{{ c.status || '—' }}</td>
                  <td class="run-mono">{{ c.duration_ms }}ms</td>
                  <td>
                    <n-button text size="tiny" @click="toggle(i)">
                      {{ expanded === i ? t('run.hideDetail') : t('run.showDetail') }}
                    </n-button>
                  </td>
                </tr>
                <tr v-if="expanded === i" class="run-detail">
                  <td colspan="6">
                    <p v-if="caseError(c)" class="run-error">{{ caseError(c) }}</p>
                    <table v-if="c.asserts?.length" class="run-asserts">
                      <thead>
                        <tr>
                          <th>{{ t('run.assertName') }}</th>
                          <th>{{ t('run.assertExpr') }}</th>
                          <th>{{ t('run.result') }}</th>
                          <th>{{ t('run.assertError') }}</th>
                        </tr>
                      </thead>
                      <tbody>
                        <tr v-for="(a, j) in c.asserts" :key="j">
                          <td>{{ a.name || '—' }}</td>
                          <td class="run-mono">{{ a.expr }}</td>
                          <td>
                            <n-tag :type="a.passed ? 'success' : 'error'" size="small">
                              {{ a.passed ? t('run.pass') : t('run.fail') }}
                            </n-tag>
                          </td>
                          <td>{{ a.error || '—' }}</td>
                        </tr>
                      </tbody>
                    </table>
                    <p v-else-if="!caseError(c)" class="run-muted">{{ t('run.noDetail') }}</p>
                  </td>
                </tr>
              </template>
            </tbody>
          </table>
          <p v-if="!report.cases.length" class="run-muted">{{ t('run.noCases') }}</p>
        </div>
      </template>
      <p v-else-if="!running && !runError" class="run-muted">{{ t('run.empty') }}</p>
    </n-spin>

    <template #footer>
      <div class="run-footer">
        <n-button size="small" :disabled="!report" data-testid="run.export" @click="exportJunit">
          {{ t('run.exportJunit') }}
        </n-button>
        <n-button size="small" type="primary" @click="open = false">{{ t('common.close') }}</n-button>
      </div>
    </template>
  </n-modal>
</template>

<style scoped>
.run-form {
  display: flex;
  align-items: flex-end;
  flex-wrap: wrap;
  gap: 10px;
}

.field {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 130px;
  font-size: 12px;
  color: var(--app-muted);
}

.field--wide {
  flex: 1 1 180px;
}

.field--narrow {
  width: 108px;
}

.field--switch {
  align-items: center;
}

.run-hint {
  margin: 10px 0 12px;
  color: var(--app-muted);
  font-size: 12px;
}

.run-summary {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 10px;
}

.run-meta {
  color: var(--app-muted);
  font-size: 12px;
}

.run-table-wrap {
  max-height: 46vh;
  overflow: auto;
}

.run-table,
.run-asserts {
  width: 100%;
  border-collapse: collapse;
  font-size: 12px;
}

.run-table th,
.run-table td,
.run-asserts th,
.run-asserts td {
  padding: 6px 8px;
  border-bottom: 1px solid var(--app-border);
  text-align: left;
  vertical-align: top;
}

.run-table th,
.run-asserts th {
  position: sticky;
  top: 0;
  background: var(--app-panel);
  color: var(--app-muted);
  font-weight: 500;
}

.run-row--fail {
  background: var(--app-danger-tint);
}

.run-name {
  color: var(--app-text);
}

.run-path {
  max-width: 280px;
  color: var(--app-muted);
  font-size: 11.5px;
  word-break: break-all;
}

.run-mono {
  font-family: var(--app-mono);
}

.run-detail > td {
  background: var(--app-bg);
}

.run-asserts {
  margin-top: 8px;
}

.run-error {
  margin: 0 0 10px;
  color: var(--app-danger);
  font-size: 12px;
  white-space: pre-wrap;
  word-break: break-word;
}

.run-muted {
  margin: 12px 0;
  color: var(--app-muted);
  font-size: 12px;
}

.run-footer {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
</style>

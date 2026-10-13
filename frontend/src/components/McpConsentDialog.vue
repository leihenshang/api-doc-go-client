<script setup lang="ts">
// MCP 风险工具交互授权弹窗（T2）：外部 AI 调用 send_request 或写工具前，
// Go 侧经 mcp:authz-request 事件下发本组件，用户三选一后 AnswerMCPAuthz 应答。
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { NButton, NModal, NTag } from 'naive-ui'
import { api } from '@/lib/ipc'
import type { AuthzDecision, AuthzRequest } from '@/types'

const { t } = useI18n()

const props = defineProps<{ req: AuthzRequest | null }>()

const answering = ref(false)
const timedOut = ref(false)
const localError = ref('')

// 60s 本地倒计时：与 Go 侧 authzTimeout 对齐；超时后禁用按钮（Go 会先按拒绝收尾）。
const remainSec = ref(60)
let timer: number | undefined

watch(
  () => props.req?.requestId,
  () => {
    answering.value = false
    timedOut.value = false
    localError.value = ''
    remainSec.value = 60
    window.clearInterval(timer)
    if (props.req) {
      timer = window.setInterval(() => {
        remainSec.value -= 1
        if (remainSec.value <= 0) {
          timedOut.value = true
          window.clearInterval(timer)
        }
      }, 1000)
    }
  },
  { immediate: true },
)

const riskType = computed(() => (props.req?.risk === 'high' ? 'error' : 'warning'))
const riskLabel = computed(() =>
  props.req?.risk === 'high' ? t('mcpAuthz.riskHigh') : t('mcpAuthz.riskMedium'),
)

// 摘要 JSON 美化展示；解析失败就按纯文本显示。
const prettyArgs = computed(() => {
  if (!props.req?.argsSummary) return '{}'
  try {
    return JSON.stringify(JSON.parse(props.req.argsSummary), null, 2)
  } catch {
    return props.req.argsSummary
  }
})

async function answer(decision: AuthzDecision) {
  if (!props.req || answering.value || timedOut.value) return
  answering.value = true
  try {
    await api.answerMCPAuthz(props.req.requestId, decision)
  } catch (e) {
    // 已超时/已结束：Go 侧已按拒绝处理，弹窗等 settled 事件关闭；这里只提示
    localError.value = e instanceof Error ? e.message : String(e)
  } finally {
    answering.value = false
  }
}

</script>

<template>
  <n-modal
    :show="req !== null"
    preset="card"
    :title="t('mcpAuthz.title')"
    :mask-closable="false"
    :closable="false"
    :close-on-esc="false"
    style="width: 620px"
    data-testid="mcp.consent.dialog"
  >
    <div v-if="req" class="consent">
      <div class="row">
        <span class="label">{{ t('mcpAuthz.tool') }}</span>
        <code class="tool" data-testid="mcp.consent.tool">{{ req.tool }}</code>
        <n-tag :type="riskType" size="small" round>{{ riskLabel }}</n-tag>
        <span v-if="remainSec > 0 && !timedOut" class="countdown">{{ remainSec }}s</span>
      </div>
      <div class="row">
        <span class="label">{{ t('mcpAuthz.transport') }}</span>
        <span>{{ req.transport || req.clientHint || 'MCP' }}</span>
      </div>
      <div class="row">
        <span class="label">{{ t('mcpAuthz.project') }}</span>
        <span data-testid="mcp.consent.project">{{ req.project || t('mcpAuthz.projectAny') }}</span>
      </div>
      <div class="row kind">
        <span v-if="req.netEgress" class="kind-warn">{{ t('mcpAuthz.kindEgress') }}</span>
        <span v-if="req.write" class="kind-danger">{{ t('mcpAuthz.kindWrite') }}</span>
      </div>
      <div class="args-label">{{ t('mcpAuthz.args') }}</div>
      <pre class="args" data-testid="mcp.consent.args">{{ prettyArgs }}</pre>
      <p class="hint">{{ t('mcpAuthz.argsHint') }}</p>
      <p v-if="timedOut" class="error">{{ t('mcpAuthz.timeout') }}</p>
      <p v-else-if="localError" class="error">{{ localError }}</p>
    </div>
    <template #footer>
      <div class="footer">
        <n-button
          type="error"
          ghost
          :disabled="answering || timedOut"
          data-testid="mcp.consent.deny"
          @click="answer('deny')"
        >
          {{ t('mcpAuthz.btnDeny') }}
        </n-button>
        <n-button
          type="warning"
          :loading="answering"
          :disabled="timedOut"
          data-testid="mcp.consent.allow15m"
          @click="answer('allow_15m')"
        >
          {{ t('mcpAuthz.btnAllow15m') }}
        </n-button>
        <n-button
          type="primary"
          :loading="answering"
          :disabled="timedOut"
          data-testid="mcp.consent.always"
          @click="answer('allow_always')"
        >
          {{ t('mcpAuthz.btnAlways') }}
        </n-button>
      </div>
    </template>
  </n-modal>
</template>

<style scoped>
.consent .row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}
.consent .label {
  color: var(--app-text-3, #888);
  min-width: 64px;
}
.tool {
  background: var(--app-fill, rgba(127, 127, 127, 0.12));
  padding: 2px 8px;
  border-radius: 4px;
  font-weight: 600;
}
.countdown {
  margin-left: auto;
  color: var(--app-warn);
  font-variant-numeric: tabular-nums;
}
.kind-warn {
  color: var(--app-warn);
}
.kind-danger {
  color: var(--app-danger);
}
.args-label {
  margin: 10px 0 4px;
  color: var(--app-text-3, #888);
}
.args {
  background: var(--app-fill, rgba(127, 127, 127, 0.1));
  border: 1px solid var(--app-border, rgba(127, 127, 127, 0.2));
  border-radius: 6px;
  padding: 10px;
  max-height: 220px;
  overflow: auto;
  font-size: 12px;
  margin: 0;
  white-space: pre-wrap;
  word-break: break-all;
}
.hint {
  margin: 8px 0 0;
  font-size: 12px;
  color: var(--app-text-3, #999);
}
.error {
  color: var(--app-danger);
  margin: 8px 0 0;
}
.footer {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
}
</style>

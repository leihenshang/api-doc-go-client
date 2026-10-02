<script setup lang="ts">
// gRPC Schema 分段（设计文档 G2/G3/G4）：定义来源与时间、导入 / 更新 / 移除、服务方法列表、解析错误常驻。
// 语义（D3）：不做文件监听 —— 只有「导入 / 更新定义」才重新解析；缓存随集合切换失效（Go 侧负责）。
// 语义（D4）：导入是原子的 —— 解析失败时不复制、不落盘，只在界面留常驻错误（proto 路径保持不变）。
import { NButton, NIcon, NInput, NPopconfirm, NSelect, NSpin } from 'naive-ui'
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { CloudDownloadOutline, FolderOpenOutline, TrashOutline } from '@vicons/ionicons5'
import { grpcOf, grpcStreamKey } from '@/lib/grpc'
import { api } from '@/lib/ipc'
import { message } from '@/lib/notice'
import { useCollectionStore } from '@/stores/collection'
import { useTabsStore } from '@/stores/tabs'
import type { Tab } from '@/stores/tabs'
import type { GrpcDefault, GrpcMethodInfo, GrpcProtoFile } from '@/types'

const props = defineProps<{ tab: Tab }>()
const tabs = useTabsStore()
const coll = useCollectionStore()
const { t } = useI18n()

const busy = ref(false)
/** 集合内已导入的定义（选择即切换入口定义，免点文件选择器） */
const protos = ref<GrpcProtoFile[]>([])

const grpc = computed(() => grpcOf(props.tab.request))
const schema = computed(() => props.tab.grpcSchema)
const services = computed(() => schema.value?.services ?? [])
const readonly = computed(() => coll.isReadOnly)
/** 入口定义（集合内相对路径） */
const protoRel = computed(() => (grpc.value.proto ?? '').trim())
/** 当前入口定义的文件时间 = 导入 / 更新时间 */
const protoTime = computed(() => {
  const hit = protos.value.find((p) => p.rel === protoRel.value)
  return hit ? formatTime(hit.mod) : ''
})

const protoOptions = computed(() => {
  const list = protos.value.map((p) => ({ label: p.rel, value: p.rel }))
  // 引用了集合外路径时（高级用法）也让它出现在下拉里，避免显示成空白
  if (protoRel.value && !list.some((o) => o.value === protoRel.value)) {
    list.unshift({ label: protoRel.value, value: protoRel.value })
  }
  return list
})

/** 选中的方法是否还在当前定义里（换定义后可能失效，发送前会提示）。 */
const methodMissing = computed(() => {
  const s = grpc.value.service
  const m = grpc.value.method
  if (!s || !m || !services.value.length) return false
  return !services.value.some((x) => x.name === s && x.methods.some((y) => y.name === m))
})

/** Go 侧的 mod 是 Unix 秒（collection.ProtoFileInfo.Mod），这里按本地时间显示到分钟。 */
function formatTime(sec: number): string {
  if (!sec) return ''
  const d = new Date(sec * 1000)
  const pad = (n: number): string => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function touch(): void {
  tabs.touch(props.tab.key)
}

async function refreshProtos(): Promise<void> {
  try {
    protos.value = await api.listGrpcProtos()
  } catch {
    protos.value = []
  }
}

// ---- 集合级默认定义（P8）：一次配置，多个请求共享 ----
const collectionDefault = ref<GrpcDefault | null>(null)
const isDefaultProto = computed(
  () => !!collectionDefault.value?.proto && collectionDefault.value.proto === protoRel.value,
)

async function refreshDefault(): Promise<void> {
  try {
    collectionDefault.value = await api.getGrpcDefault()
  } catch {
    collectionDefault.value = null
  }
}

async function setAsDefault(): Promise<void> {
  if (!protoRel.value || busy.value || readonly.value) return
  busy.value = true
  try {
    await api.setGrpcDefault(protoRel.value, grpc.value.imports ?? [])
    await refreshDefault()
    message.success(t('grpc.defaultOn', { rel: protoRel.value }))
  } catch (e) {
    message.error(e instanceof Error ? e.message : String(e))
  } finally {
    busy.value = false
  }
}

async function clearDefault(): Promise<void> {
  if (busy.value || readonly.value) return
  busy.value = true
  try {
    await api.setGrpcDefault('', [])
    await refreshDefault()
    message.success(t('grpc.defaultCleared'))
  } catch (e) {
    message.error(e instanceof Error ? e.message : String(e))
  } finally {
    busy.value = false
  }
}

/** 空草稿一键用上集合默认定义。 */
async function useDefaultProto(): Promise<void> {
  const def = collectionDefault.value
  if (!def?.proto) return
  grpc.value.proto = def.proto
  if (!(grpc.value.imports ?? []).length) grpc.value.imports = [...(def.imports ?? [])]
  touch()
  await tabs.loadGrpcSchema(props.tab.key)
}

function refreshAll(): void {
  void refreshProtos()
  void refreshDefault()
}

onMounted(refreshAll)
// RequestEditor 在同一实例里切换 tab：跟着刷新定义清单与默认定义（每个集合各自一份）
watch(() => props.tab.key, refreshAll)

/** 导入 / 更新定义：选 .proto（可多选）→ Go 侧先编译校验，成功才落盘并重新解析（失败不写盘）。 */
async function importProtos(): Promise<void> {
  if (readonly.value || busy.value) return
  let files: string[] = []
  try {
    files = await api.pickGrpcProtos()
  } catch (e) {
    message.error(e instanceof Error ? e.message : String(e))
    return
  }
  if (!files.length) return
  // 已有定义时这次动作就是「更新定义」（Go 侧按相对路径覆盖同名文件）
  const updating = !!protoRel.value
  busy.value = true
  try {
    const info = await api.importGrpcProtos(files, grpc.value.imports ?? [])
    // 入口定义由 Go 侧回填（多文件导入时取「自身定义了服务的第一个文件」）
    grpc.value.proto = info.proto
    grpc.value.imports = info.imports
    props.tab.grpcSchema = info
    props.tab.grpcError = ''
    touch()
    if (updating) message.success(t('grpc.updated', { rel: info.proto }))
    else message.success(t('grpc.imported', { n: info.protos.length, services: info.services.length }))
  } catch (e) {
    // 失败：请求里的 proto / imports 一律不动（导入是原子的），错误常驻到重新导入
    props.tab.grpcError = e instanceof Error ? e.message : String(e)
    message.error(props.tab.grpcError)
  } finally {
    busy.value = false
    await refreshProtos()
  }
}

/** 切换入口定义（集合内已导入的定义）。 */
async function useProto(rel: string): Promise<void> {
  if (!rel || rel === protoRel.value) return
  grpc.value.proto = rel
  touch()
  await tabs.loadGrpcSchema(props.tab.key)
}

/** 从集合里移除定义（引用它的请求会解析失败，故先二次确认）。 */
async function removeProto(): Promise<void> {
  const rel = protoRel.value
  if (!rel || busy.value || readonly.value) return
  busy.value = true
  try {
    await api.removeGrpcProto(rel)
    grpc.value.proto = ''
    await refreshProtos()
    await tabs.loadGrpcSchema(props.tab.key)
    touch()
    message.success(t('grpc.removed', { rel }))
  } catch (e) {
    message.error(e instanceof Error ? e.message : String(e))
  } finally {
    busy.value = false
  }
}

// ---- import 搜索路径（解析失败时的修复通道，§5 的唯一例外字段） ----
function setImports(list: string[]): void {
  grpc.value.imports = list
  touch()
}
function setImportPath(i: number, v: string): void {
  const list = [...(grpc.value.imports ?? [])]
  list[i] = v
  setImports(list)
}
function addImportPath(): void {
  if (readonly.value) return
  setImports([...(grpc.value.imports ?? []), ''])
}
function removeImportPath(i: number): void {
  setImports((grpc.value.imports ?? []).filter((_, j) => j !== i))
}
async function browseImportPath(i: number): Promise<void> {
  try {
    const dir = await api.pickDirectory()
    if (dir) setImportPath(i, dir)
  } catch (e) {
    message.error(e instanceof Error ? e.message : String(e))
  }
}

/** 选中服务方法（与请求栏下拉同一份选择）。 */
function pickMethod(service: string, m: GrpcMethodInfo): void {
  if (readonly.value) return
  grpc.value.service = service
  grpc.value.method = m.name
  grpc.value.stream = m.stream
  touch()
}
function isPicked(service: string, m: GrpcMethodInfo): boolean {
  return grpc.value.service === service && grpc.value.method === m.name
}
</script>

<template>
  <div class="schema-pane">
    <div class="tool">
      <n-button
        size="small"
        type="primary"
        :loading="busy"
        :disabled="readonly"
        data-testid="grpc.schema.import"
        @click="importProtos"
      >
        <template #icon><n-icon :component="CloudDownloadOutline" /></template>
        {{ protoRel ? t('grpc.updateProtos') : t('grpc.importProtos') }}
      </n-button>
      <n-select
        v-if="protoOptions.length"
        :value="protoRel"
        :options="protoOptions"
        size="small"
        class="pick"
        :placeholder="t('grpc.useProto')"
        data-testid="grpc.schema.pick"
        @update:value="useProto"
      />
      <n-popconfirm v-if="protoRel" @positive-click="removeProto">
        <template #trigger>
          <n-button size="small" quaternary type="error" :disabled="readonly" data-testid="grpc.schema.remove">
            <template #icon><n-icon :component="TrashOutline" /></template>
            {{ t('grpc.removeProto') }}
          </n-button>
        </template>
        <span>{{ t('grpc.removeConfirm', { rel: protoRel }) }}</span>
      </n-popconfirm>
      <span class="sp" />
      <!-- P8：集合级默认定义（当前定义即默认时显示状态 + 取消） -->
      <n-popconfirm v-if="protoRel && !isDefaultProto" @positive-click="setAsDefault">
        <template #trigger>
          <n-button size="small" quaternary :disabled="readonly || busy" data-testid="grpc.schema.setDefault">
            {{ t('grpc.defaultProto') }}
          </n-button>
        </template>
        <span>{{ t('grpc.defaultConfirm', { rel: protoRel }) }}</span>
      </n-popconfirm>
      <span v-else-if="isDefaultProto" class="def" data-testid="grpc.schema.isDefault">
        {{ t('grpc.defaultOn', { rel: protoRel }) }}
        <button class="link" type="button" :disabled="readonly || busy" data-testid="grpc.schema.clearDefault" @click="clearDefault">
          {{ t('grpc.defaultClear') }}
        </button>
      </span>
      <span v-if="busy" class="parsing"><n-spin :size="12" />{{ t('grpc.parsing') }}</span>
    </div>

    <!-- 来源与时间（G2.4） -->
    <p v-if="protoRel" class="src mono" data-testid="grpc.schema.source">
      {{ protoRel }}
      <template v-if="protoTime"> · {{ t('grpc.importedAt', { time: protoTime }) }}</template>
      <template v-if="services.length"> · {{ t('grpc.serviceCount', { n: services.length }) }}</template>
    </p>

    <!-- 解析失败常驻（G3.3/G3.4）：错误原文 + 重新导入；已有旧定义时说明仍在使用 -->
    <div v-if="tab.grpcError" class="err" data-testid="grpc.schema.error">
      <div class="err-t">{{ t('grpc.errTitle') }}</div>
      <pre class="err-b mono">{{ tab.grpcError }}</pre>
      <div class="err-f">
        <span v-if="schema" class="err-keep">{{ t('grpc.errKeepOld') }}</span>
        <n-button size="tiny" type="primary" :disabled="readonly" @click="importProtos">
          {{ t('grpc.reimport') }}
        </n-button>
      </div>
    </div>

    <!-- import 搜索路径：定义解析失败时的修复通道 -->
    <div class="paths">
      <div class="ph">
        <span class="lbl">{{ t('grpc.importPaths') }}</span>
        <span class="hint">{{ t('grpc.importPathHint') }}</span>
        <span class="sp" />
        <button class="addp" type="button" :disabled="readonly" data-testid="grpc.schema.addImport" @click="addImportPath">
          + {{ t('grpc.addImportPath') }}
        </button>
      </div>
      <div v-for="(p, i) in grpc.imports ?? []" :key="i" class="prow" data-testid="grpc.schema.importRow">
        <n-input
          :value="p"
          size="small"
          class="mono"
          :placeholder="t('grpc.importPathPlaceholder')"
          @update:value="setImportPath(i, $event)"
        />
        <n-button size="small" quaternary data-testid="grpc.schema.browse" @click="browseImportPath(i)">
          <template #icon><n-icon :component="FolderOpenOutline" /></template>
          {{ t('grpc.browse') }}
        </n-button>
        <button class="rm" type="button" :title="t('common.delete')" @click="removeImportPath(i)">×</button>
      </div>
      <p v-if="methodMissing" class="warn" data-testid="grpc.methodMissing">
        {{ t('grpc.methodMissing', { method: grpc.method }) }}
      </p>
    </div>

    <!-- 服务与方法（G4）：点击即选中，选中项与请求栏下拉同一个值 -->
    <div v-if="services.length" class="svcs" data-testid="grpc.services">
      <div v-for="s in services" :key="s.name" class="svc">
        <div class="svc-h">
          <span class="svc-n mono">{{ s.name }}</span>
          <span v-if="s.comment" class="svc-c" :title="s.comment">{{ s.comment }}</span>
        </div>
        <button
          v-for="m in s.methods"
          :key="m.fullName"
          class="mrow"
          :class="{ on: isPicked(s.name, m) }"
          type="button"
          :title="m.fullName"
          :data-testid="`grpc.method.${m.name}`"
          @click="pickMethod(s.name, m)"
        >
          <span class="m-n mono">{{ m.name }}</span>
          <span class="m-s" :class="`k-${m.stream}`">{{ t(grpcStreamKey(m.stream)) }}</span>
          <span class="m-t mono">{{ m.input }} → {{ m.output }}</span>
        </button>
        <p v-if="!s.methods.length" class="svc-e">{{ t('grpc.serviceEmpty') }}</p>
      </div>
    </div>

    <!-- 空态（G4.5）：给出下一步 -->
    <div v-else-if="!tab.grpcError" class="empty" data-testid="grpc.schema.empty">
      <p class="e-t">{{ protoRel ? t('grpc.noService') : t('grpc.emptyTitle') }}</p>
      <p class="e-h">{{ t('grpc.emptyHint') }}</p>
      <!-- P8：集合已配默认定义时给一键使用 -->
      <button
        v-if="!protoRel && collectionDefault?.proto"
        class="use-def"
        type="button"
        data-testid="grpc.schema.useDefault"
        @click="useDefaultProto"
      >
        {{ t('grpc.defaultUse', { rel: collectionDefault.proto }) }}
      </button>
    </div>
  </div>
</template>

<style scoped>
.schema-pane {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 10px 12px;
  overflow: auto;
}

.tool {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.pick {
  width: 260px;
}

.sp {
  flex: 1 1 auto;
}

.parsing {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: var(--app-muted);
}

/* 集合默认定义的当前状态（P8）：浅底胶囊 + 取消链接 */
.def {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 2px 8px;
  border-radius: 999px;
  background: var(--app-method-grpc-tint);
  color: var(--app-method-grpc);
  font-size: 11.5px;
}

.link {
  border: none;
  background: none;
  padding: 0;
  font-family: inherit;
  font-size: 11.5px;
  color: var(--app-muted);
  cursor: pointer;
  text-decoration: underline;
}

.link:hover {
  color: var(--app-danger);
}

.use-def {
  margin-top: 8px;
  border: 1px solid var(--app-accent);
  background: var(--app-panel);
  color: var(--app-accent);
  border-radius: 6px;
  font-family: inherit;
  font-size: 11.5px;
  padding: 4px 10px;
  cursor: pointer;
}

.use-def:hover {
  background: var(--app-accent-tint);
}

.src {
  margin: 0;
  font-size: 11.5px;
  color: var(--app-muted);
  word-break: break-all;
}

/* 解析失败：常驻错误卡片 */
.err {
  border: 1px solid var(--app-danger);
  background: var(--app-danger-tint);
  border-radius: 6px;
  padding: 8px 10px;
}

.err-t {
  font-size: 12px;
  font-weight: 600;
  color: var(--app-danger);
}

.err-b {
  margin: 6px 0;
  font-size: 11.5px;
  line-height: 1.6;
  color: var(--app-text);
  white-space: pre-wrap;
  word-break: break-all;
}

.err-f {
  display: flex;
  align-items: center;
  gap: 10px;
}

.err-keep {
  flex: 1 1 auto;
  font-size: 11.5px;
  color: var(--app-muted);
}

.paths {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.ph {
  display: flex;
  align-items: center;
  gap: 8px;
}

.lbl {
  font-size: 12px;
  font-weight: 600;
  color: var(--app-text);
}

.hint {
  font-size: 11px;
  color: var(--app-muted);
}

.addp {
  border: 1px solid var(--app-border);
  background: var(--app-panel);
  color: var(--app-text);
  border-radius: 6px;
  font-family: inherit;
  font-size: 11.5px;
  padding: 3px 8px;
  cursor: pointer;
}

.addp:hover {
  border-color: var(--app-accent);
  color: var(--app-accent);
}

.prow {
  display: flex;
  align-items: center;
  gap: 6px;
}

.rm {
  border: none;
  background: none;
  color: var(--app-placeholder);
  font-size: 15px;
  line-height: 1;
  padding: 2px 6px;
  cursor: pointer;
  border-radius: 4px;
}

.rm:hover {
  color: var(--app-danger);
  background: var(--app-chip-hover);
}

.warn {
  margin: 2px 0 0;
  font-size: 11.5px;
  color: var(--app-method-put);
}

.svcs {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.svc-h {
  display: flex;
  align-items: baseline;
  gap: 8px;
}

.svc-n {
  font-size: 12px;
  font-weight: 600;
  color: var(--app-accent-dark);
}

.svc-c {
  font-size: 11.5px;
  color: var(--app-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.mrow {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  max-width: 100%;
  box-sizing: border-box;
  border: 1px solid transparent;
  background: none;
  border-radius: 6px;
  padding: 5px 8px;
  font-family: inherit;
  text-align: left;
  cursor: pointer;
}

.mrow:hover {
  background: var(--app-row-hover);
}

.mrow.on {
  border-color: var(--app-method-grpc);
  background: var(--app-method-grpc-tint);
}

.m-n {
  font-size: 12px;
  color: var(--app-text);
  flex: 0 0 auto;
}

.m-s {
  flex: 0 0 auto;
  font-size: 10px;
  padding: 0 5px;
  border-radius: 999px;
  background: var(--app-chip);
  color: var(--app-muted);
  line-height: 15px;
}

.m-s.k-unary {
  background: var(--app-method-grpc-tint);
  color: var(--app-method-grpc);
}

/* 入参 / 出参类型可能很长：占满剩余宽度并省略（min-width:0 才会真的收缩） */
.m-t {
  flex: 1 1 auto;
  min-width: 0;
  font-size: 11px;
  color: var(--app-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.svc-e {
  margin: 2px 0 0 8px;
  font-size: 11.5px;
  color: var(--app-muted);
}

.empty {
  border: 1px dashed var(--app-border);
  border-radius: 6px;
  padding: 14px;
  text-align: center;
}

.e-t {
  margin: 0 0 6px;
  font-size: 12.5px;
  font-weight: 600;
  color: var(--app-text);
}

.e-h {
  margin: 0;
  font-size: 11.5px;
  color: var(--app-muted);
  line-height: 1.6;
}
</style>

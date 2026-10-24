/**
 * 前后端模型契约门禁（T5）：字段子集一致性的「防回归绊索」。
 *
 * 背景：body.data（Bruno 结构化 JSON 体）曾在 Go 侧存在、TS 侧缺失、IPC 归一化时又被剥离，
 * 导致导入 Bruno 后保存/同步丢字段、发送发空体。全量结构体比对成本过高，这里对已出过
 * 漂移事故的关键契约做静态校验，新增漂移点时往 CONTRACTS 里加一行即可。
 *
 * 运行：node --experimental-strip-types? 本仓库用 esbuild 加载（见 type-check 同源依赖），
 * 这里保持纯 Node 可直接跑：node frontend/scripts/type-contract.mjs。
 */
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, resolve } from 'node:path'

const here = dirname(fileURLToPath(import.meta.url))
const root = resolve(here, '../..')
const read = (p) => readFileSync(resolve(root, p), 'utf8')

/** 契约项：文件内必须出现的正则/字符串（全部命中才通过）。 */
const CONTRACTS = [
  {
    name: 'TS ReqBody 必须声明 data（share Body.data 的前端映射）',
    file: 'frontend/src/types.ts',
    needles: [/export interface ReqBody \{[\s\S]*?data\?: string/],
  },
  {
    name: 'IPC 归一化必须保留 body.data（T5 事故点：曾在此被剥离）',
    file: 'frontend/src/lib/ipc.ts',
    needles: [/data:\s*r\.body\?\.data/],
  },
  {
    name: 'Go share Body 必须有 data 的 json/yaml 标签',
    file: '../api-doc-go-share/collection/types.go',
    fallbackFile: undefined, // 该文件在 go.work 同级目录；不存在时跳过（不阻塞独立检出）
    needles: [/Data\s+string\s+`yaml:"data,omitempty" json:"data,omitempty"`/],
  },
  {
    name: 'runner buildBody 必须保留 raw→data 回落（T5）',
    file: 'internal/runner/runner.go',
    needles: [/src = b\.Data/],
  },
  {
    name: 'TS ReqBody 必须声明 fields（请求体字段表格，share Body.Fields 的前端映射）',
    file: 'frontend/src/types.ts',
    needles: [/export interface ReqBody \{[\s\S]*?fields\?: BodyField\[\]/],
  },
  {
    name: 'IPC 归一化必须保留 body.fields（漏了重载后整张表格消失）',
    file: 'frontend/src/lib/ipc.ts',
    needles: [/fields:\s*r\.body\?\.fields/, /fields:\s*ex\.request\?\.body\?\.fields/],
  },
  {
    name: 'Go share Body 必须有 fields 的 yaml/json 标签',
    file: '../api-doc-go-share/collection/types.go',
    needles: [/Fields\s+\[\]BodyField\s+`yaml:"fields,omitempty" json:"fields,omitempty"`/],
  },
  {
    name: 'Go share RequestFile 必须有 summary 顶层字段',
    file: '../api-doc-go-share/collection/request.go',
    needles: [/Summary\s+string\s+`yaml:"summary,omitempty"/],
  },
]

let failed = 0
for (const c of CONTRACTS) {
  let src
  try {
    src = read(c.file)
  } catch {
    // 外部依赖（go.work 兄弟仓库）缺失时不阻塞独立检出
    if (c.file.startsWith('../')) {
      console.log(`SKIP  ${c.name}（缺少 ${c.file}）`)
      continue
    }
    console.error(`FAIL  ${c.name}：文件不存在 ${c.file}`)
    failed++
    continue
  }
  const missing = c.needles.filter((re) => !re.test(src))
  if (missing.length) {
    console.error(`FAIL  ${c.name}`)
    missing.forEach(() => console.error(`        缺少期望片段（${c.file}）`))
    failed++
  } else {
    console.log(`OK    ${c.name}`)
  }
}

if (failed) {
  console.error(`\n契约门禁失败：${failed} 项。前后端模型已漂移，请同步修改并更新本脚本。`)
  process.exit(1)
}
console.log('\nTYPE_CONTRACT_OK')

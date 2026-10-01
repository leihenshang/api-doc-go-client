// 校验客户端语言包：
//   1) 每条文案能被 vue-i18n 的消息编译器解析（拦截嵌套占位符/缺引号等运行期才暴露的报错）
//   2) 中英两份 key 完全一致
// 用法：npm run i18n:check（在 frontend/ 下）
import fs from 'node:fs'
import path from 'node:path'
import { createRequire } from 'node:module'
import { pathToFileURL } from 'node:url'

const require = createRequire(import.meta.url)
// 直接载入 @intlify/message-compiler 的 ESM 产物：其 require 入口不导出 baseCompile
const pkgDir = path.dirname(require.resolve('@intlify/message-compiler/package.json'))
const { baseCompile, CompileErrorCodes } = await import(
  pathToFileURL(path.join(pkgDir, 'dist/message-compiler.mjs')).href
)

const files = ['src/i18n/locales/zh-CN.ts', 'src/i18n/locales/en-US.ts']
let bad = 0

function collect(f) {
  const keys = new Set()
  const lines = fs.readFileSync(f, 'utf8').split('\n')
  let section = ''
  lines.forEach((line, idx) => {
    const sec = line.match(/^  ([A-Za-z0-9_]+):\s*\{/)
    if (sec) section = sec[1]
    // 单引号与双引号两种写法都要扫：双引号里常放含引号的示例（如 curl 命令），
    // 曾经只扫单引号 => `-d '{"name":"a"}'` 这种非法插值漏检，直到弹窗渲染才抛 SyntaxError。
    const m =
      line.match(/^\s{4}([A-Za-z0-9_]+):\s*'((?:[^'\\]|\\.)*)'/) ??
      line.match(/^\s{4}([A-Za-z0-9_]+):\s*"((?:[^"\\]|\\.)*)"/)
    if (!m) return
    keys.add(`${section}.${m[1]}`)
    const msg = m[2].replace(/\\'/g, "'").replace(/\\"/g, '"')
    let err = null
    baseCompile(msg, { onError: (e) => { err = err ?? e } })
    if (err) {
      bad += 1
      const name = CompileErrorCodes[err.code] ?? err.code
      console.log(`${f}:${idx + 1}  key=${section}.${m[1]}  code=${err.code}(${name})  text=${msg}`)
    }
  })
  return keys
}

const [zh, en] = files.map(collect)
for (const k of zh) if (!en.has(k)) console.log(`en-US 缺少 key: ${k}`)
for (const k of en) if (!zh.has(k)) console.log(`zh-CN 缺少 key: ${k}`)

console.log(bad === 0 ? 'MESSAGES_OK' : `FAILED=${bad}`)
const match = zh.size === en.size
console.log(match ? `KEYS_MATCH (${zh.size})` : `KEYS_MISMATCH zh=${zh.size} en=${en.size}`)
process.exitCode = bad === 0 && match ? 0 : 1

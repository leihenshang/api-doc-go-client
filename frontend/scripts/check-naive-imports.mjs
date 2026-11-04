#!/usr/bin/env node
// 检查 .vue 里用到的 naive-ui 组件是否都从 'naive-ui' 显式导入。
//
// 为什么需要：项目没有 app.use(naive) 全局注册，模板里写 <n-checkbox> 却没 import NCheckbox 时，
// Vue 会把它当未知自定义元素静默渲染成空标签 —— 界面上「控件凭空消失」，控制台还只是 warning。
// 之前 MCP 设置页的勾选框整片消失就是这么来的（改其它控件时把 NCheckbox 从 import 里删掉了）。
import { readFileSync, readdirSync } from 'node:fs'
import path from 'node:path'

const root = path.resolve(import.meta.dirname, '..')

/** 递归收集 .vue（不依赖 fs.globSync：Node 20 早期版本没有它） */
function walk(dir) {
  return readdirSync(path.join(root, dir), { withFileTypes: true }).flatMap((e) => {
    const rel = path.posix.join(dir, e.name)
    if (e.isDirectory()) return walk(rel)
    return e.name.endsWith('.vue') ? [rel] : []
  })
}

const files = walk('src')

/** kebab-case → PascalCase：n-checkbox → NCheckbox */
const pascal = (tag) =>
  tag
    .split('-')
    .map((s) => s.charAt(0).toUpperCase() + s.slice(1))
    .join('')

const problems = []
for (const rel of files) {
  const src = readFileSync(path.join(root, rel), 'utf8')
  const used = new Set(
    [...src.matchAll(/<(n-[a-z0-9-]+)/g)].map((m) => m[1]),
  )
  if (used.size === 0) continue
  // 本文件从 naive-ui 导入了哪些标识符
  const imported = new Set()
  for (const m of src.matchAll(/import\s*\{([^}]*)\}\s*from\s*['"]naive-ui['"]/g)) {
    for (const name of m[1].split(',')) {
      const id = name.trim().split(/\s+as\s+/)[0]
      if (id) imported.add(id)
    }
  }
  for (const tag of used) {
    const want = pascal(tag)
    if (!imported.has(want)) problems.push(`${rel}: 用到 <${tag}> 但未导入 ${want}`)
  }
}

if (problems.length) {
  console.error('❌ naive-ui 组件未导入（会渲染成空标签，界面上表现为控件消失）：')
  for (const p of problems) console.error('  ✗ ' + p)
  process.exit(1)
}
console.log(`naive-ui 导入检查通过（${files.length} 个 .vue）`)

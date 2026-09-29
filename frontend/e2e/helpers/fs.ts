// 集合是纯文本（文件即真相源），所以 UI 操作后可以直接读盘做交叉断言。
import { readFileSync, readdirSync, statSync } from 'node:fs'
import path from 'node:path'

/** 读取集合内某个文件（相对集合根）；不存在返回 null，便于 expect.poll 轮询。 */
export function readCollectionFile(collectionDir: string, rel: string): string | null {
  try {
    return readFileSync(path.join(collectionDir, rel), 'utf8')
  } catch {
    return null
  }
}

/** 列出集合内所有相对文件路径（失败诊断用，跳过 .trash）。 */
export function listCollectionFiles(collectionDir: string): string[] {
  const out: string[] = []
  const walk = (dir: string, prefix: string): void => {
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      if (entry.name === '.trash') continue
      const rel = prefix ? `${prefix}/${entry.name}` : entry.name
      const abs = path.join(dir, entry.name)
      if (entry.isDirectory()) walk(abs, rel)
      else if (statSync(abs).isFile()) out.push(rel)
    }
  }
  walk(collectionDir, '')
  return out.sort()
}

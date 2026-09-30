// 每个 worker 起一套隔离环境：
//   devserver（反射桥 POST /api/App/<Method> + 自签 TLS 回显）
//   testfixtures（受控故障矩阵：状态码/重定向/延迟/大响应/二进制/压缩/非 UTF-8/Cookie/认证/multipart）
// 用例可同时断言 UI、集合文件与真实响应；配置目录走独立 XDG_CONFIG_HOME，绝不碰用户真实配置。
import { execFileSync, spawn, type ChildProcess } from 'node:child_process'
import { cpSync, existsSync, mkdirSync, readFileSync, readdirSync, statSync, writeFileSync } from 'node:fs'
import { createServer } from 'node:net'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { expect as baseExpect, test as base } from '@playwright/test'

const here = path.dirname(fileURLToPath(import.meta.url)) // frontend/e2e/helpers
const frontendDir = path.resolve(here, '../..') // frontend
const repoDir = path.resolve(frontendDir, '..') // 仓库根

/** 可用的集合种子（frontend/e2e/fixtures/<name>） */
export type SeedName = 'basic' | 'bruno-sample'

export interface AppFixture {
  /** devserver 根地址（前端与反射桥都在这） */
  url: string
  /** 故障矩阵服务根地址 */
  fixtureUrl: string
  /** devserver 的自签 TLS 端点（https://127.0.0.1:<port>/echo） */
  tlsUrl: string
  /** 本次运行目录（集合快照、截图、日志都在这） */
  runDir: string
  /** 隔离的 XDG_CONFIG_HOME（可读 config.json 断言落盘） */
  configDir: string
  /** 用种子新建一个集合目录并让 devserver 打开它（返回集合目录，可读盘断言） */
  newCollection(seed?: SeedName): Promise<string>
  /** 直接调 App 门面方法（等价于前端 IPC） */
  ipc<T = unknown>(method: string, ...args: unknown[]): Promise<T>
}

async function freePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const srv = createServer()
    srv.on('error', reject)
    srv.listen(0, '127.0.0.1', () => {
      const { port } = srv.address() as { port: number }
      srv.close(() => resolve(port))
    })
  })
}

async function waitReady(url: string, init: RequestInit, timeoutMs = 30_000): Promise<void> {
  const deadline = Date.now() + timeoutMs
  while (Date.now() < deadline) {
    try {
      const res = await fetch(url, init)
      if (res.ok) return
    } catch {
      // 端口还没起来，继续等
    }
    await new Promise((r) => setTimeout(r, 200))
  }
  throw new Error(`服务未在 ${timeoutMs}ms 内就绪: ${url}`)
}

/** Windows 下可执行文件需要 .exe 后缀（go build -o 与 spawn 都按字面名处理，缺后缀会 ENOENT）。 */
function binName(name: string): string {
  return process.platform === 'win32' ? `${name}.exe` : name
}

function spawnServer(bin: string, args: string[], env: NodeJS.ProcessEnv, tag: string): ChildProcess {
  const child = spawn(bin, args, { cwd: repoDir, env, stdio: ['ignore', 'pipe', 'pipe'] })
  child.stdout?.on('data', (d: Buffer) => process.stdout.write(`[${tag}] ${d}`))
  child.stderr?.on('data', (d: Buffer) => process.stderr.write(`[${tag}] ${d}`))
  return child
}

/** 把种子里的占位符换成运行时地址（种子本身不写死端口） */
function replaceTokens(dir: string, tokens: Record<string, string>): void {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const abs = path.join(dir, entry.name)
    if (entry.isDirectory()) {
      replaceTokens(abs, tokens)
      continue
    }
    if (!entry.name.endsWith('.yml')) continue
    let text = readFileSync(abs, 'utf8')
    for (const [key, value] of Object.entries(tokens)) text = text.split(key).join(value)
    writeFileSync(abs, text)
  }
}

export const test = base.extend<{ app: AppFixture }>({
  app: [
    async ({}, use) => {
      if (!existsSync(path.join(frontendDir, 'dist', 'index.html'))) {
        throw new Error('缺少 frontend/dist/index.html，请先在 frontend 下执行 npm run build')
      }
      // 每次运行独立目录，不覆盖也不清理历史产物（便于回看）
      const runDir = path.join(repoDir, '.tmp', 'e2e', `run-${Date.now()}`)
      const configDir = path.join(runDir, 'config')
      mkdirSync(configDir, { recursive: true })

      execFileSync('go', ['build', '-o', binName(path.join(runDir, 'devserver')), './cmd/devserver'], { cwd: repoDir, stdio: 'inherit' })
      execFileSync('go', ['build', '-o', binName(path.join(runDir, 'testfixtures')), './cmd/testfixtures'], { cwd: repoDir, stdio: 'inherit' })

      const [apiPort, fxPort, tlsPort] = [await freePort(), await freePort(), await freePort()]
      const url = `http://127.0.0.1:${apiPort}`
      const fixtureUrl = `http://127.0.0.1:${fxPort}`
      const tlsUrl = `https://127.0.0.1:${tlsPort}`
      const env = { ...process.env, XDG_CONFIG_HOME: configDir }

      const fixtures = spawnServer(binName(path.join(runDir, 'testfixtures')), ['-addr', `127.0.0.1:${fxPort}`], env, 'fixtures')
      const dev = spawnServer(
        binName(path.join(runDir, 'devserver')),
        ['-dir', path.join(runDir, 'headless'), '-addr', `127.0.0.1:${apiPort}`, '-web', path.join(frontendDir, 'dist'), '-tls-echo', `127.0.0.1:${tlsPort}`],
        env,
        'devserver',
      )
      await waitReady(`${fixtureUrl}/json/flat`, { method: 'GET' })
      await waitReady(`${url}/api/App/GetSettings`, { method: 'POST', body: '[]' })

      let counter = 0
      const api = {
        url,
        fixtureUrl,
        tlsUrl,
        runDir,
        configDir,
        ipc: async <T>(method: string, ...args: unknown[]): Promise<T> => {
          const res = await fetch(`${url}/api/App/${method}`, { method: 'POST', body: JSON.stringify(args) })
          const payload = (await res.json()) as { ok: boolean; data?: unknown; error?: string }
          if (!payload.ok) throw new Error(`${method} 调用失败: ${payload.error}`)
          return payload.data as T
        },
        newCollection: async (seed: SeedName = 'basic'): Promise<string> => {
          const dir = path.join(runDir, `collection-${++counter}-${seed}`)
          cpSync(path.join(frontendDir, 'e2e', 'fixtures', seed), dir, { recursive: true })
          replaceTokens(dir, { __FIXTURE__: fixtureUrl, __TLS__: tlsUrl })
          await api.ipc('SetHeadlessDir', dir)
          await api.ipc('OpenCollection', dir)
          return dir
        },
      }
      console.log(
        `\n  ▶ devserver ${url}\n  ▶ fixtures  ${fixtureUrl}\n  ▶ tls       ${tlsUrl}\n  ▶ runDir    ${runDir}\n`,
      )

      await use(api)

      dev.kill('SIGTERM')
      fixtures.kill('SIGTERM')
      await new Promise((r) => setTimeout(r, 300))
    },
    { scope: 'worker' },
  ],
})

export const expect = baseExpect

/** 集合内是否有文件（诊断/断言用） */
export function collectionHasFile(collectionDir: string, rel: string): boolean {
  const p = path.join(collectionDir, rel)
  return existsSync(p) && statSync(p).isFile()
}

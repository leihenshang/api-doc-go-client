// e2e 运行配置：真实构建产物（frontend/dist）+ devserver + testfixtures，每 worker 一套、端口随机。
//   npm run test:e2e            有头（看得见操作，slowMo=300）
//   npm run test:e2e:headless   无头（CI / 快速）
//   E2E_SLOWMO=0 npm run test:e2e   全速有头
import { defineConfig } from '@playwright/test'

const headed =
  process.argv.includes('--headed') || process.argv.includes('--ui') || process.env.E2E_HEADED === '1'
const slowMo = Number(process.env.E2E_SLOWMO ?? (headed ? 300 : 0))

// 每次运行独立目录：Playwright 会在启动时清空 outputDir，共用一个目录会删掉上一轮
// 现场（trace/截图）—— 而且上千个文件的批量删除容易被开发机的安全删除策略拦下。
const stamp = new Date().toISOString().replace(/[:.]/g, '-').slice(0, 19)
const artifactDir = `../.tmp/e2e-artifacts/run-${stamp}`

export default defineConfig({
  testDir: './e2e',
  testMatch: '**/*.spec.ts',
  // 一套桌面应用 + 每 worker 两个后端进程：串行跑最稳（全量约 2 分钟）
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  timeout: 60_000,
  expect: { timeout: 10_000 },
  reporter: process.env.CI
    ? [['list'], ['html', { open: 'never', outputFolder: `../.tmp/e2e-report/run-${stamp}` }]]
    : [['list']],
  outputDir: artifactDir,
  use: {
    headless: !headed,
    launchOptions: { slowMo },
    viewport: { width: 1280, height: 800 },
    locale: 'zh-CN',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
})

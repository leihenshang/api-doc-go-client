// 从 design/icon/api-color.svg 生成应用图标资产：
//   build/appicon.png            1024×1024（Wails 构建源）
//   build/windows/icon.ico       Windows exe 图标（16/32/48/64/128/256，PNG 条目）
//   frontend/public/favicon.svg  浏览器 / 开发态标签页图标（直接复制 SVG）
//
// 用法：node scripts/make-icons.mjs
// 用 Chromium 光栅化的原因：本机没有 rsvg / inkscape / ImageMagick / Pillow。
import { createRequire } from 'node:module'
import { copyFileSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = join(dirname(fileURLToPath(import.meta.url)), '..')
const SRC = join(root, 'design', 'icon', 'api-color.svg')
const ICO_SIZES = [256, 128, 64, 48, 32, 16]

const require = createRequire(join(root, 'frontend', 'package.json'))
const { chromium } = require('playwright')

const svg = readFileSync(SRC, 'utf8')
const tmp = join(root, '.tmp', 'icons')
mkdirSync(tmp, { recursive: true })

/** 透明底渲染指定边长的 PNG：大图留 10% 边距，小尺寸留 8%（小图下更满）。 */
async function render(page, size) {
  const pad = size >= 512 ? 0.1 : 0.08
  await page.setViewportSize({ width: size, height: size })
  await page.setContent(
    '<style>html,body{margin:0;padding:0;background:transparent}' +
      '#b{width:' + size + 'px;height:' + size + 'px;display:flex;align-items:center;justify-content:center}' +
      '#b>svg{width:' + Math.round((1 - pad * 2) * 100) + '%;height:auto;display:block}</style><div id="b">' + svg + '</div>',
  )
  return page.screenshot({ omitBackground: true })
}

/** 打包 ICO：6 字节头 + 连续的 16 字节/项目录，之后才是各尺寸 PNG 数据。 */
function packIco(images) {
  const header = Buffer.alloc(6)
  header.writeUInt16LE(0, 0)
  header.writeUInt16LE(1, 2) // 1 = icon
  header.writeUInt16LE(images.length, 4)
  let offset = header.length + 16 * images.length
  const dir = Buffer.alloc(16 * images.length)
  images.forEach((img, i) => {
    const e = 16 * i
    dir.writeUInt8(img.size % 256, e) // 0 表示 256
    dir.writeUInt8(img.size % 256, e + 1)
    dir.writeUInt8(0, e + 2) // 调色板色数
    dir.writeUInt8(0, e + 3) // 保留
    dir.writeUInt16LE(1, e + 4) // color planes
    dir.writeUInt16LE(32, e + 6) // bpp
    dir.writeUInt32LE(img.data.length, e + 8)
    dir.writeUInt32LE(offset, e + 12)
    offset += img.data.length
  })
  return Buffer.concat([header, dir, ...images.map((i) => i.data)])
}

const browser = await chromium.launch()
const page = await browser.newPage()
const images = []
for (const size of [1024].concat(ICO_SIZES)) {
  const data = await render(page, size)
  writeFileSync(join(tmp, size + '.png'), data)
  console.log('  ' + size + 'px  ' + data.length + ' bytes')
  if (size !== 1024) images.push({ size, data })
}
await browser.close()

writeFileSync(join(root, 'build', 'appicon.png'), readFileSync(join(tmp, '1024.png')))
writeFileSync(join(root, 'build', 'windows', 'icon.ico'), packIco(images))
copyFileSync(SRC, join(root, 'frontend', 'public', 'favicon.svg'))
console.log('已更新 build/appicon.png、build/windows/icon.ico、frontend/public/favicon.svg')

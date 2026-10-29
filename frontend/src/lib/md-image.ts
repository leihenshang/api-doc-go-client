// Markdown 编辑器的图片上传（md-editor-v3 的 onUploadImg 回调）。
//
// 流程：读文件 → base64 → App.SaveDocImage（落到 <集合>/assets/<sha256>）→
// 把返回的 /doc-assets/<hash> 交给编辑器插入 ![alt](url)。
// 图片只在本地集合里，不上传服务端；同图内容寻址自动去重。
import { i18n } from '@/i18n'
import { api } from '@/lib/ipc'
import { message } from '@/lib/notice'

/** 与 Go 侧 collection.MaxDocImageBytes 保持一致。 */
const MAX_BYTES = 5 * 1024 * 1024
/** 与 Go 侧 SNIFF 白名单一致（SVG 会在同源下执行脚本，明确不收）。 */
const ACCEPT = ['image/png', 'image/jpeg', 'image/gif', 'image/webp']

function t(key: string, params: Record<string, unknown> = {}): string {
  return i18n.global.t(key, params)
}

/** File → 纯 base64（去掉 data URL 前缀）。 */
function readBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onerror = () => reject(reader.error ?? new Error('read failed'))
    reader.onload = () => {
      const s = String(reader.result ?? '')
      const i = s.indexOf(',')
      resolve(i >= 0 ? s.slice(i + 1) : s)
    }
    reader.readAsDataURL(file)
  })
}

/**
 * md-editor-v3 的 onUploadImg 回调：逐个上传，把成功的 URL 交给编辑器插入。
 * 失败逐个提示、不中断其余文件；全部失败时也调用 done([]) 让弹窗收起（避免卡住）。
 */
export async function uploadMdImages(files: File[], done: (urls: string[]) => void): Promise<void> {
  const urls: string[] = []
  for (const f of files) {
    if (!ACCEPT.includes(f.type)) {
      message.error(t('editor.imageTypeUnsupported', { name: f.name }))
      continue
    }
    if (f.size > MAX_BYTES) {
      message.error(t('editor.imageTooLarge', { name: f.name, mb: MAX_BYTES / 1024 / 1024 }))
      continue
    }
    try {
      urls.push(await api.saveDocImage(await readBase64(f)))
    } catch (e) {
      message.error(
        t('editor.imageFailed', { name: f.name, err: e instanceof Error ? e.message : String(e) }),
      )
    }
  }
  done(urls)
}

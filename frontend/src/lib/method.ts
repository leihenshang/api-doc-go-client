// HTTP 方法语义色（design-spec §1/§6.3）：GET=primary 绿（读），POST=info 蓝（写），其余按语义区分。
// 集合树、标签栏、方法选择器、命令面板共用，避免各处各写一份色表。
// 取值统一走 CSS 变量（styles/base.css 里明暗各一套），这样内联样式与暗色主题天然同步。
// GRPC 不是 HTTP 方法，而是 gRPC 请求在 Method 字段上的取值（collection.MethodGRPC）：
// 索引/历史/搜索沿用「Method + URL」，故这里给它一个独立色（青），列表里一眼区分协议。
const SUFFIX: Record<string, string> = {
  GET: 'get',
  POST: 'post',
  PUT: 'put',
  DELETE: 'delete',
  PATCH: 'patch',
  GRPC: 'grpc',
}

// 未收录的方法（HEAD/OPTIONS/自定义）统一用中性灰
const OTHER = 'other'

function suffixOf(method: string): string {
  return SUFFIX[method.toUpperCase()] ?? OTHER
}

/** 方法主色（文字 / 图标）。 */
export function methodColor(method: string): string {
  return `var(--app-method-${suffixOf(method)})`
}

/** 方法浅底色（选中态、方法选择器背景）。 */
export function methodTint(method: string): string {
  return `var(--app-method-${suffixOf(method)}-tint)`
}

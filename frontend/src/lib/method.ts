// HTTP 方法语义色（design-spec §1/§6.3）：GET=primary 绿（读），POST=info 蓝（写），其余按语义区分。
// 集合树、标签栏、方法选择器、命令面板共用，避免各处各写一份色表。
const COLORS: Record<string, string> = {
  GET: '#18a058',
  POST: '#2080f0',
  PUT: '#f0a020',
  DELETE: '#d03050',
  PATCH: '#8a2be2',
  HEAD: '#8a9199',
  OPTIONS: '#8a9199',
}

const TINTS: Record<string, string> = {
  GET: '#e8f5ee',
  POST: '#e8f0fe',
  PUT: '#fff7e6',
  DELETE: '#fdecec',
  PATCH: '#f5ecfd',
  HEAD: '#f0f1f2',
  OPTIONS: '#f0f1f2',
}

/** 方法主色（文字 / 图标）。 */
export function methodColor(method: string): string {
  return COLORS[method.toUpperCase()] ?? '#909399'
}

/** 方法浅底色（选中态、方法选择器背景）。 */
export function methodTint(method: string): string {
  return TINTS[method.toUpperCase()] ?? '#f0f1f2'
}

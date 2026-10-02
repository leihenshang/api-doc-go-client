// gRPC 前端公共逻辑：协议判定、发送门禁、服务/方法的取值换算。
// 与 Go 侧的分工（设计文档 §10 第 1/2 条）：调用路径 `/包.服务/方法`、状态码 → gRPC code、
// 响应体 → protojson 文本，全部由 runner 产出；前端只做门禁与展示，不重复实现协议语义。
import type { GrpcDoc, RequestDoc } from '@/types'

/** 是否 gRPC 请求：内存里以 grpc 段是否存在为准（对齐 collection.Request.IsGRPC）。 */
export function isGrpc(r: RequestDoc): boolean {
  return !!r.grpc
}

/** 取 grpc 段：gRPC 请求一定有值，这里给调用方省掉空判断。 */
export function grpcOf(r: RequestDoc): GrpcDoc {
  return r.grpc ?? { target: '', service: '', method: '' }
}

/**
 * 发送门禁（设计文档 G3.6）：不能发送时返回原因对应的 i18n key，可发送时返回 ''。
 * 顺序按修复成本从低到高：先填地址，再导定义，再选服务方法。
 *
 * 消息不参与门禁：空消息是合法的（= 全默认值消息，执行器同样跳过解析），
 * 内容是否合规由 Message 分段按定义实时校验（G5.3）。
 *
 * @param parsed 定义是否已成功解析（Schema 分段的状态）：未解析 / 解析失败同样不允许发送。
 */
export function grpcSendBlocker(r: RequestDoc, parsed = true): string {
  const g = r.grpc
  if (!g) return ''
  if (!g.target.trim()) return 'grpc.block.target'
  if (!(g.proto ?? '').trim()) return 'grpc.block.proto'
  if (!parsed) return 'grpc.block.invalid'
  if (!g.service.trim() || !g.method.trim()) return 'grpc.block.method'
  return ''
}

/**
 * gRPC 状态码名（SCREAMING_SNAKE，与 Go 侧 runner.GrpcCodeName 同一套，设计文档 §10 第 3 条）：
 * codes.Code.String() 是 CamelCase，界面与断言按官方大写名展示。
 */
const CODE_NAMES: Record<number, string> = {
  0: 'OK',
  1: 'CANCELLED',
  2: 'UNKNOWN',
  3: 'INVALID_ARGUMENT',
  4: 'DEADLINE_EXCEEDED',
  5: 'NOT_FOUND',
  6: 'ALREADY_EXISTS',
  7: 'PERMISSION_DENIED',
  8: 'RESOURCE_EXHAUSTED',
  9: 'FAILED_PRECONDITION',
  10: 'ABORTED',
  11: 'OUT_OF_RANGE',
  12: 'UNIMPLEMENTED',
  13: 'INTERNAL',
  14: 'UNAVAILABLE',
  15: 'DATA_LOSS',
  16: 'UNAUTHENTICATED',
}

/** 状态码名：未收录的码（服务端自定义扩展码）退回 UNKNOWN。 */
export function grpcCodeName(code: number): string {
  return CODE_NAMES[code] ?? 'UNKNOWN'
}

/** 徽章文案：`OK(0)` / `NOT_FOUND(5)`（对齐 G8.2 的展示形态）。 */
export function grpcCodeLabel(code: number): string {
  return `${grpcCodeName(code)}(${code})`
}

/** 状态徽章语义：OK 绿；被取消 / 超时用黄（多半是客户端侧）；其余红。 */
export function grpcStatusType(code: number): 'success' | 'warning' | 'error' {
  if (code === 0) return 'success'
  if (code === 1 || code === 4) return 'warning'
  return 'error'
}

/** 方法全名（proto 的 `包.服务.方法`）：请求里 service + '.' + method 即全名，可直接对齐选项值。 */
export function grpcMethodFullName(service: string, method: string): string {
  return service && method ? `${service}.${method}` : ''
}

/** 拆全名为「服务全名 + 方法名」——proto3 的服务是顶层声明，故按最后一个点切分即可。 */
export function splitGrpcMethod(fullName: string): { service: string; method: string } {
  const cut = fullName.lastIndexOf('.')
  if (cut < 0) return { service: '', method: fullName }
  return { service: fullName.slice(0, cut), method: fullName.slice(cut + 1) }
}

/** 服务短名（去掉包名前缀）：下拉标签与服务来源行共用，避免 `demo.Greeter` 占位过长。 */
export function grpcServiceShort(name: string): string {
  const cut = name.lastIndexOf('.')
  return cut < 0 ? name : name.slice(cut + 1)
}

/** 流式形态的展示名（i18n key 后缀，取值来自 proto.Method.Stream）。 */
export function grpcStreamKey(stream: string): string {
  return `grpc.stream.${stream || 'unary'}`
}

// 与 Go 侧通信的唯一入口：
//   - Wails 运行时：window.go.app.App.<Method>(args…)（bindings 注入）
//   - 浏览器开发：POST /api/App/<Method>（vite 代理到 cmd/devserver）
// 两态走同一套 App 方法，前端无感知。
import { i18n } from '@/i18n';
// Wails 绑定按「包名」注册：本仓库为 window.go.app.App（见 frontend/wailsjs/go/app/App.js）。
// 兼容 go.main.App 写法，避免硬编码查不到而静默回退到 HTTP。
function wailsApp() {
    const go = window.go;
    return go?.app?.App ?? go?.main?.App;
}
/** 是否运行在 Wails 桌面壳内：窗口控制等能力仅桌面端可用。 */
export function hasWailsRuntime() {
    return wailsApp() !== undefined;
}
/**
 * 运行平台（同步判断，保证首帧就用对样式）：windows / darwin / linux / other。
 *
 * 用 UA 而不是 `runtime.Environment()`：后者是异步 IPC，等它返回时首帧已经画完，
 * 会先闪一下别平台的窗口留白。桌面壳里的 UA 由系统 WebView 给出
 * （Windows 是 WebView2，串里必有 "Windows"），区分三端足够可靠。
 */
export function runtimePlatform() {
    const ua = navigator.userAgent;
    if (ua.includes('Windows'))
        return 'windows';
    if (ua.includes('Macintosh') || ua.includes('Mac OS X'))
        return 'darwin';
    if (ua.includes('Linux') || ua.includes('X11'))
        return 'linux';
    return 'other';
}
/**
 * 订阅 Go 侧 EventsEmit（桌面端）；devserver 无此通道。
 * 统一转发为 window CustomEvent，App 用 addEventListener 接。
 */
/** 浏览器态的 SSE 事件桥（devserver /api/events）：按事件名分发，多条 handler 共用一条连接。 */
let eventSource = null;
const eventHandlers = new Map();
function ensureEventSource() {
    if (eventSource)
        return;
    eventSource = new EventSource('/api/events');
    eventSource.onmessage = (ev) => {
        try {
            const { name, payload } = JSON.parse(ev.data);
            eventHandlers.get(name)?.forEach((h) => h(payload));
        }
        catch {
            // 忽略坏帧
        }
    };
}
export function onAppEvent(name, handler) {
    const rt = window.runtime;
    if (rt?.EventsOn) {
        rt.EventsOn(name, handler);
        return;
    }
    // 浏览器调试：无 Wails 事件总线，走 devserver 的 SSE
    ensureEventSource();
    if (!eventHandlers.has(name))
        eventHandlers.set(name, new Set());
    eventHandlers.get(name)?.add(handler);
}
/** 自绘标题栏的窗口控制（无边框模式下替代系统装饰）。 */
export const windowCtl = {
    minimise: () => void wailsApp()?.WindowMinimise(),
    toggleMaximise: () => void wailsApp()?.WindowToggleMaximise(),
    // 先放行关闭守卫（见 Go 侧 WindowShouldClose）再 Quit，避免程序化退出被 Alt+F4 拦截逻辑挡住
    quit: async () => {
        await wailsApp()?.SetQuitAllowed(true);
        await wailsApp()?.Quit();
    },
    // 任务栏驻留开启时的「关闭」：隐藏窗口（数据仍在，托盘菜单可再唤出）
    hideToTray: () => void wailsApp()?.HideToTray(),
    isMaximised: async () => (await wailsApp()?.WindowIsMaximised()) === true,
};
async function call(method, ...args) {
    const fn = wailsApp()?.[method];
    if (fn) {
        return fn(...args);
    }
    // devserver 反射桥要求：POST + application/json（跨站简单请求打不进来）。
    // 若 devserver 用 -token 启动（绑非回环地址时必填），令牌从 localStorage 读并随请求带上。
    const headers = { 'Content-Type': 'application/json' };
    const devToken = localStorage.getItem('dev.token');
    if (devToken)
        headers['X-Dev-Token'] = devToken;
    const res = await fetch(`/api/App/${method}`, {
        method: 'POST',
        headers,
        body: JSON.stringify(args),
    });
    const text = await res.text();
    // 桌面端若误走 HTTP（Wails 资源服务对 POST 返回 405），这里给出可定位的报错而非 JSON 解析异常
    if (!res.ok || !text.trim().startsWith('{')) {
        throw new Error(i18n.global.t('ipc.callFailed', { method, status: res.status }));
    }
    const payload = JSON.parse(text);
    if (!payload.ok) {
        throw new Error(payload.error ?? i18n.global.t('ipc.callFailedGeneric'));
    }
    return payload.data;
}
// Go 侧 nil 切片会序列化为 null（params/headers/body.form/envs/tree），
// 在 IPC 边界统一归一化，组件层可直接按 types.ts 的必填数组契约使用。
function normalizeRequest(r) {
    return {
        ...r,
        params: r.params ?? [],
        headers: r.headers ?? [],
        auth: r.auth ?? { type: 'none' },
        body: {
            type: r.body?.type ?? 'none',
            raw: r.body?.raw ?? '',
            // data（Bruno 结构化 JSON 体）必须原样保留：之前在这里被剥掉，
            // 导致导入 Bruno 后再保存/同步时 body.data 丢失（T5）。
            data: r.body?.data ?? '',
            form: r.body?.form ?? [],
            // descs（JSON 体字段备注，share Body.Descs）同样必须在边界保留：
            // 之前被重建剥掉，重载后表格视图备注与生成文档的请求体表丢备注。
            descs: r.body?.descs,
            // fields（请求体字段表格，share Body.Fields）是表格的唯一真源：
            // 边界重建漏掉它，重载后用户手写的整张表凭空消失。
            fields: r.body?.fields ?? [],
        },
        // grpc 段的 imports/metadata 同样是可省略字段（Go 侧 omitempty）：归一成数组，界面免判空
        grpc: r.grpc
            ? { ...r.grpc, imports: r.grpc.imports ?? [], metadata: r.grpc.metadata ?? [] }
            : null,
    };
}
function normalizeInfo(info) {
    return {
        ...info,
        tree: info.tree ?? [],
        envs: (info.envs ?? []).map((e) => ({ ...e, vars: e.vars ?? [] })),
    };
}
/** 多根概要：树的形状与 CollectionInfo 一致，这里逐项归一化（null 树会让渲染层炸）。 */
function normalizeSummaries(list) {
    return (list ?? []).map((s) => ({ ...s, info: normalizeInfo(s.info) }));
}
function normalizeSend(res) {
    return { ...res, headers: res.headers ?? [], warnings: res.warnings ?? [] };
}
function normalizeResolve(res) {
    return { ...res, missing: res.missing ?? [], values: res.values ?? {} };
}
function normalizeExample(ex) {
    return {
        ...ex,
        request: {
            ...ex.request,
            headers: ex.request?.headers ?? [],
            body: {
                type: ex.request?.body?.type ?? 'none',
                raw: ex.request?.body?.raw ?? '',
                data: ex.request?.body?.data ?? '',
                form: ex.request?.body?.form ?? [],
                // 历史快照的请求体也会被重建：这里同样必须保留 fields，否则回看历史时表格为空。
                fields: ex.request?.body?.fields ?? [],
            },
        },
        response: { ...ex.response, headers: ex.response?.headers ?? [] },
    };
}
export const api = {
    /** 是否开发版构建（dev 标签）：标题栏 dev 气泡与 -dev 窗口标题用。 */
    isDev: () => call('IsDev'),
    pickDirectory: () => call('PickDirectory'),
    pickFile: () => call('PickFile'),
    /** 打开工作目录（多根：追加并置为活动根；同一目录重复打开是幂等的）。 */
    openCollection: (dir) => call('OpenCollection', dir).then(normalizeInfo),
    /** 全部已打开的工作目录（按打开顺序），用于侧栏单树多根。 */
    listCollections: () => call('ListCollections').then(normalizeSummaries),
    /** 当前活动根的标识（空串 = 未打开任何目录）。 */
    activeCollection: () => call('ActiveCollection'),
    /** 切换活动根（集合级动作都作用于活动根）。 */
    setActiveCollection: (root) => call('SetActiveCollection', root),
    /** 关闭一个工作目录（其它根不受影响）。 */
    closeCollection: (root) => call('CloseCollection', root),
    /** 拖动集合调整侧栏根行顺序（roots 传 ListCollections 的 root，按目标顺序）。 */
    reorderCollections: (roots) => call('ReorderCollections', roots),
    /** 在系统文件管理器中打开该工作目录。 */
    revealInFolder: (root) => call('RevealInFolder', root),
    /** 重载指定工作目录（外部改动 / git 操作后）；不传 = 活动根。 */
    reloadCollectionOf: (root = '') => call('ReloadCollectionOf', root).then(normalizeInfo),
    reload: () => call('ReloadCollection').then(normalizeInfo),
    createRequest: (folder, name, method) => call('CreateRequest', folder, name, method).then(normalizeRequest),
    /** 新建流程：把内存草稿落盘成新请求（uid/路径/序号由集合层分配）。 */
    createRequestFromDraft: (folder, name, doc) => call('CreateRequestFromDraft', folder, name, doc).then(normalizeRequest),
    /** 导入 cURL：把 curl 命令解析成请求草稿（不落盘）。 */
    parseCurl: (text) => call('ParseCurl', text).then(normalizeRequest),
    /** gRPC：选择 .proto 定义（可多选，带 .proto 过滤；桌面端原生对话框）。 */
    pickGrpcProtos: () => call('PickGrpcProtoFiles').then((v) => v ?? []),
    /** gRPC：导入 .proto（先编译校验，成功才复制进集合 protos/ 并重新解析；失败不落盘）。 */
    importGrpcProtos: (files, imports) => call('ImportGrpcProtos', files, imports),
    /** gRPC：集合内已导入的定义清单（免点文件选择器）。 */
    listGrpcProtos: () => call('ListGrpcProtos').then((v) => v ?? []),
    /** gRPC：删除集合内的定义（引用检查由界面负责）。 */
    removeGrpcProto: (rel) => call('RemoveGrpcProto', rel),
    /** gRPC：解析请求里已保存的定义（打开请求 / 更新定义后调用，命中缓存）。 */
    loadGrpcSchema: (protoRel, imports) => call('LoadGrpcSchema', protoRel, imports),
    /** gRPC：入参消息的字段提示（Message 分段；G5.4）。 */
    grpcMessageFields: (protoRel, imports, service, method) => call('GrpcMessageFields', protoRel, imports, service, method).then((v) => v ?? []),
    /** gRPC：按定义校验请求消息（Message 分段的内联提示；G5.3）。空串 = 通过。 */
    grpcValidateMessage: (protoRel, imports, service, method, message) => call('GrpcValidateMessage', protoRel, imports, service, method, message),
    /** gRPC：按定义生成请求消息样例（protojson 文本；G5.2）。 */
    grpcSampleMessage: (protoRel, imports, service, method) => call('GrpcSampleMessage', protoRel, imports, service, method),
    /** gRPC：集合级默认定义（P8；未配置返回 null）。 */
    getGrpcDefault: () => call('GetGrpcDefault'),
    /** gRPC：写集合级默认定义（proto 为空 = 清除）。 */
    setGrpcDefault: (proto, imports) => call('SetGrpcDefault', proto, imports),
    /** 集合级公共请求头/认证（清单 defaults 段）；未配置时返回空结构。 */
    getCollectionDefaults: () => call('GetCollectionDefaults'),
    /** 保存集合级公共请求头/认证（全空 = 清除 defaults 段）。 */
    setCollectionDefaults: (d) => call('SetCollectionDefaults', d),
    /** 字段字典：当前集合的条目（项目级，随集合同步）。 */
    listFieldMappings: () => call('ListFieldMappings').then((v) => v ?? []),
    /** 字段字典：新增或更新一条（uid 为空 = 新建），返回落盘后的条目。 */
    saveFieldMapping: (fm) => call('SaveFieldMapping', fm),
    /** 字段字典：删除一条。 */
    deleteFieldMapping: (uid) => call('DeleteFieldMapping', uid),
    createFolder: (parent, name) => call('CreateFolder', parent, name),
    renameFolder: (uid, name) => call('RenameFolder', uid, name),
    deleteFolder: (uid) => call('DeleteFolder', uid),
    /** 递归删除分组：整棵进 .trash（前端须先勾选「删除所有子项」）。 */
    deleteFolderTree: (uid) => call('DeleteFolderTree', uid),
    readRequest: (uid) => call('ReadRequest', uid).then(normalizeRequest),
    saveRequest: (r) => call('SaveRequest', r),
    renameRequest: (uid, name) => call('RenameRequest', uid, name),
    moveRequest: (uid, destFolder) => call('MoveRequest', uid, destFolder),
    moveFolder: (uid, destParent) => call('MoveFolder', uid, destParent),
    /**
     * 拖动排序：把节点放到目标分组的指定位置（beforeUID 空 = 同类型末尾）。
     * 与 moveRequest/moveDoc/moveFolder 的区别：同时改上级并按落点重排同层 seq。
     */
    arrangeNode: (uid, destFolder, beforeUID) => call('ArrangeNode', uid, destFolder, beforeUID),
    /** 跨工作目录移动请求：目标侧重建（新 uid/路径），源文件进源集合 .trash；返回新 uid。 */
    moveRequestToCollection: (srcRoot, uid, destRoot, destFolder) => call('MoveRequestToCollection', srcRoot, uid, destRoot, destFolder),
    /**
     * 跨工作目录移动分组（连同其下子分组与请求）：目标侧整棵重建，源分组进源集合 .trash。
     * 返回**被搬走的源请求 uid**（界面据此清掉源根里已失效的标签）。
     */
    moveFolderToCollection: (srcRoot, uid, destRoot, destParent) => call('MoveFolderToCollection', srcRoot, uid, destRoot, destParent),
    deleteRequest: (uid) => call('DeleteRequest', uid),
    saveEnv: (env) => call('SaveEnv', env),
    /** 全局变量（服务端 scope=3 的只读镜像，落在集合根 globals.yml） */
    listGlobals: () => call('ListGlobals'),
    /** 给环境改名：两个文件（主文件 + secrets）一起搬，旧文件进 .trash */
    renameEnv: (oldName, newName) => call('RenameEnv', oldName, newName, ''),
    deleteEnv: (name) => call('DeleteEnv', name),
    send: (r, envName) => call('SendRequest', r, envName).then(normalizeSend),
    cancelSend: (uid) => call('CancelSend', uid),
    saveResponseBody: (defaultName, binary, body) => call('SaveResponseBody', defaultName, binary, body),
    listResponseExamples: (reqUid) => call('ListResponseExamples', reqUid).then((v) => (v ?? []).map(normalizeExample)),
    saveResponseExample: (r, name, res) => call('SaveResponseExample', r, name, res).then(normalizeExample),
    deleteResponseExample: (reqUid, exampleUid) => call('DeleteResponseExample', reqUid, exampleUid),
    // 响应字段 UI → 请求模型 RespDoc（落盘并随同步往返）
    updateResponseDoc: (uid, doc) => 
    // 快照字段必须一并下发（item 18）：曾因只重建 type/params/sample 而丢 status/headers；
    // contentType 同理（否则编辑字段/数据时会把「响应类型」重置回 json）。
    call('UpdateResponseDoc', {
        uid,
        type: doc.type,
        params: doc.params,
        sample: doc.sample,
        contentType: doc.contentType,
        status: doc.status,
        headers: doc.headers,
    }),
    resolveText: (text, envName) => call('ResolveText', text, envName).then(normalizeResolve),
    getSettings: () => call('GetSettings'),
    saveSettings: (s) => call('SaveSettings', s),
    /** MCP 默认端口（后端按构建标签下发，前端不写死） */
    mcpPortDefault: () => call('MCPPortDefault'),
    mcpStatus: () => call('MCPStatusText'),
    applyMCPSettings: () => call('ApplyMCPSettings'),
    regenerateMCPToken: () => call('RegenerateMCPToken'),
    /** T2：对风险工具授权弹窗给出决策 */
    answerMCPAuthz: (id, decision) => call('AnswerMCPAuthz', id, decision),
    /** T2：「始终允许」授权清单管理 */
    listMCPGrants: () => call('ListMCPGrants').then((v) => v ?? []),
    removeMCPGrant: (tool, project) => call('RemoveMCPGrant', tool, project),
    clearMCPGrants: () => call('ClearMCPGrants'),
    /** T3：MCP 调用审计查询/导出/清空 */
    listMCPAudit: (q) => call('ListMCPAudit', q),
    exportMCPAudit: (dest = '') => call('ExportMCPAudit', dest),
    clearMCPAudit: () => call('ClearMCPAudit'),
    listHistory: (limit) => call('ListHistory', limit).then((v) => v ?? []),
    clearHistory: () => call('ClearHistory'),
    /** 回收站列表（item 20；root 空 = 活动工作区）。 */
    listTrash: (root = '') => call('ListTrashFor', root).then((v) => v ?? []),
    /** 预览回收站条目（item 21）：文件给内容（截断），目录给文件清单。 */
    previewTrash: (file, root = '') => call('PreviewTrashFor', root, file),
    /** 彻底删除回收站条目（item 21；不可恢复，调用前必须二次确认）。 */
    deleteFromTrash: (file, root = '') => call('DeleteFromTrash', file, root),
    /** 恢复回收站条目：targetRoot 空 = 原工作区原位；否则恢复到指定（已打开）工作区的同一相对路径。 */
    restoreTrash: (file, root = '', targetRoot = '') => call('RestoreTrash', file, root, targetRoot),
    listCookies: () => call('ListCookies').then((v) => v ?? []),
    clearCookies: () => call('ClearCookies'),
    deleteCookie: (domain, name, path) => call('DeleteCookie', domain, name, path),
    searchIndex: (q, limit = 200) => call('SearchIndex', q, limit).then((v) => v ?? []),
    // 按工作目录检索索引（root 为空 = 活动根）；多根侧栏/面板逐根取 URL·方法·路径命中集
    searchIndexFor: (root, q, limit = 200) => call('SearchIndexFor', root, q, limit).then((v) => v ?? []),
    /** 预留（无 UI 入口）：对应 Go 门面 RebuildIndex，索引现由文件监听/保存自动维护；保留桥接以便手动重建排障。 */
    rebuildIndex: () => call('RebuildIndex'),
    generateCode: (lang, envName, r) => call('GenerateCode', lang, envName, r),
    importCollection: (parent, format, text) => call('ImportCollection', parent, format, text),
    importBrunoDir: (srcDir) => call('ImportBrunoDir', srcDir),
    exportDoc: (format, defaultName) => call('ExportDoc', format, defaultName),
    /** RM-02：运行当前集合（读取磁盘文件），返回逐请求报告。 */
    runCollection: (opts) => call('RunCollection', opts),
    /** 导出最近一次运行报告（junit / json / text）。 */
    exportRunReport: (format, defaultName) => call('ExportRunReport', format, defaultName),
    startMock: (port) => call('StartMock', port),
    stopMock: () => call('StopMock'),
    mockStatus: () => call('MockStatus'),
    // 预留（无 UI 入口）：活动根版同步门面。多根改造后 UI 一律走下面的 *For 变体（显式传根），
    // 这几个「隐式活动根」入口保留以维持与 Go 门面 / wailsjs 绑定的契约对齐，暂无调用方。
    getSyncBind: () => call('GetSyncBind'),
    setSyncBind: (serverUrl, projectId, mode, token) => call('SetSyncBind', serverUrl, projectId, mode, token),
    unbindSync: () => call('UnbindSync'),
    runSync: (token) => call('RunSync', token),
    // 按根（工作目录）版本：侧栏根行刷新/非活动根的冲突页用，不切换活动根
    getSyncBindFor: (root) => call('GetSyncBindFor', root),
    // 项目发现：用当前地址 + 令牌列服务端可见项目（避免手填数字 ID 出错）
    listServerProjects: (serverUrl, token) => call('ListServerProjects', serverUrl, token).then((v) => v ?? []),
    setSyncBindFor: (req) => call('SetSyncBindFor', req),
    unbindSyncFor: (root) => call('UnbindSyncFor', root),
    runSyncForRoot: (root) => call('RunSyncForRoot', root),
    // 同步口令（secret 变量加解密）：只查询「是否已设置」，永不回传口令本身
    getSyncPassphraseSet: () => call('GetSyncPassphraseSet'),
    setSyncPassphrase: (pass) => call('SetSyncPassphrase', pass),
    listConflictsFor: (root) => call('ListConflictsFor', root).then((v) => v ?? []),
    getConflictDetailFor: (root, file) => call('ConflictDetailFor', root, file),
    resolveConflictFor: (root, file, choice) => call('ResolveConflictFor', root, file, choice),
    // 预留（无 UI 入口）：活动根版冲突门面。UI 走上面的 *For 变体（侧栏冲突页按根展示）。
    listConflicts: () => call('ListConflicts').then((v) => v ?? []),
    getConflictDetail: (file) => call('ConflictDetail', file),
    resolveConflict: (file, choice) => call('ResolveConflict', file, choice),
    // 文档条目（B19）门面：树叶子 + 独立页签（与请求同构）。folder 为空 = docs/ 默认位。
    listDocs: () => call('ListDocs').then((v) => v ?? []),
    readDoc: (uid) => call('ReadDoc', uid),
    createDoc: (name, folder = '') => call('CreateDoc', name, folder),
    saveDoc: (d) => call('SaveDoc', d),
    /** B19：带期望哈希保存（磁盘 hash 不一致返回 [conflict] 前缀错误） */
    saveDocExpectHash: (d, expectHash) => call('SaveDocExpectHash', d, expectHash),
    /** 文档图片：base64（可带 data URL 前缀）→ /doc-assets/<sha256>（Markdown 编辑器上传用） */
    saveDocImage: (dataBase64) => call('SaveDocImage', dataBase64),
    /** B19：文档文件当前磁盘 hash（文档不进索引，单独取；空串 = 取不到） */
    docFileHash: (uid) => call('DocFileHash', uid),
    deleteDoc: (uid) => call('DeleteDoc', uid),
    moveDoc: (uid, destFolder) => call('MoveDoc', uid, destFolder),
    renameDoc: (uid, name) => call('RenameDoc', uid, name),
    getSyncStatus: () => call('GetSyncStatus'),
    startAutoSync: (intervalSec) => call('StartAutoSync', intervalSec),
    /** 预留（无 UI 入口）：对应 Go 门面 StopAutoSync；切根/关根时后端已内部停表，前端未单设「停止自动同步」按钮。 */
    stopAutoSync: () => call('StopAutoSync'),
};

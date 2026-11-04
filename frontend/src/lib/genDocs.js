/** 类型列留空时的默认值（JSON 类型名，不随语言变化）。 */
const DEFAULT_TYPE = 'string';
/** Markdown 表格单元格转义：管道符与换行会破坏表格行。 */
function esc(text) {
    return (text ?? '').replace(/\|/g, '\\|').replace(/\r?\n/g, ' ').trim();
}
/** 提取 URL 的 scheme://authority（协议+主机[:端口]），找不到返回空串。 */
function authorityOf(url) {
    const m = url.match(/^[a-zA-Z][a-zA-Z0-9+.-]*:\/\/[^/?#]+/);
    return m ? m[0] : '';
}
/** 把 URL 主机地址替换成 {{host}} 占位（保留路径 / 查询 / 锚点）；非绝对 URL 原样返回。 */
function maskHost(url) {
    const auth = authorityOf(url);
    return auth ? `{{host}}${url.slice(auth.length)}` : url;
}
/** 只保留 URL 的路径部分：去掉查询串与锚点（查询参数交由「请求参数」表体现）。 */
function urlPathOnly(url) {
    return url.replace(/[?#].*$/, '');
}
/** 把 JSON 原文缩进 2 格美化；解析失败（含 {{变量}} 占位）原样返回。 */
function prettyJson(text) {
    try {
        return JSON.stringify(JSON.parse(text), null, 2);
    }
    catch {
        return text.trim();
    }
}
export function buildApiDoc(input) {
    const { request: r, curl, fields, exampleBody, labels: L } = input;
    const out = [];
    // 简要描述
    const title = r.name || r.url;
    if (title)
        out.push(`## ${L.desc}`, `- ${title}`, '');
    // 请求URI（只留路径，查询参数由下方「请求参数」表体现）
    const rawUrl = r.grpc ? (r.grpc.target ?? '') : (r.url ?? '');
    const urlLine = maskHost(urlPathOnly(rawUrl));
    if (urlLine)
        out.push(`## ${L.uri}`, `- ${urlLine}`, '');
    // 请求方式
    if (r.method)
        out.push(`## ${L.method}`, `- ${r.method}`, '');
    // 请求头：`- **名称**: 值`，每条启用且命名的 header 一行
    const headers = r.headers.filter((h) => h.enabled && h.name.trim());
    if (headers.length) {
        out.push(`## ${L.headers}`);
        for (const h of headers)
            out.push(`- **${esc(h.name)}**: ${esc(h.value)}`);
        out.push('');
    }
    // 请求参数及说明（query 参数）：导出文档保留全部有名字的行；
    // 勾选状态只表示「必选」，取消勾选不能让参数从文档里消失。类型默认 string。
    const params = r.params.filter((p) => p.name.trim());
    if (params.length) {
        const hdr = [L.colName, L.colRequired, L.colType, L.colDesc].join(' | ');
        out.push(`## ${L.params}`, `| ${hdr} |`, '| --- | --- | --- | --- |');
        for (const p of params)
            out.push(`| ${esc(p.name)} | ${p.enabled ? L.requiredYes : L.requiredNo} | ${DEFAULT_TYPE} | ${esc(p.description ?? '')} |`);
        out.push('');
    }
    // 请求体：与查询参数**并存互不影响**（同章节内参数表在前、请求体在后）。
    // json 等原始体：先附「请求体字段」表（数据取 body.fields，5 列），再给原文代码块；
    // form/multipart 字段另起一张表，与 B14 导出语义一致。
    // 表格为空（用户没点「生成数据表格」）时只输出原文，不出表。
    const body = r.body;
    const bodyRaw = (body?.raw ?? '').trim();
    const bodyForm = (body?.form ?? []).filter((f) => f.enabled && f.name.trim());
    const bodyFields = body?.type === 'json' ? (body.fields ?? []) : [];
    if (body && body.type !== 'none' && (bodyRaw || bodyForm.length || bodyFields.length)) {
        out.push(`## ${L.body}`, `- ${body.type}`);
        if (bodyFields.length) {
            const hdr = [L.colName, L.colRequired, L.colType, L.colExample, L.colDesc].join(' | ');
            out.push('', `**${L.bodyParams}**`, '', `| ${hdr} |`, '| --- | --- | --- | --- | --- |');
            for (const f of bodyFields)
                out.push(`| ${esc(f.name)} | ${f.required ? L.requiredYes : L.requiredNo} | ${esc(f.type) || DEFAULT_TYPE} | ${esc(f.example)} | ${esc(f.description)} |`);
        }
        if (bodyRaw) {
            const lang = body.type === 'json' ? 'json' : '';
            out.push('', '```' + lang, bodyRaw, '```');
        }
        if (bodyForm.length) {
            const hdr = [L.colName, L.colValue, L.colDesc].join(' | ');
            out.push('', `| ${hdr} |`, '| --- | --- | --- |');
            for (const f of bodyForm)
                out.push(`| ${esc(f.name)} | ${esc(f.value)} | ${esc(f.description ?? '')} |`);
        }
        out.push('');
    }
    // 请求示例 curl（把生成命令里的主机也替换成 {{host}}）
    const curlAuth = authorityOf(rawUrl);
    const curlOut = curlAuth && curl.includes(curlAuth) ? curl.split(curlAuth).join('{{host}}') : curl;
    // JSON 体的「请求数据示例」：缩进美化后的原文（含变量占位则原样），与 curl 同节展示
    const reqBodyJson = body && body.type === 'json' && bodyRaw ? prettyJson(bodyRaw) : '';
    if (curlOut.trim() || reqBodyJson) {
        out.push(`## ${L.reqExample}`, '');
        if (curlOut.trim())
            out.push('```shell', curlOut.trimEnd(), '```', '');
        if (reqBodyJson)
            out.push('```json', reqBodyJson, '```', '');
    }
    // 响应字段（参数名=字段路径、必选按 required 写「是」/「否」、类型=f.type（空则 string）、
    // 示例值=f.value、说明=f.meaning；表格为空时不输出该章节，响应示例照常输出）
    if (fields.length) {
        const hdr = [L.colName, L.colRequired, L.colType, L.colExample, L.colDesc].join(' | ');
        out.push(`## ${L.resFields}`, `| ${hdr} |`, '| --- | --- | --- | --- | --- |');
        for (const f of fields)
            out.push(`| ${esc(f.path)} | ${f.required ? L.requiredYes : L.requiredNo} | ${esc(f.type) || DEFAULT_TYPE} | ${esc(f.value)} | ${esc(f.meaning)} |`);
        out.push('');
    }
    // 响应示例
    if (exampleBody.trim()) {
        out.push(`## ${L.resExample}`, '', '```json', exampleBody.trimEnd(), '```', '');
    }
    // 备注：空模板占位，留给用户手写
    out.push(`## ${L.note}`, '');
    return out.join('\n');
}

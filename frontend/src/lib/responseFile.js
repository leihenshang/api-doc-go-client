/** 视为「文件下载」的 URL 末段扩展名（保守：不含 .json/.xml —— 那些通常当结构化数据在线看）。 */
const FILE_EXT = /\.(html?|md|markdown|mdx|txt|text|log|csv|tsv|rtf|pdf|docx?|xlsx?|pptx?|zip|gz|tgz|bz2|xz|7z|rar|tar|iso|dmg|exe|jar|msi|png|jpe?g|gif|webp|avif|bmp|ico|svg|webmanifest|mp3|mp4|wav|flac|avi|mov|mkv|woff2?|ttf|otf|eot)$/i;
/** content-type → 建议扩展名（文件名无扩展时兜底）。 */
const MIME_EXT = {
    'text/html': '.html',
    'text/markdown': '.md',
    'text/plain': '.txt',
    'text/csv': '.csv',
    'application/pdf': '.pdf',
    'application/zip': '.zip',
    'application/octet-stream': '.bin',
};
function headerValue(headers, name) {
    const want = name.toLowerCase();
    const h = headers?.find((x) => x.name?.toLowerCase() === want);
    return h?.value ?? '';
}
/** 从 Content-Disposition 抽 filename / filename*（RFC 5987 的 UTF-8''xxx 去掉编码前缀）。 */
export function dispositionFilename(value) {
    if (!value)
        return '';
    const star = /filename\*\s*=\s*[^']*''([^;]+)/i.exec(value);
    if (star?.[1]) {
        try {
            return decodeURIComponent(star[1].trim());
        }
        catch {
            return star[1].trim();
        }
    }
    const plain = /filename\s*=\s*("([^"]*)"|([^;]+))/i.exec(value);
    const raw = (plain?.[2] ?? plain?.[3] ?? '').trim();
    return raw;
}
/** URL（含 query）最后一段路径的文件名，判断是否像文件。 */
function urlBasename(rawUrl) {
    let path = rawUrl;
    const q = path.indexOf('?');
    if (q >= 0)
        path = path.slice(0, q);
    const h = path.indexOf('#');
    if (h >= 0)
        path = path.slice(0, h);
    const seg = path.split('/').filter(Boolean).pop() ?? '';
    try {
        return decodeURIComponent(seg);
    }
    catch {
        return seg;
    }
}
/**
 * 判定响应是否为「文件类」并给出下载文件名。命中任一条件即视为文件：
 * ① Content-Disposition 带 filename（attachment 或 inline 都算，说明服务端把它当一份文件）；
 * ② 请求 URL 末段是已知文档/资源扩展名（如 xxx/file.md、report.csv）。
 * 注：纯二进制但既无附件名、URL 也无扩展（如内部端点 /binary）不改判，仍走原 base64 提示，避免误伤。
 * 文件名优先级：disposition > URL 末段 > response+MIME 扩展。
 */
export function detectFileResponse(input) {
    const dispo = dispositionFilename(headerValue(input.headers, 'content-disposition'));
    const base = urlBasename(input.url ?? '');
    const urlIsFile = FILE_EXT.test(base);
    const isFile = !!dispo || urlIsFile;
    if (!isFile)
        return { isFile: false, filename: '' };
    let filename = dispo || (urlIsFile ? base : '');
    if (!filename) {
        const ct = (input.contentType ?? '').split(';')[0].trim().toLowerCase();
        filename = 'response' + (MIME_EXT[ct] ?? (input.binary ? '.bin' : '.txt'));
    }
    return { isFile: true, filename };
}

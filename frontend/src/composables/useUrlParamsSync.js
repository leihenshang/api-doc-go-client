import { ref, watch } from 'vue';
/** 各页签上一次见到的 URL：用来区分「用户改了 URL」与「切页签导致取值自然变化」。
 *  模块级跨组件实例常驻（切 tab 时组件不卸载也要保留基线）。 */
const lastUrlByTab = new Map();
/**
 * 建立 URL ↔ Params 联动，返回参数表变更时应调用的 onParamsChange。
 * 内部维护两个 watch：URL 改动回填 params、页签集合收缩时回收 lastUrlByTab。
 */
export function useUrlParamsSync(ctx) {
    const syncingFrom = ref(null);
    /** 从 URL 的 query 段回填 params（URL 编辑时调用）。 */
    function syncParamsFromUrl() {
        syncingFrom.value = 'url';
        try {
            const req = ctx.getRequest();
            const raw = req.url;
            const idx = raw.indexOf('?');
            const queryStr = idx >= 0 ? raw.slice(idx + 1) : '';
            const existing = new Map(req.params.map((p) => [p.name, p]));
            const next = [];
            if (queryStr) {
                for (const pair of queryStr.split('&')) {
                    if (!pair)
                        continue;
                    const eq = pair.indexOf('=');
                    const name = decodeURIComponent(eq < 0 ? pair : pair.slice(0, eq));
                    const value = eq < 0 ? '' : decodeURIComponent(pair.slice(eq + 1));
                    if (!name)
                        continue;
                    const old = existing.get(name);
                    next.push({ name, value, enabled: old?.enabled ?? true, description: old?.description });
                }
            }
            // 保留表格里手动加的、URL 里没有的未启用行
            for (const p of req.params) {
                if (p.name && !queryStr.includes(encodeURIComponent(p.name))) {
                    next.push(p);
                }
            }
            req.params.splice(0, req.params.length, ...next);
        }
        finally {
            setTimeout(() => (syncingFrom.value = null), 0);
        }
    }
    /** 从 params 重拼 URL 的 query 段（参数表编辑时调用）。 */
    function syncUrlFromParams() {
        syncingFrom.value = 'params';
        try {
            const req = ctx.getRequest();
            const raw = req.url;
            const idx = raw.indexOf('?');
            const base = idx >= 0 ? raw.slice(0, idx) : raw;
            const parts = [];
            for (const p of req.params) {
                if (!p.enabled || !p.name.trim())
                    continue;
                const k = encodeURIComponent(p.name.trim());
                const v = encodeURIComponent(p.value ?? '');
                parts.push(`${k}=${v}`);
            }
            req.url = parts.length ? `${base}?${parts.join('&')}` : base;
        }
        finally {
            setTimeout(() => (syncingFrom.value = null), 0);
        }
    }
    // URL 改了 → 回填 params（gRPC 没有 query 段，跳过）。
    // 切页签同样会走到这里 —— 取值是「当前 tab 的 url」，props.tab 一换取值就变了 ——
    // 但那是换了个请求、不是编辑：只重建参数表，不标脏。否则什么都没改也会冒出未保存圆点
    // （实测打开第二个请求、右键切换页签都会触发），关闭页签时还会为此多问一次。
    watch(() => [ctx.getKey(), ctx.getRequest().url], ([key, url]) => {
        const known = lastUrlByTab.get(key);
        const edited = known !== undefined && known !== url;
        // 记录必须放在守卫之前：被守卫挡掉的那次也要更新基线，否则下次会被误判成编辑
        lastUrlByTab.set(key, url);
        if (ctx.isGrpc() || ctx.isReadonly() || syncingFrom.value === 'params')
            return;
        syncParamsFromUrl();
        if (edited)
            ctx.touch();
    });
    // 模块级 Map 跨组件实例常驻：页签关闭后要回收对应 key，否则长期使用会累积已关闭页签的条目。
    watch(() => ctx.openKeys(), (keys) => {
        const alive = new Set(keys);
        for (const k of lastUrlByTab.keys()) {
            if (!alive.has(k))
                lastUrlByTab.delete(k);
        }
    });
    function onParamsChange() {
        if (ctx.isReadonly() || syncingFrom.value === 'url')
            return;
        syncUrlFromParams();
        ctx.touch();
    }
    return { onParamsChange };
}

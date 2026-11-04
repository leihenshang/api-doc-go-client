import { reactive } from 'vue';
const byTab = reactive(new Map());
/** 记录某页签右侧面板当前展示的响应（null = 空态）。 */
export function setCurrentResponse(tabKey, resp) {
    if (!tabKey)
        return;
    byTab.set(tabKey, resp);
}
/** 读取某页签右侧面板当前展示的响应；从未写入时返回 null。 */
export function getCurrentResponse(tabKey) {
    return byTab.get(tabKey) ?? null;
}
/** 页签切换/关闭时清掉记录，避免旧页签残留的数据被误当成「当前响应」。 */
export function clearCurrentResponse(tabKey) {
    if (!tabKey)
        return;
    byTab.delete(tabKey);
}

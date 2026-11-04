// 按 tab.key 记住二级页签/视图状态（仅本次应用运行，不落盘）。
// RequestEditor 会在同一个实例上切换 props.tab；切走再切回时恢复上次停留的分段，
// 而不是一律回到「请求参数 / 响应数据」。
export const requestSegByTab = new Map();
// RequestEditor「响应字段」分段内的子页签：body / headers / fields
export const respSubViewByTab = new Map();
// ResponsePanel（右侧实时响应）的二级页签与展示偏好
export const respPanelSegByTab = new Map();
export const respPanelViewByTab = new Map();
export const respPanelWrapByTab = new Map();

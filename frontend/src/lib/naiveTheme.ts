// naive-ui 主题覆盖：控件圆角/字号 + 主/信息色的明暗两套 + 少量组件微调。
// 色值属 naive 必需（naive 运行时吃真实色值，不吃 CSS 变量），故集中在此并注释「与 base.css 令牌同源」——
// 改 base.css 的 --app-accent / --app-info / --app-* 底色时，这里对应的 hex 必须同步改，否则主题不一致。
import type { GlobalThemeOverrides } from 'naive-ui'

const commonBase: GlobalThemeOverrides['common'] = {
    fontSize: '13px',
    borderRadius: '6px',
    borderRadiusSmall: '4px',
    fontFamily:
        "'Noto Sans SC Variable', -apple-system, BlinkMacSystemFont, 'Segoe UI', 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', sans-serif",
}

// 浅色：信息色 #2080f0（= --app-info）、底 #f5f5f5。
//
// 主色分两个角色（UI-07）：品牌绿 #18a058 保留给 logo/装饰（--app-accent），
// 但**可交互主色**（按钮底、选中态）必须满足白字 4.5:1 —— #18a058 只有 3.38:1，
// 因此动作色用加深一档的 #0f7a41（白字 5.41:1，hover #12814a 4.92:1，pressed #0c6b39 6.61:1）。
const commonLight: GlobalThemeOverrides['common'] = {
    ...commonBase,
    primaryColor: '#0f7a41',
    primaryColorHover: '#12814a',
    primaryColorPressed: '#0c6b39',
    primaryColorSuppl: '#0f7a41',
    infoColor: '#2080f0',
    infoColorHover: '#4098fc',
    infoColorPressed: '#1060c0',
    borderColor: '#e3e3e3',
    bodyColor: '#f5f5f5',
    cardColor: '#ffffff',
    textColorBase: '#202124',
}

// 暗色：沿用同一套语义，只把底色/边框换成 base.css 的深夜配值。
// 暗色背景上「亮绿 + 白字」只有 2.86:1：这里保留亮绿（作为色块/文字 5.96:1 没问题），
// 但把 primary 按钮的**文字**改成深色（见 Button.textColorPrimary），对比 5.96:1。
const commonDark: GlobalThemeOverrides['common'] = {
    ...commonBase,
    primaryColor: '#36ad6a',
    primaryColorHover: '#4cc47c',
    primaryColorPressed: '#2a9a5c',
    primaryColorSuppl: '#36ad6a',
    infoColor: '#4098fc',
    infoColorHover: '#5ea9ff',
    infoColorPressed: '#2a7fd4',
    borderColor: '#2b2d31',
    bodyColor: '#17181a',
    cardColor: '#1e1f22',
    textColorBase: '#e6e7e9',
}

/** 按当前是否暗色产出 naive 主题覆盖（App 的 computed 直接调用，色值与 base.css 令牌同源）。 */
export function buildThemeOverrides(dark: boolean): GlobalThemeOverrides {
    return {
        common: dark ? commonDark : commonLight,
        // 暗色下主按钮是亮绿底：白字只有 2.86:1，改用近黑的墨绿字（5.96:1）。
        // 浅色下主按钮是深绿底，白字 5.41:1 已达标，无需覆盖。
        Button: dark
            ? { textColorPrimary: '#06210f', textColorHoverPrimary: '#06210f', textColorPressedPrimary: '#06210f', textColorFocusPrimary: '#06210f' }
            : {},
        Card: {
            paddingMedium: '16px 20px',
            borderRadiusMedium: '10px',
        },
        DataTable: {
            thColor: dark ? '#202124' : '#fafafa',
            thFontWeight: '500',
        },
        // 浮层（保存▾ 等）：naive 默认那套 `0 9px 28px` 浓投影看着像"大边框盒子"，
        // 换成 app 的轻投影令牌 + 8px 圆角；底色浅色用面板白、暗色用 surface-3（否则与面板同色，糊在一起）
        Popover: {
            borderRadius: '8px',
            boxShadow: 'var(--app-shadow-menu)',
            color: dark ? 'var(--app-surface-3)' : 'var(--app-panel)',
            padding: '4px',
        },
    }
}

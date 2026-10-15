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

// 浅色：主色 #18a058（= --app-accent）、信息色 #2080f0（= --app-info）、底 #f5f5f5。
const commonLight: GlobalThemeOverrides['common'] = {
    ...commonBase,
    primaryColor: '#18a058',
    primaryColorHover: '#36ad6a',
    primaryColorPressed: '#0c7a43',
    primaryColorSuppl: '#18a058',
    infoColor: '#2080f0',
    infoColorHover: '#4098fc',
    infoColorPressed: '#1060c0',
    borderColor: '#e3e3e3',
    bodyColor: '#f5f5f5',
    cardColor: '#ffffff',
    textColorBase: '#202124',
}

// 暗色：沿用同一套语义，只把底色/边框换成 base.css 的深夜配值（主色提亮一档 = --app-accent 暗值）。
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

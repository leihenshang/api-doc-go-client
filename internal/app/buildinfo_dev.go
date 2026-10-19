//go:build dev || devbuild

// 开发版构建标记（wails dev 自动注入 dev 标签）：前端据此在标题栏渲染绿色 dev 气泡。
package app

// DevBuild 是否开发版构建。
const DevBuild = true

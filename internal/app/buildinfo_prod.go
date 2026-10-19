//go:build !(dev || devbuild)

// 正式版构建标记（不带 dev 标签）。
package app

// DevBuild 是否开发版构建。
const DevBuild = false

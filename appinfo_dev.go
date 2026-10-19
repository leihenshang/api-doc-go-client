//go:build dev || devbuild

// 开发版（wails dev 自动注入 dev 标签）：窗口标题带 -dev 后缀，任务栏/标题栏气泡据此区分。
package main

const appTitle = "api-doc-go-dev"

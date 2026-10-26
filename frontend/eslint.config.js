// ESLint 扁平配置：只做「推荐集」静态检查，**不引入 Prettier**（避免一次性重排既有文件）。
// 目标：拦住类型检查发现不了的真问题（未使用变量、floating promise、Vue 模板误用），不纠缠纯格式。
import pluginVue from 'eslint-plugin-vue'
import globals from 'globals'
import tseslint from 'typescript-eslint'

export default tseslint.config(
  {
    // 构建产物、Wails 生成的绑定与第三方目录不检查；v.mjs 是本地手工调试脚本（带固定绝对路径）
    ignores: ['dist/**', 'dist-*/**', 'node_modules/**', 'public/**', 'wailsjs/**', 'v.mjs', '*.min.js'],
  },
  ...tseslint.configs.recommended,
  ...pluginVue.configs['flat/recommended'],
  {
    files: ['**/*.{js,mjs,cjs,ts,mts,tsx,vue}'],
    languageOptions: {
      ecmaVersion: 'latest',
      sourceType: 'module',
      globals: { ...globals.browser, ...globals.node },
    },
    rules: {
      // 允许 _ 前缀的未使用参数（沿用既有写法）
      '@typescript-eslint/no-unused-vars': [
        'warn',
        { argsIgnorePattern: '^_', varsIgnorePattern: '^_', caughtErrorsIgnorePattern: '^_' },
      ],
      // 既有架构：tab / rows 是共享的可变模型对象（文件即真相源的编辑入口），不是值传递的 prop。
      // 子组件直接改它是有意设计（改完由父级统一落盘）；若改成 emit 会引入大量样板，收益不抵风险。
      'vue/no-mutating-props': 'off',
      // Overview / Sidebar / Toolbar / Welcome 等既有组件名是单字单词，改名会牵动模板与测试
      'vue/multi-word-component-names': 'off',
      // 纯风格类规则关闭：本仓库不做统一格式化（未引入 Prettier）
      'vue/max-attributes-per-line': 'off',
      'vue/singleline-html-element-content-newline': 'off',
      'vue/html-indent': 'off',
      'vue/html-self-closing': 'off',
      'vue/attributes-order': 'off',
      'vue/first-attribute-linebreak': 'off',
      'vue/html-closing-bracket-newline': 'off',
      'vue/html-closing-bracket-spacing': 'off',
      'vue/html-quotes': 'off',
      'vue/attribute-hyphenation': 'off',
      'vue/v-on-event-hyphenation': 'off',
      'vue/no-v-html': 'off', // Markdown 预览/代码块等场景确实需要
      'vue/multiline-html-element-content-newline': 'off',
      'vue/mustache-interpolation-spacing': 'off',
    },
  },
  {
    // 类型声明文件（Vue SFC shim 等）允许 `{}` 与 `any`
    files: ['**/*.d.ts'],
    rules: {
      '@typescript-eslint/no-empty-object-type': 'off',
      '@typescript-eslint/no-explicit-any': 'off',
    },
  },
  {
    // .vue 的 <script lang="ts"> 交给 TS 解析器
    files: ['**/*.vue'],
    languageOptions: {
      parserOptions: { parser: tseslint.parser },
    },
  },
  {
    // 配置文件与脚本放宽
    files: ['*.config.{js,ts,mjs}', 'vite.config.ts', 'playwright.config.ts'],
    rules: {
      '@typescript-eslint/no-explicit-any': 'off',
    },
  },
)

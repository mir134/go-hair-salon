import js from '@eslint/js'
import pluginVue from 'eslint-plugin-vue'
import globals from 'globals'
import tseslint from 'typescript-eslint'

// 前端分层（02-AGENTS.md:28-33）：views -> composables/stores -> api -> backend。
// 只有 src/api/** 可以直接依赖 axios；其余层必须走 @/api 暴露的方法。
const AXIOS_GUARD_MESSAGE =
  '禁止在 src/api 之外直接使用 axios：请通过 @/api 暴露的方法发起请求（views -> composables/stores -> api -> backend）。'

export default [
  { ignores: ['dist/**', 'node_modules/**'] },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  ...pluginVue.configs['flat/recommended'],
  {
    languageOptions: {
      globals: globals.browser,
    },
  },
  {
    // 让 <script setup lang="ts"> 走 TypeScript 解析
    files: ['**/*.vue'],
    languageOptions: {
      parserOptions: {
        parser: tseslint.parser,
      },
    },
  },
  {
    rules: {
      'no-restricted-imports': [
        'error',
        {
          paths: [{ name: 'axios', message: AXIOS_GUARD_MESSAGE }],
        },
      ],
    },
  },
  {
    // 唯一允许直连 axios 的目录
    files: ['src/api/**/*.ts'],
    rules: {
      'no-restricted-imports': 'off',
    },
  },
]

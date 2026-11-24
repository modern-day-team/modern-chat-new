import { globalIgnores } from 'eslint/config'
import { defineConfigWithVueTs, vueTsConfigs } from '@vue/eslint-config-typescript'
import pluginVue from 'eslint-plugin-vue'
import pluginOxlint from 'eslint-plugin-oxlint'
import skipFormatting from 'eslint-config-prettier/flat'

// 官网配置与主仓库 web 同体系；public/content 下是静态文档与插件副本，不参与源码 lint。
export default defineConfigWithVueTs(
  {
    name: 'website/files-to-lint',
    files: ['src/**/*.{vue,ts,mts,tsx}', 'scripts/**/*.ts'],
  },

  globalIgnores(['**/dist/**', '**/public/content/**']),

  ...pluginVue.configs['flat/essential'],
  vueTsConfigs.recommended,

  ...pluginOxlint.buildFromOxlintConfigFile('.oxlintrc.json'),

  skipFormatting,
)

import eslint from '@eslint/js'
import svelte from 'eslint-plugin-svelte'
import globals from 'globals'
import tseslint from 'typescript-eslint'

export default tseslint.config(
  eslint.configs.recommended,
  ...tseslint.configs.recommended,
  ...svelte.configs.recommended,
  {
    languageOptions: {
      globals: globals.browser,
    },
  },
  {
    files: ['**/*.svelte', '**/*.svelte.ts'],
    languageOptions: {
      parserOptions: {
        parser: tseslint.parser,
      },
    },
  },
  {
    files: ['scripts/**/*.ts'],
    languageOptions: {
      globals: {
        Bun: 'readonly',
        Buffer: 'readonly',
        process: 'readonly',
      },
    },
  },
  {
    ignores: ['dist/', 'node_modules/', 'coverage/', 'playwright-report/', 'test-results/'],
  },
)

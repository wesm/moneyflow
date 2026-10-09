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
    // Money stays exact: minor-unit strings and BigInt, never a JavaScript number.
    files: ['src/lib/money/**/*.ts', 'src/lib/charts/**/*.ts', 'src/lib/insights/**/*.ts'],
    rules: {
      'no-restricted-syntax': [
        'error',
        {
          selector: 'CallExpression[callee.name=/^(Number|parseFloat|parseInt)$/]',
          message: 'Keep money exact: use parseMinor and BigInt, not a number conversion.',
        },
        {
          selector: "CallExpression[callee.object.name='Number'][callee.property.name=/^parse/]",
          message: 'Keep money exact: use parseMinor and BigInt, not a number conversion.',
        },
        {
          selector: "UnaryExpression[operator='+']",
          message: 'Keep money exact: use parseMinor and BigInt, not unary plus.',
        },
      ],
    },
  },
  {
    ignores: ['dist/', 'node_modules/', 'coverage/', 'playwright-report/', 'test-results/'],
  },
)

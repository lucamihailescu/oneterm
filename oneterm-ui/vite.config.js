// Vite build for the Vue 2 codebase. Phase 1 of the Vue 3 migration:
// swap Vue CLI 4 / webpack 4 for Vite while staying on Vue 2 (bumped to 2.7
// because @vitejs/plugin-vue2 requires it). Once this lands, subsequent
// migration phases (Vue 3 core swap, library replacements, component
// pattern migrations) will all build faster and on a maintained toolchain.

import { defineConfig, loadEnv } from 'vite'
import vue from '@vitejs/plugin-vue2'
import vueJsx from '@vitejs/plugin-vue2-jsx'
import { createSvgPlugin } from 'vite-plugin-vue2-svg'
import path from 'path'
import fs from 'fs'
import { fileURLToPath } from 'url'

const __dirname = path.dirname(fileURLToPath(import.meta.url))

export default defineConfig(({ mode }) => {
  // Vite only auto-exposes VITE_-prefixed env vars to client code via
  // import.meta.env. The existing source uses process.env.VUE_APP_*; rather
  // than rewrite every reference, we read the .env files manually and
  // expose those variables via the `define` block below.
  const env = loadEnv(mode, process.cwd(), ['VUE_APP_', 'BASE_URL'])

  return {
    // The webpack build prepended public-path / BASE_URL via EJS in
    // index.html. Vite uses `base` instead and rewrites asset URLs at build
    // time, so the rest of the app keeps relative-to-root semantics.
    base: env.BASE_URL || '/',

    plugins: [
      // Vue 2.7 supports the Composition API natively; no shim needed.
      vue(),
      // Some .vue files use JSX in their render functions (the codebase
      // mixes templates and JSX for table column renderers). The JSX
      // plugin compiles those.
      vueJsx(),
      // Replaces vue-svg-icon-loader. Imports of `@/foo.svg?inline` resolve
      // to a Vue 2 component; bare `@/foo.svg` imports resolve to the URL.
      createSvgPlugin({
        defaultImport: 'url',
      }),
    ],

    resolve: {
      alias: {
        '@': path.resolve(__dirname, 'src'),
        // The legacy alias '@$' was set in vue.config.js; preserved here
        // for any existing imports that depend on it.
        '@$': path.resolve(__dirname, 'src'),
        // Less @import "~package" syntax (webpack convention) -> bare
        // package resolution under Vite.
        '~ant-design-vue': path.resolve(__dirname, 'node_modules/ant-design-vue'),
        '~@': path.resolve(__dirname, 'src'),
      },
      extensions: ['.mjs', '.js', '.ts', '.jsx', '.tsx', '.json', '.vue'],
    },

    css: {
      preprocessorOptions: {
        less: {
          // ant-design-vue@1.x customizes its primary color via Less
          // modifyVars; same setting that lived in vue.config.js.
          modifyVars: {
            'primary-color': '#2f54eb',
          },
          javascriptEnabled: true,
          // Replaces vue-cli-plugin-style-resources-loader: prepend the
          // shared Less helper file to every Less stylesheet.
          additionalData: fs.existsSync(path.resolve(__dirname, 'src/style/static.less'))
            ? `@import "${path.resolve(__dirname, 'src/style/static.less').replace(/\\/g, '/')}";`
            : '',
        },
      },
    },

    define: {
      // Bridge process.env.VUE_APP_* references in source to compile-time
      // constants without rewriting every callsite. This is intentionally
      // a phase 1 shim; a later phase can migrate these to
      // `import.meta.env.VITE_*` and rename the .env keys.
      'process.env.NODE_ENV': JSON.stringify(mode === 'development' ? 'development' : 'production'),
      'process.env.VUE_APP_API_BASE_URL': JSON.stringify(env.VUE_APP_API_BASE_URL || '/api'),
      'process.env.VUE_APP_PREVIEW': JSON.stringify(env.VUE_APP_PREVIEW || 'false'),
      'process.env.VUE_APP_BUILD_PACKAGES': JSON.stringify(env.VUE_APP_BUILD_PACKAGES || ''),
      'process.env.VUE_APP_IS_OUTER': JSON.stringify(env.VUE_APP_IS_OUTER || 'true'),
      'process.env.VUE_APP_IS_OPEN_SOURCE': JSON.stringify(env.VUE_APP_IS_OPEN_SOURCE || 'true'),
      'process.env.BASE_URL': JSON.stringify(env.BASE_URL || '/'),
    },

    server: {
      // Listen on every interface so the dev server is reachable from
      // containers / browsers on the LAN, matching `disableHostCheck: true`
      // semantics from the old vue.config.js.
      host: '0.0.0.0',
      port: Number(process.env.DEV_PORT) || 8000,
      // Same proxy convention the old setup expected: requests to /api
      // should hit the backend during local development.
      proxy: {
        '/api': {
          target: process.env.DEV_API_PROXY || 'http://127.0.0.1:8888',
          changeOrigin: true,
        },
      },
    },

    build: {
      // Match Vue CLI's default output dir so deploy/Dockerfile copy paths
      // ("/oneterm-ui/dist") keep working unchanged.
      outDir: 'dist',
      sourcemap: false,
      // Drop console.* in production (replaces babel-plugin-transform-
      // remove-console). esbuild handles this in a single pass.
      minify: 'esbuild',
      // ant-design-vue@1 imports named bindings (e.g. `isMoment`) from
      // moment, and src/utils/download.js does `import XLSX from 'xlsx'`.
      // Both target CommonJS modules whose .mjs builds don't expose those
      // bindings the way webpack's CJS-interop did. transformMixedEsModules
      // tells Rollup to treat such imports as named requires from the
      // module.exports object.
      commonjsOptions: {
        transformMixedEsModules: true,
        include: [/node_modules/],
      },
    },

    optimizeDeps: {
      // Pre-bundle these CJS deps with esbuild so dev mode also resolves
      // their named imports (esbuild handles CJS<->ESM interop more
      // permissively than Rollup).
      include: ['moment', 'xlsx', 'ant-design-vue'],
    },

    esbuild: {
      drop: mode === 'production' ? ['console', 'debugger'] : [],
    },
  }
})

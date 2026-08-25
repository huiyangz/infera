/// <reference types="vitest/config" />
import path from 'path'
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { tanstackRouter } from '@tanstack/router-plugin/vite'
import { playwright } from '@vitest/browser-playwright'

// https://vite.dev/config/
export default defineConfig({
  plugins: [
    tanstackRouter({
      target: 'react',
      autoCodeSplitting: true,
    }),
    react(),
    tailwindcss(),
  ],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  optimizeDeps: {
    // vitest-browser-react 的 render() 是动态 import('react-dom/client')，
    // vite 启动期的静态依赖扫描看不见它：全量并行跑测试时，首个 render() 执行
    // 才触发 "new dependencies found: react-dom/client" → 重新预构建 →
    // "optimized dependencies changed. reloading" 全体测试页中途 full-reload，
    // 正在动态加载的文件就偶发 "Failed to fetch dynamically imported module" /
    // "Vitest failed to find the current suite"（INFERA-255）。
    // 预置进 include 让它在首跑就一次构建完成，消除该竞态（冷缓存/新装依赖时最易复现）。
    include: ['react-dom/client'],
  },
  server: {
    // dev 下把 /api 与 /ws（WebSocket）转发到 infera Go 后端：
    // 前端一律用同源地址，环境差异收敛在代理层（不再硬编码 ws://localhost:8080）
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/ws': {
        target: 'http://localhost:8080',
        ws: true,
        changeOrigin: true,
      },
    },
  },
  test: {
    silent: 'passed-only',
    unstubEnvs: true,
    browser: {
      enabled: true,
      provider: playwright(),
      instances: [{ browser: 'chromium' }],
      // 本地跑测试不弹可见浏览器窗口，统一 headless
      headless: true,
    },
    coverage: {
      // include: ['src/**/*.{js,jsx,ts,tsx}'], // Uncomment to expand the report to all src/**/* so untested modules appear as 0% coverage.
      exclude: [
        'src/components/ui/**',
        'src/assets/**',
        'src/routeTree.gen.ts',
        'src/test-utils/**',
        'src/routes/**',
      ],
    },
  },
})

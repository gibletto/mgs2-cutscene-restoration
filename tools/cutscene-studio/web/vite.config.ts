import { defineConfig, type Plugin } from 'vite'
import react from '@vitejs/plugin-react'

// dist/ is build output and is gitignored, except dist/.keep: the Go binary embeds dist/
// and `go build` needs it to exist before the UI was ever built. Vite empties dist/ on
// every build, so emit the (empty) placeholder as part of the build.
const keepDist = (): Plugin => ({
  name: 'keep-dist-placeholder',
  apply: 'build',
  generateBundle() { this.emitFile({ type: 'asset', fileName: '.keep', source: '' }) },
})

// base is relative so the Go binary can embed dist/ and serve it from any mount point.
export default defineConfig({
  plugins: [react(), keepDist()],
  base: './',
  build: { outDir: 'dist', chunkSizeWarningLimit: 1200 },
  server: {
    proxy: {
      '/api': 'http://127.0.0.1:8080',
      '/ws': { target: 'ws://127.0.0.1:8080', ws: true },
    },
  },
})

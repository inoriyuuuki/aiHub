import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// Build into the Go server's embedded static directory.
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: '../internal/web/dist',
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
      '/mcp': 'http://localhost:8080',
    },
  },
})

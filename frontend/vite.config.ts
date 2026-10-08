import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import path from 'node:path'
import { defineConfig } from 'vite'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: { alias: { '@': path.resolve(import.meta.dirname, './src') } },
  server: {
    // MyGo loads this address during `mygo dev` (devUrl in mygo.json).
    port: 5173,
    strictPort: true,
    watch: { ignored: ['**/.mygo/**', '**/build/**'] },
  },
})

import { defineConfig } from 'vite'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [tailwindcss()],
  server: {
    port: 5173,
    host: true,
    proxy: {
      '/api': 'http://127.0.0.1:5392',
      '/files': 'http://127.0.0.1:5392',
      '/pxe': 'http://127.0.0.1:5392'
    }
  }
})

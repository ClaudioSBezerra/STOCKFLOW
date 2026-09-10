/// <reference types="vitest/config" />
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';
import path from 'node:path';

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    // Mesmo comportamento do proxy /api do nginx em produção
    // (frontend/nginx.conf): `npm run dev` roda em outra origem que a API
    // (backend em :8080, ver .env.example), então chamadas relativas a
    // /api/* precisam ser encaminhadas para o backend local.
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      // Story 9.1 (Multi-Empresa): toda rota de negócio vive sob
      // `/e/{slug}/api/...`. Sem esta chave o `npm run dev` serviria o
      // index.html do SPA no lugar da resposta da API.
      //
      // O prefixo `/e` também casa os deep-links do próprio SPA
      // (`/e/{slug}/pedidos`), que precisam continuar recebendo o index.html
      // — por isso o `bypass`: só o que casa `^/e/{slug}/api/` vai para o
      // backend; qualquer outro caminho sob `/e/` é devolvido ao dev server.
      // Em produção o nginx faz a mesma separação por regex
      // (frontend/nginx.conf).
      '/e': {
        target: 'http://localhost:8080',
        changeOrigin: true,
        bypass: (req) => (/^\/e\/[^/]+\/api\//.test(req.url ?? '') ? undefined : '/index.html'),
      },
    },
  },
  test: {
    globals: true,
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
  },
});

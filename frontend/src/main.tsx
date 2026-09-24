import { StrictMode, type ReactNode } from 'react';
import { createRoot } from 'react-dom/client';
import { Toaster } from '@/components/ui/sonner';
import { decidirEntrada } from '@/lib/entrada';
import './index.css';

/**
 * Ponto de entrada — Story 9.2 (spec-9-2). Decide, ANTES de montar qualquer
 * provider, qual das três apps sobe para o caminho atual (ver
 * `lib/entrada.ts`): a da Plataforma (`/plataforma/...`), a da Empresa
 * (`/e/{slug}/...`) ou a página sem Empresa. Nesta última, antes de montar,
 * consulta a Empresa padrão do servidor (Story 15.4): se houver, a página é
 * trocada por `/e/{slug}/` e nada é montado.
 *
 * Import dinâmico: o módulo de cada app — e o router que ele cria ao ser
 * importado — só é carregado para o próprio caminho. A app da Plataforma
 * nunca carrega `AuthProvider`/`CarrinhoProvider`, e a da Empresa nunca sobe
 * sem slug.
 */
async function carregarApp(): Promise<ReactNode | null> {
  switch (await decidirEntrada(window.location.pathname)) {
    case 'redirecionado':
      return null;
    case 'plataforma': {
      const { PlataformaApp } = await import('./pages/plataforma/PlataformaApp');
      return <PlataformaApp />;
    }
    case 'empresa': {
      const { default: App } = await import('./App.tsx');
      return <App />;
    }
    default: {
      const { SemEmpresaPage } = await import('./pages/SemEmpresaPage');
      return <SemEmpresaPage />;
    }
  }
}

void carregarApp().then((app) => {
  if (app === null) {
    return;
  }
  createRoot(document.getElementById('root')!).render(
    <StrictMode>
      {app}
      <Toaster />
    </StrictMode>,
  );
});

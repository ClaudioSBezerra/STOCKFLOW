import { slugDaURL } from '@/lib/api';

/**
 * Qual app o `main.tsx` monta para o caminho atual — Story 9.2 (spec-9-2),
 * handoff (b) da Story 9.1. A escolha acontece ANTES de montar qualquer
 * provider, e as três apps são disjuntas:
 *
 * - `plataforma`: `/plataforma` e tudo abaixo — a área do Dono da
 *   Plataforma, com sessão própria (nunca `AuthProvider`/`CarrinhoProvider`);
 * - `empresa`: `/e/{slug}/...` com um slug canônico — a app de sempre, sob a
 *   Empresa do slug;
 * - `sem-empresa`: qualquer outro caminho — uma página que só explica que o
 *   acesso é pelo endereço da própria empresa (a app da Empresa nunca sobe
 *   sem slug).
 */
export type AppDeEntrada = 'plataforma' | 'empresa' | 'sem-empresa';

export function escolherApp(pathname: string): AppDeEntrada {
  if (pathname === '/plataforma' || pathname.startsWith('/plataforma/')) {
    return 'plataforma';
  }
  if (slugDaURL(pathname) !== '') {
    return 'empresa';
  }
  return 'sem-empresa';
}

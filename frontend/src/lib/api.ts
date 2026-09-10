/**
 * Prefixo de Empresa da URL e montagem de caminhos de API — Story 9.1
 * (Epic 9, Multi-Empresa e Plataforma), spec-9-1.
 *
 * Toda rota de negócio do backend passou a viver sob `/e/{slug}/api/...`: o
 * slug é a ÚNICA chave de resolução da Empresa, lida uma vez por requisição
 * no middleware do servidor (AD-19). No navegador, o slug é o primeiro
 * segmento do `location.pathname` depois de `/e/` — o mesmo prefixo com que
 * o SPA é servido (`basename` do router). Nenhum componente monta esse
 * prefixo à mão: todos passam por `apiUrl`.
 *
 * Sem slug na URL (app servido em `/`), `prefixoEmpresa()` devolve `''` e
 * `apiUrl()` devolve o caminho INTOCADO — o comportamento de antes desta
 * story. É o que mantém `GET /api/health` alcançável e o que preserva os
 * testes existentes, que rodam em jsdom com `location.pathname === '/'` e
 * afirmam a URL literal `/api/...`.
 */

import { getAccessToken } from '@/lib/session';

/** Forma canônica de um slug de Empresa — espelha `services.NormalizarSlug`
 * (backend): minúsculas ASCII e dígitos, segmentos separados por UM hífen,
 * sem hífen nas pontas, de 2 a 63 caracteres. Um primeiro segmento fora
 * dessa forma NUNCA poderia ter sido gravado em `empresas.slug`, então é
 * tratado como "sem Empresa" em vez de virar um prefixo inválido. */
const SLUG_VALIDO = /^[a-z0-9]+(?:-[a-z0-9]+)*$/;
const SLUG_MIN = 2;
const SLUG_MAX = 63;

/**
 * Lê o slug da Empresa do caminho atual do navegador (`/e/{slug}/...`).
 * Devolve `''` quando o caminho não começa com `/e/`, quando o segmento
 * seguinte está vazio ou quando ele não é um slug canônico.
 */
export function slugDaURL(pathname: string = window.location.pathname): string {
  const segmentos = pathname.split('/');
  // ['', 'e', '<slug>', ...] — qualquer outra forma não carrega Empresa.
  if (segmentos.length < 3 || segmentos[1] !== 'e') return '';
  const slug = segmentos[2];
  if (slug.length < SLUG_MIN || slug.length > SLUG_MAX) return '';
  return SLUG_VALIDO.test(slug) ? slug : '';
}

/**
 * Prefixo de rota da Empresa atual — `/e/{slug}` ou `''` quando não há
 * Empresa na URL. É o mesmo valor usado como `basename` do
 * `createBrowserRouter` (App.tsx), para que todo `to=`/`navigate()` do SPA
 * continue escrito sem prefixo e ainda assim resolva sob a Empresa.
 */
export function prefixoEmpresa(pathname?: string): string {
  const slug = slugDaURL(pathname);
  return slug ? `/e/${slug}` : '';
}

/**
 * Monta a URL final de uma chamada de API a partir de um caminho escrito
 * SEM prefixo (`/api/...`), acrescentando o prefixo da Empresa atual.
 *
 * Um caminho que já começa com `/e/` é devolvido intocado (nunca prefixa
 * duas vezes) — é o caso das URLs que o próprio backend devolve prontas.
 */
export function apiUrl(caminho: string): string {
  if (caminho.startsWith('/e/')) return caminho;
  return prefixoEmpresa() + caminho;
}

/**
 * Cabeçalho `Authorization` da sessão em memória — antes desta story estava
 * duplicado literalmente em 17 arquivos. Sem token (visitante, ou sessão
 * ainda não restaurada) devolve um objeto vazio, nunca um header vazio.
 */
export function authHeaders(): Record<string, string> {
  const token = getAccessToken();
  return token ? { Authorization: `Bearer ${token}` } : {};
}

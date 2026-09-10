import { afterEach, describe, expect, it } from 'vitest';
import { apiUrl, authHeaders, prefixoEmpresa, slugDaURL } from './api';
import { clearAccessToken, setAccessToken } from './session';

// Prefixo de Empresa na URL — Story 9.1 (Multi-Empresa), spec-9-1.
//
// `slugDaURL` aceita o pathname como argumento justamente para ser testável
// sem mexer em `window.location`; `apiUrl`/`authHeaders` leem o pathname real
// do jsdom, que é `/` — o caso "sem Empresa", que mantém o comportamento de
// antes desta story e é o que os 46 arquivos de teste existentes assumem.

describe('slugDaURL', () => {
  it('lê o slug do primeiro segmento depois de /e/', () => {
    expect(slugDaURL('/e/acme/catalogo')).toBe('acme');
    expect(slugDaURL('/e/ferreira-costa/')).toBe('ferreira-costa');
    expect(slugDaURL('/e/acme')).toBe('acme');
  });

  it('devolve vazio quando o caminho não carrega Empresa', () => {
    for (const pathname of ['/', '/catalogo', '/e', '/e/', '/x/acme', '/eacme/x']) {
      expect(slugDaURL(pathname)).toBe('');
    }
  });

  it('recusa um primeiro segmento fora da forma canônica de slug', () => {
    // Nenhum destes poderia ter sido gravado em `empresas.slug` — tratá-los
    // como "sem Empresa" evita montar um prefixo que nunca resolveria.
    for (const slug of ['ACME', 'a', 'acme--x', '-acme', 'acme-', 'ac me', 'açme', 'a'.repeat(64)]) {
      expect(slugDaURL(`/e/${slug}/catalogo`)).toBe('');
    }
  });
});

describe('prefixoEmpresa', () => {
  it('devolve /e/{slug} quando há Empresa na URL', () => {
    expect(prefixoEmpresa('/e/acme/catalogo')).toBe('/e/acme');
  });

  it('devolve vazio sem Empresa na URL (basename vazio do router)', () => {
    expect(prefixoEmpresa('/')).toBe('');
    expect(prefixoEmpresa('/catalogo')).toBe('');
  });
});

describe('apiUrl', () => {
  it('devolve o caminho intocado quando não há Empresa na URL', () => {
    expect(apiUrl('/api/produtos')).toBe('/api/produtos');
    expect(apiUrl('/api/health')).toBe('/api/health');
  });

  it('nunca prefixa duas vezes um caminho que já começa com /e/', () => {
    expect(apiUrl('/e/acme/api/produtos')).toBe('/e/acme/api/produtos');
  });
});

describe('authHeaders', () => {
  afterEach(() => {
    clearAccessToken();
  });

  it('devolve objeto vazio sem sessão', () => {
    expect(authHeaders()).toEqual({});
  });

  it('devolve o Bearer da sessão em memória', () => {
    setAccessToken('token-de-teste');
    expect(authHeaders()).toEqual({ Authorization: 'Bearer token-de-teste' });
  });
});

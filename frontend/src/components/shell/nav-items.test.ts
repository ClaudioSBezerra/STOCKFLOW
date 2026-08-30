import { describe, expect, it } from 'vitest';
import {
  adminNavItems,
  filtrarNavPorPapel,
  navItems,
  primaryNavItems,
  profileNavItem,
  rankPapel,
} from './nav-items';

describe('rankPapel', () => {
  it('espelha a ordem total do backend (usuario<almoxarife<gestor<adm)', () => {
    expect(rankPapel('usuario')).toBe(1);
    expect(rankPapel('almoxarife')).toBe(2);
    expect(rankPapel('gestor')).toBe(3);
    expect(rankPapel('adm')).toBe(4);
    expect(rankPapel('usuario')).toBeLessThan(rankPapel('almoxarife'));
    expect(rankPapel('almoxarife')).toBeLessThan(rankPapel('gestor'));
    expect(rankPapel('gestor')).toBeLessThan(rankPapel('adm'));
  });

  it('devolve 0 para papel desconhecido/vazio', () => {
    expect(rankPapel('')).toBe(0);
    expect(rankPapel('root')).toBe(0);
    expect(rankPapel('ADM')).toBe(0);
  });
});

describe('nav-items: papelMinimo por item', () => {
  it('primary e profile exigem no mínimo usuario; admin exige no mínimo almoxarife', () => {
    for (const item of primaryNavItems) {
      expect(item.papelMinimo).toBe('usuario');
    }
    expect(profileNavItem.papelMinimo).toBe('usuario');
    for (const item of adminNavItems) {
      expect(item.papelMinimo).toBe('almoxarife');
    }
  });
});

describe('filtrarNavPorPapel', () => {
  it('papel usuario esconde todos os itens admin', () => {
    const visiveis = filtrarNavPorPapel(navItems, 'usuario');
    const ids = visiveis.map((i) => i.id);
    expect(ids).toEqual(['catalogo', 'carrinho', 'pedidos', 'perfil']);
    expect(ids).not.toContain('estoques');
    expect(ids).not.toContain('normalizacao');
    expect(ids).not.toContain('relatorios');
  });

  it('papel almoxarife revela os itens admin', () => {
    const visiveis = filtrarNavPorPapel(navItems, 'almoxarife');
    const ids = visiveis.map((i) => i.id);
    expect(ids).toContain('estoques');
    expect(ids).toContain('normalizacao');
    expect(ids).toContain('relatorios');
  });

  it('papel gestor e adm veem tudo', () => {
    for (const papel of ['gestor', 'adm']) {
      expect(filtrarNavPorPapel(navItems, papel)).toHaveLength(navItems.length);
    }
  });

  it('papel desconhecido/vazio não vê nenhum item', () => {
    expect(filtrarNavPorPapel(navItems, '')).toHaveLength(0);
    expect(filtrarNavPorPapel(navItems, 'invalido')).toHaveLength(0);
  });

  it('esconde (fail-closed) um item cujo papelMinimo não está no mapa de ranks', () => {
    // Simula um item mal configurado (typo / papel novo não espelhado no TS):
    // rankPapel(papelMinimo) === 0 deve escondê-lo, nunca liberá-lo a todos.
    const itemQuebrado = { id: 'x', papelMinimo: 'supervisor' as unknown as 'adm' };
    expect(filtrarNavPorPapel([itemQuebrado], 'adm')).toHaveLength(0);
    expect(filtrarNavPorPapel([itemQuebrado], 'usuario')).toHaveLength(0);
  });

  it('é uma função pura — não muta a lista de entrada', () => {
    const entrada = [...navItems];
    filtrarNavPorPapel(entrada, 'usuario');
    expect(entrada).toEqual(navItems);
  });
});

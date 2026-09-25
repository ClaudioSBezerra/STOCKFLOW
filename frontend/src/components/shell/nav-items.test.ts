import { describe, expect, it } from 'vitest';
import {
  filtrarNavPorPapel,
  gruposVisiveis,
  itemAtivo,
  navGrupos,
  perfil,
  rankPapel,
} from './nav-items';

const todosItens = navGrupos.flatMap((g) => g.itens);
const idsVisiveis = (papel: string) =>
  gruposVisiveis(navGrupos, papel).flatMap((g) => g.itens.map((i) => i.id));

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

describe('nav-items: grupos e papéis', () => {
  it('declara os grupos e as rotas do EXPERIENCE.md', () => {
    expect(navGrupos.map((g) => g.label)).toEqual([
      'Catálogo',
      'Pedidos',
      'Estoque',
      'Qualidade dos dados',
    ]);
    expect(todosItens.map((i) => i.to)).toEqual([
      '/',
      '/produtos/novo',
      '/produtos/importar',
      '/carrinho',
      '/pedidos',
      '/pedidos/fila',
      '/estoques',
      '/estoques/lancar-saldo',
      '/estoques/movimentacoes',
      '/normalizacao',
      '/normalizacao/duplicatas',
    ]);
    expect(todosItens.some((i) => i.to === '/relatorios')).toBe(false);
    expect(perfil.to).toBe('/configuracoes');
    expect(perfil.papelMinimo).toBe('usuario');
  });

  it('papel usuario vê só Produtos, Carrinho e Meus pedidos; Estoque e Qualidade somem', () => {
    expect(idsVisiveis('usuario')).toEqual(['produtos', 'carrinho', 'meus-pedidos']);
    expect(gruposVisiveis(navGrupos, 'usuario').map((g) => g.id)).toEqual(['catalogo', 'pedidos']);
  });

  it('papel almoxarife, gestor e adm veem todos os itens desta story', () => {
    for (const papel of ['almoxarife', 'gestor', 'adm']) {
      expect(idsVisiveis(papel)).toHaveLength(todosItens.length);
    }
  });

  it('papel desconhecido/vazio não vê grupo nenhum', () => {
    expect(gruposVisiveis(navGrupos, '')).toHaveLength(0);
    expect(gruposVisiveis(navGrupos, 'invalido')).toHaveLength(0);
  });

  it('grupo sem item visível some inteiro', () => {
    const grupos = [{ ...navGrupos[2], itens: navGrupos[2].itens }];
    expect(gruposVisiveis(grupos, 'usuario')).toEqual([]);
  });
});

describe('itemAtivo', () => {
  it('casa a rota exata e devolve o grupo do item', () => {
    expect(itemAtivo(navGrupos, '/estoques/movimentacoes')).toEqual({
      grupoId: 'estoque',
      itemId: 'movimentacoes',
    });
    expect(itemAtivo(navGrupos, '/estoques')).toEqual({ grupoId: 'estoque', itemId: 'locais' });
    expect(itemAtivo(navGrupos, '/')).toEqual({ grupoId: 'catalogo', itemId: 'produtos' });
    expect(itemAtivo(navGrupos, '/pedidos/')).toEqual({ grupoId: 'pedidos', itemId: 'meus-pedidos' });
  });

  it('rota fora do menu não tem item ativo', () => {
    expect(itemAtivo(navGrupos, '/configuracoes')).toBeNull();
    expect(itemAtivo(navGrupos, '/produtos/abc')).toBeNull();
  });
});

describe('filtrarNavPorPapel', () => {
  it('esconde (fail-closed) um item cujo papelMinimo não está no mapa de ranks', () => {
    // Simula um item mal configurado (typo / papel novo não espelhado no TS):
    // rankPapel(papelMinimo) === 0 deve escondê-lo, nunca liberá-lo a todos.
    const itemQuebrado = { id: 'x', papelMinimo: 'supervisor' as unknown as 'adm' };
    expect(filtrarNavPorPapel([itemQuebrado], 'adm')).toHaveLength(0);
    expect(filtrarNavPorPapel([itemQuebrado], 'usuario')).toHaveLength(0);
  });

  it('é uma função pura — não muta a lista de entrada', () => {
    const entrada = [...todosItens];
    filtrarNavPorPapel(entrada, 'usuario');
    expect(entrada).toEqual(todosItens);
  });
});

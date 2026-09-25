import {
  ClipboardList,
  LayoutGrid,
  ScanSearch,
  UserCircle,
  Warehouse,
  type LucideIcon,
} from 'lucide-react';

/**
 * Menu lateral agrupado (Epic 17, Story 17.1; EXPERIENCE.md "Menu agrupado").
 * Cada grupo tem itens com rota própria e `papelMinimo`: o menu só renderiza
 * os itens cujo papel mínimo o papel do usuário alcança, e o grupo sem item
 * visível some inteiro. Item sem permissão simplesmente não aparece — nunca
 * desabilitado. Os grupos Cadastros e Administração chegam na Story 17.2.
 */

/**
 * Papéis da hierarquia de acesso (AD-8). Espelho MÍNIMO e deliberado de
 * `services.RankPapel` no backend (backend/services/papel.go) — a duplicação
 * entre Go e TS é inevitável; a autoridade de aplicação é sempre o servidor,
 * o frontend só decide visibilidade de navegação.
 */
export type Papel = 'usuario' | 'almoxarife' | 'gestor' | 'adm';

const RANK_PAPEL: Record<Papel, number> = {
  usuario: 1,
  almoxarife: 2,
  gestor: 3,
  adm: 4,
};

/** Rank do papel na ordem total; papel desconhecido/vazio -> 0. */
export function rankPapel(papel: string): number {
  return RANK_PAPEL[papel as Papel] ?? 0;
}

export interface NavItem {
  id: string;
  label: string;
  to: string;
  papelMinimo: Papel;
}

export interface NavGrupo {
  id: string;
  label: string;
  icon: LucideIcon;
  itens: NavItem[];
}

export const navGrupos: NavGrupo[] = [
  {
    id: 'catalogo',
    label: 'Catálogo',
    icon: LayoutGrid,
    itens: [
      { id: 'produtos', label: 'Produtos', to: '/', papelMinimo: 'usuario' },
      { id: 'cadastrar-produto', label: 'Cadastrar produto', to: '/produtos/novo', papelMinimo: 'almoxarife' },
      { id: 'importar-planilha', label: 'Importar planilha', to: '/produtos/importar', papelMinimo: 'almoxarife' },
      { id: 'carrinho', label: 'Carrinho', to: '/carrinho', papelMinimo: 'usuario' },
    ],
  },
  {
    id: 'pedidos',
    label: 'Pedidos',
    icon: ClipboardList,
    itens: [
      { id: 'meus-pedidos', label: 'Meus pedidos', to: '/pedidos', papelMinimo: 'usuario' },
      { id: 'fila-aprovacao', label: 'Fila de aprovação', to: '/pedidos/fila', papelMinimo: 'almoxarife' },
    ],
  },
  {
    id: 'estoque',
    label: 'Estoque',
    icon: Warehouse,
    itens: [
      { id: 'locais', label: 'Locais', to: '/estoques', papelMinimo: 'almoxarife' },
      { id: 'lancar-saldo', label: 'Lançar saldo', to: '/estoques/lancar-saldo', papelMinimo: 'almoxarife' },
      { id: 'movimentacoes', label: 'Movimentações', to: '/estoques/movimentacoes', papelMinimo: 'almoxarife' },
    ],
  },
  {
    id: 'qualidade',
    label: 'Qualidade dos dados',
    icon: ScanSearch,
    itens: [
      { id: 'inconsistencias', label: 'Inconsistências', to: '/normalizacao', papelMinimo: 'almoxarife' },
      { id: 'duplicatas', label: 'Duplicatas', to: '/normalizacao/duplicatas', papelMinimo: 'almoxarife' },
    ],
  },
];

/** "Meu perfil" — rodapé do menu (as seções de conta continuam em `/configuracoes`). */
export const perfil: NavItem & { icon: LucideIcon } = {
  id: 'perfil',
  label: 'Meu perfil',
  to: '/configuracoes',
  papelMinimo: 'usuario',
  icon: UserCircle,
};

/**
 * Filtra uma lista de itens de navegação pelo papel do usuário: mantém só os
 * itens cujo `papelMinimo` o papel alcança na ordem total (`rankPapel`).
 * Função pura — sem estado, sem React.
 */
export function filtrarNavPorPapel<T extends { papelMinimo: Papel }>(items: T[], papel: string): T[] {
  const rank = rankPapel(papel);
  return items.filter((item) => {
    const min = rankPapel(item.papelMinimo);
    // `min > 0` fecha o fail-open: um item com `papelMinimo` fora do mapa de
    // ranks (typo, papel novo não espelhado) fica escondido para todos em vez
    // de `rank >= 0` liberar para qualquer papel.
    return min > 0 && rank >= min;
  });
}

/**
 * Grupos visíveis para o papel: filtra os itens por `filtrarNavPorPapel` e
 * descarta o grupo que ficou sem nenhum item. Função pura.
 */
export function gruposVisiveis(grupos: NavGrupo[], papel: string): NavGrupo[] {
  return grupos
    .map((g) => ({ ...g, itens: filtrarNavPorPapel(g.itens, papel) }))
    .filter((g) => g.itens.length > 0);
}

function semBarraFinal(caminho: string): string {
  return caminho.length > 1 ? caminho.replace(/\/+$/, '') : caminho;
}

/**
 * Item ativo para o `pathname` (já sem o basename da Empresa): casamento
 * exato — `/estoques` não marca Locais quando a rota é `/estoques/movimentacoes`.
 */
export function itemAtivo(
  grupos: NavGrupo[],
  pathname: string,
): { grupoId: string; itemId: string } | null {
  const caminho = semBarraFinal(pathname);
  for (const g of grupos) {
    for (const item of g.itens) {
      if (item.to === caminho) return { grupoId: g.id, itemId: item.id };
    }
  }
  return null;
}

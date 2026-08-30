import {
  ClipboardList,
  FileBarChart,
  LayoutGrid,
  ShoppingCart,
  UserCircle,
  Wand2,
  Warehouse,
  type LucideIcon,
} from 'lucide-react';

/**
 * Áreas de navegação, refletindo a Information Architecture de EXPERIENCE.md:
 * - `primary`: sempre visíveis (rail e bottom nav) — Catálogo, Carrinho, Pedidos.
 * - `admin`: visíveis diretamente no rail (desktop); recolhidos para dentro
 *   do Sheet de "Mais" na bottom nav (mobile) — Estoques, Normalização, Relatórios.
 * - `profile`: Configurações/Meu Perfil — rodapé do rail (avatar + DropdownMenu)
 *   no desktop; dentro do mesmo Sheet de "Mais" no mobile.
 *
 * Cada item declara um `papelMinimo` (Story 1.5): o `AppShell` só renderiza
 * os itens cujo papel mínimo o papel do usuário alcança, nas três superfícies
 * (rail, bottom nav, Sheet "Mais"). Item sem permissão simplesmente não
 * aparece — nunca desabilitado, nunca tela de "acesso negado".
 */
export type NavArea = 'primary' | 'admin' | 'profile';

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
  icon: LucideIcon;
  to: string;
  area: NavArea;
  papelMinimo: Papel;
}

export const navItems: NavItem[] = [
  { id: 'catalogo', label: 'Catálogo', icon: LayoutGrid, to: '/', area: 'primary', papelMinimo: 'usuario' },
  { id: 'carrinho', label: 'Carrinho', icon: ShoppingCart, to: '/carrinho', area: 'primary', papelMinimo: 'usuario' },
  { id: 'pedidos', label: 'Pedidos', icon: ClipboardList, to: '/pedidos', area: 'primary', papelMinimo: 'usuario' },
  { id: 'estoques', label: 'Estoques', icon: Warehouse, to: '/estoques', area: 'admin', papelMinimo: 'almoxarife' },
  { id: 'normalizacao', label: 'Normalização', icon: Wand2, to: '/normalizacao', area: 'admin', papelMinimo: 'almoxarife' },
  { id: 'relatorios', label: 'Relatórios', icon: FileBarChart, to: '/relatorios', area: 'admin', papelMinimo: 'almoxarife' },
  { id: 'perfil', label: 'Meu Perfil', icon: UserCircle, to: '/configuracoes', area: 'profile', papelMinimo: 'usuario' },
];

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

export const primaryNavItems = navItems.filter((item) => item.area === 'primary');
export const adminNavItems = navItems.filter((item) => item.area === 'admin');
export const profileNavItem = navItems.find((item) => item.area === 'profile') as NavItem;

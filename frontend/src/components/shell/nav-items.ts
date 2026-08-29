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
 * Nenhum item é gated por papel nesta story — não há autenticação ainda
 * (Story 1.3/1.4/1.5); todo item renderiza incondicionalmente.
 */
export type NavArea = 'primary' | 'admin' | 'profile';

export interface NavItem {
  id: string;
  label: string;
  icon: LucideIcon;
  to: string;
  area: NavArea;
}

export const navItems: NavItem[] = [
  { id: 'catalogo', label: 'Catálogo', icon: LayoutGrid, to: '/', area: 'primary' },
  { id: 'carrinho', label: 'Carrinho', icon: ShoppingCart, to: '/carrinho', area: 'primary' },
  { id: 'pedidos', label: 'Pedidos', icon: ClipboardList, to: '/pedidos', area: 'primary' },
  { id: 'estoques', label: 'Estoques', icon: Warehouse, to: '/estoques', area: 'admin' },
  { id: 'normalizacao', label: 'Normalização', icon: Wand2, to: '/normalizacao', area: 'admin' },
  { id: 'relatorios', label: 'Relatórios', icon: FileBarChart, to: '/relatorios', area: 'admin' },
  { id: 'perfil', label: 'Meu Perfil', icon: UserCircle, to: '/configuracoes', area: 'profile' },
];

export const primaryNavItems = navItems.filter((item) => item.area === 'primary');
export const adminNavItems = navItems.filter((item) => item.area === 'admin');
export const profileNavItem = navItems.find((item) => item.area === 'profile') as NavItem;

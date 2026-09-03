import { useState, type ReactNode } from 'react';
import { NavLink, Outlet, useMatch } from 'react-router-dom';
import { Menu } from 'lucide-react';
import { cn } from '@/lib/utils';
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip';
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from '@/components/ui/sheet';
import { Avatar, AvatarFallback } from '@/components/ui/avatar';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { useAuth } from '@/lib/auth';
import { useCarrinho } from '@/lib/carrinho';
import {
  adminNavItems,
  filtrarNavPorPapel,
  primaryNavItems,
  profileNavItem,
  type NavItem,
} from './nav-items';

export interface AppShellProps {
  /** Conteúdo da página atual. Se omitido, renderiza o `<Outlet />` do react-router. */
  children?: ReactNode;
  /**
   * Abas horizontais opcionais do módulo atual (ex.: Catálogo/Pedidos).
   * Sem consumidor real nesta story — capacidade provada por teste de componente.
   */
  tabs?: ReactNode;
  /**
   * Submenu vertical opcional de {spacing.sidenav-width} (224px), usado por
   * módulos como Estoques/Normalização. Sem consumidor real nesta story.
   */
  sideNav?: ReactNode;
}

const touchTarget = 'min-h-touch-target-min min-w-touch-target-min';

function navLinkClasses(isActive: boolean, extra?: string) {
  return cn(
    'flex items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground',
    touchTarget,
    isActive && 'nav-item-active',
    extra,
  );
}

// CartBadge é o `cart-badge` (UX-DR5, epic-7-context.md): contador circular
// com preenchimento `destructive` sobreposto ao ícone do item `carrinho` —
// nunca renderizado sozinho, sempre atrás de `item.id === 'carrinho' &&
// count > 0` no chamador (o chamador também garante que nunca aparece
// "0"). `aria-hidden`: o número é decorativo aqui — o rótulo acessível do
// link continua sendo só `item.label` ("Carrinho"), a contagem em si não é
// parte do nome do link; quem precisa da contagem falada tem `/carrinho`
// (Story 7.1) como fonte, não o ícone de navegação.
function CartBadge({ count }: { count: number }) {
  return (
    <span
      aria-hidden="true"
      className="absolute -top-1 -right-1 flex h-4 min-w-4 items-center justify-center rounded-full bg-destructive px-1 text-[10px] leading-none font-medium text-white"
    >
      {count > 99 ? '99+' : count}
    </span>
  );
}

function RailNavIcon({ item, count }: { item: NavItem; count: number }) {
  const Icon = item.icon;
  // `TooltipTrigger asChild` clona este NavLink via Radix Slot, que mescla
  // `className` assumindo string dos dois lados — um `className` em forma de
  // função (a API padrão do NavLink) seria serializado incorretamente nesse
  // merge. Por isso o `isActive` é resolvido aqui fora e passado como string.
  const isActive = Boolean(useMatch({ path: item.to, end: item.to === '/' }));
  const mostrarBadge = item.id === 'carrinho' && count > 0;
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <NavLink
          to={item.to}
          end={item.to === '/'}
          aria-label={item.label}
          className={navLinkClasses(isActive, 'relative')}
        >
          <Icon className="size-5" aria-hidden="true" />
          {mostrarBadge && <CartBadge count={count} />}
        </NavLink>
      </TooltipTrigger>
      <TooltipContent side="right">{item.label}</TooltipContent>
    </Tooltip>
  );
}

function BottomNavIcon({ item, count }: { item: NavItem; count: number }) {
  const Icon = item.icon;
  const mostrarBadge = item.id === 'carrinho' && count > 0;
  return (
    <NavLink
      to={item.to}
      end={item.to === '/'}
      className={({ isActive }) =>
        navLinkClasses(isActive, 'relative flex-1 flex-col gap-0.5 text-label')
      }
    >
      <Icon className="size-5" aria-hidden="true" />
      {mostrarBadge && <CartBadge count={count} />}
      <span>{item.label}</span>
    </NavLink>
  );
}

function SheetNavRow({ item, onNavigate }: { item: NavItem; onNavigate: () => void }) {
  const Icon = item.icon;
  return (
    <NavLink
      to={item.to}
      onClick={onNavigate}
      className={({ isActive }) =>
        navLinkClasses(isActive, 'w-full justify-start gap-3 px-3 text-body')
      }
    >
      <Icon className="size-5 shrink-0" aria-hidden="true" />
      {item.label}
    </NavLink>
  );
}

/**
 * Layout raiz do stockflow: rail + header no desktop (`>= md`, 768px),
 * bottom nav + "Mais" no mobile (`< md`, a partir de 360px). Ver
 * EXPERIENCE.md (Information Architecture, Responsive & Platform) e
 * DESIGN.md (spacing, nav-item-active).
 */
export function AppShell({ children, tabs, sideNav }: AppShellProps) {
  const content = children ?? <Outlet />;
  // `Sheet` precisa ser controlado para poder ser fechado a partir do clique
  // num item de navegação dentro dele — descontrolado, o Radix só fecha via
  // Esc/clique fora, deixando o overlay/painel aberto sobre a rota nova.
  const [moreOpen, setMoreOpen] = useState(false);

  // Navegação gated por papel (Story 1.5): cada superfície só renderiza os
  // itens cujo `papelMinimo` o papel do usuário alcança. Item sem permissão
  // simplesmente não aparece — nunca desabilitado, nunca "acesso negado".
  const { usuario, logout } = useAuth();
  // cart-badge (Story 7.1, UX-DR5): contagem do carrinho do usuário, lida do
  // estado global já mantido por CarrinhoProvider (App.tsx) — este
  // componente nunca busca /api/carrinho por conta própria.
  const { count } = useCarrinho();
  const papel = usuario?.papel ?? '';
  const primaryItems = filtrarNavPorPapel(primaryNavItems, papel);
  const adminItems = filtrarNavPorPapel(adminNavItems, papel);
  // Mesma regra de visibilidade que os demais itens usam — uma única
  // implementação (`filtrarNavPorPapel`), nunca uma comparação de rank inline.
  const mostrarPerfil = filtrarNavPorPapel([profileNavItem], papel).length > 0;

  return (
    <TooltipProvider>
      <div className="flex min-h-svh flex-col bg-background text-foreground md:flex-row">
        {/* Rail — desktop (>= md) */}
        <nav
          aria-label="Navegação principal"
          className="hidden w-rail-width shrink-0 flex-col items-center justify-between border-r border-border bg-card py-3 md:flex"
        >
          <div className="flex flex-col items-center gap-1">
            {primaryItems.map((item) => (
              <RailNavIcon key={item.id} item={item} count={count} />
            ))}
            {adminItems.length > 0 ? <hr className="my-2 w-8 border-border" /> : null}
            {adminItems.map((item) => (
              <RailNavIcon key={item.id} item={item} count={count} />
            ))}
          </div>

          {mostrarPerfil ? (
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <button
                  type="button"
                  aria-label={profileNavItem.label}
                  className={cn('flex items-center justify-center rounded-full', touchTarget)}
                >
                  <Avatar className="size-8">
                    <AvatarFallback>
                      <profileNavItem.icon className="size-4" aria-hidden="true" />
                    </AvatarFallback>
                  </Avatar>
                </button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" side="right">
                <DropdownMenuItem asChild className={touchTarget}>
                  <NavLink to={profileNavItem.to}>{profileNavItem.label}</NavLink>
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem asChild className={touchTarget}>
                  <button type="button" onClick={logout} aria-label="Sair">
                    Sair
                  </button>
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          ) : null}
        </nav>

        <div className="flex min-w-0 flex-1 flex-col">
          {/* Header fino — desktop */}
          <header className="hidden h-12 shrink-0 items-center border-b border-border bg-card px-4 md:flex">
            <span className="text-heading-md">stockflow</span>
          </header>

          {tabs ? <div className="border-b border-border">{tabs}</div> : null}

          <div className="flex min-h-0 flex-1">
            {sideNav ? (
              <aside className="hidden w-sidenav-width shrink-0 border-r border-border md:block">
                {sideNav}
              </aside>
            ) : null}

            <main className="min-w-0 flex-1 overflow-y-auto pb-bottom-nav-height md:pb-0">
              {content}
            </main>
          </div>
        </div>

        {/* Bottom nav — mobile (< md) */}
        <nav
          aria-label="Navegação principal"
          className="fixed inset-x-0 bottom-0 z-40 flex h-bottom-nav-height items-stretch border-t border-border bg-card md:hidden"
        >
          {primaryItems.map((item) => (
            <BottomNavIcon key={item.id} item={item} count={count} />
          ))}

          <Sheet open={moreOpen} onOpenChange={setMoreOpen}>
            <SheetTrigger asChild>
              <button
                type="button"
                className={cn(
                  'flex flex-1 flex-col items-center justify-center gap-0.5 text-label text-muted-foreground',
                  touchTarget,
                )}
              >
                <Menu className="size-5" aria-hidden="true" />
                <span>Mais</span>
              </button>
            </SheetTrigger>
            <SheetContent side="bottom">
              <SheetHeader>
                <SheetTitle>Mais</SheetTitle>
              </SheetHeader>
              <div className="flex flex-col gap-1 px-4 pb-4">
                {adminItems.map((item) => (
                  <SheetNavRow key={item.id} item={item} onNavigate={() => setMoreOpen(false)} />
                ))}
                {adminItems.length > 0 && mostrarPerfil ? (
                  <hr className="my-1 border-border" />
                ) : null}
                {mostrarPerfil ? (
                  <>
                    <SheetNavRow item={profileNavItem} onNavigate={() => setMoreOpen(false)} />
                    <hr className="my-1 border-border" />
                    <button
                      type="button"
                      aria-label="Sair"
                      onClick={() => {
                        setMoreOpen(false);
                        logout();
                      }}
                      className={cn(
                        'flex w-full items-center justify-start gap-3 rounded-md px-3 text-body text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground',
                        touchTarget,
                      )}
                    >
                      Sair
                    </button>
                  </>
                ) : null}
              </div>
            </SheetContent>
          </Sheet>
        </nav>
      </div>
    </TooltipProvider>
  );
}

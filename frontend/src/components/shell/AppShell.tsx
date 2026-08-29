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
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { adminNavItems, primaryNavItems, profileNavItem, type NavItem } from './nav-items';

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

function RailNavIcon({ item }: { item: NavItem }) {
  const Icon = item.icon;
  // `TooltipTrigger asChild` clona este NavLink via Radix Slot, que mescla
  // `className` assumindo string dos dois lados — um `className` em forma de
  // função (a API padrão do NavLink) seria serializado incorretamente nesse
  // merge. Por isso o `isActive` é resolvido aqui fora e passado como string.
  const isActive = Boolean(useMatch({ path: item.to, end: item.to === '/' }));
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <NavLink
          to={item.to}
          end={item.to === '/'}
          aria-label={item.label}
          className={navLinkClasses(isActive)}
        >
          <Icon className="size-5" aria-hidden="true" />
        </NavLink>
      </TooltipTrigger>
      <TooltipContent side="right">{item.label}</TooltipContent>
    </Tooltip>
  );
}

function BottomNavIcon({ item }: { item: NavItem }) {
  const Icon = item.icon;
  return (
    <NavLink
      to={item.to}
      end={item.to === '/'}
      className={({ isActive }) =>
        navLinkClasses(isActive, 'flex-1 flex-col gap-0.5 text-label')
      }
    >
      <Icon className="size-5" aria-hidden="true" />
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

  return (
    <TooltipProvider>
      <div className="flex min-h-svh flex-col bg-background text-foreground md:flex-row">
        {/* Rail — desktop (>= md) */}
        <nav
          aria-label="Navegação principal"
          className="hidden w-rail-width shrink-0 flex-col items-center justify-between border-r border-border bg-card py-3 md:flex"
        >
          <div className="flex flex-col items-center gap-1">
            {primaryNavItems.map((item) => (
              <RailNavIcon key={item.id} item={item} />
            ))}
            <hr className="my-2 w-8 border-border" />
            {adminNavItems.map((item) => (
              <RailNavIcon key={item.id} item={item} />
            ))}
          </div>

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
            </DropdownMenuContent>
          </DropdownMenu>
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
          {primaryNavItems.map((item) => (
            <BottomNavIcon key={item.id} item={item} />
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
                {adminNavItems.map((item) => (
                  <SheetNavRow key={item.id} item={item} onNavigate={() => setMoreOpen(false)} />
                ))}
                <hr className="my-1 border-border" />
                <SheetNavRow item={profileNavItem} onNavigate={() => setMoreOpen(false)} />
              </div>
            </SheetContent>
          </Sheet>
        </nav>
      </div>
    </TooltipProvider>
  );
}

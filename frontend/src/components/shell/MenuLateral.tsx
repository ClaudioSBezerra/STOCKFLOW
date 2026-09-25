import { NavLink } from 'react-router-dom';
import { ChevronDown, PanelLeftClose, PanelLeftOpen } from 'lucide-react';
import { cn } from '@/lib/utils';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';
import { filtrarNavPorPapel, perfil, type NavGrupo, type NavItem } from './nav-items';
import type { MenuEstado } from './useMenuEstado';

const alvo = 'min-h-touch-target-min';

// CartBadge é o `cart-badge` (UX-DR5): contador circular `destructive` sobre o
// item Carrinho. `aria-hidden`: o número é decorativo — o nome acessível do
// link continua sendo só "Carrinho". O chamador garante `count > 0`.
export function CartBadge({ count, className }: { count: number; className?: string }) {
  return (
    <span
      aria-hidden="true"
      className={cn(
        'flex h-4 min-w-4 items-center justify-center rounded-full bg-destructive px-1 text-[10px] leading-none font-medium text-white',
        className,
      )}
    >
      {count > 99 ? '99+' : count}
    </span>
  );
}

function classesItem(ativo: boolean, extra?: string) {
  return cn(
    'flex items-center gap-2 rounded-md px-3 text-body text-sidebar-foreground transition-colors hover:bg-sidebar-item-active hover:text-sidebar-foreground-active',
    alvo,
    ativo && 'sidebar-item-active',
    extra,
  );
}

export interface MenuLateralProps {
  /** Grupos já filtrados por papel (grupo vazio já removido). */
  grupos: NavGrupo[];
  papel: string;
  itemAtivoId: string | null;
  perfilAtivo: boolean;
  marca: string;
  estado: MenuEstado;
  cartCount: number;
  /** Menu recolhido (só na sidebar fixa do desktop; a gaveta nunca recolhe). */
  recolhido?: boolean;
  /** Mostra o botão de recolher (só na sidebar fixa). */
  podeRecolher?: boolean;
  /** Chamado ao escolher um item (a gaveta se fecha por aqui). */
  onNavigate?: () => void;
}

function ItemLink({
  item,
  ativo,
  cartCount,
  onNavigate,
}: {
  item: NavItem;
  ativo: boolean;
  cartCount: number;
  onNavigate?: () => void;
}) {
  return (
    <NavLink
      to={item.to}
      end
      onClick={onNavigate}
      className={classesItem(ativo, 'justify-between')}
    >
      <span>{item.label}</span>
      {item.id === 'carrinho' && cartCount > 0 ? <CartBadge count={cartCount} /> : null}
    </NavLink>
  );
}

function GrupoExpandido({
  grupo,
  itemAtivoId,
  estado,
  cartCount,
  onNavigate,
}: {
  grupo: NavGrupo;
  itemAtivoId: string | null;
  estado: MenuEstado;
  cartCount: number;
  onNavigate?: () => void;
}) {
  const Icon = grupo.icon;
  const aberto = estado.grupoAberto(grupo.id);
  const painelId = `menu-grupo-${grupo.id}`;
  return (
    <li>
      <button
        type="button"
        aria-expanded={aberto}
        aria-controls={painelId}
        onClick={() => estado.alternarGrupo(grupo.id)}
        className={cn(
          'flex w-full items-center gap-2 rounded-md px-3 text-body font-semibold text-sidebar-group-title transition-colors hover:bg-sidebar-item-active',
          alvo,
        )}
      >
        <Icon className="size-5 shrink-0" aria-hidden="true" />
        <span className="flex-1 text-left">{grupo.label}</span>
        <ChevronDown
          className={cn('size-4 shrink-0 transition-transform', !aberto && '-rotate-90')}
          aria-hidden="true"
        />
      </button>
      {aberto ? (
        <ul id={painelId} className="mt-1 flex flex-col gap-1 pl-4">
          {grupo.itens.map((item) => (
            <li key={item.id}>
              <ItemLink
                item={item}
                ativo={item.id === itemAtivoId}
                cartCount={cartCount}
                onNavigate={onNavigate}
              />
            </li>
          ))}
        </ul>
      ) : null}
    </li>
  );
}

function GrupoRecolhido({
  grupo,
  itemAtivoId,
  cartCount,
}: {
  grupo: NavGrupo;
  itemAtivoId: string | null;
  cartCount: number;
}) {
  const Icon = grupo.icon;
  const temAtivo = grupo.itens.some((i) => i.id === itemAtivoId);
  const mostrarBadge = cartCount > 0 && grupo.itens.some((i) => i.id === 'carrinho');
  return (
    <li className="flex justify-center">
      <DropdownMenu>
        <Tooltip>
          <TooltipTrigger asChild>
            <DropdownMenuTrigger asChild>
              <button
                type="button"
                aria-label={grupo.label}
                className={cn(
                  'relative flex min-w-touch-target-min items-center justify-center rounded-md text-sidebar-group-title transition-colors hover:bg-sidebar-item-active',
                  alvo,
                  temAtivo && 'sidebar-item-active',
                )}
              >
                <Icon className="size-5" aria-hidden="true" />
                {mostrarBadge ? (
                  <CartBadge count={cartCount} className="absolute top-1 right-1" />
                ) : null}
              </button>
            </DropdownMenuTrigger>
          </TooltipTrigger>
          <TooltipContent side="right">{grupo.label}</TooltipContent>
        </Tooltip>
        <DropdownMenuContent side="right" align="start">
          <DropdownMenuLabel>{grupo.label}</DropdownMenuLabel>
          {grupo.itens.map((item) => (
            <DropdownMenuItem key={item.id} asChild className={alvo}>
              <NavLink
                to={item.to}
                end
                className="flex items-center justify-between gap-3"
                aria-current={item.id === itemAtivoId ? 'page' : undefined}
              >
                <span className={cn(item.id === itemAtivoId && 'font-semibold')}>{item.label}</span>
                {item.id === 'carrinho' && cartCount > 0 ? <CartBadge count={cartCount} /> : null}
              </NavLink>
            </DropdownMenuItem>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>
    </li>
  );
}

/**
 * Conteúdo do menu lateral (Epic 17): marca do ambiente, grupos escritos e
 * "Meu perfil" no rodapé. Reusado pela sidebar fixa (desktop) e pela gaveta
 * do celular. Item ativo com `aria-current="page"` (via NavLink), grupo é
 * botão com `aria-expanded`.
 */
export function MenuLateral({
  grupos,
  papel,
  itemAtivoId,
  perfilAtivo,
  marca,
  estado,
  cartCount,
  recolhido = false,
  podeRecolher = false,
  onNavigate,
}: MenuLateralProps) {
  const mostrarPerfil = filtrarNavPorPapel([perfil], papel).length > 0;
  const PerfilIcon = perfil.icon;
  return (
    <div className="flex h-full min-h-0 flex-col bg-sidebar text-sidebar-foreground">
      <div
        className={cn(
          'flex h-topbar-height shrink-0 items-center gap-2 px-4',
          recolhido && 'justify-center px-0',
        )}
      >
        {recolhido ? null : (
          <span className="flex-1 truncate text-heading-md text-sidebar-group-title">{marca}</span>
        )}
        {podeRecolher ? (
          <button
            type="button"
            aria-label={recolhido ? 'Expandir menu' : 'Recolher menu'}
            onClick={estado.alternarRecolhido}
            className={cn(
              'flex min-w-touch-target-min items-center justify-center rounded-md text-sidebar-foreground transition-colors hover:bg-sidebar-item-active hover:text-sidebar-foreground-active',
              alvo,
            )}
          >
            {recolhido ? (
              <PanelLeftOpen className="size-5" aria-hidden="true" />
            ) : (
              <PanelLeftClose className="size-5" aria-hidden="true" />
            )}
          </button>
        ) : null}
      </div>

      <nav aria-label="Menu principal" className="min-h-0 flex-1 overflow-y-auto px-2 py-2">
        <ul className="flex flex-col gap-2">
          {grupos.map((grupo) =>
            recolhido ? (
              <GrupoRecolhido
                key={grupo.id}
                grupo={grupo}
                itemAtivoId={itemAtivoId}
                cartCount={cartCount}
              />
            ) : (
              <GrupoExpandido
                key={grupo.id}
                grupo={grupo}
                itemAtivoId={itemAtivoId}
                estado={estado}
                cartCount={cartCount}
                onNavigate={onNavigate}
              />
            ),
          )}
        </ul>
      </nav>

      {mostrarPerfil ? (
        <div className="shrink-0 border-t border-sidebar-item-active px-2 py-2">
          {recolhido ? (
            <Tooltip>
              <TooltipTrigger asChild>
                <NavLink
                  to={perfil.to}
                  end
                  aria-label={perfil.label}
                  className={classesItem(perfilAtivo, 'justify-center px-0')}
                >
                  <PerfilIcon className="size-5" aria-hidden="true" />
                </NavLink>
              </TooltipTrigger>
              <TooltipContent side="right">{perfil.label}</TooltipContent>
            </Tooltip>
          ) : (
            <NavLink to={perfil.to} end onClick={onNavigate} className={classesItem(perfilAtivo)}>
              <PerfilIcon className="size-5 shrink-0" aria-hidden="true" />
              <span>{perfil.label}</span>
            </NavLink>
          )}
        </div>
      ) : null}
    </div>
  );
}

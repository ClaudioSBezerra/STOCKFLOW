import { useEffect, useState } from 'react';
import { NavLink, Outlet, useLocation } from 'react-router-dom';
import { CircleHelp, Menu, TriangleAlert } from 'lucide-react';
import { cn } from '@/lib/utils';
import { TooltipProvider } from '@/components/ui/tooltip';
import { Sheet, SheetContent, SheetTitle, SheetTrigger } from '@/components/ui/sheet';
import { Avatar, AvatarFallback } from '@/components/ui/avatar';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { useAuth } from '@/lib/auth';
import { useCarrinho } from '@/lib/carrinho';
import { nomeDaMarca } from '@/lib/marca';
import { gruposVisiveis, itemAtivo, navGrupos, perfil } from './nav-items';
import { MenuLateral } from './MenuLateral';
import { useMenuEstado } from './useMenuEstado';

const touchTarget = 'min-h-touch-target-min min-w-touch-target-min';

/**
 * Faixa do Ambiente de Treinamento (Story 9.2, UX do épico 9): persistente no
 * topo, em TODAS as larguras, fundo warning com texto escuro — difícil de
 * ignorar, para ninguém confundir o Treinamento com a operação real.
 */
function FaixaTreinamento() {
  return (
    <div
      role="note"
      className="sticky top-0 z-50 flex items-center justify-center gap-2 bg-warning px-4 py-2 text-center text-body font-semibold text-foreground"
    >
      <TriangleAlert className="size-5 shrink-0" aria-hidden="true" />
      <span>AMBIENTE DE TREINAMENTO — dados de exemplo, nada aqui afeta a operação real</span>
    </div>
  );
}

/** Ajuda: diálogo curto, sem página de ajuda nova (Story 17.1). */
function BotaoAjuda() {
  return (
    <Dialog>
      <DialogTrigger asChild>
        <Button variant="ghost" className={cn('gap-2', touchTarget)}>
          <CircleHelp className="size-5" aria-hidden="true" />
          Ajuda
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Ajuda</DialogTitle>
          <DialogDescription>
            Dúvidas de uso? Fale com o administrador da sua empresa.
          </DialogDescription>
        </DialogHeader>
      </DialogContent>
    </Dialog>
  );
}

/**
 * Layout raiz do stockflow (Epic 17, Story 17.1): menu lateral azul-marinho
 * de 240px (64px recolhido) no desktop (`>= md`, 768px) e topo branco de 56px
 * com ☰ que abre o mesmo menu numa gaveta à esquerda no celular. Ver
 * EXPERIENCE.md (Menu agrupado) e DESIGN.md (sidebar-item-active).
 */
export function AppShell({ children }: { children?: React.ReactNode }) {
  const content = children ?? <Outlet />;
  const { pathname } = useLocation();
  // Guarda a rota em que a gaveta foi aberta: rota mudou (botão voltar, link
  // dentro da página) e ela fecha sozinha, sem ficar por cima do conteúdo novo.
  const [gavetaEm, setGavetaEm] = useState<string | null>(null);
  const gavetaAberta = gavetaEm === pathname;
  const setGavetaAberta = (aberta: boolean) => setGavetaEm(aberta ? pathname : null);

  // Navegação gated por papel: item sem permissão some (nunca desabilitado) e
  // grupo sem item visível some inteiro — `gruposVisiveis` usa
  // `filtrarNavPorPapel`, a mesma regra de sempre.
  const { usuario, logout } = useAuth();
  // cart-badge: contagem lida do estado global do CarrinhoProvider.
  const { count } = useCarrinho();
  const papel = usuario?.papel ?? '';
  const grupos = gruposVisiveis(navGrupos, papel);
  const ativo = itemAtivo(grupos, pathname);
  const perfilAtivo = pathname === perfil.to;
  const emTreinamento = usuario?.ambienteTreinamento === true;
  const marca = nomeDaMarca(window.location.hostname);

  const estado = useMenuEstado(usuario?.id ?? '');
  const { abrirGrupo } = estado;
  const grupoAtivoId = ativo?.grupoId ?? null;
  // O grupo do item ativo abre sozinho ao navegar até ele.
  useEffect(() => {
    if (grupoAtivoId) abrirGrupo(grupoAtivoId);
  }, [grupoAtivoId, abrirGrupo]);

  const menuProps = {
    grupos,
    papel,
    itemAtivoId: ativo?.itemId ?? null,
    perfilAtivo,
    marca,
    estado,
    cartCount: count,
  };

  const shell = (
    <div
      className={cn(
        'flex flex-col bg-background text-foreground md:h-svh md:flex-row md:overflow-hidden',
        emTreinamento ? 'min-h-0 flex-1 md:h-auto' : 'min-h-svh',
      )}
    >
      {/* Menu lateral fixo — desktop (>= md) */}
      <aside
        className={cn(
          'hidden shrink-0 md:block',
          estado.recolhido ? 'w-sidebar-width-collapsed' : 'w-sidebar-width',
        )}
      >
        <MenuLateral {...menuProps} recolhido={estado.recolhido} podeRecolher />
      </aside>

      <div className="flex min-w-0 flex-1 flex-col md:min-h-0">
        {/* Topo branco de 56px */}
        <header className="sticky top-0 z-30 flex h-topbar-height shrink-0 items-center gap-2 border-b border-border bg-card px-4 md:static">
          <Sheet open={gavetaAberta} onOpenChange={setGavetaAberta}>
            <SheetTrigger asChild>
              <Button
                variant="ghost"
                aria-label="Abrir menu"
                className={cn('md:hidden', touchTarget)}
              >
                <Menu className="size-5" aria-hidden="true" />
              </Button>
            </SheetTrigger>
            <SheetContent
              side="left"
              showCloseButton={false}
              aria-describedby={undefined}
              className="w-sidebar-width max-w-[85vw] gap-0 border-sidebar-item-active bg-sidebar p-0 sm:max-w-none"
            >
              <SheetTitle className="sr-only">Menu</SheetTitle>
              <MenuLateral {...menuProps} onNavigate={() => setGavetaAberta(false)} />
            </SheetContent>
          </Sheet>

          <div className="ml-auto flex items-center gap-2">
            {emTreinamento ? (
              <span className="rounded-full bg-warning/10 px-3 py-1 text-label text-text-on-tint-warning">
                Treinamento
              </span>
            ) : null}
            <BotaoAjuda />
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <button
                  type="button"
                  aria-label="Menu da conta"
                  className={cn('flex items-center justify-center rounded-full', touchTarget)}
                >
                  <Avatar className="size-8">
                    <AvatarFallback>
                      <perfil.icon className="size-4" aria-hidden="true" />
                    </AvatarFallback>
                  </Avatar>
                </button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem asChild className={touchTarget}>
                  <NavLink to={perfil.to}>{perfil.label}</NavLink>
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem className={touchTarget} onSelect={() => logout()}>
                  Sair
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </header>

        <main className="min-w-0 flex-1 md:min-h-0 md:overflow-y-auto">{content}</main>
      </div>
    </div>
  );

  return (
    <TooltipProvider>
      {emTreinamento ? (
        <div className="flex min-h-svh flex-col border-4 border-warning md:h-svh">
          <FaixaTreinamento />
          {shell}
        </div>
      ) : (
        shell
      )}
    </TooltipProvider>
  );
}

import { describe, expect, it, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { AppShell } from './AppShell';

// AppShell consome useAuth() para gatear a navegação por papel (Story 1.5).
// O mock deixa o papel do usuário configurável por teste; o padrão é `adm`,
// que vê todas as superfícies — mantendo os testes de layout pré-Story-1.5
// inalterados.
const authState = vi.hoisted(() => ({
  papel: 'adm' as string,
  logout: vi.fn(),
}));

vi.mock('@/lib/auth', () => ({
  useAuth: () => ({
    estado: 'autenticado',
    usuario: { id: '1', nome: 'Teste', email: 'teste@empresa.com', papel: authState.papel },
    definirSessao: vi.fn(),
    logout: authState.logout,
  }),
}));

// AppShell consome useCarrinho() para o cart-badge (Story 7.1) — mock com
// contagem configurável por teste, padrão 0 (badge ausente), mantendo os
// testes de layout pré-Story-7.1 inalterados.
const carrinhoState = vi.hoisted(() => ({ count: 0 }));
vi.mock('@/lib/carrinho', () => ({
  useCarrinho: () => ({
    itens: [],
    count: carrinhoState.count,
    carregando: false,
    refresh: vi.fn(),
    adicionarItem: vi.fn(),
    removerItem: vi.fn(),
  }),
}));

// Reset global — cobre TODOS os describes deste arquivo, não só o primeiro,
// para nenhum teste herdar uma contagem deixada por um teste anterior do
// cart-badge.
beforeEach(() => {
  carrinhoState.count = 0;
});

function renderShell(initialPath = '/') {
  return render(
    <MemoryRouter initialEntries={[initialPath]}>
      <Routes>
        <Route path="/" element={<AppShell />}>
          <Route index element={<div>página inicial</div>} />
          <Route path="*" element={<div>página inicial</div>} />
        </Route>
      </Routes>
    </MemoryRouter>,
  );
}

describe('AppShell', () => {
  beforeEach(() => {
    authState.papel = 'adm';
    authState.logout.mockClear();
  });

  it('renderiza o rail (desktop) e a bottom nav (mobile) com as classes de breakpoint corretas', () => {
    renderShell();

    const navs = screen.getAllByRole('navigation', { name: 'Navegação principal' });
    expect(navs).toHaveLength(2);

    const [rail, bottomNav] = navs;
    expect(rail.className).toContain('hidden');
    expect(rail.className).toContain('md:flex');

    expect(bottomNav.className).toContain('flex');
    expect(bottomNav.className).toContain('md:hidden');
  });

  it('mostra Catálogo, Carrinho, Pedidos e Mais na bottom nav', () => {
    renderShell();
    const bottomNav = screen.getAllByRole('navigation', { name: 'Navegação principal' })[1];
    const scoped = within(bottomNav);

    expect(scoped.getByRole('link', { name: 'Catálogo' })).toBeInTheDocument();
    expect(scoped.getByRole('link', { name: 'Carrinho' })).toBeInTheDocument();
    expect(scoped.getByRole('link', { name: 'Pedidos' })).toBeInTheDocument();
    expect(scoped.getByRole('button', { name: 'Mais' })).toBeInTheDocument();
  });

  it('marca o item da rota atual com nav-item-active no rail', () => {
    renderShell('/carrinho');
    const rail = screen.getAllByRole('navigation', { name: 'Navegação principal' })[0];
    const scoped = within(rail);

    expect(scoped.getByRole('link', { name: 'Carrinho' }).className).toContain('nav-item-active');
    expect(scoped.getByRole('link', { name: 'Catálogo' }).className).not.toContain(
      'nav-item-active',
    );
  });

  it('mostra um Tooltip com o rótulo ao passar o mouse sobre um ícone do rail', async () => {
    const user = userEvent.setup();
    renderShell();
    const rail = screen.getAllByRole('navigation', { name: 'Navegação principal' })[0];
    const catalogoLink = within(rail).getByRole('link', { name: 'Catálogo' });

    await user.hover(catalogoLink);

    const tooltip = await screen.findByRole('tooltip');
    expect(tooltip).toHaveTextContent('Catálogo');
  });

  it('não marca um item inativo do rail com aria-current', () => {
    renderShell('/carrinho');
    const rail = screen.getAllByRole('navigation', { name: 'Navegação principal' })[0];
    const scoped = within(rail);

    expect(scoped.getByRole('link', { name: 'Carrinho' })).toHaveAttribute(
      'aria-current',
      'page',
    );
    expect(scoped.getByRole('link', { name: 'Catálogo' })).not.toHaveAttribute('aria-current');
  });

  it('abre o DropdownMenu de perfil no rail com o link Meu Perfil', async () => {
    const user = userEvent.setup();
    renderShell();
    const rail = screen.getAllByRole('navigation', { name: 'Navegação principal' })[0];

    await user.click(within(rail).getByRole('button', { name: 'Meu Perfil' }));

    const menu = await screen.findByRole('menu');
    const link = within(menu).getByRole('menuitem', { name: 'Meu Perfil' });
    expect(link).toHaveAttribute('href', '/configuracoes');
  });

  it('aplica o alvo de toque mínimo (48px) ao item "Meu Perfil" dentro do DropdownMenu do rail', async () => {
    const user = userEvent.setup();
    renderShell();
    const rail = screen.getAllByRole('navigation', { name: 'Navegação principal' })[0];

    await user.click(within(rail).getByRole('button', { name: 'Meu Perfil' }));

    const menu = await screen.findByRole('menu');
    const link = within(menu).getByRole('menuitem', { name: 'Meu Perfil' });
    expect(link.className).toContain('min-h-touch-target-min');
    expect(link.className).toContain('min-w-touch-target-min');
  });

  it('mostra "Sair" no DropdownMenu de perfil do rail e chama logout ao clicar', async () => {
    const user = userEvent.setup();
    renderShell();
    const rail = screen.getAllByRole('navigation', { name: 'Navegação principal' })[0];

    await user.click(within(rail).getByRole('button', { name: 'Meu Perfil' }));

    const menu = await screen.findByRole('menu');
    const sair = within(menu).getByRole('menuitem', { name: 'Sair' });
    await user.click(sair);

    expect(authState.logout).toHaveBeenCalledTimes(1);
  });

  it('mostra "Sair" no Sheet "Mais" (mobile) e chama logout ao clicar', async () => {
    const user = userEvent.setup();
    renderShell();
    const bottomNav = screen.getAllByRole('navigation', { name: 'Navegação principal' })[1];

    await user.click(within(bottomNav).getByRole('button', { name: 'Mais' }));
    const dialog = await screen.findByRole('dialog');

    await user.click(within(dialog).getByRole('button', { name: 'Sair' }));

    expect(authState.logout).toHaveBeenCalledTimes(1);
  });

  it('abre o Sheet de "Mais" com os itens administrativos e Meu Perfil', async () => {
    const user = userEvent.setup();
    renderShell();
    const bottomNav = screen.getAllByRole('navigation', { name: 'Navegação principal' })[1];

    await user.click(within(bottomNav).getByRole('button', { name: 'Mais' }));

    const dialog = await screen.findByRole('dialog');
    const scoped = within(dialog);
    expect(scoped.getByRole('link', { name: /Estoques/ })).toBeInTheDocument();
    expect(scoped.getByRole('link', { name: /Normalização/ })).toBeInTheDocument();
    expect(scoped.getByRole('link', { name: /Relatórios/ })).toBeInTheDocument();
    expect(scoped.getByRole('link', { name: /Meu Perfil/ })).toBeInTheDocument();
  });

  it('fecha o Sheet de "Mais" ao clicar num item de navegação administrativo', async () => {
    const user = userEvent.setup();
    renderShell();
    const bottomNav = screen.getAllByRole('navigation', { name: 'Navegação principal' })[1];

    await user.click(within(bottomNav).getByRole('button', { name: 'Mais' }));
    const dialog = await screen.findByRole('dialog');

    await user.click(within(dialog).getByRole('link', { name: /Estoques/ }));

    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
  });

  it('aplica o alvo de toque mínimo (48px) a todo ícone de navegação', () => {
    renderShell();
    const rail = screen.getAllByRole('navigation', { name: 'Navegação principal' })[0];
    const catalogoLink = within(rail).getByRole('link', { name: 'Catálogo' });

    expect(catalogoLink.className).toContain('min-h-touch-target-min');
    expect(catalogoLink.className).toContain('min-w-touch-target-min');
  });

  it('renderiza tabs e sideNav quando fornecidos', () => {
    render(
      <MemoryRouter>
        <AppShell tabs={<div>abas do módulo</div>} sideNav={<div>submenu do módulo</div>}>
          <div>conteúdo</div>
        </AppShell>
      </MemoryRouter>,
    );

    expect(screen.getByText('abas do módulo')).toBeInTheDocument();
    expect(screen.getByText('submenu do módulo')).toBeInTheDocument();
    expect(screen.getByText('conteúdo')).toBeInTheDocument();
  });

  it('não renderiza tabs/sideNav quando omitidos (sem consumidor real nesta story)', () => {
    renderShell();
    expect(screen.queryByText('abas do módulo')).not.toBeInTheDocument();
  });
});

describe('AppShell — navegação gated por papel (Story 1.5)', () => {
  beforeEach(() => {
    authState.papel = 'usuario';
  });

  it('esconde Estoques/Normalização/Relatórios no rail para papel usuario', () => {
    renderShell();
    const rail = screen.getAllByRole('navigation', { name: 'Navegação principal' })[0];
    const scoped = within(rail);

    expect(scoped.getByRole('link', { name: 'Catálogo' })).toBeInTheDocument();
    expect(scoped.getByRole('link', { name: 'Carrinho' })).toBeInTheDocument();
    expect(scoped.getByRole('link', { name: 'Pedidos' })).toBeInTheDocument();
    expect(scoped.queryByRole('link', { name: 'Estoques' })).not.toBeInTheDocument();
    expect(scoped.queryByRole('link', { name: 'Normalização' })).not.toBeInTheDocument();
    expect(scoped.queryByRole('link', { name: 'Relatórios' })).not.toBeInTheDocument();
  });

  it('esconde os itens admin na bottom nav para papel usuario', () => {
    renderShell();
    const bottomNav = screen.getAllByRole('navigation', { name: 'Navegação principal' })[1];
    const scoped = within(bottomNav);

    expect(scoped.getByRole('link', { name: 'Catálogo' })).toBeInTheDocument();
    expect(scoped.queryByRole('link', { name: 'Estoques' })).not.toBeInTheDocument();
    expect(scoped.queryByRole('link', { name: 'Normalização' })).not.toBeInTheDocument();
    expect(scoped.queryByRole('link', { name: 'Relatórios' })).not.toBeInTheDocument();
  });

  it('esconde os itens admin no Sheet "Mais" para papel usuario (sem tela de acesso negado)', async () => {
    const user = userEvent.setup();
    renderShell();
    const bottomNav = screen.getAllByRole('navigation', { name: 'Navegação principal' })[1];

    await user.click(within(bottomNav).getByRole('button', { name: 'Mais' }));
    const dialog = await screen.findByRole('dialog');
    const scoped = within(dialog);

    expect(scoped.queryByRole('link', { name: /Estoques/ })).not.toBeInTheDocument();
    expect(scoped.queryByRole('link', { name: /Normalização/ })).not.toBeInTheDocument();
    expect(scoped.queryByRole('link', { name: /Relatórios/ })).not.toBeInTheDocument();
    // "Meu Perfil" (papelMinimo 'usuario') continua visível.
    expect(scoped.getByRole('link', { name: /Meu Perfil/ })).toBeInTheDocument();
    expect(screen.queryByText(/acesso negado/i)).not.toBeInTheDocument();
  });

  it('mostra os itens admin quando o papel é almoxarife', () => {
    authState.papel = 'almoxarife';
    renderShell();
    const rail = screen.getAllByRole('navigation', { name: 'Navegação principal' })[0];
    const scoped = within(rail);

    expect(scoped.getByRole('link', { name: 'Estoques' })).toBeInTheDocument();
    expect(scoped.getByRole('link', { name: 'Normalização' })).toBeInTheDocument();
    expect(scoped.getByRole('link', { name: 'Relatórios' })).toBeInTheDocument();
  });
});

describe('AppShell — cart-badge (Story 7.1)', () => {
  it('não mostra o cart-badge quando o carrinho está vazio (count 0)', () => {
    carrinhoState.count = 0;
    renderShell();
    const rail = screen.getAllByRole('navigation', { name: 'Navegação principal' })[0];
    const bottomNav = screen.getAllByRole('navigation', { name: 'Navegação principal' })[1];

    expect(within(rail).getByRole('link', { name: 'Carrinho' })).toBeInTheDocument();
    expect(within(rail).queryByText('3')).not.toBeInTheDocument();
    expect(within(bottomNav).getByRole('link', { name: 'Carrinho' })).toBeInTheDocument();
  });

  it('mostra o cart-badge com a contagem no rail e na bottom nav quando count > 0', () => {
    carrinhoState.count = 3;
    renderShell();
    const rail = screen.getAllByRole('navigation', { name: 'Navegação principal' })[0];
    const bottomNav = screen.getAllByRole('navigation', { name: 'Navegação principal' })[1];

    expect(within(rail).getByText('3')).toBeInTheDocument();
    expect(within(bottomNav).getByText('3')).toBeInTheDocument();
  });

  it('o cart-badge nunca muda o nome acessível do link "Carrinho"', () => {
    carrinhoState.count = 5;
    renderShell();
    const rail = screen.getAllByRole('navigation', { name: 'Navegação principal' })[0];

    // O número é aria-hidden — o link continua com o rótulo acessível "Carrinho"
    // (nunca "Carrinho 5"), então getByRole com o nome exato ainda o encontra.
    expect(within(rail).getByRole('link', { name: 'Carrinho' })).toBeInTheDocument();
  });

  it('trunca a contagem acima de 99 para "99+"', () => {
    carrinhoState.count = 150;
    renderShell();
    const rail = screen.getAllByRole('navigation', { name: 'Navegação principal' })[0];

    expect(within(rail).getByText('99+')).toBeInTheDocument();
  });

  it('mostra "99" por extenso no limite (count = 99, fronteira nunca trunca)', () => {
    carrinhoState.count = 99;
    renderShell();
    const rail = screen.getAllByRole('navigation', { name: 'Navegação principal' })[0];

    expect(within(rail).getByText('99')).toBeInTheDocument();
    expect(within(rail).queryByText('99+')).not.toBeInTheDocument();
  });

  it('trunca para "99+" já em count = 100 (um a mais que a fronteira)', () => {
    carrinhoState.count = 100;
    renderShell();
    const rail = screen.getAllByRole('navigation', { name: 'Navegação principal' })[0];

    expect(within(rail).getByText('99+')).toBeInTheDocument();
  });

  it('nunca mostra o cart-badge em outro item de navegação além de Carrinho', () => {
    carrinhoState.count = 4;
    authState.papel = 'almoxarife';
    renderShell();
    const rail = screen.getAllByRole('navigation', { name: 'Navegação principal' })[0];

    expect(within(rail).getByRole('link', { name: 'Estoques' })).toBeInTheDocument();
    // O rail inteiro tem exatamente UM "4" (o badge do Carrinho) — não duas
    // ocorrências (o que aconteceria se outro item também ganhasse badge).
    expect(within(rail).getAllByText('4')).toHaveLength(1);
  });
});

describe('AppShell (sem consumo de erro global)', () => {
  beforeEach(() => {
    authState.papel = 'adm';
  });

  it('não quebra ao montar sem props obrigatórias além do Router', () => {
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {});
    renderShell();
    expect(consoleError).not.toHaveBeenCalled();
    consoleError.mockRestore();
  });
});

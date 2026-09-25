import { describe, expect, it, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom';
import { AppShell } from './AppShell';

// AppShell consome useAuth() para gatear a navegação por papel (Story 1.5).
// O mock deixa o papel do usuário configurável por teste; o padrão é `adm`,
// que vê todas as superfícies — mantendo os testes de layout pré-Story-1.5
// inalterados.
const authState = vi.hoisted(() => ({
  papel: 'adm' as string,
  id: '1',
  logout: vi.fn(),
  // Story 9.2: undefined = usuário SEM os campos novos (o mock de sempre).
  ambienteTreinamento: undefined as boolean | undefined,
}));

function LocalizacaoAtual() {
  return <output data-testid="local">{useLocation().pathname}</output>;
}

vi.mock('@/lib/auth', () => ({
  useAuth: () => ({
    estado: 'autenticado',
    usuario: {
      id: authState.id,
      nome: 'Teste',
      email: 'teste@empresa.com',
      papel: authState.papel,
      ...(authState.ambienteTreinamento === undefined
        ? {}
        : { ambienteTreinamento: authState.ambienteTreinamento, empresaNome: 'Acme - Treinamento' }),
    },
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
  window.localStorage.clear();
  carrinhoState.count = 0;
  authState.ambienteTreinamento = undefined;
});

function renderShell(initialPath = '/') {
  return render(
    <MemoryRouter initialEntries={[initialPath]}>
      <Routes>
        <Route path="/" element={<AppShell />}>
          <Route index element={<div>página inicial</div>} />
          <Route path="*" element={<LocalizacaoAtual />} />
        </Route>
      </Routes>
    </MemoryRouter>,
  );
}

// A classe do item ativo, não o `hover:bg-sidebar-item-active` de todos os itens.
const ATIVO = /(^|\s)sidebar-item-active(\s|$)/;
const menu = () => screen.getByRole('navigation', { name: 'Menu principal' });
const link = (nome: string | RegExp) => within(menu()).getByRole('link', { name: nome });
const semLink = (nome: string | RegExp) =>
  expect(within(menu()).queryByRole('link', { name: nome })).not.toBeInTheDocument();

describe('AppShell — menu lateral', () => {
  beforeEach(() => {
    authState.papel = 'adm';
    authState.logout.mockClear();
  });

  it('renderiza um único nav "Menu principal", sem bottom nav nem navegação antiga', () => {
    renderShell();

    expect(screen.getAllByRole('navigation', { name: 'Menu principal' })).toHaveLength(1);
    expect(screen.queryByRole('navigation', { name: 'Navegação principal' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Mais' })).not.toBeInTheDocument();
    // Sidebar fixa só a partir de md; o topo continua em todas as larguras.
    expect(menu().closest('aside')?.className).toContain('hidden');
    expect(menu().closest('aside')?.className).toContain('md:block');
    expect(menu().closest('aside')?.className).toContain('w-sidebar-width');
  });

  it('mostra a marca do ambiente e "Meu perfil" no rodapé do menu', () => {
    renderShell();

    expect(screen.getByText('stockflow')).toBeInTheDocument();
    const perfil = screen.getByRole('link', { name: 'Meu perfil' });
    expect(perfil).toHaveAttribute('href', '/configuracoes');
    expect(menu()).not.toContainElement(perfil);
  });

  it('mostra todos os grupos e itens desta story para adm', () => {
    renderShell();
    const nav = within(menu());

    for (const g of ['Catálogo', 'Pedidos', 'Estoque', 'Qualidade dos dados']) {
      expect(nav.getByRole('button', { name: g })).toHaveAttribute('aria-expanded', 'true');
    }
    for (const item of [
      'Produtos',
      'Cadastrar produto',
      'Importar planilha',
      'Carrinho',
      'Meus pedidos',
      'Fila de aprovação',
      'Locais',
      'Lançar saldo',
      'Movimentações',
      'Inconsistências',
      'Duplicatas',
    ]) {
      expect(nav.getByRole('link', { name: item })).toBeInTheDocument();
    }
    semLink('Relatórios');
  });

  it('cada item aponta para a rota própria', () => {
    renderShell();
    expect(link('Cadastrar produto')).toHaveAttribute('href', '/produtos/novo');
    expect(link('Importar planilha')).toHaveAttribute('href', '/produtos/importar');
    expect(link('Fila de aprovação')).toHaveAttribute('href', '/pedidos/fila');
    expect(link('Lançar saldo')).toHaveAttribute('href', '/estoques/lancar-saldo');
    expect(link('Movimentações')).toHaveAttribute('href', '/estoques/movimentacoes');
    expect(link('Duplicatas')).toHaveAttribute('href', '/normalizacao/duplicatas');
  });

  it('papel usuario: só Produtos, Carrinho e Meus pedidos; grupos Estoque e Qualidade somem', () => {
    authState.papel = 'usuario';
    renderShell();
    const nav = within(menu());

    expect(nav.getByRole('link', { name: 'Produtos' })).toBeInTheDocument();
    expect(nav.getByRole('link', { name: 'Carrinho' })).toBeInTheDocument();
    expect(nav.getByRole('link', { name: 'Meus pedidos' })).toBeInTheDocument();
    semLink('Cadastrar produto');
    semLink('Importar planilha');
    semLink('Fila de aprovação');
    expect(nav.queryByRole('button', { name: 'Estoque' })).not.toBeInTheDocument();
    expect(nav.queryByRole('button', { name: 'Qualidade dos dados' })).not.toBeInTheDocument();
    expect(nav.getAllByRole('button')).toHaveLength(2);
    expect(screen.queryByText(/acesso negado/i)).not.toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Meu perfil' })).toBeInTheDocument();
  });

  it('papel almoxarife vê os grupos Estoque e Qualidade dos dados', () => {
    authState.papel = 'almoxarife';
    renderShell();
    expect(link('Locais')).toBeInTheDocument();
    expect(link('Inconsistências')).toBeInTheDocument();
  });

  it('item da rota atual: aria-current="page", negrito branco, barra vermelha e grupo aberto', () => {
    renderShell('/estoques/movimentacoes');

    const ativo = link('Movimentações');
    expect(ativo).toHaveAttribute('aria-current', 'page');
    expect(ativo.className).toMatch(ATIVO);
    // Casamento exato: Locais (/estoques) não fica ativo em /estoques/movimentacoes.
    expect(link('Locais')).not.toHaveAttribute('aria-current');
    expect(link('Locais').className).not.toMatch(ATIVO);
    expect(link('Produtos')).not.toHaveAttribute('aria-current');
    expect(within(menu()).getByRole('button', { name: 'Estoque' })).toHaveAttribute(
      'aria-expanded',
      'true',
    );
  });

  it('a raiz marca só Produtos, não os demais', () => {
    renderShell('/');
    expect(link('Produtos')).toHaveAttribute('aria-current', 'page');
    expect(link('Carrinho')).not.toHaveAttribute('aria-current');
  });

  it('"Meu perfil" fica ativo em /configuracoes', () => {
    renderShell('/configuracoes');
    const perfil = screen.getByRole('link', { name: 'Meu perfil' });
    expect(perfil).toHaveAttribute('aria-current', 'page');
    expect(perfil.className).toMatch(ATIVO);
  });

  it('grupo é botão com aria-expanded: recolhe e reabre os itens', async () => {
    const user = userEvent.setup();
    renderShell();
    const grupo = within(menu()).getByRole('button', { name: 'Pedidos' });

    await user.click(grupo);
    expect(grupo).toHaveAttribute('aria-expanded', 'false');
    semLink('Meus pedidos');

    await user.click(grupo);
    expect(grupo).toHaveAttribute('aria-expanded', 'true');
    expect(link('Meus pedidos')).toBeInTheDocument();
  });

  it('o grupo do item ativo abre sozinho ao navegar, mesmo lembrado como fechado', async () => {
    const user = userEvent.setup();
    renderShell('/');
    const estoque = within(menu()).getByRole('button', { name: 'Estoque' });
    await user.click(estoque);
    expect(estoque).toHaveAttribute('aria-expanded', 'false');

    // Navegar para um item fora do grupo e voltar: o grupo abre sozinho.
    await user.click(link('Meus pedidos'));
    await user.click(within(menu()).getByRole('button', { name: 'Estoque' }));
    await user.click(link('Locais'));
    expect(within(menu()).getByRole('button', { name: 'Estoque' })).toHaveAttribute(
      'aria-expanded',
      'true',
    );
  });

  it('lembra os grupos fechados no localStorage e, ao remontar, mantém o estado', async () => {
    const user = userEvent.setup();
    const { unmount } = renderShell();
    await user.click(within(menu()).getByRole('button', { name: 'Pedidos' }));
    unmount();

    renderShell();
    expect(within(menu()).getByRole('button', { name: 'Pedidos' })).toHaveAttribute(
      'aria-expanded',
      'false',
    );
  });

  it('topo: Ajuda abre o diálogo e o menu da conta tem Meu perfil e Sair', async () => {
    const user = userEvent.setup();
    renderShell();

    await user.click(screen.getByRole('button', { name: 'Ajuda' }));
    const dialogo = await screen.findByRole('dialog');
    expect(dialogo).toHaveTextContent('Dúvidas de uso? Fale com o administrador da sua empresa.');
    await user.keyboard('{Escape}');
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());

    await user.click(screen.getByRole('button', { name: 'Menu da conta' }));
    const conta = await screen.findByRole('menu');
    expect(within(conta).getByRole('menuitem', { name: 'Meu perfil' })).toHaveAttribute(
      'href',
      '/configuracoes',
    );
    await user.click(within(conta).getByRole('menuitem', { name: 'Sair' }));
    expect(authState.logout).toHaveBeenCalledTimes(1);
  });

  it('o topo tem 56px (topbar-height) e fundo branco', () => {
    renderShell();
    const topo = screen.getByRole('banner');
    expect(topo.className).toContain('h-topbar-height');
    expect(topo.className).toContain('bg-card');
  });

  it('itens do menu têm alvo de toque mínimo (48px)', () => {
    renderShell();
    expect(link('Produtos').className).toContain('min-h-touch-target-min');
  });
});

describe('AppShell — marca do ambiente', () => {
  it('usa "Suprimentos" quando o host começa com "suprimentos."', () => {
    vi.stubGlobal('location', { ...window.location, hostname: 'suprimentos.fcxlabs.com' });
    try {
      renderShell();
      expect(screen.getByText('Suprimentos')).toBeInTheDocument();
      expect(screen.queryByText('stockflow')).not.toBeInTheDocument();
    } finally {
      vi.unstubAllGlobals();
    }
  });
});

describe('AppShell — menu recolhido', () => {
  beforeEach(() => {
    authState.papel = 'adm';
  });

  it('recolher leva a 64px, mostra ícones de grupo com tooltip e lembra a escolha', async () => {
    const user = userEvent.setup();
    const { unmount } = renderShell();

    await user.click(screen.getByRole('button', { name: 'Recolher menu' }));

    expect(menu().closest('aside')?.className).toContain('w-sidebar-width-collapsed');
    expect(within(menu()).queryByRole('link', { name: 'Produtos' })).not.toBeInTheDocument();
    const grupo = within(menu()).getByRole('button', { name: 'Catálogo' });
    await user.hover(grupo);
    expect(await screen.findByRole('tooltip')).toHaveTextContent('Catálogo');
    unmount();

    // Lembrado: ao remontar continua recolhido.
    renderShell();
    expect(menu().closest('aside')?.className).toContain('w-sidebar-width-collapsed');
    expect(screen.getByRole('button', { name: 'Expandir menu' })).toBeInTheDocument();
  });

  it('a escolha de recolher é por pessoa: outro usuário no mesmo navegador abre expandido', async () => {
    const user = userEvent.setup();
    const { unmount } = renderShell();
    await user.click(screen.getByRole('button', { name: 'Recolher menu' }));
    unmount();

    authState.id = '2';
    try {
      renderShell();
      expect(menu().closest('aside')?.className).not.toContain('w-sidebar-width-collapsed');
    } finally {
      authState.id = '1';
    }
  });

  it('recolhido, o cart-badge aparece no ícone do grupo Catálogo e o grupo da rota ativa é destacado', async () => {
    const user = userEvent.setup();
    carrinhoState.count = 3;
    renderShell('/estoques');
    await user.click(screen.getByRole('button', { name: 'Recolher menu' }));

    expect(within(within(menu()).getByRole('button', { name: 'Catálogo' })).getByText('3')).toBeInTheDocument();
    expect(within(menu()).getByRole('button', { name: 'Estoque' }).className).toMatch(ATIVO);
    carrinhoState.count = 0;
  });

  it('clicar no ícone do grupo abre o painel flutuante com os itens e navega', async () => {
    const user = userEvent.setup();
    renderShell();
    await user.click(screen.getByRole('button', { name: 'Recolher menu' }));

    await user.click(within(menu()).getByRole('button', { name: 'Estoque' }));
    const painel = await screen.findByRole('menu');
    expect(within(painel).getByRole('menuitem', { name: 'Locais' })).toBeInTheDocument();
    expect(within(painel).getByRole('menuitem', { name: 'Movimentações' })).toBeInTheDocument();

    await user.click(within(painel).getByRole('menuitem', { name: 'Movimentações' }));
    expect(screen.getByTestId('local')).toHaveTextContent('/estoques/movimentacoes');
  });

  it('expandir de volta mostra os grupos escritos', async () => {
    const user = userEvent.setup();
    renderShell();
    await user.click(screen.getByRole('button', { name: 'Recolher menu' }));
    await user.click(screen.getByRole('button', { name: 'Expandir menu' }));
    expect(link('Produtos')).toBeInTheDocument();
    expect(menu().closest('aside')?.className).toContain('w-sidebar-width');
    expect(menu().closest('aside')?.className).not.toContain('collapsed');
  });

  it('sem localStorage (lança) abre expandido e continua funcionando', async () => {
    const user = userEvent.setup();
    const getItem = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('bloqueado');
    });
    const setItem = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('bloqueado');
    });
    try {
      renderShell();
      expect(link('Produtos')).toBeInTheDocument();
      expect(within(menu()).getByRole('button', { name: 'Catálogo' })).toHaveAttribute(
        'aria-expanded',
        'true',
      );
      await user.click(screen.getByRole('button', { name: 'Recolher menu' }));
      expect(screen.getByRole('button', { name: 'Expandir menu' })).toBeInTheDocument();
    } finally {
      getItem.mockRestore();
      setItem.mockRestore();
    }
  });
});

describe('AppShell — gaveta no celular', () => {
  beforeEach(() => {
    authState.papel = 'adm';
  });

  it('☰ abre a gaveta com os mesmos grupos, foco vai para ela e volta ao ☰ ao fechar com Esc', async () => {
    const user = userEvent.setup();
    renderShell();
    const hamburguer = screen.getByRole('button', { name: 'Abrir menu' });
    expect(hamburguer.className).toContain('md:hidden');

    await user.click(hamburguer);
    const gaveta = await screen.findByRole('dialog');
    expect(within(gaveta).getByRole('navigation', { name: 'Menu principal' })).toBeInTheDocument();
    expect(within(gaveta).getByRole('link', { name: 'Fila de aprovação' })).toBeInTheDocument();
    expect(within(gaveta).getByRole('link', { name: 'Meu perfil' })).toBeInTheDocument();
    expect(gaveta.className).toContain('left-0');
    await waitFor(() => expect(gaveta).toContainElement(document.activeElement as HTMLElement));

    await user.keyboard('{Escape}');
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    await waitFor(() => expect(hamburguer).toHaveFocus());
  });

  it('fecha ao escolher um item e navega', async () => {
    const user = userEvent.setup();
    renderShell();

    await user.click(screen.getByRole('button', { name: 'Abrir menu' }));
    const gaveta = await screen.findByRole('dialog');
    await user.click(within(gaveta).getByRole('link', { name: 'Locais' }));

    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    expect(screen.getByTestId('local')).toHaveTextContent('/estoques');
  });

  it('fecha ao tocar fora (overlay)', async () => {
    const user = userEvent.setup();
    renderShell();

    await user.click(screen.getByRole('button', { name: 'Abrir menu' }));
    await screen.findByRole('dialog');
    const overlay = document.querySelector('[data-slot="sheet-overlay"]') as HTMLElement;
    await user.click(overlay);

    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
  });

  it('a gaveta respeita o papel: usuario não vê Estoque nem Qualidade', async () => {
    authState.papel = 'usuario';
    const user = userEvent.setup();
    renderShell();

    await user.click(screen.getByRole('button', { name: 'Abrir menu' }));
    const gaveta = await screen.findByRole('dialog');
    expect(within(gaveta).queryByRole('button', { name: 'Estoque' })).not.toBeInTheDocument();
    expect(within(gaveta).getByRole('link', { name: 'Meus pedidos' })).toBeInTheDocument();
  });
});

describe('AppShell — cart-badge', () => {
  it('não mostra o cart-badge quando o carrinho está vazio (count 0)', () => {
    carrinhoState.count = 0;
    renderShell();
    expect(within(link('Carrinho')).queryByText(/^\d+\+?$/)).not.toBeInTheDocument();
  });

  it('mostra o cart-badge com a contagem só no item Carrinho', () => {
    carrinhoState.count = 3;
    renderShell();
    expect(within(link('Carrinho')).getByText('3')).toBeInTheDocument();
    expect(within(menu()).getAllByText('3')).toHaveLength(1);
  });

  it('o cart-badge nunca muda o nome acessível do link "Carrinho"', () => {
    carrinhoState.count = 5;
    renderShell();
    expect(link('Carrinho')).toHaveAccessibleName('Carrinho');
  });

  it.each([
    [99, '99'],
    [100, '99+'],
    [150, '99+'],
  ])('contagem %i aparece como "%s"', (count, texto) => {
    carrinhoState.count = count;
    renderShell();
    expect(within(link('Carrinho')).getByText(texto)).toBeInTheDocument();
  });
});

describe('AppShell — Ambiente de Treinamento (Story 9.2)', () => {
  const TEXTO_FAIXA =
    'AMBIENTE DE TREINAMENTO — dados de exemplo, nada aqui afeta a operação real';

  it('sem o flag, não há faixa nem indicador no topo', () => {
    renderShell();

    expect(screen.queryByRole('note')).not.toBeInTheDocument();
    expect(screen.queryByText('Treinamento')).not.toBeInTheDocument();
  });

  it('com ambienteTreinamento:false, também não há faixa', () => {
    authState.ambienteTreinamento = false;
    renderShell();

    expect(screen.queryByText(TEXTO_FAIXA)).not.toBeInTheDocument();
  });

  it('com ambienteTreinamento:true, mostra a faixa persistente, o indicador no topo e o menu', () => {
    authState.ambienteTreinamento = true;
    renderShell();

    const faixa = screen.getByRole('note');
    expect(faixa).toHaveTextContent(TEXTO_FAIXA);
    expect(faixa.className).toContain('bg-warning');
    expect(faixa.className).not.toMatch(/(^|\s)(hidden|md:hidden|md:flex)(\s|$)/);
    expect(faixa.parentElement?.className).toContain('border-warning');
    expect(within(screen.getByRole('banner')).getByText('Treinamento')).toBeInTheDocument();
    expect(screen.getAllByRole('navigation', { name: 'Menu principal' })).toHaveLength(1);
  });
});

describe('AppShell — conteúdo', () => {
  it('renderiza os children quando fornecidos', () => {
    render(
      <MemoryRouter>
        <AppShell>
          <div>conteúdo</div>
        </AppShell>
      </MemoryRouter>,
    );
    expect(screen.getByText('conteúdo')).toBeInTheDocument();
  });

  it('não quebra ao montar sem props obrigatórias além do Router', () => {
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {});
    const consoleWarn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    renderShell();
    expect(consoleError).not.toHaveBeenCalled();
    expect(consoleWarn).not.toHaveBeenCalled();
    consoleError.mockRestore();
    consoleWarn.mockRestore();
  });
});

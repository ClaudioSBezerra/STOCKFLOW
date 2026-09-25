import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import type { ReactNode } from 'react';
import { render, screen, waitFor, within } from '@testing-library/react';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom';
import * as authModule from '@/lib/auth';
import { clearAccessToken, getAccessToken } from '@/lib/session';
import App, { RotaProtegida, router } from './App';

// `useAuth` vira um `vi.fn` que, por padrão, roda a implementação REAL
// (usada pelos testes de wiring de <App /> abaixo — AuthProvider + router de
// verdade). Os testes unitários de RotaProtegida sobrescrevem o retorno para
// dirigir cada estado sem rede.
const authReal = await vi.importActual<typeof import('@/lib/auth')>('@/lib/auth');

vi.mock('@/lib/auth', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/auth')>();
  return { ...actual, useAuth: vi.fn(actual.useAuth) };
});

const useAuthMock = vi.mocked(authModule.useAuth);

// useCarrinho() é mockado por inteiro (mesmo molde de AppShell.test.tsx e
// ProdutoDetalhePage.test.tsx) — sem isso, os casos `estado: 'autenticado'`
// abaixo montavam o `CarrinhoProvider` REAL, que dispara um `GET
// /api/carrinho` de verdade sem `fetch` stubado (risco de flake: chamada de
// rede não-mockada). `CarrinhoProvider` também é mockado como um passthrough
// — <App /> (testes de "wiring real" abaixo) continua importando-o de
// verdade internamente, só que agora resolve para este mock — então o
// wrapper manual de CarrinhoProvider deixou de ser necessário neste arquivo.
vi.mock('@/lib/carrinho', () => ({
  CarrinhoProvider: ({ children }: { children: ReactNode }) => children,
  useCarrinho: () => ({
    itens: [],
    count: 0,
    carregando: false,
    erro: false,
    refresh: vi.fn(),
    adicionarItem: vi.fn(),
    removerItem: vi.fn(),
  }),
}));

// `@/lib/realtime/client` é mockado por inteiro (mesmo motivo do
// CarrinhoProvider acima) — só PedidosPage (via MeusPedidosSection/
// FilaPedidosSection) o usa entre as páginas exercitadas neste arquivo,
// então este mock não afeta as demais rotas. Sem ele, o teste de `/pedidos`
// abaixo montaria um `EventSource` de verdade sem stub, risco de flake.
const conectarRealtimeMock = vi.hoisted(() => vi.fn());
vi.mock('@/lib/realtime/client', () => ({
  conectarRealtime: conectarRealtimeMock,
}));

function LocationDisplay() {
  const { pathname } = useLocation();
  return <span data-testid="pathname">{pathname}</span>;
}

function renderRota() {
  return render(
    <MemoryRouter initialEntries={['/']}>
      <LocationDisplay />
      <Routes>
        <Route path="/" element={<RotaProtegida />}>
          <Route index element={<div>árvore protegida</div>} />
        </Route>
        <Route path="/login" element={<div>tela de login</div>} />
      </Routes>
    </MemoryRouter>,
  );
}

describe('RotaProtegida (unidade)', () => {
  afterEach(() => {
    useAuthMock.mockReset();
  });

  it('estado carregando: mostra "Carregando..." e não redireciona', () => {
    useAuthMock.mockReturnValue({
      estado: 'carregando',
      usuario: null,
      definirSessao: vi.fn(),
      atualizarUsuario: vi.fn(),
      logout: vi.fn(),
    });
    renderRota();

    expect(screen.getByText('Carregando...')).toBeInTheDocument();
    expect(screen.getByTestId('pathname')).toHaveTextContent('/');
    expect(screen.queryByText('tela de login')).not.toBeInTheDocument();
    expect(screen.queryByText('árvore protegida')).not.toBeInTheDocument();
  });

  it('estado anonimo: redireciona para /login', () => {
    useAuthMock.mockReturnValue({
      estado: 'anonimo',
      usuario: null,
      definirSessao: vi.fn(),
      atualizarUsuario: vi.fn(),
      logout: vi.fn(),
    });
    renderRota();

    expect(screen.getByText('tela de login')).toBeInTheDocument();
    expect(screen.getByTestId('pathname')).toHaveTextContent('/login');
    expect(screen.queryByText('árvore protegida')).not.toBeInTheDocument();
    expect(screen.queryByText('Carregando...')).not.toBeInTheDocument();
  });

  it('estado autenticado: renderiza o AppShell e permanece em /', () => {
    useAuthMock.mockReturnValue({
      estado: 'autenticado',
      usuario: {
        id: '1',
        nome: 'Teste',
        email: 'teste@empresa.com',
        papel: 'usuario',
        mfaHabilitado: false,
        origem: 'senha',
      },
      definirSessao: vi.fn(),
      atualizarUsuario: vi.fn(),
      logout: vi.fn(),
    });
    renderRota();

    expect(screen.getByText('árvore protegida')).toBeInTheDocument();
    expect(screen.getByTestId('pathname')).toHaveTextContent('/');
    expect(screen.getAllByRole('navigation', { name: 'Menu principal' })).toHaveLength(1);
    expect(screen.queryByText('tela de login')).not.toBeInTheDocument();
  });

  it('gestor por senha sem MFA em / numa Empresa que exige: redireciona para /configuracoes (Story 1.11, AC1; Story 14.1)', () => {
    useAuthMock.mockReturnValue({
      estado: 'autenticado',
      usuario: {
        id: '1',
        nome: 'Gestora',
        email: 'gestora@empresa.com',
        papel: 'gestor',
        mfaHabilitado: false,
        origem: 'senha',
        empresa: { mfaObrigatorio: true },
      },
      definirSessao: vi.fn(),
      atualizarUsuario: vi.fn(),
      logout: vi.fn(),
    });
    renderRota();

    expect(screen.getByTestId('pathname')).toHaveTextContent('/configuracoes');
    expect(screen.queryByText('árvore protegida')).not.toBeInTheDocument();
  });

  it.each([
    ['Empresa não exige (mfaObrigatorio=false)', { mfaObrigatorio: false }],
    ['resposta sem o campo empresa (equivale a "não exige")', undefined],
  ])('gestor por senha sem MFA em /, %s: NÃO redireciona (Story 14.1)', (_nome, empresa) => {
    useAuthMock.mockReturnValue({
      estado: 'autenticado',
      usuario: {
        id: '1',
        nome: 'Gestora',
        email: 'gestora@empresa.com',
        papel: 'gestor',
        mfaHabilitado: false,
        origem: 'senha',
        empresa,
      },
      definirSessao: vi.fn(),
      atualizarUsuario: vi.fn(),
      logout: vi.fn(),
    });
    renderRota();

    expect(screen.getByText('árvore protegida')).toBeInTheDocument();
    expect(screen.getByTestId('pathname')).toHaveTextContent('/');
  });

  it.each([
    ['usuario', 'senha', false],
    ['almoxarife', 'senha', false],
    ['gestor', 'sso', false],
    ['adm', 'senha', true],
  ])('papel %s, origem %s, mfaHabilitado=%s numa Empresa que exige: NÃO redireciona (Story 14.1)', (papel, origem, mfaHabilitado) => {
    useAuthMock.mockReturnValue({
      estado: 'autenticado',
      usuario: {
        id: '1',
        nome: 'Pessoa',
        email: 'pessoa@empresa.com',
        papel,
        mfaHabilitado,
        origem,
        empresa: { mfaObrigatorio: true },
      },
      definirSessao: vi.fn(),
      atualizarUsuario: vi.fn(),
      logout: vi.fn(),
    });
    renderRota();

    expect(screen.getByText('árvore protegida')).toBeInTheDocument();
    expect(screen.getByTestId('pathname')).toHaveTextContent('/');
  });

  it('gestor por senha sem MFA já em /configuracoes: NÃO redireciona (evita loop)', () => {
    useAuthMock.mockReturnValue({
      estado: 'autenticado',
      usuario: {
        id: '1',
        nome: 'Gestora',
        email: 'gestora@empresa.com',
        papel: 'gestor',
        mfaHabilitado: false,
        origem: 'senha',
        empresa: { mfaObrigatorio: true },
      },
      definirSessao: vi.fn(),
      atualizarUsuario: vi.fn(),
      logout: vi.fn(),
    });
    render(
      <MemoryRouter initialEntries={['/configuracoes']}>
        <LocationDisplay />
        <Routes>
          <Route path="/" element={<RotaProtegida />}>
            <Route path="configuracoes" element={<div>árvore protegida</div>} />
          </Route>
        </Routes>
      </MemoryRouter>,
    );

    expect(screen.getByText('árvore protegida')).toBeInTheDocument();
    expect(screen.getByTestId('pathname')).toHaveTextContent('/configuracoes');
  });

  it('estado inesperado (fail-closed): redireciona para /login', () => {
    useAuthMock.mockReturnValue({
      estado: 'estado-novo-nao-previsto' as never,
      usuario: null,
      definirSessao: vi.fn(),
      atualizarUsuario: vi.fn(),
      logout: vi.fn(),
    });
    renderRota();

    expect(screen.getByText('tela de login')).toBeInTheDocument();
    expect(screen.queryByText('árvore protegida')).not.toBeInTheDocument();
  });
});

describe('<App /> — wiring real de AuthProvider + RotaProtegida', () => {
  const fetchMock = vi.fn();

  beforeEach(async () => {
    useAuthMock.mockImplementation(authReal.useAuth);
    vi.stubGlobal('fetch', fetchMock);
    fetchMock.mockReset();
    clearAccessToken();
    // Reseta a localização do router (compartilhado, criado em App.tsx) entre
    // os casos — um caso anterior pode tê-lo deixado em /login.
    await router.navigate('/');
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    clearAccessToken();
    useAuthMock.mockReset();
    conectarRealtimeMock.mockReset();
  });

  it('sem cookie de refresh: bootstrap falha e a rota protegida cai em /login', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (url === '/api/auth/refresh') {
        return Promise.resolve({ ok: false, status: 401, json: async () => ({}) });
      }
      throw new Error(`não deveria chamar ${url}`);
    });

    render(<App />);

    // Bootstrap resolve como anônimo -> RotaProtegida redireciona para a rota
    // pública /login (LoginPage real).
    expect(await screen.findByText('Acesse sua conta do stockflow.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Entrar' })).toBeInTheDocument();
    expect(window.location.pathname).toBe('/login');
    expect(screen.queryByRole('navigation', { name: 'Menu principal' })).not.toBeInTheDocument();
  });

  it('/esqueci-senha renderiza EsqueciSenhaPage sem passar pelo RotaProtegida', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (url === '/api/auth/refresh') {
        return Promise.resolve({ ok: false, status: 401, json: async () => ({}) });
      }
      throw new Error(`não deveria chamar ${url}`);
    });
    await router.navigate('/esqueci-senha');

    render(<App />);

    expect(
      await screen.findByText('Informe seu e-mail e enviaremos um link para redefinir a senha.'),
    ).toBeInTheDocument();
    expect(window.location.pathname).toBe('/esqueci-senha');
    expect(screen.queryByRole('navigation', { name: 'Menu principal' })).not.toBeInTheDocument();
  });

  it('/redefinir-senha renderiza RedefinirSenhaPage sem passar pelo RotaProtegida', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (url === '/api/auth/refresh') {
        return Promise.resolve({ ok: false, status: 401, json: async () => ({}) });
      }
      throw new Error(`não deveria chamar ${url}`);
    });
    await router.navigate('/redefinir-senha');

    render(<App />);

    // Sem `?token=` a página já resolve para o estado de link inválido, sem
    // tocar a API — prova que a rota pública renderiza a página, não o shell.
    expect(await screen.findByText('Este link de redefinição é inválido.')).toBeInTheDocument();
    expect(window.location.pathname).toBe('/redefinir-senha');
    expect(screen.queryByRole('navigation', { name: 'Menu principal' })).not.toBeInTheDocument();
  });

  it('/configuracoes renderiza ConfiguracoesPage dentro do shell, não a PlaceholderPage', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (url === '/api/auth/refresh') {
        return Promise.resolve({ ok: true, json: async () => ({ token: 'access-abc' }) });
      }
      if (url === '/api/auth/me') {
        return Promise.resolve({
          ok: true,
          json: async () => ({ id: '1', nome: 'Fulano', email: 'f@empresa.com', papel: 'usuario' }),
        });
      }
      if (url === '/api/promocoes/minha') {
        return Promise.resolve({ ok: true, json: async () => ({ solicitacao: null }) });
      }
      throw new Error(`URL inesperada: ${url}`);
    });
    await router.navigate('/configuracoes');

    render(<App />);

    expect(await screen.findByRole('heading', { name: 'Meu Perfil' })).toBeInTheDocument();
    expect(window.location.pathname).toBe('/configuracoes');
    expect(screen.getByRole('button', { name: 'Solicitar promoção para Almoxarife' })).toBeInTheDocument();
    expect(screen.getAllByRole('navigation', { name: 'Menu principal' }).length).toBeGreaterThan(0);
    expect(screen.queryByText('Em construção')).not.toBeInTheDocument();
  });

  it('/pedidos renderiza PedidosPage (aba "Meus Pedidos") dentro do shell, não a PlaceholderPage', async () => {
    conectarRealtimeMock.mockImplementation((_receber, mudar) => {
      mudar('conectado');
      return vi.fn();
    });
    fetchMock.mockImplementation((url: string) => {
      if (url === '/api/auth/refresh') {
        return Promise.resolve({ ok: true, json: async () => ({ token: 'access-abc' }) });
      }
      if (url === '/api/auth/me') {
        return Promise.resolve({
          ok: true,
          json: async () => ({ id: '1', nome: 'Fulano', email: 'f@empresa.com', papel: 'usuario' }),
        });
      }
      if (url === '/api/pedidos') {
        return Promise.resolve({ ok: true, json: async () => ({ pedidos: [] }) });
      }
      throw new Error(`URL inesperada: ${url}`);
    });
    await router.navigate('/pedidos');

    render(<App />);

    expect(await screen.findByRole('heading', { name: 'Meus Pedidos' })).toBeInTheDocument();
    expect(window.location.pathname).toBe('/pedidos');
    expect(screen.getAllByRole('navigation', { name: 'Menu principal' }).length).toBeGreaterThan(0);
    expect(screen.queryByText('Em construção')).not.toBeInTheDocument();
  });

  it('/ renderiza CatalogoPage dentro do shell, não a PlaceholderPage (papel usuario: só o aviso)', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (url === '/api/auth/refresh') {
        return Promise.resolve({ ok: true, json: async () => ({ token: 'access-abc' }) });
      }
      if (url === '/api/auth/me') {
        return Promise.resolve({
          ok: true,
          json: async () => ({ id: '1', nome: 'Fulano', email: 'f@empresa.com', papel: 'usuario' }),
        });
      }
      if (typeof url === 'string' && url.startsWith('/api/produtos/catalogo')) {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            produtos: [],
            paginacao: { pagina: 1, tamanho: 24, total: 0, totalPaginas: 0 },
          }),
        });
      }
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<App />);

    expect(await screen.findByText('Nenhum produto no catálogo.')).toBeInTheDocument();
    expect(screen.getByLabelText('Catálogo de produtos')).toBeInTheDocument();
    expect(window.location.pathname).toBe('/');
    expect(screen.queryByText('Em construção')).not.toBeInTheDocument();
    expect(screen.queryByText('Cadastrar Produto')).not.toBeInTheDocument();
  });

  it('/ com papel almoxarife+ NÃO mostra mais o cadastro/abas: só busca + listagem (Story 17.1)', async () => {
    fetchMock.mockImplementation(sessaoFetch('almoxarife', () => catalogoVazio));

    render(<App />);

    expect(await screen.findByText('Nenhum produto no catálogo.')).toBeInTheDocument();
    expect(screen.getByRole('heading', { level: 1, name: 'Produtos' })).toBeInTheDocument();
    expect(screen.queryByText('Cadastrar Produto')).not.toBeInTheDocument();
    expect(screen.queryByRole('tab')).not.toBeInTheDocument();
    expect(window.location.pathname).toBe('/');
  });

  it('cookie válido: refresh + /me resolvem e o AppShell é renderizado em /', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (url === '/api/auth/refresh') {
        return Promise.resolve({ ok: true, json: async () => ({ token: 'access-abc' }) });
      }
      if (url === '/api/auth/me') {
        return Promise.resolve({
          ok: true,
          json: async () => ({ id: '1', nome: 'Fulano', email: 'f@empresa.com', papel: 'gestor' }),
        });
      }
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<App />);

    await waitFor(() =>
      expect(screen.getAllByRole('navigation', { name: 'Menu principal' })).toHaveLength(1),
    );
    expect(window.location.pathname).toBe('/');
    expect(getAccessToken()).toBe('access-abc');
    expect(screen.queryByText('Acesse sua conta do stockflow.')).not.toBeInTheDocument();
  });
});

// ---- Story 17.1: rotas próprias no lugar das abas --------------------------

const catalogoVazio = {
  ok: true,
  json: async () => ({
    produtos: [],
    paginacao: { pagina: 1, tamanho: 24, total: 0, totalPaginas: 0 },
  }),
};

// Sessão autenticada com o papel dado; `outras` responde as demais URLs (undefined = 404 seco).
function sessaoFetch(
  papel: string,
  outras: (url: string) => unknown = () => undefined,
) {
  return (url: string) => {
    if (url === '/api/auth/refresh') {
      return Promise.resolve({ ok: true, json: async () => ({ token: 'access-abc' }) });
    }
    if (url === '/api/auth/me') {
      return Promise.resolve({
        ok: true,
        json: async () => ({ id: '1', nome: 'Fulano', email: 'f@empresa.com', papel }),
      });
    }
    const resposta = outras(url);
    if (resposta) return Promise.resolve(resposta);
    if (url === '/api/categorias') return Promise.resolve({ ok: true, json: async () => ({ categorias: [] }) });
    if (url === '/api/estoques') return Promise.resolve({ ok: true, json: async () => ({ estoques: [] }) });
    if (url.startsWith('/api/movimentacoes')) {
      return Promise.resolve({ ok: true, json: async () => ({ movimentacoes: [] }) });
    }
    throw new Error(`URL inesperada: ${url}`);
  };
}

describe('<App /> — rotas próprias do menu (Story 17.1)', () => {
  const fetchMock = vi.fn();

  afterEach(() => {
    vi.unstubAllGlobals();
    clearAccessToken();
    useAuthMock.mockReset();
  });

  beforeEach(async () => {
    useAuthMock.mockImplementation(authReal.useAuth);
    vi.stubGlobal('fetch', fetchMock);
    fetchMock.mockReset();
    clearAccessToken();
    await router.navigate('/');
    window.localStorage.clear();
    conectarRealtimeMock.mockImplementation((_receber, mudar) => {
      mudar('conectado');
      return vi.fn();
    });
  });

  it.each([
    ['/produtos/novo', 'Cadastrar produto', 'Cadastrar produto'],
    ['/produtos/importar', 'Importar planilha', 'Importar planilha'],
    ['/estoques', 'Locais', 'Locais'],
    ['/estoques/lancar-saldo', 'Lançar saldo', 'Lançar saldo'],
    ['/estoques/movimentacoes', 'Movimentações', 'Movimentações'],
    ['/normalizacao', 'Inconsistências', 'Inconsistências'],
    ['/normalizacao/duplicatas', 'Duplicatas', 'Duplicatas'],
  ])('almoxarife abre %s direto: h1 "%s", item ativo "%s" no menu, sem abas', async (rota, titulo, itemMenu) => {
    fetchMock.mockImplementation(sessaoFetch('almoxarife'));
    await router.navigate(rota);

    render(<App />);

    expect(await screen.findByRole('heading', { level: 1, name: titulo })).toBeInTheDocument();
    expect(window.location.pathname).toBe(rota);
    const menu = screen.getByRole('navigation', { name: 'Menu principal' });
    expect(within(menu).getByRole('link', { name: itemMenu })).toHaveAttribute('aria-current', 'page');
    expect(screen.queryByRole('tab')).not.toBeInTheDocument();
    expect(screen.queryByText('Em construção')).not.toBeInTheDocument();
  });

  it('/pedidos/fila abre a Fila de Pedidos para almoxarife', async () => {
    fetchMock.mockImplementation(
      sessaoFetch('almoxarife', (url) =>
        url.startsWith('/api/pedidos')
          ? { ok: true, json: async () => ({ pedidos: [] }) }
          : undefined,
      ),
    );
    await router.navigate('/pedidos/fila');

    render(<App />);

    expect(await screen.findByRole('heading', { name: 'Fila de Pedidos' })).toBeInTheDocument();
    const menu = screen.getByRole('navigation', { name: 'Menu principal' });
    expect(within(menu).getByRole('link', { name: 'Fila de aprovação' })).toHaveAttribute(
      'aria-current',
      'page',
    );
  });

  it.each([
    ['/pedidos/fila', 'Você não tem acesso à Fila de aprovação.'],
    ['/estoques/movimentacoes', 'Você não tem acesso à área de Estoques.'],
    ['/estoques/lancar-saldo', 'Você não tem acesso à área de Estoques.'],
    ['/normalizacao/duplicatas', 'Você não tem acesso à área de Normalização.'],
    ['/produtos/novo', 'Você não tem acesso ao cadastro de produtos.'],
    ['/produtos/importar', 'Você não tem acesso à importação de produtos.'],
  ])('%s como usuario mostra a recusa de hoje e o menu sem os itens gated', async (rota, recusa) => {
    fetchMock.mockImplementation(sessaoFetch('usuario'));
    await router.navigate(rota);

    render(<App />);

    expect(await screen.findByText(recusa)).toBeInTheDocument();
    const menu = screen.getByRole('navigation', { name: 'Menu principal' });
    expect(within(menu).queryByRole('button', { name: 'Estoque' })).not.toBeInTheDocument();
    expect(within(menu).queryByRole('link', { name: 'Fila de aprovação' })).not.toBeInTheDocument();
  });

  it('link antigo /normalizacao?verificarDuplicatas=1 redireciona para Duplicatas e analisa uma vez', async () => {
    fetchMock.mockImplementation(
      sessaoFetch('almoxarife', (url) =>
        url === '/api/normalizacao/duplicatas'
          ? { ok: true, json: async () => ({ grupos: [] }) }
          : undefined,
      ),
    );
    await router.navigate('/normalizacao?verificarDuplicatas=1');

    render(<App />);

    expect(await screen.findByText('Nenhuma duplicata encontrada.')).toBeInTheDocument();
    expect(window.location.pathname).toBe('/normalizacao/duplicatas');
    expect(window.location.search).toBe('?verificarDuplicatas=1');
    expect(
      fetchMock.mock.calls.filter(([url]) => url === '/api/normalizacao/duplicatas'),
    ).toHaveLength(1);
  });

  it('gate de MFA: menu visível e toda rota do menu leva a /configuracoes', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (url === '/api/auth/refresh') {
        return Promise.resolve({ ok: true, json: async () => ({ token: 'access-abc' }) });
      }
      if (url === '/api/auth/me') {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            id: '1',
            nome: 'Gestora',
            email: 'g@empresa.com',
            papel: 'gestor',
            mfaHabilitado: false,
            origem: 'senha',
            empresa: { mfaObrigatorio: true },
          }),
        });
      }
      return Promise.resolve({ ok: true, json: async () => ({}) });
    });
    await router.navigate('/estoques/movimentacoes');

    render(<App />);

    const menu = await screen.findByRole('navigation', { name: 'Menu principal' });
    await waitFor(() => expect(window.location.pathname).toBe('/configuracoes'));
    expect(within(menu).getByRole('link', { name: 'Movimentações' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Meu perfil' })).toHaveAttribute('aria-current', 'page');
  });
});

import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { CatalogoPage } from './CatalogoPage';

// useAuth() fornece o papel — configurável por teste.
const authState = vi.hoisted(() => ({ papel: 'usuario' as string }));

// ScannerProdutoFab (Story 4.5) é montado por CatalogoPage — mock do wrapper
// de câmera para os testes desta página não tocarem em `@zxing/browser`.
const iniciarScannerMock = vi.hoisted(() => vi.fn());
vi.mock('@/lib/scanner/leitor', () => ({
  criarLeitorCodigo: () => ({ iniciar: iniciarScannerMock }),
}));

vi.mock('sonner', () => ({
  toast: { error: vi.fn(), success: vi.fn(), info: vi.fn() },
}));

vi.mock('@/lib/auth', () => ({
  useAuth: () => ({
    estado: 'autenticado',
    usuario: { id: '1', nome: 'Ator', email: 'ator@empresa.com', papel: authState.papel },
    definirSessao: vi.fn(),
    atualizarUsuario: vi.fn(),
    logout: vi.fn(),
  }),
}));

vi.mock('@/lib/session', () => ({
  getAccessToken: () => 'token-de-teste',
}));

beforeEach(() => {
  authState.papel = 'usuario';
  Object.defineProperty(window, 'isSecureContext', { configurable: true, value: true });
  iniciarScannerMock.mockReset();
  // Por padrão, abrir a câmera falha por falta de hardware — o cenário que
  // exercita `aoFalharLeitura` (foco de volta na busca).
  iniciarScannerMock.mockRejectedValue(
    Object.assign(new Error('no device'), { name: 'NotFoundError' }),
  );
  // CadastroProdutoSection busca categorias/estoques no mount (quando
  // montada) — resposta vazia basta para os testes desta página.
  // ImportacaoProdutosSection só monta quando a aba "Importação" é
  // selecionada (Radix Tabs desmonta o conteúdo inativo por padrão) — busca
  // GET /api/importacoes/ultima no próprio mount.
  vi.stubGlobal(
    'fetch',
    vi.fn((url: string) => {
      if (url === '/api/categorias') return Promise.resolve({ ok: true, json: async () => ({ categorias: [] }) });
      if (url === '/api/estoques') return Promise.resolve({ ok: true, json: async () => ({ estoques: [] }) });
      if (url === '/api/importacoes/ultima')
        return Promise.resolve({ ok: true, json: async () => ({ importacao: null }) });
      if (typeof url === 'string' && url.startsWith('/api/produtos/busca'))
        return Promise.resolve({ ok: true, json: async () => ({ produtos: [] }) });
      if (typeof url === 'string' && url.startsWith('/api/produtos/catalogo'))
        return Promise.resolve({
          ok: true,
          json: async () => ({
            produtos: [],
            paginacao: { pagina: 1, tamanho: 24, total: 0, totalPaginas: 0 },
          }),
        });
      throw new Error(`URL inesperada: ${url}`);
    }),
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe('CatalogoPage — gate de papel', () => {
  it('papel usuario vê a listagem do catálogo, sem o formulário de cadastro', async () => {
    authState.papel = 'usuario';
    render(<CatalogoPage />, { wrapper: MemoryRouter });

    // CatalogoListagem busca o catálogo no mount, mesmo para `usuario`.
    await screen.findByText('Nenhum produto no catálogo.');
    expect(screen.getByLabelText('Catálogo de produtos')).toBeInTheDocument();
    expect(screen.queryByText('Cadastrar Produto')).not.toBeInTheDocument();
    expect(fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/produtos/catalogo'),
      expect.anything(),
    );
    // O gate `podeCadastrar` continua fechado: SEM CadastroProdutoSection —
    // já verificado acima ('Cadastrar Produto' ausente). GET
    // /api/categorias/GET /api/estoques SÃO chamados mesmo para `usuario`
    // agora (Story 4.2): CatalogoListagem busca as duas listas para popular
    // seus próprios `<Select>` de filtro, independente do gate de cadastro.
    expect(fetch).toHaveBeenCalledWith('/api/categorias', expect.anything());
    expect(fetch).toHaveBeenCalledWith('/api/estoques', expect.anything());
  });

  it.each(['almoxarife', 'gestor', 'adm'])(
    'papel %s vê a listagem E a seção de cadastro',
    async (papel) => {
      authState.papel = papel;
      render(<CatalogoPage />, { wrapper: MemoryRouter });

      expect(screen.getByLabelText('Catálogo de produtos')).toBeInTheDocument();
      expect(await screen.findByText('Cadastrar Produto')).toBeInTheDocument();
    },
  );
});

describe('CatalogoPage — abas Cadastro/Importação (Story 3.3)', () => {
  it('almoxarife+ vê as abas "Cadastro"/"Importação", com Cadastro ativa por padrão', async () => {
    authState.papel = 'almoxarife';
    render(<CatalogoPage />, { wrapper: MemoryRouter });

    expect(await screen.findByText('Cadastrar Produto')).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Cadastro' })).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByRole('tab', { name: 'Importação' })).toHaveAttribute('aria-selected', 'false');
    // Radix Tabs desmonta o conteúdo da aba inativa por padrão — a seção de
    // Importação não deveria estar no DOM antes de a aba ser selecionada.
    expect(screen.queryByText('Importar Produtos')).not.toBeInTheDocument();
  });

  it('selecionar a aba "Importação" monta ImportacaoProdutosSection', async () => {
    authState.papel = 'almoxarife';
    const user = userEvent.setup();
    render(<CatalogoPage />, { wrapper: MemoryRouter });

    await screen.findByText('Cadastrar Produto');
    await user.click(screen.getByRole('tab', { name: 'Importação' }));

    expect(await screen.findByText('Importar Produtos')).toBeInTheDocument();
  });
});

describe('CatalogoPage — ponte busca->filtro (Story 4.2)', () => {
  it('termo digitado em BuscaCatalogo reflete em ?q= na chamada de /api/produtos/catalogo após o debounce', async () => {
    authState.papel = 'usuario';
    const user = userEvent.setup();
    render(<CatalogoPage />, { wrapper: MemoryRouter });

    await screen.findByText('Nenhum produto no catálogo.');
    (fetch as ReturnType<typeof vi.fn>).mockClear();

    await user.type(screen.getByLabelText('Buscar produtos'), 'parafuso');

    // Logo após digitar, o debounce (300ms) ainda não decorreu — nenhuma
    // chamada nova a /api/produtos/catalogo com `q=` ainda.
    expect(fetch).not.toHaveBeenCalledWith(
      expect.stringContaining('/api/produtos/catalogo?agrupar=false&pagina=1&q=parafuso'),
      expect.anything(),
    );

    await waitFor(
      () =>
        expect(fetch).toHaveBeenCalledWith(
          '/api/produtos/catalogo?agrupar=false&pagina=1&q=parafuso',
          expect.anything(),
        ),
      { timeout: 2000 },
    );

    // BuscaCatalogo continua funcionando de forma independente (suas
    // próprias até-7 sugestões, sem depender dos filtros).
    expect(fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/produtos/busca?q=parafuso'),
      expect.anything(),
    );
  });
});

describe('CatalogoPage — scanner de código (Story 4.5)', () => {
  it('monta o FAB do scanner no Catálogo', async () => {
    render(<CatalogoPage />, { wrapper: MemoryRouter });

    await screen.findByText('Nenhum produto no catálogo.');
    expect(screen.getByRole('button', { name: 'Escanear código do produto' })).toBeInTheDocument();
  });

  it('falha de leitura devolve o foco ao campo de busca por texto (aoFalharLeitura)', async () => {
    const user = userEvent.setup();
    render(<CatalogoPage />, { wrapper: MemoryRouter });

    await screen.findByText('Nenhum produto no catálogo.');
    const campoBusca = screen.getByLabelText('Buscar produtos');
    expect(campoBusca).not.toHaveFocus();

    await user.click(screen.getByRole('button', { name: 'Escanear código do produto' }));

    await waitFor(() => expect(campoBusca).toHaveFocus());
  });
});

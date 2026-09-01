import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { act, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { CatalogoListagem } from './CatalogoListagem';

vi.mock('@/lib/session', () => ({
  getAccessToken: () => 'token-de-teste',
}));

// sonner (Story 4.6, spec-4-6): mesmo padrão de mock de ScannerProdutoFab.test.tsx.
const toastErrorMock = vi.hoisted(() => vi.fn());
vi.mock('sonner', () => ({ toast: { error: toastErrorMock } }));

// renderCatalogo envolve o componente num MemoryRouter — os cards da grade
// (Story 4.4, spec-4-4) agora são `<Link>`, que exige um contexto de rota.
// `termo` (Story 4.2, spec-4-2) é opcional, default '' (nenhum filtro de
// texto). `podeExportar` (Story 4.6, spec-4-6) é opcional, default `false`
// (mesmo default do componente).
function renderCatalogo(termo?: string, podeExportar?: boolean) {
  return render(
    <MemoryRouter>
      <CatalogoListagem termo={termo} podeExportar={podeExportar} />
    </MemoryRouter>,
  );
}

const DIMENSOES_NULAS = {
  comprimento: null,
  largura: null,
  diametro: null,
  altura: null,
  espessura: null,
};

// stubMatchMedia sobrescreve `window.matchMedia` para um MediaQueryList
// controlável — `matches` inicial + um `emit()` que dispara o evento
// `change` para os listeners registrados pelo componente (o stub global de
// `src/test/setup.ts` devolve sempre `matches:false` e listeners no-op).
function stubMatchMedia(matches: boolean) {
  const listeners = new Set<(evento: MediaQueryListEvent) => void>();
  const mql = {
    matches,
    media: '(min-width: 768px)',
    onchange: null,
    addEventListener: (_tipo: string, cb: (evento: MediaQueryListEvent) => void) => {
      listeners.add(cb);
    },
    removeEventListener: (_tipo: string, cb: (evento: MediaQueryListEvent) => void) => {
      listeners.delete(cb);
    },
    addListener: () => {},
    removeListener: () => {},
    dispatchEvent: () => false,
  };
  vi.stubGlobal('matchMedia', () => mql);
  return {
    emit(proximo: boolean) {
      mql.matches = proximo;
      listeners.forEach((cb) => cb({ matches: proximo } as MediaQueryListEvent));
    },
  };
}

beforeEach(() => {
  vi.restoreAllMocks();
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe('CatalogoListagem — grade (padrão)', () => {
  it('renderiza cards e não mostra o alternador quando a viewport é < 768px', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve({
          ok: true,
          json: async () => ({
            produtos: [
              {
                id: 'p1',
                nome: 'Parafuso Sextavado',
                codigo: 'PAR-001',
                categoria: { id: 'c1', codigo: '04.001', nome: 'Construção Civil' },
                dimensoes: DIMENSOES_NULAS,
                quantidadeTotal: 5,
                disponivel: true,
              },
            ],
            paginacao: { pagina: 1, tamanho: 24, total: 1, totalPaginas: 1 },
          }),
        }),
      ),
    );

    renderCatalogo();

    expect(await screen.findByText('Parafuso Sextavado')).toBeInTheDocument();
    const codigo = screen.getByText('PAR-001');
    expect(codigo).toHaveClass('font-mono');
    expect(screen.getByText(/Construção Civil/)).toBeInTheDocument();

    // Indicador de disponibilidade: ícone + texto (nunca só cor).
    const badge = screen.getByText('Disponível');
    expect(badge).toBeInTheDocument();
    expect(badge.querySelector('svg')).not.toBeNull();

    // Card da grade (Story 4.4, spec-4-4) navega para o detalhe do Produto
    // clicado — asserção direta do destino, não só que ALGUM link existe.
    expect(screen.getByRole('link', { name: /Parafuso Sextavado/ })).toHaveAttribute(
      'href',
      '/produtos/p1',
    );

    // Alternador ausente abaixo de 768px.
    expect(screen.queryByRole('button', { name: 'Tabela' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Grade' })).not.toBeInTheDocument();

    // Catálogo de página única (totalPaginas === 1): sem controles de paginação.
    expect(
      screen.queryByRole('navigation', { name: 'Paginação do catálogo' }),
    ).not.toBeInTheDocument();

    expect(fetch).toHaveBeenCalledWith(
      '/api/produtos/catalogo?agrupar=false&pagina=1',
      expect.anything(),
    );
  });

  it('mostra "Sem estoque" com ícone quando o produto está indisponível', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve({
          ok: true,
          json: async () => ({
            produtos: [
              {
                id: 'p1',
                nome: 'Cimento CP-II',
                codigo: null,
                categoria: { id: 'c1', codigo: '04.001', nome: 'Construção Civil' },
                dimensoes: DIMENSOES_NULAS,
                quantidadeTotal: 0,
                disponivel: false,
              },
            ],
            paginacao: { pagina: 1, tamanho: 24, total: 1, totalPaginas: 1 },
          }),
        }),
      ),
    );

    renderCatalogo();

    const badge = await screen.findByText('Sem estoque');
    expect(badge.querySelector('svg')).not.toBeNull();
  });

  it('estado vazio: total 0 mostra a mensagem e nenhum controle de paginação', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve({
          ok: true,
          json: async () => ({
            produtos: [],
            paginacao: { pagina: 1, tamanho: 24, total: 0, totalPaginas: 0 },
          }),
        }),
      ),
    );

    renderCatalogo();

    expect(await screen.findByText('Nenhum produto no catálogo.')).toBeInTheDocument();
    expect(screen.queryByRole('navigation', { name: 'Paginação do catálogo' })).not.toBeInTheDocument();
  });

  it('estado de erro: resposta não-OK mostra um alerta', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve({ ok: false, status: 500, json: async () => ({}) })),
    );

    renderCatalogo();

    const alerta = await screen.findByRole('alert');
    expect(alerta).toHaveTextContent(
      'Não foi possível carregar o catálogo. Tente novamente em instantes.',
    );
  });

  it('estado de erro: falha de rede (fetch rejeitado) mostra o mesmo alerta, sem o estado vazio', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.reject(new Error('rede'))),
    );

    renderCatalogo();

    const alerta = await screen.findByRole('alert');
    expect(alerta).toHaveTextContent(
      'Não foi possível carregar o catálogo. Tente novamente em instantes.',
    );
    expect(screen.queryByText('Nenhum produto no catálogo.')).not.toBeInTheDocument();
  });

  it('estado de carregamento: "Carregando catálogo..." aparece antes de a resposta chegar', () => {
    // fetch pendente que nunca resolve — o componente fica no estado inicial
    // `carregando`.
    vi.stubGlobal('fetch', vi.fn(() => new Promise(() => {})));

    renderCatalogo();

    const status = screen.getByRole('status');
    expect(status).toHaveTextContent('Carregando catálogo...');
    expect(screen.queryByText('Nenhum produto no catálogo.')).not.toBeInTheDocument();
  });

  it('paginação: "Próxima" busca a página seguinte e "Anterior" fica desabilitada na página 1', async () => {
    const fetchMock = vi.fn((url: string) => {
      const pagina2 = String(url).includes('pagina=2');
      return Promise.resolve({
        ok: true,
        json: async () => ({
          produtos: [
            {
              id: pagina2 ? 'p2' : 'p1',
              nome: pagina2 ? 'Produto Página 2' : 'Produto Página 1',
              codigo: null,
              categoria: { id: 'c1', codigo: '04.001', nome: 'Construção Civil' },
              dimensoes: DIMENSOES_NULAS,
              quantidadeTotal: 1,
              disponivel: true,
            },
          ],
          paginacao: { pagina: pagina2 ? 2 : 1, tamanho: 24, total: 30, totalPaginas: 2 },
        }),
      });
    });
    vi.stubGlobal('fetch', fetchMock);

    const user = userEvent.setup();
    renderCatalogo();

    expect(await screen.findByText('Produto Página 1')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Anterior' })).toBeDisabled();

    await user.click(screen.getByRole('button', { name: 'Próxima' }));

    expect(await screen.findByText('Produto Página 2')).toBeInTheDocument();
    expect(screen.getByText('Página 2 de 2')).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/produtos/catalogo?agrupar=false&pagina=2',
      expect.anything(),
    );
  });
});

describe('CatalogoListagem — tabela agrupada (viewport ≥ 768px)', () => {
  function stubFetchGradeETabela(porEstoque: Array<{ estoqueId: string; estoqueNome: string; quantidade: number }>) {
    const fetchMock = vi.fn((url: string) => {
      const agrupar = String(url).includes('agrupar=true');
      if (!agrupar) {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            produtos: [
              {
                id: 'p1',
                nome: 'Parafuso',
                codigo: null,
                categoria: { id: 'c1', codigo: '04.001', nome: 'Construção Civil' },
                dimensoes: DIMENSOES_NULAS,
                quantidadeTotal: 17,
                disponivel: true,
              },
            ],
            paginacao: { pagina: 1, tamanho: 24, total: 1, totalPaginas: 1 },
          }),
        });
      }
      return Promise.resolve({
        ok: true,
        json: async () => ({
          grupos: [
            {
              chave: 'abc123',
              nome: 'Parafuso',
              dimensoes: { ...DIMENSOES_NULAS, comprimento: { valor: 20, unidade: 'mm' } },
              quantidadeTotal: porEstoque.reduce((soma, e) => soma + e.quantidade, 0),
              disponivel: porEstoque.length > 0,
              porEstoque,
            },
          ],
          paginacao: { pagina: 1, tamanho: 24, total: 1, totalPaginas: 1 },
        }),
      });
    });
    vi.stubGlobal('fetch', fetchMock);
    return fetchMock;
  }

  it('o alternador aparece e alterna para a tabela agrupada', async () => {
    stubMatchMedia(true);
    const fetchMock = stubFetchGradeETabela([
      { estoqueId: 'e1', estoqueNome: 'Almoxarifado Central', quantidade: 15 },
      { estoqueId: 'e2', estoqueNome: 'Obra Norte', quantidade: 2 },
    ]);

    const user = userEvent.setup();
    renderCatalogo();

    await screen.findByText('Parafuso');
    await user.click(screen.getByRole('button', { name: 'Tabela' }));

    // Cabeçalho da tabela + a linha do grupo.
    expect(await screen.findByRole('columnheader', { name: 'Produto' })).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/produtos/catalogo?agrupar=true&pagina=1',
      expect.anything(),
    );
  });

  it('expandir uma linha agrupada revela a quantidade por Estoque', async () => {
    stubMatchMedia(true);
    stubFetchGradeETabela([
      { estoqueId: 'e1', estoqueNome: 'Almoxarifado Central', quantidade: 15 },
      { estoqueId: 'e2', estoqueNome: 'Obra Norte', quantidade: 2 },
    ]);

    const user = userEvent.setup();
    renderCatalogo();

    await screen.findByText('Parafuso');
    await user.click(screen.getByRole('button', { name: 'Tabela' }));

    const botaoExpandir = await screen.findByRole('button', { name: /Parafuso/ });
    expect(botaoExpandir).toHaveAttribute('aria-expanded', 'false');

    await user.click(botaoExpandir);

    expect(botaoExpandir).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getByText('Almoxarifado Central')).toBeInTheDocument();
    expect(screen.getByText('Obra Norte')).toBeInTheDocument();
    expect(screen.getByText('15')).toBeInTheDocument();
  });

  it('tabela: "Próxima" busca a página seguinte no modo agrupado', async () => {
    stubMatchMedia(true);
    const fetchMock = vi.fn((url: string) => {
      const u = String(url);
      if (!u.includes('agrupar=true')) {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            produtos: [
              {
                id: 'p1',
                nome: 'Parafuso',
                codigo: null,
                categoria: { id: 'c1', codigo: '04.001', nome: 'Construção Civil' },
                dimensoes: DIMENSOES_NULAS,
                quantidadeTotal: 1,
                disponivel: true,
              },
            ],
            paginacao: { pagina: 1, tamanho: 24, total: 1, totalPaginas: 1 },
          }),
        });
      }
      const pagina2 = u.includes('pagina=2');
      return Promise.resolve({
        ok: true,
        json: async () => ({
          grupos: [
            {
              chave: pagina2 ? 'grupo-p2' : 'grupo-p1',
              nome: pagina2 ? 'Grupo Página 2' : 'Grupo Página 1',
              dimensoes: DIMENSOES_NULAS,
              quantidadeTotal: 1,
              disponivel: true,
              porEstoque: [],
            },
          ],
          paginacao: { pagina: pagina2 ? 2 : 1, tamanho: 24, total: 30, totalPaginas: 2 },
        }),
      });
    });
    vi.stubGlobal('fetch', fetchMock);

    const user = userEvent.setup();
    renderCatalogo();

    await screen.findByText('Parafuso');
    await user.click(screen.getByRole('button', { name: 'Tabela' }));

    expect(await screen.findByText('Grupo Página 1')).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Próxima' }));

    expect(await screen.findByText('Grupo Página 2')).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/produtos/catalogo?agrupar=true&pagina=2',
      expect.anything(),
    );
  });

  it('tabela: quantidade fracionária é formatada em pt-BR ("15,5")', async () => {
    stubMatchMedia(true);
    stubFetchGradeETabela([
      { estoqueId: 'e1', estoqueNome: 'Almoxarifado Central', quantidade: 15.5 },
    ]);

    const user = userEvent.setup();
    renderCatalogo();

    await screen.findByText('Parafuso');
    await user.click(screen.getByRole('button', { name: 'Tabela' }));

    // Célula de quantidade total do grupo (quantidadeTotal === 15.5).
    expect(await screen.findByText('15,5')).toBeInTheDocument();

    // E também na linha por Estoque, ao expandir.
    await user.click(await screen.findByRole('button', { name: /Parafuso/ }));
    expect(screen.getAllByText('15,5').length).toBeGreaterThanOrEqual(2);
  });

  it('linha agrupada sem porEstoque mostra o aviso ao expandir', async () => {
    stubMatchMedia(true);
    stubFetchGradeETabela([]);

    const user = userEvent.setup();
    renderCatalogo();

    await screen.findByText('Parafuso');
    await user.click(screen.getByRole('button', { name: 'Tabela' }));

    const botaoExpandir = await screen.findByRole('button', { name: /Parafuso/ });
    await user.click(botaoExpandir);

    expect(screen.getByText('Sem quantidade registrada por estoque.')).toBeInTheDocument();
  });

  it('a célula "Dimensões" resume as dimensões preenchidas (pt-BR) e mostra "—" quando todas são nulas', async () => {
    stubMatchMedia(true);
    const fetchMock = vi.fn((url: string) => {
      if (!String(url).includes('agrupar=true')) {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            produtos: [
              {
                id: 'p1',
                nome: 'Parafuso',
                codigo: null,
                categoria: { id: 'c1', codigo: '04.001', nome: 'Construção Civil' },
                dimensoes: DIMENSOES_NULAS,
                quantidadeTotal: 1,
                disponivel: true,
              },
            ],
            paginacao: { pagina: 1, tamanho: 24, total: 1, totalPaginas: 1 },
          }),
        });
      }
      return Promise.resolve({
        ok: true,
        json: async () => ({
          grupos: [
            {
              chave: 'com-dim',
              nome: 'Tubo PVC',
              dimensoes: {
                ...DIMENSOES_NULAS,
                comprimento: { valor: 6.5, unidade: 'm' },
                diametro: { valor: 10, unidade: 'cm' },
              },
              quantidadeTotal: 3,
              disponivel: true,
              porEstoque: [],
            },
            {
              chave: 'sem-dim',
              nome: 'Cimento CP-II',
              dimensoes: DIMENSOES_NULAS,
              quantidadeTotal: 2,
              disponivel: true,
              porEstoque: [],
            },
          ],
          paginacao: { pagina: 1, tamanho: 24, total: 2, totalPaginas: 1 },
        }),
      });
    });
    vi.stubGlobal('fetch', fetchMock);

    const user = userEvent.setup();
    renderCatalogo();

    await screen.findByText('Parafuso');
    await user.click(screen.getByRole('button', { name: 'Tabela' }));

    // Valor fracionário formatado em pt-BR ("6,5", não "6.5") + rótulos.
    expect(await screen.findByText('C 6,5m · ⌀ 10cm')).toBeInTheDocument();
    // Grupo sem nenhuma dimensão -> travessão.
    expect(screen.getByText('—')).toBeInTheDocument();
  });

  it('encolher a viewport abaixo de 768px volta para grade e esconde o alternador', async () => {
    const controle = stubMatchMedia(true);
    stubFetchGradeETabela([{ estoqueId: 'e1', estoqueNome: 'Almox', quantidade: 3 }]);

    const user = userEvent.setup();
    renderCatalogo();

    await screen.findByText('Parafuso');
    await user.click(screen.getByRole('button', { name: 'Tabela' }));
    await screen.findByRole('columnheader', { name: 'Produto' });

    act(() => {
      controle.emit(false);
    });

    // Alternador some e a grade volta (o card volta a aparecer).
    expect(screen.queryByRole('button', { name: 'Tabela' })).not.toBeInTheDocument();
    expect(await screen.findByText(/Construção Civil/)).toBeInTheDocument();
  });
});

describe('CatalogoListagem — filtros (Story 4.2)', () => {
  const CATEGORIAS = [
    { id: 'c1', codigo: '04.001', nome: 'Materiais Civis' },
    { id: 'c2', codigo: '04.002', nome: 'Materiais Elétricos' },
  ];
  const ESTOQUES = [
    { id: 'e1', nome: 'Canteiro A' },
    { id: 'e2', nome: 'Canteiro B' },
  ];

  // stubFetchComFiltros diferencia por URL: /api/categorias e /api/estoques
  // devolvem as listas passadas (ou rejeitam, simulando falha de rede,
  // quando o argumento correspondente é `'erro'`); qualquer outra URL (o
  // catálogo) devolve uma página vazia por padrão — o suficiente para os
  // testes desta seção, que verificam a URL da CHAMADA, não o conteúdo
  // renderizado da grade.
  function stubFetchComFiltros(opts?: {
    categorias?: typeof CATEGORIAS | 'erro';
    estoques?: typeof ESTOQUES | 'erro';
  }) {
    const categorias = opts?.categorias ?? CATEGORIAS;
    const estoques = opts?.estoques ?? ESTOQUES;
    const fetchMock = vi.fn((url: string) => {
      if (url === '/api/categorias') {
        return categorias === 'erro'
          ? Promise.reject(new Error('rede'))
          : Promise.resolve({ ok: true, json: async () => ({ categorias }) });
      }
      if (url === '/api/estoques') {
        return estoques === 'erro'
          ? Promise.reject(new Error('rede'))
          : Promise.resolve({ ok: true, json: async () => ({ estoques }) });
      }
      return Promise.resolve({
        ok: true,
        json: async () => ({
          produtos: [],
          paginacao: { pagina: 1, tamanho: 24, total: 0, totalPaginas: 0 },
        }),
      });
    });
    vi.stubGlobal('fetch', fetchMock);
    return fetchMock;
  }

  it('busca /api/categorias e /api/estoques no mount e popula os selects', async () => {
    stubFetchComFiltros();
    const user = userEvent.setup();
    renderCatalogo();

    await screen.findByText('Nenhum produto no catálogo.');

    await user.click(screen.getByRole('combobox', { name: 'Categoria' }));
    expect(await screen.findByRole('option', { name: 'Todas as categorias' })).toBeInTheDocument();
    expect(screen.getByRole('option', { name: 'Materiais Civis' })).toBeInTheDocument();
    expect(screen.getByRole('option', { name: 'Materiais Elétricos' })).toBeInTheDocument();

    await user.click(screen.getByRole('option', { name: 'Materiais Civis' }));
    await user.click(screen.getByRole('combobox', { name: 'Estoque' }));
    expect(await screen.findByRole('option', { name: 'Todos os Estoques' })).toBeInTheDocument();
    expect(screen.getByRole('option', { name: 'Canteiro A' })).toBeInTheDocument();
    expect(screen.getByRole('option', { name: 'Canteiro B' })).toBeInTheDocument();
  });

  it('selecionar uma categoria dispara o fetch com categoriaId', async () => {
    const fetchMock = stubFetchComFiltros();
    const user = userEvent.setup();
    renderCatalogo();

    await screen.findByText('Nenhum produto no catálogo.');
    fetchMock.mockClear();

    await user.click(screen.getByRole('combobox', { name: 'Categoria' }));
    await user.click(await screen.findByRole('option', { name: 'Materiais Civis' }));

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/produtos/catalogo?agrupar=false&pagina=1&categoriaId=c1',
        expect.anything(),
      ),
    );
  });

  it('selecionar um Estoque dispara o fetch com estoqueId', async () => {
    const fetchMock = stubFetchComFiltros();
    const user = userEvent.setup();
    renderCatalogo();

    await screen.findByText('Nenhum produto no catálogo.');
    fetchMock.mockClear();

    await user.click(screen.getByRole('combobox', { name: 'Estoque' }));
    await user.click(await screen.findByRole('option', { name: 'Canteiro B' }));

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/produtos/catalogo?agrupar=false&pagina=1&estoqueId=e2',
        expect.anything(),
      ),
    );
  });

  it('marcar "Com estoque disponível" dispara o fetch com comEstoque=true', async () => {
    const fetchMock = stubFetchComFiltros();
    const user = userEvent.setup();
    renderCatalogo();

    await screen.findByText('Nenhum produto no catálogo.');
    fetchMock.mockClear();

    await user.click(screen.getByRole('checkbox', { name: 'Com estoque disponível' }));

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/produtos/catalogo?agrupar=false&pagina=1&comEstoque=true',
        expect.anything(),
      ),
    );
  });

  it('categoria + Estoque + "Com estoque" + termo combinados aparecem juntos numa única chamada', async () => {
    const fetchMock = stubFetchComFiltros();
    const user = userEvent.setup();
    renderCatalogo('parafuso');

    await screen.findByText('Nenhum produto no catálogo.');
    fetchMock.mockClear();

    await user.click(screen.getByRole('combobox', { name: 'Categoria' }));
    await user.click(await screen.findByRole('option', { name: 'Materiais Civis' }));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    fetchMock.mockClear();

    await user.click(screen.getByRole('combobox', { name: 'Estoque' }));
    await user.click(await screen.findByRole('option', { name: 'Canteiro A' }));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    fetchMock.mockClear();

    await user.click(screen.getByRole('checkbox', { name: 'Com estoque disponível' }));

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/produtos/catalogo?agrupar=false&pagina=1&q=parafuso&categoriaId=c1&estoqueId=e1&comEstoque=true',
        expect.anything(),
      ),
    );
  });

  it('trocar de filtro volta a paginação para a página 1', async () => {
    const fetchMock = vi.fn((url: string) => {
      if (url === '/api/categorias') return Promise.resolve({ ok: true, json: async () => ({ categorias: CATEGORIAS }) });
      if (url === '/api/estoques') return Promise.resolve({ ok: true, json: async () => ({ estoques: ESTOQUES }) });
      const pagina2 = url.includes('pagina=2');
      return Promise.resolve({
        ok: true,
        json: async () => ({
          produtos: [
            {
              id: pagina2 ? 'p2' : 'p1',
              nome: pagina2 ? 'Produto Página 2' : 'Produto Página 1',
              codigo: null,
              categoria: { id: 'c1', codigo: '04.001', nome: 'Materiais Civis' },
              dimensoes: DIMENSOES_NULAS,
              quantidadeTotal: 1,
              disponivel: true,
            },
          ],
          paginacao: { pagina: pagina2 ? 2 : 1, tamanho: 24, total: 30, totalPaginas: 2 },
        }),
      });
    });
    vi.stubGlobal('fetch', fetchMock);

    const user = userEvent.setup();
    renderCatalogo();

    expect(await screen.findByText('Produto Página 1')).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Próxima' }));
    expect(await screen.findByText('Produto Página 2')).toBeInTheDocument();

    fetchMock.mockClear();
    await user.click(screen.getByRole('combobox', { name: 'Categoria' }));
    await user.click(await screen.findByRole('option', { name: 'Materiais Civis' }));

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/produtos/catalogo?agrupar=false&pagina=1&categoriaId=c1',
        expect.anything(),
      ),
    );
    // Nunca uma chamada intermediária com a página velha (2) + o filtro novo.
    expect(fetchMock).not.toHaveBeenCalledWith(
      expect.stringContaining('pagina=2&categoriaId=c1'),
      expect.anything(),
    );
  });

  it('falha ao carregar categorias/Estoques não impede a listagem (degrada para só a opção sentinela)', async () => {
    stubFetchComFiltros({ categorias: 'erro', estoques: 'erro' });
    const user = userEvent.setup();
    renderCatalogo();

    expect(await screen.findByText('Nenhum produto no catálogo.')).toBeInTheDocument();
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();

    await user.click(screen.getByRole('combobox', { name: 'Categoria' }));
    expect(await screen.findByRole('option', { name: 'Todas as categorias' })).toBeInTheDocument();
    expect(screen.queryByRole('option', { name: 'Materiais Civis' })).not.toBeInTheDocument();
  });

  // Degradação é POR CAMPO, não tudo-ou-nada: falha só em /api/categorias
  // não pode derrubar também o Select de Estoque (achado do Blind Hunter na
  // revisão desta story — as duas buscas não podem usar um único
  // Promise.all/try-catch combinado).
  it('falha só em categorias não impede o Select de Estoque de popular normalmente', async () => {
    stubFetchComFiltros({ categorias: 'erro' });
    const user = userEvent.setup();
    renderCatalogo();

    expect(await screen.findByText('Nenhum produto no catálogo.')).toBeInTheDocument();

    await user.click(screen.getByRole('combobox', { name: 'Categoria' }));
    expect(await screen.findByRole('option', { name: 'Todas as categorias' })).toBeInTheDocument();
    expect(screen.queryByRole('option', { name: 'Materiais Civis' })).not.toBeInTheDocument();
    await user.keyboard('{Escape}');

    await user.click(screen.getByRole('combobox', { name: 'Estoque' }));
    expect(await screen.findByRole('option', { name: 'Canteiro A' })).toBeInTheDocument();
    expect(screen.getByRole('option', { name: 'Canteiro B' })).toBeInTheDocument();
  });

  it('falha só em Estoques não impede o Select de categoria de popular normalmente', async () => {
    stubFetchComFiltros({ estoques: 'erro' });
    const user = userEvent.setup();
    renderCatalogo();

    expect(await screen.findByText('Nenhum produto no catálogo.')).toBeInTheDocument();

    await user.click(screen.getByRole('combobox', { name: 'Categoria' }));
    expect(await screen.findByRole('option', { name: 'Materiais Civis' })).toBeInTheDocument();
    expect(screen.getByRole('option', { name: 'Materiais Elétricos' })).toBeInTheDocument();
    await user.keyboard('{Escape}');

    await user.click(screen.getByRole('combobox', { name: 'Estoque' }));
    expect(await screen.findByRole('option', { name: 'Todos os Estoques' })).toBeInTheDocument();
    expect(screen.queryByRole('option', { name: 'Canteiro A' })).not.toBeInTheDocument();
  });

  // NFR de usabilidade em campo (alvo de toque mínimo 48px, mesmo padrão de
  // asserção já usado na Story 4.1) nos 3 novos controles de filtro.
  it('os 3 controles de filtro têm a classe de alvo de toque mínimo', async () => {
    stubFetchComFiltros();
    renderCatalogo();

    await screen.findByText('Nenhum produto no catálogo.');

    expect(screen.getByRole('combobox', { name: 'Categoria' })).toHaveClass('min-h-touch-target-min');
    expect(screen.getByRole('combobox', { name: 'Estoque' })).toHaveClass('min-h-touch-target-min');
    expect(screen.getByRole('checkbox', { name: 'Com estoque disponível' }).closest('div')).toHaveClass(
      'min-h-touch-target-min',
    );
  });

  it('termo (prop) mudando dispara refetch com q e volta a paginação para 1', async () => {
    const fetchMock = vi.fn((url: string) => {
      if (url === '/api/categorias') return Promise.resolve({ ok: true, json: async () => ({ categorias: CATEGORIAS }) });
      if (url === '/api/estoques') return Promise.resolve({ ok: true, json: async () => ({ estoques: ESTOQUES }) });
      const pagina2 = url.includes('pagina=2');
      return Promise.resolve({
        ok: true,
        json: async () => ({
          produtos: [
            {
              id: pagina2 ? 'p2' : 'p1',
              nome: pagina2 ? 'Produto Página 2' : 'Produto Página 1',
              codigo: null,
              categoria: { id: 'c1', codigo: '04.001', nome: 'Materiais Civis' },
              dimensoes: DIMENSOES_NULAS,
              quantidadeTotal: 1,
              disponivel: true,
            },
          ],
          paginacao: { pagina: pagina2 ? 2 : 1, tamanho: 24, total: 30, totalPaginas: 2 },
        }),
      });
    });
    vi.stubGlobal('fetch', fetchMock);

    const user = userEvent.setup();
    const { rerender } = renderCatalogo('');

    expect(await screen.findByText('Produto Página 1')).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Próxima' }));
    expect(await screen.findByText('Produto Página 2')).toBeInTheDocument();

    fetchMock.mockClear();
    rerender(
      <MemoryRouter>
        <CatalogoListagem termo="parafuso" />
      </MemoryRouter>,
    );

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/produtos/catalogo?agrupar=false&pagina=1&q=parafuso',
        expect.anything(),
      ),
    );
    // Nunca uma chamada intermediária com a página velha (2) + o termo novo.
    expect(fetchMock).not.toHaveBeenCalledWith(
      expect.stringContaining('pagina=2&q=parafuso'),
      expect.anything(),
    );
  });
});

describe('CatalogoListagem — exportação (Story 4.6)', () => {
  // jsdom não implementa URL.createObjectURL/revokeObjectURL (mesmo padrão
  // de CadastroProdutoSection.test.tsx/ProdutoDetalhePage.test.tsx).
  beforeEach(() => {
    URL.createObjectURL = vi.fn(() => 'blob:catalogo-teste');
    URL.revokeObjectURL = vi.fn();
  });

  function stubFetchTabelaEExportar(opts?: { exportarOk?: boolean; exportarRejeita?: boolean }) {
    const blob = new Blob(['conteudo-xlsx'], {
      type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
    });
    const fetchMock = vi.fn((url: string) => {
      const u = String(url);
      if (u.startsWith('/api/produtos/catalogo/exportar')) {
        if (opts?.exportarRejeita) {
          return Promise.reject(new Error('rede'));
        }
        return Promise.resolve({ ok: opts?.exportarOk ?? true, blob: async () => blob });
      }
      if (u.includes('agrupar=true')) {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            grupos: [
              {
                chave: 'g1',
                nome: 'Parafuso',
                dimensoes: DIMENSOES_NULAS,
                quantidadeTotal: 5,
                disponivel: true,
                porEstoque: [],
              },
            ],
            paginacao: { pagina: 1, tamanho: 24, total: 1, totalPaginas: 1 },
          }),
        });
      }
      return Promise.resolve({
        ok: true,
        json: async () => ({
          produtos: [
            {
              id: 'p1',
              nome: 'Parafuso',
              codigo: null,
              categoria: { id: 'c1', codigo: '04.001', nome: 'Construção Civil' },
              dimensoes: DIMENSOES_NULAS,
              quantidadeTotal: 5,
              disponivel: true,
            },
          ],
          paginacao: { pagina: 1, tamanho: 24, total: 1, totalPaginas: 1 },
        }),
      });
    });
    vi.stubGlobal('fetch', fetchMock);
    return fetchMock;
  }

  it('botão "Exportar" não aparece no modo grade, mesmo com podeExportar=true', async () => {
    stubMatchMedia(true);
    stubFetchTabelaEExportar();

    renderCatalogo(undefined, true);

    await screen.findByText('Parafuso');
    expect(screen.queryByRole('button', { name: 'Exportar' })).not.toBeInTheDocument();
  });

  it('botão "Exportar" não aparece no modo tabela quando podeExportar=false', async () => {
    stubMatchMedia(true);
    stubFetchTabelaEExportar();
    const user = userEvent.setup();

    renderCatalogo(undefined, false);

    await screen.findByText('Parafuso');
    await user.click(screen.getByRole('button', { name: 'Tabela' }));
    await screen.findByRole('columnheader', { name: 'Produto' });

    expect(screen.queryByRole('button', { name: 'Exportar' })).not.toBeInTheDocument();
  });

  it('botão "Exportar" aparece no modo tabela quando podeExportar=true e o clique baixa o arquivo com os filtros ativos', async () => {
    stubMatchMedia(true);
    const fetchMock = stubFetchTabelaEExportar();
    const cliqueSpy = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {});
    const user = userEvent.setup();

    renderCatalogo('parafuso', true);

    await screen.findByText('Parafuso');
    await user.click(screen.getByRole('button', { name: 'Tabela' }));
    await screen.findByRole('columnheader', { name: 'Produto' });

    const botaoExportar = screen.getByRole('button', { name: 'Exportar' });
    expect(botaoExportar).toHaveClass('min-h-touch-target-min');

    await user.click(botaoExportar);

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/produtos/catalogo/exportar?q=parafuso',
        expect.anything(),
      ),
    );
    await waitFor(() => expect(cliqueSpy).toHaveBeenCalled());
    expect(URL.createObjectURL).toHaveBeenCalled();
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:catalogo-teste');
    // Sem pagina/agrupar na query de exportação (Never, spec-4-6) — só os
    // filtros ativos (aqui, `q=parafuso`, já coberto pela asserção acima).
    expect(botaoExportar).not.toHaveTextContent('Exportando...');

    cliqueSpy.mockRestore();
  });

  it('estado "Exportando..." desabilita o botão enquanto a requisição está em voo', async () => {
    stubMatchMedia(true);
    let resolverExportacao: (() => void) | undefined;
    const fetchMock = vi.fn((url: string) => {
      const u = String(url);
      if (u.startsWith('/api/produtos/catalogo/exportar')) {
        return new Promise((resolve) => {
          resolverExportacao = () =>
            resolve({
              ok: true,
              blob: async () => new Blob(['x'], { type: 'application/octet-stream' }),
            });
        });
      }
      if (u.includes('agrupar=true')) {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            grupos: [
              {
                chave: 'g1',
                nome: 'Parafuso',
                dimensoes: DIMENSOES_NULAS,
                quantidadeTotal: 5,
                disponivel: true,
                porEstoque: [],
              },
            ],
            paginacao: { pagina: 1, tamanho: 24, total: 1, totalPaginas: 1 },
          }),
        });
      }
      return Promise.resolve({
        ok: true,
        json: async () => ({
          produtos: [
            {
              id: 'p1',
              nome: 'Parafuso',
              codigo: null,
              categoria: { id: 'c1', codigo: '04.001', nome: 'Construção Civil' },
              dimensoes: DIMENSOES_NULAS,
              quantidadeTotal: 5,
              disponivel: true,
            },
          ],
          paginacao: { pagina: 1, tamanho: 24, total: 1, totalPaginas: 1 },
        }),
      });
    });
    vi.stubGlobal('fetch', fetchMock);
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {});
    const user = userEvent.setup();

    renderCatalogo(undefined, true);

    await screen.findByText('Parafuso');
    await user.click(screen.getByRole('button', { name: 'Tabela' }));
    await screen.findByRole('columnheader', { name: 'Produto' });

    const botaoExportar = screen.getByRole('button', { name: 'Exportar' });
    await user.click(botaoExportar);

    await waitFor(() => expect(screen.getByRole('button', { name: 'Exportando...' })).toBeDisabled());

    resolverExportacao?.();

    await waitFor(() => expect(screen.getByRole('button', { name: 'Exportar' })).not.toBeDisabled());
  });

  it('falha na exportação (resposta não-OK) mostra toast.error', async () => {
    stubMatchMedia(true);
    stubFetchTabelaEExportar({ exportarOk: false });
    const user = userEvent.setup();

    renderCatalogo(undefined, true);

    await screen.findByText('Parafuso');
    await user.click(screen.getByRole('button', { name: 'Tabela' }));
    await screen.findByRole('columnheader', { name: 'Produto' });

    await user.click(screen.getByRole('button', { name: 'Exportar' }));

    await waitFor(() =>
      expect(toastErrorMock).toHaveBeenCalledWith(
        'Não foi possível exportar o catálogo. Tente novamente em instantes.',
      ),
    );
  });

  it('falha de rede na exportação também mostra toast.error', async () => {
    stubMatchMedia(true);
    stubFetchTabelaEExportar({ exportarRejeita: true });
    const user = userEvent.setup();

    renderCatalogo(undefined, true);

    await screen.findByText('Parafuso');
    await user.click(screen.getByRole('button', { name: 'Tabela' }));
    await screen.findByRole('columnheader', { name: 'Produto' });

    await user.click(screen.getByRole('button', { name: 'Exportar' }));

    await waitFor(() =>
      expect(toastErrorMock).toHaveBeenCalledWith(
        'Não foi possível exportar o catálogo. Tente novamente em instantes.',
      ),
    );
  });
});

import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { act, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { CatalogoListagem } from './CatalogoListagem';

vi.mock('@/lib/session', () => ({
  getAccessToken: () => 'token-de-teste',
}));

// renderCatalogo envolve o componente num MemoryRouter — os cards da grade
// (Story 4.4, spec-4-4) agora são `<Link>`, que exige um contexto de rota.
function renderCatalogo() {
  return render(
    <MemoryRouter>
      <CatalogoListagem />
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

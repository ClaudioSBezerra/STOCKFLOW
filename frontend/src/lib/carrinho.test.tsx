import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, act } from '@testing-library/react';
import { CarrinhoProvider, useCarrinho } from './carrinho';

vi.mock('@/lib/session', () => ({
  getAccessToken: () => 'token-de-teste',
}));

// useAuth() decide QUANDO CarrinhoProvider busca o carrinho pela primeira
// vez — mock com estado configurável por teste, molde de AppShell.test.tsx.
const authState = vi.hoisted(() => ({ estado: 'autenticado' as string }));
vi.mock('@/lib/auth', () => ({
  useAuth: () => ({ estado: authState.estado }),
}));

const toastInfo = vi.hoisted(() => vi.fn());
vi.mock('sonner', () => ({ toast: { info: toastInfo, success: vi.fn(), error: vi.fn() } }));

function Sonda() {
  const { itens, count, carregando, erro, refresh, adicionarItem, removerItem } = useCarrinho();
  return (
    <div>
      <span data-testid="count">{count}</span>
      <span data-testid="carregando">{String(carregando)}</span>
      <span data-testid="erro">{String(erro)}</span>
      <ul>
        {itens.map((item) => (
          <li key={`${item.produtoId}:${item.estoqueId}`}>{item.produtoNome}</li>
        ))}
      </ul>
      <button type="button" onClick={() => void refresh()}>
        refresh
      </button>
      <button type="button" onClick={() => void adicionarItem('p1', 'e1', 2)}>
        adicionar
      </button>
      <button type="button" onClick={() => void removerItem('p1', 'e1')}>
        remover
      </button>
    </div>
  );
}

function renderProvider() {
  return render(
    <CarrinhoProvider>
      <Sonda />
    </CarrinhoProvider>,
  );
}

const fetchMock = vi.fn();

function jsonOk(body: unknown) {
  return Promise.resolve({ ok: true, status: 200, json: async () => body });
}

beforeEach(() => {
  authState.estado = 'autenticado';
  vi.stubGlobal('fetch', fetchMock);
  fetchMock.mockReset();
  toastInfo.mockClear();
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('CarrinhoProvider — busca inicial', () => {
  it('busca GET /api/carrinho quando a sessão está autenticada e popula itens/count', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (url === '/api/carrinho') {
        return jsonOk({
          itens: [
            { produtoId: 'p1', produtoNome: 'Cabo Flexível', estoqueId: 'e1', estoqueNome: 'Central', quantidade: 3 },
          ],
          removidos: [],
        });
      }
      throw new Error(`URL inesperada: ${url}`);
    });

    renderProvider();

    await waitFor(() => expect(screen.getByTestId('count')).toHaveTextContent('1'));
    expect(screen.getByText('Cabo Flexível')).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith('/api/carrinho', expect.objectContaining({ headers: expect.anything() }));
  });

  it('não busca nada enquanto a sessão está "carregando"', () => {
    authState.estado = 'carregando';
    fetchMock.mockImplementation((url: string) => {
      throw new Error(`não deveria chamar ${url}`);
    });

    renderProvider();

    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('sessão "anonimo": não busca e mantém itens/count zerados', () => {
    authState.estado = 'anonimo';
    fetchMock.mockImplementation((url: string) => {
      throw new Error(`não deveria chamar ${url}`);
    });

    renderProvider();

    expect(fetchMock).not.toHaveBeenCalled();
    expect(screen.getByTestId('count')).toHaveTextContent('0');
  });

  it('falha de rede no refresh não lança (crash), mas marca erro=true — itens continuam vazios', async () => {
    fetchMock.mockRejectedValue(new Error('network down'));

    renderProvider();

    await waitFor(() => expect(screen.getByTestId('carregando')).toHaveTextContent('false'));
    expect(screen.getByTestId('count')).toHaveTextContent('0');
    expect(screen.getByTestId('erro')).toHaveTextContent('true');
  });

  it('resposta não-ok de GET /api/carrinho marca erro=true (distinto de carrinho vazio)', async () => {
    fetchMock.mockResolvedValue({ ok: false, status: 500, json: async () => ({}) });

    renderProvider();

    await waitFor(() => expect(screen.getByTestId('carregando')).toHaveTextContent('false'));
    expect(screen.getByTestId('erro')).toHaveTextContent('true');
    expect(screen.getByTestId('count')).toHaveTextContent('0');
  });

  it('um refresh() com sucesso após uma falha anterior volta erro para false', async () => {
    let falha = true;
    fetchMock.mockImplementation(() => {
      if (falha) {
        return Promise.resolve({ ok: false, status: 500, json: async () => ({}) });
      }
      return jsonOk({ itens: [], removidos: [] });
    });

    renderProvider();
    await waitFor(() => expect(screen.getByTestId('erro')).toHaveTextContent('true'));

    falha = false;
    const user = await import('@testing-library/user-event').then((m) => m.default.setup());
    await act(async () => {
      await user.click(screen.getByRole('button', { name: 'refresh' }));
    });

    await waitFor(() => expect(screen.getByTestId('erro')).toHaveTextContent('false'));
  });
});

describe('CarrinhoProvider — proteção contra corrida entre refresh() sobrepostos', () => {
  // Cenário real (estação compartilhada de almoxarifado, spec-7-1 patch #9):
  // Usuário A tem um refresh() em voo; A faz logout e B faz login na MESMA
  // aba antes da resposta de A chegar. O refresh() de B (disparado pela
  // troca de sessão) é mais rápido e resolve PRIMEIRO; a resposta antiga de
  // A só chega depois. O estado final precisa refletir os dados de B (o
  // refresh() mais recente), nunca os de A, mesmo A tendo "começado" antes.
  it('descarta a resposta do refresh() mais antigo quando ele resolve DEPOIS de um refresh() mais novo', async () => {
    let resolverPrimeiraChamada!: (value: unknown) => void;
    const primeiraChamada = new Promise((resolve) => {
      resolverPrimeiraChamada = resolve;
    });
    let numeroChamada = 0;

    fetchMock.mockImplementation(() => {
      numeroChamada += 1;
      if (numeroChamada === 1) {
        // Primeira chamada (usuário A) fica pendurada até o teste liberar.
        return primeiraChamada.then(() =>
          jsonOk({
            itens: [{ produtoId: 'a', produtoNome: 'Item de A (obsoleto)', estoqueId: 'ea', estoqueNome: 'X', quantidade: 1 }],
            removidos: [],
          }),
        );
      }
      // Segunda chamada (usuário B) resolve imediatamente.
      return jsonOk({
        itens: [{ produtoId: 'b', produtoNome: 'Item de B (atual)', estoqueId: 'eb', estoqueNome: 'Y', quantidade: 2 }],
        removidos: [],
      });
    });

    renderProvider();
    // A primeira chamada (busca inicial, disparada pelo efeito de montagem)
    // já está em voo e pendurada — dispara a segunda chamada (refresh() de
    // B) ANTES de liberar a primeira.
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));

    const user = await import('@testing-library/user-event').then((m) => m.default.setup());
    await act(async () => {
      await user.click(screen.getByRole('button', { name: 'refresh' }));
    });
    await waitFor(() => expect(screen.getByText('Item de B (atual)')).toBeInTheDocument());

    // Só agora a resposta obsoleta da primeira chamada (A) é liberada — ela
    // resolve DEPOIS que B já aplicou seu resultado.
    await act(async () => {
      resolverPrimeiraChamada(undefined);
    });

    // O estado final continua sendo o de B — a resposta obsoleta de A nunca
    // sobrescreve, mesmo chegando por último.
    expect(screen.getByText('Item de B (atual)')).toBeInTheDocument();
    expect(screen.queryByText('Item de A (obsoleto)')).not.toBeInTheDocument();
    expect(screen.getByTestId('count')).toHaveTextContent('1');
  });
});

describe('CarrinhoProvider — limpeza preguiçosa (AC2, spec-7-1)', () => {
  it('cada item em "removidos" dispara um toast.info com o motivo', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (url === '/api/carrinho') {
        return jsonOk({
          itens: [],
          removidos: [
            { produtoId: 'p1', produtoNome: 'Cabo Mesclado', estoqueId: 'e1', estoqueNome: null, motivo: 'produto_removido' },
            { produtoId: 'p2', produtoNome: 'Cabo Sem Estoque', estoqueId: 'e2', estoqueNome: null, motivo: 'estoque_excluido' },
          ],
        });
      }
      throw new Error(`URL inesperada: ${url}`);
    });

    renderProvider();

    await waitFor(() => expect(toastInfo).toHaveBeenCalledTimes(2));
    expect(toastInfo).toHaveBeenCalledWith('"Cabo Mesclado" foi removido do carrinho: o produto não existe mais.');
    expect(toastInfo).toHaveBeenCalledWith(
      '"Cabo Sem Estoque" foi removido do carrinho: o estoque selecionado foi excluído.',
    );
  });
});

describe('CarrinhoProvider — adicionarItem', () => {
  it('POST /api/carrinho/itens com o corpo correto e refaz o refresh em caso de sucesso', async () => {
    fetchMock.mockImplementation((url: string, init?: RequestInit) => {
      if (url === '/api/carrinho' && (!init || !init.method)) {
        return jsonOk({ itens: [], removidos: [] });
      }
      if (url === '/api/carrinho/itens' && init?.method === 'POST') {
        return Promise.resolve({ ok: true, status: 201, json: async () => ({ item: {} }) });
      }
      throw new Error(`URL inesperada: ${url} (${init?.method ?? 'GET'})`);
    });

    const user = await import('@testing-library/user-event').then((m) => m.default.setup());
    renderProvider();
    await waitFor(() => expect(screen.getByTestId('carregando')).toHaveTextContent('false'));

    const chamadasAntes = fetchMock.mock.calls.filter(([u]) => u === '/api/carrinho').length;
    await act(async () => {
      await user.click(screen.getByRole('button', { name: 'adicionar' }));
    });

    const postCall = fetchMock.mock.calls.find(([u]) => u === '/api/carrinho/itens');
    expect(postCall?.[1]?.method).toBe('POST');
    expect(JSON.parse(postCall?.[1]?.body as string)).toEqual({
      produtoId: 'p1',
      estoqueId: 'e1',
      quantidade: 2,
    });
    const chamadasDepois = fetchMock.mock.calls.filter(([u]) => u === '/api/carrinho').length;
    expect(chamadasDepois).toBeGreaterThan(chamadasAntes);
  });

  it('resposta não-ok devolve {ok:false, mensagem} do envelope do servidor, sem refresh extra', async () => {
    fetchMock.mockImplementation((url: string, init?: RequestInit) => {
      if (url === '/api/carrinho' && (!init || !init.method)) {
        return jsonOk({ itens: [], removidos: [] });
      }
      if (url === '/api/carrinho/itens' && init?.method === 'POST') {
        return Promise.resolve({
          ok: false,
          status: 409,
          json: async () => ({ error: { code: 'CONFLICT', message: 'sem saldo' } }),
        });
      }
      throw new Error(`URL inesperada: ${url} (${init?.method ?? 'GET'})`);
    });

    let resultado: unknown;
    function SondaResultado() {
      const { adicionarItem } = useCarrinho();
      return (
        <button
          type="button"
          onClick={() => {
            void adicionarItem('p1', 'e1', 2).then((r) => {
              resultado = r;
            });
          }}
        >
          adicionar
        </button>
      );
    }
    render(
      <CarrinhoProvider>
        <SondaResultado />
      </CarrinhoProvider>,
    );
    await waitFor(() => expect(fetchMock.mock.calls.some(([u]) => u === '/api/carrinho')).toBe(true));

    const user = await import('@testing-library/user-event').then((m) => m.default.setup());
    await act(async () => {
      await user.click(screen.getByRole('button', { name: 'adicionar' }));
    });

    expect(resultado).toEqual({ ok: false, mensagem: 'sem saldo' });
  });
});

describe('CarrinhoProvider — removerItem', () => {
  it('DELETE /api/carrinho/itens/{produtoId}/{estoqueId} e refaz o refresh em caso de sucesso', async () => {
    fetchMock.mockImplementation((url: string, init?: RequestInit) => {
      if (url === '/api/carrinho' && (!init || !init.method)) {
        return jsonOk({ itens: [], removidos: [] });
      }
      if (url === '/api/carrinho/itens/p1/e1' && init?.method === 'DELETE') {
        // 204 real: o Fetch API sempre marca `ok:true` para 204 — um mock
        // `{ok:false, status:204}` nunca ocorre em produção e não
        // exercitaria o ramo `res.ok` do `if (!(res.status === 204 ||
        // res.ok))` do código.
        return Promise.resolve({ ok: true, status: 204, json: async () => ({}) });
      }
      throw new Error(`URL inesperada: ${url} (${init?.method ?? 'GET'})`);
    });

    const user = await import('@testing-library/user-event').then((m) => m.default.setup());
    renderProvider();
    await waitFor(() => expect(screen.getByTestId('carregando')).toHaveTextContent('false'));
    const chamadasAntes = fetchMock.mock.calls.filter(([u]) => u === '/api/carrinho').length;

    await act(async () => {
      await user.click(screen.getByRole('button', { name: 'remover' }));
    });

    const deleteCall = fetchMock.mock.calls.find(([u]) => u === '/api/carrinho/itens/p1/e1');
    expect(deleteCall?.[1]?.method).toBe('DELETE');
    const chamadasDepois = fetchMock.mock.calls.filter(([u]) => u === '/api/carrinho').length;
    expect(chamadasDepois).toBeGreaterThan(chamadasAntes);
  });

  it('resposta não-2xx/não-204 devolve {ok:false, mensagem} do envelope do servidor, sem refresh extra', async () => {
    fetchMock.mockImplementation((url: string, init?: RequestInit) => {
      if (url === '/api/carrinho' && (!init || !init.method)) {
        return jsonOk({ itens: [], removidos: [] });
      }
      if (url === '/api/carrinho/itens/p1/e1' && init?.method === 'DELETE') {
        return Promise.resolve({
          ok: false,
          status: 404,
          json: async () => ({ error: { code: 'NOT_FOUND', message: 'item não encontrado no carrinho' } }),
        });
      }
      throw new Error(`URL inesperada: ${url} (${init?.method ?? 'GET'})`);
    });

    let resultado: unknown;
    function SondaResultado() {
      const { removerItem } = useCarrinho();
      return (
        <button
          type="button"
          onClick={() => {
            void removerItem('p1', 'e1').then((r) => {
              resultado = r;
            });
          }}
        >
          remover
        </button>
      );
    }
    render(
      <CarrinhoProvider>
        <SondaResultado />
      </CarrinhoProvider>,
    );
    await waitFor(() => expect(fetchMock.mock.calls.some(([u]) => u === '/api/carrinho')).toBe(true));

    const user = await import('@testing-library/user-event').then((m) => m.default.setup());
    await act(async () => {
      await user.click(screen.getByRole('button', { name: 'remover' }));
    });

    expect(resultado).toEqual({ ok: false, mensagem: 'item não encontrado no carrinho' });
  });
});

describe('useCarrinho fora do Provider', () => {
  it('lança com uma mensagem clara', () => {
    const spy = vi.spyOn(console, 'error').mockImplementation(() => {});
    expect(() => render(<Sonda />)).toThrow(/CarrinhoProvider/);
    spy.mockRestore();
  });
});

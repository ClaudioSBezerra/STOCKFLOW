import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { BuscaCatalogo } from './BuscaCatalogo';

vi.mock('@/lib/session', () => ({
  getAccessToken: () => 'token-de-teste',
}));

// renderBusca envolve o componente num MemoryRouter — os resultados (Story
// 4.4, spec-4-4) agora são `<Link>`, que exige um contexto de rota.
function renderBusca() {
  return render(
    <MemoryRouter>
      <BuscaCatalogo />
    </MemoryRouter>,
  );
}

function queryDaUrl(url: string): string | null {
  return new URL(url, 'http://localhost').searchParams.get('q');
}

function respostaOk(produtos: unknown[]) {
  return Promise.resolve({ ok: true, json: async () => ({ produtos }) });
}

const produtoParafuso = {
  id: 'p1',
  nome: 'Parafuso Sextavado M8',
  codigo: 'PAR-001',
  categoria: { id: 'c1', codigo: '04.001', nome: 'Materiais Civis' },
};

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe('BuscaCatalogo — termo vazio', () => {
  it('não dispara requisição e não mostra lista/mensagem enquanto o termo está vazio', () => {
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);

    renderBusca();

    expect(fetch).not.toHaveBeenCalled();
    expect(screen.queryByText(/Nenhum produto encontrado/)).not.toBeInTheDocument();
  });

  it('digitar só espaços não dispara requisição (termo trimado fica vazio)', async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);

    const user = userEvent.setup();
    renderBusca();

    await user.type(screen.getByLabelText('Buscar produtos'), '   ');

    // Dá tempo bastante para o debounce (300ms) ter disparado, se o termo
    // trimado (vazio) estivesse sendo tratado como não-vazio.
    await new Promise((r) => setTimeout(r, 400));

    expect(fetch).not.toHaveBeenCalled();
  });

  it('campo de busca usa type="search" (botão nativo de limpar, teclado/IME mobile apropriado)', () => {
    renderBusca();

    expect(screen.getByLabelText('Buscar produtos')).toHaveAttribute('type', 'search');
  });

  it('desmontar antes do debounce (300ms) disparar nunca chama fetch', async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);

    const user = userEvent.setup();
    const { unmount } = renderBusca();

    await user.type(screen.getByLabelText('Buscar produtos'), 'parafuso');
    unmount();

    // Dá tempo bastante para o debounce (300ms) ter disparado, se o cleanup
    // não tivesse cancelado o timer pendente.
    await new Promise((r) => setTimeout(r, 400));

    expect(fetch).not.toHaveBeenCalled();
  });
});

describe('BuscaCatalogo — debounce e resultados', () => {
  beforeEach(() => {
    vi.stubGlobal(
      'fetch',
      vi.fn((url: string) => {
        const termo = queryDaUrl(url);
        if (termo === 'parafuso') return respostaOk([produtoParafuso]);
        return respostaOk([]);
      }),
    );
  });

  it('só busca depois do debounce (300ms), não a cada tecla', async () => {
    const user = userEvent.setup();
    renderBusca();

    await user.type(screen.getByLabelText('Buscar produtos'), 'parafuso');

    // Logo após digitar, nenhuma chamada ainda deveria ter ocorrido — o
    // debounce de 300ms não decorreu.
    expect(fetch).not.toHaveBeenCalled();

    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(1), { timeout: 2000 });
    expect(fetch).toHaveBeenCalledWith(
      '/api/produtos/busca?q=parafuso',
      expect.objectContaining({ headers: { Authorization: 'Bearer token-de-teste' } }),
    );
  });

  it('mostra nome, código (JetBrains Mono) e categoria de cada resultado', async () => {
    const user = userEvent.setup();
    renderBusca();

    await user.type(screen.getByLabelText('Buscar produtos'), 'parafuso');

    expect(await screen.findByText('Parafuso Sextavado M8')).toBeInTheDocument();
    const codigo = await screen.findByText('PAR-001');
    expect(codigo).toHaveClass('font-mono');
    expect(screen.getByText(/Materiais Civis/)).toBeInTheDocument();

    // Alvo de toque mínimo 48px (NFR de usabilidade em campo) no campo de
    // busca — classe utilitária `min-h-touch-target-min`.
    expect(screen.getByLabelText('Buscar produtos')).toHaveClass('min-h-touch-target-min');

    // Resultado (Story 4.4, spec-4-4) navega para o detalhe do Produto
    // clicado — asserção direta do destino, não só que ALGUM link existe.
    expect(screen.getByRole('link', { name: /Parafuso Sextavado M8/ })).toHaveAttribute(
      'href',
      '/produtos/p1',
    );
  });

  it('mensagem exata de "nenhum produto encontrado" com o termo buscado, após o debounce resolver', async () => {
    const user = userEvent.setup();
    renderBusca();

    await user.type(screen.getByLabelText('Buscar produtos'), 'xyzxyz-inexistente');

    expect(
      await screen.findByText("Nenhum produto encontrado para 'xyzxyz-inexistente'."),
    ).toBeInTheDocument();
  });
});

describe('BuscaCatalogo — descarte de resposta obsoleta', () => {
  it('busca "a" (lenta) seguida de "ab" (rápida): lista final reflete "ab", resposta tardia de "a" é ignorada', async () => {
    let resolverA: ((value: unknown) => void) | undefined;
    const pendingA = new Promise((resolve) => {
      resolverA = resolve;
    });

    const fetchMock = vi.fn((url: string) => {
      const termo = queryDaUrl(url);
      if (termo === 'a') return pendingA;
      if (termo === 'ab') return respostaOk([produtoParafuso]);
      throw new Error(`termo inesperado: ${termo}`);
    });
    vi.stubGlobal('fetch', fetchMock);

    const user = userEvent.setup();
    renderBusca();
    const input = screen.getByLabelText('Buscar produtos');

    // "a" dispara e fica pendurada (resolverA ainda não foi chamado).
    await user.type(input, 'a');
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(1), { timeout: 2000 });

    // Usuário continua digitando antes de "a" responder -> novo termo "ab".
    await user.type(input, 'b');
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(2), { timeout: 2000 });

    // "ab" (rápida) já resolveu — a lista mostra o resultado dela.
    expect(await screen.findByText('Parafuso Sextavado M8')).toBeInTheDocument();

    // "a" (lenta) finalmente resolve, tarde, com uma lista vazia (diferente
    // de "ab") — se não fosse descartada, o "Nenhum produto encontrado..."
    // apareceria por cima do resultado de "ab" já mostrado.
    resolverA?.(await respostaOk([]));

    // Dá tempo para a promise resolvida tardiamente processar (se for
    // aplicada por engano).
    await new Promise((r) => setTimeout(r, 50));

    expect(screen.getByText('Parafuso Sextavado M8')).toBeInTheDocument();
    expect(screen.queryByText(/Nenhum produto encontrado/)).not.toBeInTheDocument();
  });
});

describe('BuscaCatalogo — desmontagem com fetch pendente', () => {
  it('não processa a resposta (nem chama res.json()) quando ela chega depois do componente desmontar', async () => {
    let resolverBusca: ((value: unknown) => void) | undefined;
    const pendingBusca = new Promise((resolve) => {
      resolverBusca = resolve;
    });

    const fetchMock = vi.fn(() => pendingBusca);
    vi.stubGlobal('fetch', fetchMock);

    const jsonSpy = vi.fn(async () => ({ produtos: [produtoParafuso] }));

    const user = userEvent.setup();
    const { unmount } = renderBusca();
    const input = screen.getByLabelText('Buscar produtos');

    await user.type(input, 'parafuso');
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(1), { timeout: 2000 });

    // Desmonta ANTES da resposta pendente resolver — sem a guarda de
    // montagem, o `.then` abaixo seguiria em frente (inclusive chamando
    // `res.json()` e depois setResultados/setTermoBuscado) num componente
    // que já saiu da árvore.
    unmount();

    // Resolve a busca só depois de desmontado.
    resolverBusca?.({ ok: true, json: jsonSpy });

    // Dá tempo para o `.then` processar, se for aplicado por engano.
    await new Promise((r) => setTimeout(r, 50));

    // Com a guarda de montagem, o `.then` retorna antes de chegar em
    // `res.json()` — sem ela, `jsonSpy` seria chamado normalmente.
    expect(jsonSpy).not.toHaveBeenCalled();
  });
});

describe('BuscaCatalogo — erro na busca', () => {
  it('resposta não-ok mostra a mensagem de erro, sem lista nem "nenhum produto encontrado"', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve({ ok: false, json: async () => ({}) })));

    const user = userEvent.setup();
    renderBusca();

    await user.type(screen.getByLabelText('Buscar produtos'), 'parafuso');

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível buscar agora. Tente novamente em instantes.',
    );
    expect(screen.queryByText(/Nenhum produto encontrado/)).not.toBeInTheDocument();
    expect(screen.queryByRole('list')).not.toBeInTheDocument();
  });

  it('fetch rejeitado (falha de rede) mostra a mesma mensagem de erro', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.reject(new Error('falha de rede'))),
    );

    const user = userEvent.setup();
    renderBusca();

    await user.type(screen.getByLabelText('Buscar produtos'), 'parafuso');

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível buscar agora. Tente novamente em instantes.',
    );
  });

  it('uma busca com sucesso após um erro anterior remove a mensagem de erro', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce({ ok: false, json: async () => ({}) })
      .mockImplementationOnce(() => respostaOk([produtoParafuso]));
    vi.stubGlobal('fetch', fetchMock);

    const user = userEvent.setup();
    renderBusca();
    const input = screen.getByLabelText('Buscar produtos');

    await user.type(input, 'a');
    expect(await screen.findByRole('alert')).toBeInTheDocument();

    await user.clear(input);
    await user.type(input, 'parafuso');

    expect(await screen.findByText('Parafuso Sextavado M8')).toBeInTheDocument();
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });
});

describe('BuscaCatalogo — atalho de teclado "/"', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn(() => respostaOk([])));
  });

  it('"/" fora de um campo editável foca o campo de busca sem digitar "/" nele', async () => {
    const user = userEvent.setup();
    render(
      <div>
        <button type="button">Botão qualquer</button>
        <BuscaCatalogo />
      </div>,
    );

    screen.getByRole('button', { name: 'Botão qualquer' }).focus();
    await user.keyboard('/');

    const input = screen.getByLabelText('Buscar produtos');
    expect(input).toHaveFocus();
    expect(input).toHaveValue('');
  });

  it('"/" dentro de outro campo editável NÃO rouba o foco', async () => {
    const user = userEvent.setup();
    render(
      <div>
        <label htmlFor="outro-campo">Outro campo</label>
        <input id="outro-campo" />
        <BuscaCatalogo />
      </div>,
    );

    const outroCampo = screen.getByLabelText('Outro campo');
    outroCampo.focus();
    await user.keyboard('/');

    expect(outroCampo).toHaveFocus();
    expect(outroCampo).toHaveValue('/');
  });

  it('"/" com modificador (ctrl) NÃO rouba o foco', async () => {
    const user = userEvent.setup();
    render(
      <div>
        <button type="button">Botão qualquer</button>
        <BuscaCatalogo />
      </div>,
    );

    const botao = screen.getByRole('button', { name: 'Botão qualquer' });
    botao.focus();
    await user.keyboard('{Control>}/{/Control}');

    expect(screen.getByLabelText('Buscar produtos')).not.toHaveFocus();
  });
});

describe('BuscaCatalogo — onTermoChange (Story 4.2)', () => {
  it('chama onTermoChange a cada digitação, com o valor BRUTO (não trimado)', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve({ ok: true, json: async () => ({ produtos: [] }) })));
    const onTermoChange = vi.fn();
    const user = userEvent.setup();
    render(
      <MemoryRouter>
        <BuscaCatalogo onTermoChange={onTermoChange} />
      </MemoryRouter>,
    );

    await user.type(screen.getByLabelText('Buscar produtos'), 'ab');

    expect(onTermoChange).toHaveBeenCalledTimes(2);
    expect(onTermoChange).toHaveBeenNthCalledWith(1, 'a');
    expect(onTermoChange).toHaveBeenNthCalledWith(2, 'ab');
  });

  it('chama onTermoChange com o valor bruto (com espaços), mesmo quando o termo trimado é vazio', async () => {
    vi.stubGlobal('fetch', vi.fn());
    const onTermoChange = vi.fn();
    const user = userEvent.setup();
    render(
      <MemoryRouter>
        <BuscaCatalogo onTermoChange={onTermoChange} />
      </MemoryRouter>,
    );

    await user.type(screen.getByLabelText('Buscar produtos'), '  ');

    expect(onTermoChange).toHaveBeenCalledTimes(2);
    expect(onTermoChange).toHaveBeenNthCalledWith(2, '  ');
    // Termo trimado vazio -> nenhuma busca de sugestões própria disparada
    // (comportamento de BuscaCatalogo inalterado por onTermoChange).
    expect(fetch).not.toHaveBeenCalled();
  });

  it('funciona normalmente sem onTermoChange (prop opcional)', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve({ ok: true, json: async () => ({ produtos: [] }) })));
    const user = userEvent.setup();
    render(
      <MemoryRouter>
        <BuscaCatalogo />
      </MemoryRouter>,
    );

    await user.type(screen.getByLabelText('Buscar produtos'), 'x');
    expect(screen.getByLabelText('Buscar produtos')).toHaveValue('x');
  });
});

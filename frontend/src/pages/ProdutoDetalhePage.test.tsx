import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { act, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes, useNavigate } from 'react-router-dom';
import { ProdutoDetalhePage } from './ProdutoDetalhePage';
import type { EventoRealtime, StatusRealtime } from '@/lib/realtime/client';

vi.mock('@/lib/session', () => ({
  getAccessToken: () => 'token-de-teste',
}));

// useAuth() fornece o papel — configurável por teste (Story 5.1: gate do
// botão "Registrar Baixa"). Default `almoxarife`, mesmo molde de
// EstoquesPage.test.tsx.
const authState = vi.hoisted(() => ({ papel: 'almoxarife' as string }));
vi.mock('@/lib/auth', () => ({
  useAuth: () => ({
    estado: 'autenticado',
    usuario: { id: '1', nome: 'Ator', email: 'ator@empresa.com', papel: authState.papel },
    definirSessao: vi.fn(),
    atualizarUsuario: vi.fn(),
    logout: vi.fn(),
  }),
}));

const toastInfo = vi.hoisted(() => vi.fn());
const toastSuccess = vi.hoisted(() => vi.fn());
vi.mock('sonner', () => ({ toast: { info: toastInfo, success: toastSuccess } }));

// useCarrinho() fornece adicionarItem para o diálogo "Adicionar ao
// Carrinho" (Story 7.1) — mock configurável por teste; o padrão resolve com
// sucesso, para os testes pré-Story-7.1 (que nunca abrem esse diálogo)
// ficarem inalterados.
const adicionarItemMock = vi.hoisted(() => vi.fn());
vi.mock('@/lib/carrinho', () => ({
  useCarrinho: () => ({
    itens: [],
    count: 0,
    carregando: false,
    refresh: vi.fn(),
    adicionarItem: adicionarItemMock,
    removerItem: vi.fn(),
  }),
}));

// conectarRealtime é mockado: os testes desta página não reexercitam a
// mecânica de reconexão/temporizadores do EventSource (já coberta em
// src/lib/realtime/client.test.ts) — só capturam os dois callbacks
// (aoReceberEvento/aoMudarStatus) para disparar cada cenário diretamente.
const conectarRealtimeMock = vi.hoisted(() => vi.fn());
vi.mock('@/lib/realtime/client', () => ({
  conectarRealtime: conectarRealtimeMock,
}));

let aoReceberEvento: (evento: EventoRealtime) => void;
let aoMudarStatus: (status: StatusRealtime) => void;
const desconectarMock = vi.fn();

beforeEach(() => {
  authState.papel = 'almoxarife';
  adicionarItemMock.mockReset();
  adicionarItemMock.mockResolvedValue({ ok: true });
  let proximoId = 0;
  URL.createObjectURL = vi.fn(() => `blob:mock-url-${proximoId++}`);
  URL.revokeObjectURL = vi.fn();

  conectarRealtimeMock.mockImplementation(
    (receber: (evento: EventoRealtime) => void, mudar: (status: StatusRealtime) => void) => {
      aoReceberEvento = receber;
      aoMudarStatus = mudar;
      return desconectarMock;
    },
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

type FetchImpl = (
  url: string,
  init?: RequestInit,
) => Promise<{ ok: boolean; status?: number; json: () => Promise<unknown>; blob?: () => Promise<Blob> }>;

function jsonOk(body: unknown) {
  return Promise.resolve({ ok: true, status: 200, json: async () => body });
}

const PRODUTO_DETALHE = {
  id: 'p1',
  nome: 'Cabo Flexível 4mm',
  codigo: 'CAB-004',
  categoria: { id: 'c1', codigo: '05.002', nome: 'Materiais Elétricos' },
  dimensoes: {
    comprimento: { valor: 100, unidade: 'm' },
    largura: null,
    diametro: null,
    altura: null,
    espessura: null,
  },
  quantidadeTotal: 8,
  disponivel: true,
  porEstoque: [
    { estoqueId: 'e1', estoqueNome: 'Almoxarifado Central', quantidade: 5 },
    { estoqueId: 'e2', estoqueNome: 'Obra Norte', quantidade: 3 },
  ],
};

function stubFetch(impl: FetchImpl) {
  const fn = vi.fn(impl);
  vi.stubGlobal('fetch', fn);
  return fn;
}

function stubPadrao(overrides?: {
  produto?: unknown;
  fotos?: { nome: string; url: string }[];
  produtoOk?: boolean;
}) {
  const fotos = overrides?.fotos ?? [];
  return stubFetch((url, init) => {
    if (url === '/api/produtos/p1') {
      if (overrides?.produtoOk === false) {
        return Promise.resolve({ ok: false, status: 500, json: async () => ({}) });
      }
      return jsonOk({ produto: overrides?.produto ?? PRODUTO_DETALHE });
    }
    if (url === '/api/produtos/p1/fotos' && (!init?.method || init.method === 'GET')) {
      return jsonOk({ fotos });
    }
    if (/^\/api\/produtos\/p1\/fotos\/.+$/.test(url)) {
      return Promise.resolve({
        ok: true,
        status: 200,
        json: async () => ({}),
        blob: async () => new Blob(['bytes'], { type: 'image/jpeg' }),
      });
    }
    throw new Error(`URL inesperada: ${url} (${init?.method ?? 'GET'})`);
  });
}

function renderPagina(id = 'p1') {
  return render(
    <MemoryRouter initialEntries={[`/produtos/${id}`]}>
      <Routes>
        <Route path="/produtos/:id" element={<ProdutoDetalhePage />} />
      </Routes>
    </MemoryRouter>,
  );
}

// BotaoNavegar dá ao teste um jeito de trocar só o `:id` da rota SEM
// desmontar `ProdutoDetalhePage` — exatamente o cenário que o React Router
// produz na aplicação real (mesmo padrão de rota casando ambos os ids).
function BotaoNavegar({ para }: { para: string }) {
  const navigate = useNavigate();
  return (
    <button type="button" onClick={() => navigate(para)}>
      {`ir para ${para}`}
    </button>
  );
}

function renderPaginaComNavegacao() {
  return render(
    <MemoryRouter initialEntries={['/produtos/p1']}>
      <Routes>
        <Route
          path="/produtos/:id"
          element={
            <>
              <BotaoNavegar para="/produtos/p2" />
              <ProdutoDetalhePage />
            </>
          }
        />
      </Routes>
    </MemoryRouter>,
  );
}

const PRODUTO_P2 = {
  id: 'p2',
  nome: 'Outro Produto',
  codigo: 'OUT-002',
  categoria: { id: 'c2', codigo: '06.001', nome: 'Ferramentas' },
  dimensoes: {
    comprimento: null,
    largura: null,
    diametro: null,
    altura: null,
    espessura: null,
  },
  quantidadeTotal: 1,
  disponivel: true,
  porEstoque: [{ estoqueId: 'e3', estoqueNome: 'Depósito Sul', quantidade: 1 }],
};

describe('ProdutoDetalhePage', () => {
  it('carrega o detalhe SÓ quando conectarRealtime chama aoMudarStatus("conectado") (inclusive a 1ª conexão)', async () => {
    stubPadrao();
    renderPagina();

    // Antes de "conectado", nenhum GET de detalhe ainda foi disparado.
    expect(screen.queryByText('Cabo Flexível 4mm')).not.toBeInTheDocument();

    act(() => {
      aoMudarStatus('conectado');
    });

    expect(await screen.findByText('Cabo Flexível 4mm')).toBeInTheDocument();
    const codigo = screen.getByText('CAB-004');
    expect(codigo).toHaveClass('font-mono');
    expect(screen.getByText(/Materiais Elétricos/)).toBeInTheDocument();
    expect(screen.getByText('Almoxarifado Central')).toBeInTheDocument();
    expect(screen.getByText('Obra Norte')).toBeInTheDocument();
    expect(screen.getByText('Disponível')).toBeInTheDocument();
  });

  it('produto sem porEstoque mostra o aviso "Sem quantidade registrada por estoque."', async () => {
    stubPadrao({ produto: { ...PRODUTO_DETALHE, porEstoque: [], quantidadeTotal: 0, disponivel: false } });
    renderPagina();

    act(() => {
      aoMudarStatus('conectado');
    });

    expect(await screen.findByText('Sem quantidade registrada por estoque.')).toBeInTheDocument();
    expect(screen.getByText('Sem estoque')).toBeInTheDocument();
  });

  it('0 fotos: nenhuma seção de fotos aparece, sem erro', async () => {
    stubPadrao({ fotos: [] });
    renderPagina();

    act(() => {
      aoMudarStatus('conectado');
    });

    await screen.findByText('Cabo Flexível 4mm');
    expect(screen.queryByText('Fotos')).not.toBeInTheDocument();
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('exibe miniaturas e abre o lightbox em tela cheia ao clicar', async () => {
    stubPadrao({ fotos: [{ nome: 'foto1.jpg', url: '/api/produtos/p1/fotos/foto1.jpg' }] });
    const user = userEvent.setup();
    renderPagina();

    act(() => {
      aoMudarStatus('conectado');
    });

    const miniatura = await screen.findByRole('button', { name: 'Ampliar foto 1 de 1' });
    await user.click(miniatura);

    expect(screen.getByRole('dialog')).toBeInTheDocument();
  });

  it('evento SSE do MESMO produto: refetch + toast.info("Catálogo atualizado.")', async () => {
    const fetchMock = stubPadrao();
    renderPagina();

    act(() => {
      aoMudarStatus('conectado');
    });
    await screen.findByText('Cabo Flexível 4mm');
    const chamadasAntes = fetchMock.mock.calls.length;

    act(() => {
      aoReceberEvento({ resource: 'produtos', id: 'p1', change: 'updated' });
    });

    await waitFor(() => expect(fetchMock.mock.calls.length).toBeGreaterThan(chamadasAntes));
    expect(toastInfo).toHaveBeenCalledWith('Catálogo atualizado.');
  });

  it('evento SSE de OUTRO produto: nenhum refetch, nenhum toast', async () => {
    const fetchMock = stubPadrao();
    renderPagina();

    act(() => {
      aoMudarStatus('conectado');
    });
    await screen.findByText('Cabo Flexível 4mm');
    const chamadasAntes = fetchMock.mock.calls.length;

    act(() => {
      aoReceberEvento({ resource: 'produtos', id: 'outro-produto', change: 'updated' });
    });

    // Dá um tick para qualquer efeito indevido acontecer.
    await new Promise((r) => setTimeout(r, 20));

    expect(fetchMock.mock.calls.length).toBe(chamadasAntes);
    expect(toastInfo).not.toHaveBeenCalled();
  });

  it('status "reconectando" mostra o indicador persistente com aria-live="polite"', async () => {
    stubPadrao();
    renderPagina();

    act(() => {
      aoMudarStatus('conectado');
    });
    await screen.findByText('Cabo Flexível 4mm');
    expect(screen.queryByText('Reconectando...')).not.toBeInTheDocument();

    act(() => {
      aoMudarStatus('reconectando');
    });

    const indicador = screen.getByText('Reconectando...');
    expect(indicador).toHaveAttribute('aria-live', 'polite');

    act(() => {
      aoMudarStatus('conectado');
    });
    expect(screen.queryByText('Reconectando...')).not.toBeInTheDocument();
  });

  it('desmontar a página desconecta a SSE (chama a função de cleanup)', async () => {
    stubPadrao();
    const { unmount } = renderPagina();

    act(() => {
      aoMudarStatus('conectado');
    });
    await screen.findByText('Cabo Flexível 4mm');

    unmount();
    expect(desconectarMock).toHaveBeenCalled();
  });

  it('resposta não-OK (500) do detalhe mostra o alerta genérico', async () => {
    stubPadrao({ produtoOk: false });
    renderPagina();

    act(() => {
      aoMudarStatus('conectado');
    });

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível carregar o produto agora. Tente novamente em instantes.',
    );
  });

  it('resposta 404 do detalhe mostra "Produto não encontrado." (distinto do erro genérico)', async () => {
    stubFetch((url) => {
      if (url === '/api/produtos/p1') {
        return Promise.resolve({ ok: false, status: 404, json: async () => ({}) });
      }
      throw new Error(`URL inesperada: ${url}`);
    });
    renderPagina();

    act(() => {
      aoMudarStatus('conectado');
    });

    expect(await screen.findByRole('alert')).toHaveTextContent('Produto não encontrado.');
  });

  it('troca de :id enquanto uma busca antiga está em voo: a resposta obsoleta é descartada, o novo produto prevalece', async () => {
    let resolverP1: ((valor: unknown) => void) | undefined;
    const fetchMock = vi.fn((url: string) => {
      if (url === '/api/produtos/p1') {
        return new Promise((resolve) => {
          resolverP1 = resolve;
        });
      }
      if (url === '/api/produtos/p1/fotos') return jsonOk({ fotos: [] });
      if (url === '/api/produtos/p2') return jsonOk({ produto: PRODUTO_P2 });
      if (url === '/api/produtos/p2/fotos') return jsonOk({ fotos: [] });
      throw new Error(`URL inesperada: ${url}`);
    });
    vi.stubGlobal('fetch', fetchMock);

    const user = userEvent.setup();
    renderPaginaComNavegacao();

    // Dispara a busca do produto p1 — fica pendurada (resolverP1 ainda não
    // foi chamado), simulando uma resposta lenta.
    act(() => {
      aoMudarStatus('conectado');
    });

    // Navega para p2 ANTES da resposta de p1 chegar — o Router não
    // remonta o componente, só troca `:id`.
    await user.click(screen.getByText('ir para /produtos/p2'));

    act(() => {
      aoMudarStatus('conectado');
    });

    expect(await screen.findByText('Outro Produto')).toBeInTheDocument();

    // A resposta obsoleta de p1 chega só agora — não pode sobrescrever a
    // tela, que já mostra o produto p2 (mesma URL /produtos/p2).
    act(() => {
      resolverP1?.({ ok: true, status: 200, json: async () => ({ produto: PRODUTO_DETALHE }) });
    });
    await new Promise((r) => setTimeout(r, 20));

    expect(screen.getByText('Outro Produto')).toBeInTheDocument();
    expect(screen.queryByText('Cabo Flexível 4mm')).not.toBeInTheDocument();
  });

  it('SSE refetch reduz as fotos: lightbox aberto num índice que deixou de existir se fecha sozinho', async () => {
    let chamadaFotos = 0;
    const fetchMock = stubFetch((url, init) => {
      if (url === '/api/produtos/p1') return jsonOk({ produto: PRODUTO_DETALHE });
      if (url === '/api/produtos/p1/fotos' && (!init?.method || init.method === 'GET')) {
        chamadaFotos += 1;
        if (chamadaFotos === 1) {
          return jsonOk({
            fotos: [
              { nome: 'foto1.jpg', url: '/api/produtos/p1/fotos/foto1.jpg' },
              { nome: 'foto2.jpg', url: '/api/produtos/p1/fotos/foto2.jpg' },
            ],
          });
        }
        return jsonOk({ fotos: [{ nome: 'foto1.jpg', url: '/api/produtos/p1/fotos/foto1.jpg' }] });
      }
      if (/^\/api\/produtos\/p1\/fotos\/.+$/.test(url)) {
        return Promise.resolve({
          ok: true,
          status: 200,
          json: async () => ({}),
          blob: async () => new Blob(['bytes'], { type: 'image/jpeg' }),
        });
      }
      throw new Error(`URL inesperada: ${url} (${init?.method ?? 'GET'})`);
    });
    void fetchMock;

    const user = userEvent.setup();
    renderPagina();

    act(() => {
      aoMudarStatus('conectado');
    });

    const miniatura2 = await screen.findByRole('button', { name: 'Ampliar foto 2 de 2' });
    await user.click(miniatura2);
    expect(screen.getByRole('dialog')).toBeInTheDocument();

    // Evento SSE do mesmo produto: refetch — a nova galeria só tem 1 foto,
    // o índice 1 (segunda foto) aberto no lightbox deixou de existir.
    act(() => {
      aoReceberEvento({ resource: 'produtos', id: 'p1', change: 'updated' });
    });

    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
  });

  describe('Registrar Baixa (Story 5.1)', () => {
    it.each(['almoxarife', 'gestor', 'adm'])(
      'papel %s vê o botão "Registrar Baixa" em cada linha de Estoque',
      async (papel) => {
        authState.papel = papel;
        stubPadrao();
        renderPagina();

        act(() => {
          aoMudarStatus('conectado');
        });

        await screen.findByText('Cabo Flexível 4mm');
        expect(screen.getAllByRole('button', { name: /Registrar Baixa/ })).toHaveLength(2);
      },
    );

    it('papel usuario NÃO vê o botão "Registrar Baixa"', async () => {
      authState.papel = 'usuario';
      stubPadrao();
      renderPagina();

      act(() => {
        aoMudarStatus('conectado');
      });

      await screen.findByText('Cabo Flexível 4mm');
      expect(screen.queryByRole('button', { name: /Registrar Baixa/ })).not.toBeInTheDocument();
    });

    it('submissão bem-sucedida: fecha o diálogo, mostra toast.success e refaz o refetch', async () => {
      let baixaChamada = false;
      const fetchMock = stubFetch((url, init) => {
        if (url === '/api/produtos/p1' && (!init?.method || init.method === 'GET')) {
          return jsonOk({ produto: PRODUTO_DETALHE });
        }
        if (url === '/api/produtos/p1/fotos') return jsonOk({ fotos: [] });
        if (url === '/api/produtos/p1/estoques/e1/baixa' && init?.method === 'POST') {
          baixaChamada = true;
          return Promise.resolve({
            ok: true,
            status: 201,
            json: async () => ({ movimentacao: { id: 'mov1', tipo: 'baixa', quantidade: 2 } }),
          });
        }
        throw new Error(`URL inesperada: ${url} (${init?.method ?? 'GET'})`);
      });

      const user = userEvent.setup();
      renderPagina();

      act(() => {
        aoMudarStatus('conectado');
      });
      await screen.findByText('Cabo Flexível 4mm');

      const botoes = screen.getAllByRole('button', { name: /Registrar Baixa/ });
      await user.click(botoes[0]);

      const dialogo = await screen.findByRole('dialog');
      const input = within(dialogo).getByLabelText('Quantidade');
      await user.type(input, '2');
      await user.click(within(dialogo).getByRole('button', { name: 'Confirmar' }));

      await waitFor(() => expect(baixaChamada).toBe(true));
      await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
      expect(toastSuccess).toHaveBeenCalledWith('Baixa registrada.');
      // refetch: mais de uma chamada a GET /api/produtos/p1 (mount + pós-baixa).
      const chamadasDetalhe = fetchMock.mock.calls.filter(([u]) => u === '/api/produtos/p1');
      expect(chamadasDetalhe.length).toBeGreaterThan(1);
    });

    it('409 mostra a mensagem do servidor dentro do diálogo, sem fechar', async () => {
      const fetchMock = stubFetch((url, init) => {
        if (url === '/api/produtos/p1' && (!init?.method || init.method === 'GET')) {
          return jsonOk({ produto: PRODUTO_DETALHE });
        }
        if (url === '/api/produtos/p1/fotos') return jsonOk({ fotos: [] });
        if (url === '/api/produtos/p1/estoques/e1/baixa' && init?.method === 'POST') {
          return Promise.resolve({
            ok: false,
            status: 409,
            json: async () => ({
              error: {
                code: 'CONFLICT',
                message: 'quantidade indisponível: apenas 5 unidade(s) disponível(is)',
              },
            }),
          });
        }
        throw new Error(`URL inesperada: ${url} (${init?.method ?? 'GET'})`);
      });
      void fetchMock;

      const user = userEvent.setup();
      renderPagina();

      act(() => {
        aoMudarStatus('conectado');
      });
      await screen.findByText('Cabo Flexível 4mm');

      const botoes = screen.getAllByRole('button', { name: /Registrar Baixa/ });
      await user.click(botoes[0]);

      const dialogo = await screen.findByRole('dialog');
      const input = within(dialogo).getByLabelText('Quantidade');
      await user.type(input, '999');
      await user.click(within(dialogo).getByRole('button', { name: 'Confirmar' }));

      expect(
        await within(dialogo).findByText('quantidade indisponível: apenas 5 unidade(s) disponível(is)'),
      ).toBeInTheDocument();
      expect(screen.getByRole('dialog')).toBeInTheDocument();
      expect(toastSuccess).not.toHaveBeenCalled();
    });
  });

  describe('Adicionar ao Carrinho (Story 7.1)', () => {
    it.each(['usuario', 'almoxarife', 'gestor', 'adm'])(
      'papel %s vê o botão "Adicionar ao Carrinho" em cada linha de Estoque (sem gate de papel)',
      async (papel) => {
        authState.papel = papel;
        stubPadrao();
        renderPagina();

        act(() => {
          aoMudarStatus('conectado');
        });

        await screen.findByText('Cabo Flexível 4mm');
        expect(screen.getAllByRole('button', { name: /Adicionar ao Carrinho/ })).toHaveLength(2);
      },
    );

    it('desabilita o botão "Adicionar ao Carrinho" na linha com quantidade zerada (evita 409 previsível)', async () => {
      stubPadrao({
        produto: {
          ...PRODUTO_DETALHE,
          porEstoque: [
            { estoqueId: 'e1', estoqueNome: 'Almoxarifado Central', quantidade: 5 },
            { estoqueId: 'e2', estoqueNome: 'Obra Norte', quantidade: 0 },
          ],
        },
      });
      renderPagina();

      act(() => {
        aoMudarStatus('conectado');
      });
      await screen.findByText('Cabo Flexível 4mm');

      const botoes = screen.getAllByRole('button', { name: /Adicionar ao Carrinho/ });
      expect(botoes[0]).toBeEnabled();
      expect(botoes[1]).toBeDisabled();

      const user = userEvent.setup();
      await user.click(botoes[1]);
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    });

    it('submissão bem-sucedida: chama useCarrinho().adicionarItem, fecha o diálogo e mostra toast.success', async () => {
      stubPadrao();
      const user = userEvent.setup();
      renderPagina();

      act(() => {
        aoMudarStatus('conectado');
      });
      await screen.findByText('Cabo Flexível 4mm');

      const botoes = screen.getAllByRole('button', { name: /Adicionar ao Carrinho/ });
      await user.click(botoes[0]);

      const dialogo = await screen.findByRole('dialog');
      const input = within(dialogo).getByLabelText('Quantidade');
      await user.type(input, '2');
      await user.click(within(dialogo).getByRole('button', { name: 'Confirmar' }));

      await waitFor(() => expect(adicionarItemMock).toHaveBeenCalledWith('p1', 'e1', 2));
      await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
      expect(toastSuccess).toHaveBeenCalledWith('Item adicionado ao carrinho.');
    });

    it('falha (409/404): mostra a mensagem do servidor dentro do diálogo, sem fechar, sem toast', async () => {
      adicionarItemMock.mockResolvedValue({
        ok: false,
        mensagem: 'quantidade indisponível: apenas 1 unidade(s) disponível(is) para adicionar ao carrinho',
      });
      stubPadrao();
      const user = userEvent.setup();
      renderPagina();

      act(() => {
        aoMudarStatus('conectado');
      });
      await screen.findByText('Cabo Flexível 4mm');

      const botoes = screen.getAllByRole('button', { name: /Adicionar ao Carrinho/ });
      await user.click(botoes[0]);

      const dialogo = await screen.findByRole('dialog');
      const input = within(dialogo).getByLabelText('Quantidade');
      await user.type(input, '999');
      await user.click(within(dialogo).getByRole('button', { name: 'Confirmar' }));

      expect(
        await within(dialogo).findByText(
          'quantidade indisponível: apenas 1 unidade(s) disponível(is) para adicionar ao carrinho',
        ),
      ).toBeInTheDocument();
      expect(screen.getByRole('dialog')).toBeInTheDocument();
      expect(toastSuccess).not.toHaveBeenCalled();
    });

    it('sucesso NÃO refaz o refetch de GET /api/produtos/p1 (adicionar ao carrinho não muda produto_estoque)', async () => {
      const fetchMock = stubPadrao();
      const user = userEvent.setup();
      renderPagina();

      act(() => {
        aoMudarStatus('conectado');
      });
      await screen.findByText('Cabo Flexível 4mm');
      const chamadasAntes = fetchMock.mock.calls.filter(([u]) => u === '/api/produtos/p1').length;

      const botoes = screen.getAllByRole('button', { name: /Adicionar ao Carrinho/ });
      await user.click(botoes[0]);
      const dialogo = await screen.findByRole('dialog');
      const input = within(dialogo).getByLabelText('Quantidade');
      await user.type(input, '2');
      await user.click(within(dialogo).getByRole('button', { name: 'Confirmar' }));

      await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
      const chamadasDepois = fetchMock.mock.calls.filter(([u]) => u === '/api/produtos/p1').length;
      expect(chamadasDepois).toBe(chamadasAntes);
    });
  });

  describe('Transferir (Story 5.2)', () => {
    const ESTOQUES = [
      { id: 'e1', nome: 'Almoxarifado Central' },
      { id: 'e2', nome: 'Obra Norte' },
      { id: 'e9', nome: 'Depósito Leste' },
    ];

    // stubTransferencia: GET detalhe/fotos + GET /api/estoques + POST
    // .../transferencia (resultados configuráveis). `capturado` recebe o
    // corpo JSON enviado no POST.
    function stubTransferencia(opts: {
      ok: boolean;
      status: number;
      json: () => Promise<unknown>;
      onCall?: (corpo: unknown) => void;
      estoquesOk?: boolean;
      estoques?: { id: string; nome: string }[];
    }) {
      return stubFetch((url, init) => {
        if (url === '/api/produtos/p1' && (!init?.method || init.method === 'GET')) {
          return jsonOk({ produto: PRODUTO_DETALHE });
        }
        if (url === '/api/produtos/p1/fotos') return jsonOk({ fotos: [] });
        if (url === '/api/estoques') {
          if (opts.estoquesOk === false) {
            return Promise.resolve({ ok: false, status: 500, json: async () => ({}) });
          }
          return jsonOk({ estoques: opts.estoques ?? ESTOQUES });
        }
        if (url === '/api/produtos/p1/estoques/e1/transferencia' && init?.method === 'POST') {
          opts.onCall?.(init?.body ? JSON.parse(init.body as string) : undefined);
          return Promise.resolve({
            ok: opts.ok,
            status: opts.status,
            json: opts.json,
          });
        }
        throw new Error(`URL inesperada: ${url} (${init?.method ?? 'GET'})`);
      });
    }

    it.each(['almoxarife', 'gestor', 'adm'])(
      'papel %s vê o botão "Transferir" em cada linha de Estoque',
      async (papel) => {
        authState.papel = papel;
        stubPadrao();
        renderPagina();

        act(() => {
          aoMudarStatus('conectado');
        });

        await screen.findByText('Cabo Flexível 4mm');
        expect(screen.getAllByRole('button', { name: /^Transferir de / })).toHaveLength(2);
      },
    );

    it('papel usuario NÃO vê o botão "Transferir"', async () => {
      authState.papel = 'usuario';
      stubPadrao();
      renderPagina();

      act(() => {
        aoMudarStatus('conectado');
      });

      await screen.findByText('Cabo Flexível 4mm');
      expect(screen.queryByRole('button', { name: /^Transferir de / })).not.toBeInTheDocument();
    });

    it('abrir o diálogo busca a lista de Estoques e exclui a linha de origem das opções', async () => {
      stubTransferencia({ ok: true, status: 201, json: async () => ({ movimentacao: { id: 'm1' } }) });

      const user = userEvent.setup();
      renderPagina();
      act(() => {
        aoMudarStatus('conectado');
      });
      await screen.findByText('Cabo Flexível 4mm');

      await user.click(screen.getByRole('button', { name: 'Transferir de Almoxarifado Central' }));

      const combo = await screen.findByRole('combobox', { name: 'Estoque destino' });
      await user.click(combo);

      expect(await screen.findByRole('option', { name: 'Obra Norte' })).toBeInTheDocument();
      expect(screen.getByRole('option', { name: 'Depósito Leste' })).toBeInTheDocument();
      // a própria origem (Almoxarifado Central) NÃO aparece como opção.
      expect(screen.queryByRole('option', { name: 'Almoxarifado Central' })).not.toBeInTheDocument();
    });

    it('submissão bem-sucedida: envia {estoqueDestinoId, quantidade}, fecha o diálogo, mostra toast.success e refaz o refetch', async () => {
      let corpoEnviado: unknown;
      const fetchMock = stubTransferencia({
        ok: true,
        status: 201,
        json: async () => ({ movimentacao: { id: 'm1', tipo: 'transferencia' } }),
        onCall: (corpo) => {
          corpoEnviado = corpo;
        },
      });

      const user = userEvent.setup();
      renderPagina();
      act(() => {
        aoMudarStatus('conectado');
      });
      await screen.findByText('Cabo Flexível 4mm');

      await user.click(screen.getByRole('button', { name: 'Transferir de Almoxarifado Central' }));
      const dialogo = await screen.findByRole('dialog');

      await user.click(within(dialogo).getByRole('combobox', { name: 'Estoque destino' }));
      await user.click(await screen.findByRole('option', { name: 'Obra Norte' }));
      await user.type(within(dialogo).getByLabelText('Quantidade'), '2');
      await user.click(within(dialogo).getByRole('button', { name: 'Confirmar' }));

      await waitFor(() => expect(corpoEnviado).toEqual({ estoqueDestinoId: 'e2', quantidade: 2 }));
      await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
      expect(toastSuccess).toHaveBeenCalledWith('Transferência registrada.');
      const chamadasDetalhe = fetchMock.mock.calls.filter(([u]) => u === '/api/produtos/p1');
      expect(chamadasDetalhe.length).toBeGreaterThan(1);
    });

    it('falha ao carregar a lista de Estoques: mostra a mensagem e mantém Confirmar desabilitado', async () => {
      stubTransferencia({
        ok: true,
        status: 201,
        json: async () => ({}),
        estoquesOk: false,
      });

      const user = userEvent.setup();
      renderPagina();
      act(() => {
        aoMudarStatus('conectado');
      });
      await screen.findByText('Cabo Flexível 4mm');

      await user.click(screen.getByRole('button', { name: 'Transferir de Almoxarifado Central' }));
      const dialogo = await screen.findByRole('dialog');

      expect(
        await within(dialogo).findByText(
          'Não foi possível carregar a lista de estoques. Feche e tente novamente.',
        ),
      ).toBeInTheDocument();
      expect(within(dialogo).getByRole('button', { name: 'Confirmar' })).toBeDisabled();
    });

    it('lista só com a própria origem: mostra "Nenhum outro estoque disponível" e Confirmar desabilitado', async () => {
      stubTransferencia({
        ok: true,
        status: 201,
        json: async () => ({}),
        estoques: [{ id: 'e1', nome: 'Almoxarifado Central' }],
      });

      const user = userEvent.setup();
      renderPagina();
      act(() => {
        aoMudarStatus('conectado');
      });
      await screen.findByText('Cabo Flexível 4mm');

      await user.click(screen.getByRole('button', { name: 'Transferir de Almoxarifado Central' }));
      const dialogo = await screen.findByRole('dialog');

      expect(
        await within(dialogo).findByText('Nenhum outro estoque disponível para transferência.'),
      ).toBeInTheDocument();
      expect(within(dialogo).getByRole('button', { name: 'Confirmar' })).toBeDisabled();
    });

    it('erro do servidor (409) aparece no diálogo, sem fechar', async () => {
      stubTransferencia({
        ok: false,
        status: 409,
        json: async () => ({
          error: {
            code: 'CONFLICT',
            message: 'quantidade indisponível: apenas 3 unidade(s) disponível(is)',
          },
        }),
      });

      const user = userEvent.setup();
      renderPagina();
      act(() => {
        aoMudarStatus('conectado');
      });
      await screen.findByText('Cabo Flexível 4mm');

      await user.click(screen.getByRole('button', { name: 'Transferir de Almoxarifado Central' }));
      const dialogo = await screen.findByRole('dialog');

      await user.click(within(dialogo).getByRole('combobox', { name: 'Estoque destino' }));
      await user.click(await screen.findByRole('option', { name: 'Obra Norte' }));
      await user.type(within(dialogo).getByLabelText('Quantidade'), '999');
      await user.click(within(dialogo).getByRole('button', { name: 'Confirmar' }));

      expect(
        await within(dialogo).findByText('quantidade indisponível: apenas 3 unidade(s) disponível(is)'),
      ).toBeInTheDocument();
      expect(screen.getByRole('dialog')).toBeInTheDocument();
      expect(toastSuccess).not.toHaveBeenCalled();
    });
  });
});

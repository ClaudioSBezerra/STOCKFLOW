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
  quantidadeReservada: 0,
  quantidadeDisponivel: 8,
  disponivel: true,
  porEstoque: [
    { estoqueId: 'e1', estoqueNome: 'Almoxarifado Central', quantidade: 5, reservada: 0, disponivel: 5 },
    { estoqueId: 'e2', estoqueNome: 'Obra Norte', quantidade: 3, reservada: 0, disponivel: 3 },
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
  reservas?: { pedidoId: string; solicitante: string; quantidade: number; criadoEm: string }[];
}) {
  const fotos = overrides?.fotos ?? [];
  return stubFetch((url, init) => {
    if (/^\/api\/produtos\/p1\/estoques\/[^/]+\/reservas$/.test(url)) {
      return jsonOk({ reservas: overrides?.reservas ?? [] });
    }
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
  quantidadeReservada: 0,
  quantidadeDisponivel: 1,
  disponivel: true,
  porEstoque: [{ estoqueId: 'e3', estoqueNome: 'Depósito Sul', quantidade: 1, reservada: 0, disponivel: 1 }],
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

  it('fallback: busca o detalhe mesmo sem SSE conectar, após o timer de escape (incidente real 2026-09-04)', async () => {
    vi.useFakeTimers();
    try {
      stubPadrao();
      renderPagina();

      // Nunca dispara aoMudarStatus('conectado') neste teste — simula o
      // EventSource nunca completando o handshake (ex.: proxy em produção
      // que nunca fecha a conexão nem emite erro).
      expect(screen.queryByText('Cabo Flexível 4mm')).not.toBeInTheDocument();

      await act(async () => {
        await vi.advanceTimersByTimeAsync(4000);
      });

      expect(screen.getByText('Cabo Flexível 4mm')).toBeInTheDocument();
    } finally {
      vi.useRealTimers();
    }
  });

  it('fallback: não refaz a busca se "conectado" já chegou antes do timer de escape', async () => {
    vi.useFakeTimers();
    try {
      const fetchMock = stubPadrao();
      renderPagina();

      act(() => {
        aoMudarStatus('conectado');
      });
      await act(async () => {
        await vi.advanceTimersByTimeAsync(0);
      });
      expect(screen.getByText('Cabo Flexível 4mm')).toBeInTheDocument();

      const chamadasAntes = fetchMock.mock.calls.length;

      // O timer de escape venceria aqui se não tivesse sido cancelado por
      // 'conectado' já ter chegado — não deve gerar nenhuma chamada extra.
      await act(async () => {
        await vi.advanceTimersByTimeAsync(4000);
      });
      expect(fetchMock.mock.calls.length).toBe(chamadasAntes);
    } finally {
      vi.useRealTimers();
    }
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

    it('mostra o saldo disponível da linha e o aviso de reserva (Story 11.4)', async () => {
      stubFetch((url, init) => {
        if (url === '/api/produtos/p1' && (!init?.method || init.method === 'GET')) {
          return jsonOk({
            produto: {
              ...PRODUTO_DETALHE,
              porEstoque: [
                { estoqueId: 'e1', estoqueNome: 'Almoxarifado Central', quantidade: 10, reservada: 8, disponivel: 2 },
              ],
            },
          });
        }
        if (url === '/api/produtos/p1/fotos') return jsonOk({ fotos: [] });
        throw new Error(`URL inesperada: ${url} (${init?.method ?? 'GET'})`);
      });

      const user = userEvent.setup();
      renderPagina();
      act(() => {
        aoMudarStatus('conectado');
      });
      await screen.findByText('Cabo Flexível 4mm');

      await user.click(screen.getByRole('button', { name: /Registrar Baixa/ }));
      const dialogo = await screen.findByRole('dialog');
      expect(within(dialogo).getByText('Disponível para baixa: 2')).toBeInTheDocument();
      expect(
        within(dialogo).getByText(/saldo reservado por Pedidos pendentes não pode ser baixado/),
      ).toBeInTheDocument();
      expect(within(dialogo).queryByText(/lote/i)).not.toBeInTheDocument();
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
            { estoqueId: 'e1', estoqueNome: 'Almoxarifado Central', quantidade: 5, reservada: 0, disponivel: 5 },
            { estoqueId: 'e2', estoqueNome: 'Obra Norte', quantidade: 0, reservada: 0, disponivel: 0 },
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
      expect(toastSuccess).toHaveBeenCalledWith(
        'Item adicionado ao carrinho. O saldo só é reservado ao enviar o Pedido.',
      );
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

    it('mostra o saldo disponível da linha e o aviso de reserva (Story 11.4)', async () => {
      stubTransferencia({ ok: true, status: 201, json: async () => ({ movimentacao: { id: 'm1' } }) });

      const user = userEvent.setup();
      renderPagina();
      act(() => {
        aoMudarStatus('conectado');
      });
      await screen.findByText('Cabo Flexível 4mm');

      await user.click(screen.getByRole('button', { name: 'Transferir de Almoxarifado Central' }));
      const dialogo = await screen.findByRole('dialog');
      expect(within(dialogo).getByText('Disponível para transferência: 5')).toBeInTheDocument();
      expect(
        within(dialogo).getByText(/saldo reservado por Pedidos pendentes não pode ser transferido/),
      ).toBeInTheDocument();
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

describe('ProdutoDetalhePage — Lotes (Story 11.1)', () => {
  const COM_LOTES = {
    ...PRODUTO_DETALHE,
    quantidadeTotal: 20,
    porEstoque: [
      {
        estoqueId: 'e1',
        estoqueNome: 'Almoxarifado Central',
        quantidade: 20,
        reservada: 0,
        disponivel: 20,
        lotes: [
          { id: 'l1', quantidade: 10, dataValidade: '2020-01-01', vencido: true, legado: false },
          { id: 'l2', quantidade: 6, dataValidade: '2999-03-01', vencido: false, legado: false },
          { id: null, quantidade: 4, dataValidade: null, vencido: false, legado: true },
        ],
      },
    ],
  };

  it('lista os Lotes com validade, "validade desconhecida" e o badge Vencido só no vencido', async () => {
    stubPadrao({ produto: COM_LOTES });
    renderPagina();
    act(() => {
      aoMudarStatus('conectado');
    });

    const lista = await screen.findByRole('list', { name: 'Lotes' });
    const itens = within(lista).getAllByRole('listitem');
    expect(itens).toHaveLength(3);
    expect(itens[0]).toHaveTextContent('validade 01/01/2020');
    expect(within(itens[0]).getByText('Vencido')).toBeInTheDocument();
    expect(itens[1]).toHaveTextContent('validade 01/03/2999');
    expect(within(itens[1]).queryByText('Vencido')).not.toBeInTheDocument();
    expect(itens[2]).toHaveTextContent('validade desconhecida');
    expect(within(itens[2]).queryByText('Vencido')).not.toBeInTheDocument();
    // Vencido nunca é a cor destrutiva; o saldo segue contado.
    expect(within(itens[0]).getByText('Vencido').className).not.toContain('destructive');
    expect(screen.getByText('Total: 20')).toBeInTheDocument();
  });

  it('sem lotes no payload não renderiza a lista de Lotes', async () => {
    stubPadrao();
    renderPagina();
    act(() => {
      aoMudarStatus('conectado');
    });

    await screen.findByText('Almoxarifado Central');
    expect(screen.queryByRole('list', { name: 'Lotes' })).not.toBeInTheDocument();
  });
});

const reserva = (pedidoId: string, solicitante: string) => ({
  pedidoId,
  solicitante,
  quantidade: 3,
  criadoEm: '2026-09-20T12:00:00Z',
});

describe('ProdutoDetalhePage — Reservas (Story 11.3)', () => {
  const COM_RESERVA = {
    ...PRODUTO_DETALHE,
    quantidadeTotal: 8,
    quantidadeReservada: 5,
    quantidadeDisponivel: 3,
    porEstoque: [
      { estoqueId: 'e1', estoqueNome: 'Almoxarifado Central', quantidade: 5, reservada: 5, disponivel: 0 },
      { estoqueId: 'e2', estoqueNome: 'Obra Norte', quantidade: 3, reservada: 0, disponivel: 3 },
    ],
  };

  it('mostra reservado/disponível por Estoque, só oferece o botão de reservado quando > 0', async () => {
    stubPadrao({ produto: COM_RESERVA });
    renderPagina();
    act(() => {
      aoMudarStatus('conectado');
    });

    await screen.findByText('Almoxarifado Central');
    expect(screen.getAllByText(/^Disponível: /)).toHaveLength(2);
    expect(
      screen.getByRole('button', { name: 'Saldo reservado: 5 — ver pedidos em Almoxarifado Central' }),
    ).toHaveTextContent('Saldo reservado: 5');
    expect(
      screen.queryByRole('button', { name: /^Saldo reservado: .* ver pedidos em Obra Norte/ }),
    ).not.toBeInTheDocument();
    expect(screen.getByText(/Total: 8 — Reservado: 5 — Disponível: 3/)).toBeInTheDocument();
  });

  it('"Adicionar ao Carrinho" desabilita com disponível 0 mesmo com saldo físico', async () => {
    stubPadrao({ produto: COM_RESERVA });
    renderPagina();
    act(() => {
      aoMudarStatus('conectado');
    });
    await screen.findByText('Almoxarifado Central');

    const botoes = screen.getAllByRole('button', { name: /Adicionar ao Carrinho/ });
    expect(botoes[0]).toBeDisabled();
    expect(botoes[1]).toBeEnabled();
  });

  it('clicar no reservado abre a lista de Pedidos/solicitantes', async () => {
    const fetchMock = stubPadrao({
      produto: COM_RESERVA,
      reservas: [
        { pedidoId: 'aaaaaaaa-1111-2222-3333-444444444444', solicitante: 'Maria Operária', quantidade: 3, criadoEm: '2026-09-20T12:00:00Z' },
        { pedidoId: 'bbbbbbbb-1111-2222-3333-444444444444', solicitante: 'João Obra', quantidade: 2, criadoEm: '2026-09-20T12:00:00Z' },
      ],
    });
    const user = userEvent.setup();
    renderPagina();
    act(() => {
      aoMudarStatus('conectado');
    });
    await screen.findByText('Almoxarifado Central');

    await user.click(
      screen.getByRole('button', { name: 'Saldo reservado: 5 — ver pedidos em Almoxarifado Central' }),
    );

    const lista = await screen.findByRole('list', { name: 'Pedidos com saldo reservado' });
    const itens = within(lista).getAllByRole('listitem');
    expect(itens).toHaveLength(2);
    expect(itens[0]).toHaveTextContent('Maria Operária');
    expect(itens[0]).toHaveTextContent('3');
    // Linha distingue Pedidos do mesmo solicitante: id curto (8 chars) + data.
    expect(itens[0]).toHaveTextContent('Pedido aaaaaaaa');
    expect(itens[0]).toHaveTextContent(new Date('2026-09-20T12:00:00Z').toLocaleDateString('pt-BR'));
    expect(itens[1]).toHaveTextContent('João Obra');
    expect(itens[1]).toHaveTextContent('Pedido bbbbbbbb');
    expect(fetchMock).toHaveBeenCalledWith('/api/produtos/p1/estoques/e1/reservas', expect.anything());
  });

  it('o diálogo de Carrinho deixa claro que NÃO trava saldo', async () => {
    stubPadrao();
    const user = userEvent.setup();
    renderPagina();
    act(() => {
      aoMudarStatus('conectado');
    });
    await screen.findByText('Almoxarifado Central');

    await user.click(screen.getAllByRole('button', { name: /Adicionar ao Carrinho/ })[0]);
    const dialogo = await screen.findByRole('dialog');
    expect(within(dialogo).getByText(/não trava saldo/)).toBeInTheDocument();
  });

  const DUAS_RESERVAS = {
    ...PRODUTO_DETALHE,
    quantidadeReservada: 6,
    quantidadeDisponivel: 2,
    porEstoque: [
      { estoqueId: 'e1', estoqueNome: 'Almoxarifado Central', quantidade: 5, reservada: 3, disponivel: 2 },
      { estoqueId: 'e2', estoqueNome: 'Obra Norte', quantidade: 3, reservada: 3, disponivel: 0 },
    ],
  };

  it('resposta lenta de um Estoque anterior nunca sobrescreve o diálogo do Estoque novo (dados)', async () => {
    type Resp = { ok: boolean; status?: number; json: () => Promise<unknown> };
    const pendentes: Record<string, (r: Resp) => void> = {};
    stubFetch((url) => {
      if (url === '/api/produtos/p1') return jsonOk({ produto: DUAS_RESERVAS });
      if (url === '/api/produtos/p1/fotos') return jsonOk({ fotos: [] });
      const m = /\/estoques\/([^/]+)\/reservas$/.exec(url);
      if (m) {
        return new Promise<Resp>((resolve) => {
          pendentes[m[1]] = resolve;
        });
      }
      throw new Error(`URL inesperada: ${url}`);
    });
    const user = userEvent.setup();
    renderPagina();
    act(() => {
      aoMudarStatus('conectado');
    });
    await screen.findByText('Almoxarifado Central');

    // Abre e2, fecha; abre e1 (novo alvo) e responde e1 ANTES da resposta lenta de e2.
    await user.click(screen.getByRole('button', { name: /ver pedidos em Obra Norte/ }));
    await screen.findByRole('dialog');
    await user.keyboard('{Escape}');
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    await user.click(screen.getByRole('button', { name: /ver pedidos em Almoxarifado Central/ }));
    await waitFor(() => expect(pendentes.e1).toBeDefined());
    await act(async () => {
      pendentes.e1({ ok: true, status: 200, json: async () => ({ reservas: [reserva('11111111-0000', 'Nova Pessoa')] }) });
    });
    await screen.findByText('Nova Pessoa');

    // Resposta de e2 chega depois, com dados e depois como erro: ignorada.
    await act(async () => {
      pendentes.e2({ ok: true, status: 200, json: async () => ({ reservas: [reserva('22222222-0000', 'Pessoa Velha')] }) });
    });
    expect(screen.queryByText('Pessoa Velha')).not.toBeInTheDocument();
    expect(screen.getByText('Nova Pessoa')).toBeInTheDocument();
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('erro tardio de um Estoque anterior não sobrescreve a lista do Estoque novo', async () => {
    type Resp = { ok: boolean; status?: number; json: () => Promise<unknown> };
    const pendentes: Record<string, (r: Resp) => void> = {};
    stubFetch((url) => {
      if (url === '/api/produtos/p1') return jsonOk({ produto: DUAS_RESERVAS });
      if (url === '/api/produtos/p1/fotos') return jsonOk({ fotos: [] });
      const m = /\/estoques\/([^/]+)\/reservas$/.exec(url);
      if (m) {
        return new Promise<Resp>((resolve) => {
          pendentes[m[1]] = resolve;
        });
      }
      throw new Error(`URL inesperada: ${url}`);
    });
    const user = userEvent.setup();
    renderPagina();
    act(() => {
      aoMudarStatus('conectado');
    });
    await screen.findByText('Almoxarifado Central');

    await user.click(screen.getByRole('button', { name: /ver pedidos em Obra Norte/ }));
    await screen.findByRole('dialog');
    await user.keyboard('{Escape}');
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    await user.click(screen.getByRole('button', { name: /ver pedidos em Almoxarifado Central/ }));
    await waitFor(() => expect(pendentes.e1).toBeDefined());
    await act(async () => {
      pendentes.e1({ ok: true, status: 200, json: async () => ({ reservas: [reserva('11111111-0000', 'Nova Pessoa')] }) });
    });
    await screen.findByText('Nova Pessoa');

    await act(async () => {
      pendentes.e2({ ok: false, status: 500, json: async () => ({}) });
    });
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    expect(screen.getByText('Nova Pessoa')).toBeInTheDocument();
  });

  it('recarregar o detalhe (evento SSE) com o diálogo aberto refaz a lista do Estoque aberto', async () => {
    let produtoAtual: unknown = DUAS_RESERVAS;
    let reservasAtuais = [reserva('aaaaaaaa-0000', 'Maria Antes')];
    stubFetch((url) => {
      if (url === '/api/produtos/p1') return jsonOk({ produto: produtoAtual });
      if (url === '/api/produtos/p1/fotos') return jsonOk({ fotos: [] });
      if (url === '/api/produtos/p1/estoques/e1/reservas') return jsonOk({ reservas: reservasAtuais });
      throw new Error(`URL inesperada: ${url}`);
    });
    const user = userEvent.setup();
    renderPagina();
    act(() => {
      aoMudarStatus('conectado');
    });
    await screen.findByText('Almoxarifado Central');
    await user.click(screen.getByRole('button', { name: /ver pedidos em Almoxarifado Central/ }));
    await screen.findByText('Maria Antes');

    reservasAtuais = [reserva('bbbbbbbb-0000', 'João Depois')];
    produtoAtual = {
      ...DUAS_RESERVAS,
      porEstoque: [
        { estoqueId: 'e1', estoqueNome: 'Almoxarifado Central', quantidade: 5, reservada: 3, disponivel: 2 },
        DUAS_RESERVAS.porEstoque[1],
      ],
    };
    act(() => {
      aoReceberEvento({ resource: 'produtos', id: 'p1', change: 'updated' });
    });

    await screen.findByText('João Depois');
    expect(screen.queryByText('Maria Antes')).not.toBeInTheDocument();
    expect(screen.getByRole('dialog')).toBeInTheDocument();
  });

  it('recarregar o detalhe sem reserva no Estoque aberto mostra a lista vazia; Estoque removido fecha o diálogo', async () => {
    let produtoAtual: unknown = DUAS_RESERVAS;
    let reservasAtuais = [reserva('aaaaaaaa-0000', 'Maria Antes')];
    stubFetch((url) => {
      if (url === '/api/produtos/p1') return jsonOk({ produto: produtoAtual });
      if (url === '/api/produtos/p1/fotos') return jsonOk({ fotos: [] });
      if (url === '/api/produtos/p1/estoques/e1/reservas') return jsonOk({ reservas: reservasAtuais });
      throw new Error(`URL inesperada: ${url}`);
    });
    const user = userEvent.setup();
    renderPagina();
    act(() => {
      aoMudarStatus('conectado');
    });
    await screen.findByText('Almoxarifado Central');
    await user.click(screen.getByRole('button', { name: /ver pedidos em Almoxarifado Central/ }));
    await screen.findByText('Maria Antes');

    // Pedido decidido: reserva some.
    reservasAtuais = [];
    produtoAtual = {
      ...DUAS_RESERVAS,
      porEstoque: [
        { estoqueId: 'e1', estoqueNome: 'Almoxarifado Central', quantidade: 5, reservada: 0, disponivel: 5 },
        DUAS_RESERVAS.porEstoque[1],
      ],
    };
    act(() => {
      aoReceberEvento({ resource: 'produtos', id: 'p1', change: 'updated' });
    });
    await screen.findByText('Nenhum pedido reserva este saldo.');

    // Estoque some do detalhe: diálogo fecha.
    produtoAtual = { ...DUAS_RESERVAS, porEstoque: [DUAS_RESERVAS.porEstoque[1]] };
    act(() => {
      aoReceberEvento({ resource: 'produtos', id: 'p1', change: 'updated' });
    });
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
  });
});

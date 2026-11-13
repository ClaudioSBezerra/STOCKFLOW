import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { act, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { AprovacaoLotePage } from './AprovacaoLotePage';
import type { EventoRealtime, StatusRealtime } from '@/lib/realtime/client';

// useAuth() fornece o papel — configurável por teste (molde de
// EstoquesPage.test.tsx).
const authState = vi.hoisted(() => ({ papel: 'gestor' as string }));

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

const toastSuccess = vi.hoisted(() => vi.fn());
vi.mock('sonner', () => ({ toast: { success: toastSuccess, error: vi.fn(), info: vi.fn() } }));

const listarFilaPedidosMock = vi.hoisted(() => vi.fn());
const buscarPedidoMock = vi.hoisted(() => vi.fn());
const decidirLotePedidosMock = vi.hoisted(() => vi.fn());
vi.mock('@/lib/pedidos', () => ({
  listarFilaPedidos: listarFilaPedidosMock,
  buscarPedido: buscarPedidoMock,
  decidirLotePedidos: decidirLotePedidosMock,
}));

// conectarRealtime mockado (molde de FilaPedidosSection.test.tsx): captura
// os dois callbacks para disparar cada cenário direto.
const conectarRealtimeMock = vi.hoisted(() => vi.fn());
vi.mock('@/lib/realtime/client', () => ({
  conectarRealtime: conectarRealtimeMock,
}));

let aoMudarStatus: (status: StatusRealtime) => void;
let aoReceberEvento: (evento: EventoRealtime) => void;
const desconectarMock = vi.fn();

const PENDENTES = [
  {
    id: 'p-transf',
    usuarioId: 'u-1',
    solicitante: 'Ana Silva',
    obraCentroCusto: 'Obra Norte',
    observacao: null,
    status: 'pendente',
    criadoEm: '2026-09-02T12:00:00Z',
    tipo: 'transferencia',
    qtdItens: 1,
  },
  {
    id: 'p-baixa',
    usuarioId: 'u-2',
    solicitante: 'Bruno Costa',
    obraCentroCusto: 'Obra Sul',
    observacao: null,
    status: 'pendente',
    criadoEm: '2026-09-01T09:00:00Z',
    tipo: 'baixa',
    qtdItens: 1,
  },
  // Pedido de retirada pendente — NUNCA deve aparecer nesta tela (filtro
  // client-side por tipo, Code Map de spec-20-3).
  {
    id: 'p-retirada',
    usuarioId: 'u-3',
    solicitante: 'Carla Souza',
    obraCentroCusto: 'Obra Leste',
    observacao: null,
    status: 'pendente',
    criadoEm: '2026-09-03T09:00:00Z',
    tipo: 'retirada',
    qtdItens: 2,
  },
];

function detalheDe(id: string) {
  const resumo = PENDENTES.find((p) => p.id === id);
  return {
    ...resumo,
    itens: [
      {
        produtoId: 'prod-' + id,
        produtoNome: 'Produto ' + id,
        categoriaNome: 'Categoria X',
        estoqueId: 'est-' + id,
        estoqueNome: 'Estoque ' + id,
        quantidade: 5,
        quantidadeAprovada: null,
        produtoCodigo: null,
      },
    ],
  };
}

beforeEach(() => {
  authState.papel = 'gestor';
  conectarRealtimeMock.mockImplementation(
    (receber: (evento: EventoRealtime) => void, mudar: (status: StatusRealtime) => void) => {
      aoReceberEvento = receber;
      aoMudarStatus = mudar;
      return desconectarMock;
    },
  );
  listarFilaPedidosMock.mockResolvedValue(PENDENTES);
  buscarPedidoMock.mockImplementation((id: string) => Promise.resolve(detalheDe(id)));
  decidirLotePedidosMock.mockResolvedValue([
    { ...detalheDe('p-transf'), status: 'aprovado' },
    { ...detalheDe('p-baixa'), status: 'aprovado' },
  ]);
});

afterEach(() => {
  vi.clearAllMocks();
});

async function montarEConectar() {
  render(<AprovacaoLotePage />);
  act(() => {
    aoMudarStatus('conectado');
  });
  await screen.findByText('Ana Silva');
}

describe('AprovacaoLotePage', () => {
  it('gate: papel abaixo de gestor não vê a tela', () => {
    authState.papel = 'almoxarife';
    render(<AprovacaoLotePage />);
    expect(screen.getByText('Você não tem acesso à Aprovação em lote.')).toBeInTheDocument();
    expect(listarFilaPedidosMock).not.toHaveBeenCalled();
  });

  it('gestor vê a lista de pendentes de Transferência e Baixa, SEM o de retirada', async () => {
    await montarEConectar();

    expect(listarFilaPedidosMock).toHaveBeenCalledWith('pendente');
    expect(screen.getByText('Ana Silva')).toBeInTheDocument();
    expect(screen.getByText('Bruno Costa')).toBeInTheDocument();
    expect(screen.queryByText('Carla Souza')).not.toBeInTheDocument();
  });

  it('mostra Produto/Quantidade/Estoque de cada pendente (buscados do detalhe)', async () => {
    await montarEConectar();

    await screen.findByText('Produto p-transf');
    expect(screen.getByText('Estoque p-transf')).toBeInTheDocument();
    expect(screen.getByText('Produto p-baixa')).toBeInTheDocument();
  });

  it('"Selecionar todos" marca todas as linhas; os botões de ação mostram a contagem', async () => {
    const user = userEvent.setup();
    await montarEConectar();

    expect(screen.getByRole('button', { name: /Aprovar selecionados \(0\)/ })).toBeDisabled();

    await user.click(screen.getByRole('checkbox', { name: 'Selecionar todos' }));

    expect(screen.getByRole('button', { name: /Aprovar selecionados \(2\)/ })).toBeEnabled();
    expect(screen.getByRole('button', { name: /Rejeitar selecionados \(2\)/ })).toBeEnabled();
  });

  it('aprovar o lote selecionado chama decidirLotePedidos com os ids certos e aprovar=true', async () => {
    const user = userEvent.setup();
    await montarEConectar();

    await user.click(screen.getByRole('checkbox', { name: 'Selecionar todos' }));
    await user.click(screen.getByRole('button', { name: /Aprovar selecionados/ }));
    await user.click(await screen.findByRole('button', { name: 'Aprovar' }));

    await waitFor(() => expect(decidirLotePedidosMock).toHaveBeenCalledWith(['p-transf', 'p-baixa'], true));
    expect(toastSuccess).toHaveBeenCalled();
  });

  it('rejeitar o lote selecionado chama decidirLotePedidos com aprovar=false', async () => {
    const user = userEvent.setup();
    await montarEConectar();

    await user.click(
      screen.getByRole('checkbox', { name: /Selecionar pedido de Bruno Costa/ }),
    );
    await user.click(screen.getByRole('button', { name: /Rejeitar selecionados/ }));
    await user.click(await screen.findByRole('button', { name: 'Rejeitar' }));

    await waitFor(() => expect(decidirLotePedidosMock).toHaveBeenCalledWith(['p-baixa'], false));
  });

  it('erro do servidor na decisão mostra a mensagem, sem travar a tela', async () => {
    decidirLotePedidosMock.mockRejectedValue(
      new Error('o lote só pode conter Pedidos de Transferência ou de Baixa'),
    );
    const user = userEvent.setup();
    await montarEConectar();

    await user.click(screen.getByRole('checkbox', { name: 'Selecionar todos' }));
    await user.click(screen.getByRole('button', { name: /Aprovar selecionados/ }));
    await user.click(await screen.findByRole('button', { name: 'Aprovar' }));

    expect(
      await screen.findByText('o lote só pode conter Pedidos de Transferência ou de Baixa'),
    ).toBeInTheDocument();
  });

  it('lista vazia mostra a mensagem de vazio', async () => {
    listarFilaPedidosMock.mockResolvedValue([]);
    render(<AprovacaoLotePage />);
    act(() => {
      aoMudarStatus('conectado');
    });

    expect(
      await screen.findByText('Nenhum Pedido de Transferência ou de Baixa pendente.'),
    ).toBeInTheDocument();
  });

  it('evento SSE do canal pedidos recarrega a lista', async () => {
    await montarEConectar();
    listarFilaPedidosMock.mockClear();

    act(() => {
      aoReceberEvento({ resource: 'pedidos', id: 'x', change: 'updated' });
    });

    await waitFor(() => expect(listarFilaPedidosMock).toHaveBeenCalled());
  });

  // --- Revisão de código (achado #6): buscarPedido falhando por linha -------
  //
  // Antes desta revisão, `.catch(() => null)` engolia o erro: a linha ficava
  // "Carregando..." para sempre, sem erro visível, com o checkbox continuando
  // marcável/aprovável mesmo sem o Gestor nunca ter visto o conteúdo.

  it('linha cujo buscarPedido falha mostra erro visível, com checkbox desabilitado e nunca contada em "Selecionar todos"', async () => {
    buscarPedidoMock.mockImplementation((id: string) => {
      if (id === 'p-baixa') return Promise.reject(new Error('falhou'));
      return Promise.resolve(detalheDe(id));
    });
    const user = userEvent.setup();
    await montarEConectar();

    // Erro visível na linha de Bruno Costa (p-baixa), nunca "Carregando..."
    // infinito.
    await screen.findByText('Não foi possível carregar os detalhes deste pedido agora.');

    const checkboxBruno = screen.getByRole('checkbox', {
      name: /Selecionar pedido de Bruno Costa/,
    });
    expect(checkboxBruno).toBeDisabled();
    expect(checkboxBruno).not.toBeChecked();

    // "Selecionar todos" marca só Ana (Bruno tem erro, fica de fora da
    // contagem e nunca é marcado).
    await user.click(screen.getByRole('checkbox', { name: 'Selecionar todos' }));
    expect(screen.getByRole('button', { name: /Aprovar selecionados \(1\)/ })).toBeInTheDocument();
    expect(checkboxBruno).not.toBeChecked();
  });

  it('linha selecionada enquanto ainda carrega desabilita os botões de decisão até resolver (sucesso ou erro)', async () => {
    let resolverDetalheTransf: (valor: ReturnType<typeof detalheDe>) => void = () => {};
    const detalheTransfPromise = new Promise<ReturnType<typeof detalheDe>>((resolve) => {
      resolverDetalheTransf = resolve;
    });
    buscarPedidoMock.mockImplementation((id: string) => {
      if (id === 'p-transf') return detalheTransfPromise;
      return Promise.resolve(detalheDe(id));
    });
    const user = userEvent.setup();
    await montarEConectar();

    // Seleciona Ana (p-transf) ANTES do detalhe dela resolver.
    await user.click(
      screen.getByRole('checkbox', { name: /Selecionar pedido de Ana Silva/ }),
    );
    expect(screen.getByRole('button', { name: /Aprovar selecionados \(1\)/ })).toBeDisabled();
    expect(screen.getByRole('button', { name: /Rejeitar selecionados \(1\)/ })).toBeDisabled();

    // Resolve o detalhe -> os botões liberam.
    act(() => {
      resolverDetalheTransf(detalheDe('p-transf'));
    });
    await screen.findByText('Produto p-transf');
    await waitFor(() =>
      expect(screen.getByRole('button', { name: /Aprovar selecionados \(1\)/ })).toBeEnabled(),
    );
    expect(screen.getByRole('button', { name: /Rejeitar selecionados \(1\)/ })).toBeEnabled();
  });
});

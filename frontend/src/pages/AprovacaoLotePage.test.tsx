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
const toastError = vi.hoisted(() => vi.fn());
vi.mock('sonner', () => ({ toast: { success: toastSuccess, error: toastError, info: vi.fn() } }));

const listarFilaPedidosMock = vi.hoisted(() => vi.fn());
const buscarPedidoMock = vi.hoisted(() => vi.fn());
const decidirLotePedidosMock = vi.hoisted(() => vi.fn());
const baixarPreNotaPDFMock = vi.hoisted(() => vi.fn());
const baixarTermoBaixaPDFMock = vi.hoisted(() => vi.fn());
vi.mock('@/lib/pedidos', () => ({
  listarFilaPedidos: listarFilaPedidosMock,
  buscarPedido: buscarPedidoMock,
  decidirLotePedidos: decidirLotePedidosMock,
  baixarPreNotaPDF: baixarPreNotaPDFMock,
  baixarTermoBaixaPDF: baixarTermoBaixaPDFMock,
  MENSAGEM_ERRO_TERMO_BAIXA: 'Não foi possível baixar o Termo de Baixa agora. Tente novamente em instantes.',
  MENSAGEM_ERRO_PRE_NOTA: 'Não foi possível baixar a Pré-Nota agora. Tente novamente em instantes.',
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
    observacao: 'Furto no galpão 3, BO 123',
    motivoSaida: 'perda_roubo',
    status: 'pendente',
    criadoEm: '2026-09-01T09:00:00Z',
    tipo: 'baixa',
    qtdItens: 1,
  },
  {
    id: 'p-baixa2',
    usuarioId: 'u-4',
    solicitante: 'Dora Lima',
    obraCentroCusto: '',
    observacao: 'Aplicado na laje do bloco B',
    motivoSaida: 'utilizacao_obra',
    status: 'pendente',
    criadoEm: '2026-09-01T10:00:00Z',
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
  decidirLotePedidosMock.mockResolvedValue([{ ...detalheDe('p-transf'), status: 'aprovado' }]);
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

async function irParaOutrasSaidas(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole('tab', { name: /Outras saídas/ }));
  await screen.findByText('Bruno Costa');
}

describe('AprovacaoLotePage', () => {
  it('gate: papel abaixo de gestor não vê a tela', () => {
    authState.papel = 'almoxarife';
    render(<AprovacaoLotePage />);
    expect(screen.getByText('Você não tem acesso à Aprovação em lote.')).toBeInTheDocument();
    expect(listarFilaPedidosMock).not.toHaveBeenCalled();
  });

  it('duas abas com contagem própria; cada aba lista só o seu tipo, nunca retirada', async () => {
    const user = userEvent.setup();
    await montarEConectar();

    expect(listarFilaPedidosMock).toHaveBeenCalledWith('pendente');
    expect(screen.getByRole('tab', { name: 'Transferências (1)' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Outras saídas (2)' })).toBeInTheDocument();
    // Aba Transferências (padrão): só a transferência.
    expect(screen.getByText('Ana Silva')).toBeInTheDocument();
    expect(screen.queryByText('Bruno Costa')).not.toBeInTheDocument();
    expect(screen.queryByText('Dora Lima')).not.toBeInTheDocument();
    expect(screen.queryByText('Carla Souza')).not.toBeInTheDocument();

    await irParaOutrasSaidas(user);
    expect(screen.getByText('Dora Lima')).toBeInTheDocument();
    expect(screen.queryByText('Ana Silva')).not.toBeInTheDocument();
    expect(screen.queryByText('Carla Souza')).not.toBeInTheDocument();
  });

  it('Outras saídas mostram motivo e justificativa em cada linha', async () => {
    const user = userEvent.setup();
    await montarEConectar();
    await irParaOutrasSaidas(user);

    expect(screen.getByText('Perda ou roubo')).toBeInTheDocument();
    expect(screen.getByText('Furto no galpão 3, BO 123')).toBeInTheDocument();
    expect(screen.getByText('Utilização na obra')).toBeInTheDocument();
    expect(screen.getByText('Aplicado na laje do bloco B')).toBeInTheDocument();
  });

  it('mostra Produto/Quantidade/Estoque de cada pendente (buscados do detalhe)', async () => {
    const user = userEvent.setup();
    await montarEConectar();

    await screen.findByText('Produto p-transf');
    expect(screen.getByText('Estoque p-transf')).toBeInTheDocument();
    await irParaOutrasSaidas(user);
    expect(screen.getByText('Produto p-baixa')).toBeInTheDocument();
  });

  it('"Selecionar todos" marca só as linhas da aba; os botões de ação mostram a contagem', async () => {
    const user = userEvent.setup();
    await montarEConectar();

    expect(screen.getByRole('button', { name: /Aprovar selecionados \(0\)/ })).toBeDisabled();
    await user.click(screen.getByRole('checkbox', { name: 'Selecionar todos' }));
    expect(screen.getByRole('button', { name: /Aprovar selecionados \(1\)/ })).toBeEnabled();

    await irParaOutrasSaidas(user);
    // Trocar de aba limpou a seleção.
    expect(screen.getByRole('button', { name: /Aprovar selecionados \(0\)/ })).toBeDisabled();
    await user.click(screen.getByRole('checkbox', { name: 'Selecionar todos' }));
    expect(screen.getByRole('button', { name: /Aprovar selecionados \(2\)/ })).toBeEnabled();
    expect(screen.getByRole('button', { name: /Rejeitar selecionados \(2\)/ })).toBeEnabled();
  });

  it('trocar de aba limpa a seleção: um lote nunca mistura tipos', async () => {
    const user = userEvent.setup();
    await montarEConectar();

    await user.click(screen.getByRole('checkbox', { name: /Selecionar pedido de Ana Silva/ }));
    await irParaOutrasSaidas(user);
    await user.click(screen.getByRole('checkbox', { name: /Selecionar pedido de Bruno Costa/ }));
    await user.click(screen.getByRole('button', { name: /Aprovar selecionados/ }));
    await user.click(await screen.findByRole('button', { name: 'Aprovar' }));

    await waitFor(() => expect(decidirLotePedidosMock).toHaveBeenCalledWith(['p-baixa'], true));
  });

  it('aprovar o lote selecionado chama decidirLotePedidos com os ids certos e aprovar=true', async () => {
    const user = userEvent.setup();
    await montarEConectar();

    await user.click(screen.getByRole('checkbox', { name: 'Selecionar todos' }));
    await user.click(screen.getByRole('button', { name: /Aprovar selecionados/ }));
    await user.click(await screen.findByRole('button', { name: 'Aprovar' }));

    await waitFor(() => expect(decidirLotePedidosMock).toHaveBeenCalledWith(['p-transf'], true));
    expect(toastSuccess).toHaveBeenCalled();
  });

  it('rejeitar o lote selecionado chama decidirLotePedidos com aprovar=false', async () => {
    const user = userEvent.setup();
    await montarEConectar();
    await irParaOutrasSaidas(user);

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
      await screen.findByText('Nenhum Pedido de Transferência pendente.'),
    ).toBeInTheDocument();
    await userEvent.setup().click(screen.getByRole('tab', { name: /Outras saídas/ }));
    expect(await screen.findByText('Nenhum Pedido de Outras Saídas pendente.')).toBeInTheDocument();
  });

  it('carrega a lista na montagem, sem esperar o SSE; 1ª conexão não duplica; reconexão recarrega', async () => {
    render(<AprovacaoLotePage />);
    await screen.findByText('Ana Silva');
    expect(listarFilaPedidosMock).toHaveBeenCalledTimes(1);

    act(() => {
      aoMudarStatus('conectado');
    });
    expect(listarFilaPedidosMock).toHaveBeenCalledTimes(1);

    act(() => {
      aoMudarStatus('reconectando');
    });
    act(() => {
      aoMudarStatus('conectado');
    });
    await waitFor(() => expect(listarFilaPedidosMock).toHaveBeenCalledTimes(2));
  });

  it('falha na carga inicial mostra o erro sem depender do SSE', async () => {
    listarFilaPedidosMock.mockRejectedValue(new Error('sessão expirada'));
    render(<AprovacaoLotePage />);
    expect(await screen.findByRole('alert')).toHaveTextContent('sessão expirada');
  });

  it('1ª conexão recarrega quando a carga inicial falhou', async () => {
    listarFilaPedidosMock.mockRejectedValueOnce(new Error('falha inicial'));
    render(<AprovacaoLotePage />);
    expect(await screen.findByRole('alert')).toHaveTextContent('falha inicial');

    act(() => {
      aoMudarStatus('conectado');
    });

    await waitFor(() => expect(listarFilaPedidosMock).toHaveBeenCalledTimes(2));
    expect(await screen.findByText('Ana Silva')).toBeInTheDocument();
  });

  it('1ª conexão lenta (>2s desde a montagem) recarrega mesmo com a carga ok', async () => {
    vi.useFakeTimers({ toFake: ['Date'] });
    try {
      render(<AprovacaoLotePage />);
      expect(await screen.findByText('Ana Silva')).toBeInTheDocument();
      expect(listarFilaPedidosMock).toHaveBeenCalledTimes(1);

      vi.setSystemTime(Date.now() + 2500);
      act(() => {
        aoMudarStatus('conectado');
      });

      await waitFor(() => expect(listarFilaPedidosMock).toHaveBeenCalledTimes(2));
    } finally {
      vi.useRealTimers();
    }
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
    await irParaOutrasSaidas(user);

    // Erro visível na linha de Bruno Costa (p-baixa), nunca "Carregando..."
    // infinito.
    await screen.findByText('Não foi possível carregar os detalhes deste pedido agora.');

    const checkboxBruno = screen.getByRole('checkbox', {
      name: /Selecionar pedido de Bruno Costa/,
    });
    expect(checkboxBruno).toBeDisabled();
    expect(checkboxBruno).not.toBeChecked();

    // "Selecionar todos" marca só Dora (Bruno tem erro, fica de fora da
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

// --- Story 20.5 (spec-20-5): Pré-Nota no resultado do lote -----------------
describe('AprovacaoLotePage — Pré-Nota (Story 20.5)', () => {
  // jsdom não implementa URL.createObjectURL/revokeObjectURL: substituídos
  // aqui e SEMPRE restaurados no afterEach (mesmo se o teste falhar).
  const createOriginal = URL.createObjectURL;
  const revokeOriginal = URL.revokeObjectURL;
  let cliqueSpy: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    URL.createObjectURL = vi.fn(() => 'blob:pre-nota-teste');
    URL.revokeObjectURL = vi.fn();
    cliqueSpy = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {});
  });

  afterEach(() => {
    cliqueSpy.mockRestore();
    URL.createObjectURL = createOriginal;
    URL.revokeObjectURL = revokeOriginal;
  });

  const LOTE_COM_PRE_NOTA = () => [
    { ...detalheDe('p-transf'), status: 'aprovado', preNotaId: 'pn-7', docnum: 7 },
  ];

  async function aprovarTodos(user: ReturnType<typeof userEvent.setup>) {
    await user.click(screen.getByRole('checkbox', { name: 'Selecionar todos' }));
    await user.click(screen.getByRole('button', { name: /Aprovar selecionados/ }));
    await user.click(await screen.findByRole('button', { name: 'Aprovar' }));
  }

  it('lote aprovado com Pré-Nota mostra "Pré-Nota nº N" e baixa pre-nota-7.pdf sem sair da página', async () => {
    decidirLotePedidosMock.mockResolvedValue(LOTE_COM_PRE_NOTA());
    const blob = new Blob(['%PDF-fake'], { type: 'application/pdf' });
    baixarPreNotaPDFMock.mockResolvedValue(blob);
    const user = userEvent.setup();
    await montarEConectar();

    await aprovarTodos(user);

    expect(await screen.findByText('Pré-Nota nº 7 gerada.')).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Baixar Pré-Nota' }));

    await waitFor(() => expect(baixarPreNotaPDFMock).toHaveBeenCalledWith('pn-7'));
    await waitFor(() => expect(cliqueSpy).toHaveBeenCalled());
    const link = cliqueSpy.mock.contexts[0] as HTMLAnchorElement;
    expect(link.download).toBe('pre-nota-7.pdf');
    expect(URL.createObjectURL).toHaveBeenCalledWith(blob);
    await waitFor(() => expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:pre-nota-teste'));
    expect(toastError).not.toHaveBeenCalled();
    // Continua na tela da Aprovação em lote.
    expect(screen.getByRole('heading', { name: 'Aprovação em lote' })).toBeInTheDocument();
  });

  it('decisão seguinte que FALHA mantém o link da Pré-Nota anterior', async () => {
    decidirLotePedidosMock.mockResolvedValueOnce(LOTE_COM_PRE_NOTA());
    const user = userEvent.setup();
    await montarEConectar();

    await aprovarTodos(user);
    expect(await screen.findByText('Pré-Nota nº 7 gerada.')).toBeInTheDocument();

    decidirLotePedidosMock.mockRejectedValueOnce(new Error('falha no segundo lote'));
    await waitFor(() =>
      expect(screen.getByRole('button', { name: /Aprovar selecionados \(0\)/ })).toBeInTheDocument(),
    );
    await aprovarTodos(user);

    expect(await screen.findByText('falha no segundo lote')).toBeInTheDocument();
    expect(screen.getByText('Pré-Nota nº 7 gerada.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Baixar Pré-Nota' })).toBeInTheDocument();
  });

  it('lote rejeitado (sem Pré-Nota) não mostra Pré-Nota nem botão de download', async () => {
    decidirLotePedidosMock.mockResolvedValue([
      { ...detalheDe('p-baixa'), status: 'rejeitado', preNotaId: null, docnum: null },
    ]);
    const user = userEvent.setup();
    await montarEConectar();
    await irParaOutrasSaidas(user);

    await user.click(
      screen.getByRole('checkbox', { name: /Selecionar pedido de Bruno Costa/ }),
    );
    await user.click(screen.getByRole('button', { name: /Rejeitar selecionados/ }));
    await user.click(await screen.findByRole('button', { name: 'Rejeitar' }));

    await waitFor(() => expect(toastSuccess).toHaveBeenCalled());
    expect(screen.queryByText(/Pré-Nota nº/)).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Baixar Pré-Nota' })).not.toBeInTheDocument();
  });
});

// --- spec-pedido-outras-saidas: Termo de Baixa no resultado do lote ----------
describe('AprovacaoLotePage — Termo de Baixa', () => {
  const createOriginal = URL.createObjectURL;
  const revokeOriginal = URL.revokeObjectURL;
  let cliqueSpy: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    URL.createObjectURL = vi.fn(() => 'blob:termo-teste');
    URL.revokeObjectURL = vi.fn();
    cliqueSpy = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {});
  });

  afterEach(() => {
    cliqueSpy.mockRestore();
    URL.createObjectURL = createOriginal;
    URL.revokeObjectURL = revokeOriginal;
  });

  const LOTE_COM_TERMOS = () => [
    { ...detalheDe('p-baixa'), status: 'aprovado', termoBaixaId: 'tb-1', numeroTermo: 3, preNotaId: null },
    { ...detalheDe('p-baixa2'), status: 'aprovado', termoBaixaId: 'tb-2', numeroTermo: 4, preNotaId: null },
  ];

  it('Outras saídas aprovadas mostram um botão "Termo de Baixa nº N" por documento e baixam o PDF', async () => {
    decidirLotePedidosMock.mockResolvedValue(LOTE_COM_TERMOS());
    const blob = new Blob(['%PDF-fake'], { type: 'application/pdf' });
    baixarTermoBaixaPDFMock.mockResolvedValue(blob);
    const user = userEvent.setup();
    await montarEConectar();
    await irParaOutrasSaidas(user);

    await user.click(screen.getByRole('checkbox', { name: 'Selecionar todos' }));
    await user.click(screen.getByRole('button', { name: /Aprovar selecionados/ }));
    await user.click(await screen.findByRole('button', { name: 'Aprovar' }));

    expect(await screen.findByText('2 Termos de Baixa gerados (um por motivo).')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Baixar Termo de Baixa nº 4' })).toBeInTheDocument();
    expect(screen.queryByText(/Pré-Nota nº/)).not.toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Baixar Termo de Baixa nº 3' }));
    await waitFor(() => expect(baixarTermoBaixaPDFMock).toHaveBeenCalledWith('tb-1'));
    await waitFor(() => expect(cliqueSpy).toHaveBeenCalled());
    expect((cliqueSpy.mock.contexts[0] as HTMLAnchorElement).download).toBe('termo-baixa-3.pdf');
    expect(toastError).not.toHaveBeenCalled();
  });

  it('os botões de Termo saem em ordem crescente de número, mesmo que a resposta venha fora de ordem', async () => {
    decidirLotePedidosMock.mockResolvedValue([
      { ...detalheDe('p-baixa'), status: 'aprovado', termoBaixaId: 'tb-2', numeroTermo: 2, preNotaId: null },
      { ...detalheDe('p-baixa2'), status: 'aprovado', termoBaixaId: 'tb-1', numeroTermo: 1, preNotaId: null },
      { ...detalheDe('p-baixa'), status: 'aprovado', termoBaixaId: 'tb-3', numeroTermo: 3, preNotaId: null },
    ]);
    const user = userEvent.setup();
    await montarEConectar();
    await irParaOutrasSaidas(user);
    await user.click(screen.getByRole('checkbox', { name: 'Selecionar todos' }));
    await user.click(screen.getByRole('button', { name: /Aprovar selecionados/ }));
    await user.click(await screen.findByRole('button', { name: 'Aprovar' }));
    await screen.findByText('3 Termos de Baixa gerados (um por motivo).');

    const rotulos = screen
      .getAllByRole('button', { name: /^Baixar Termo de Baixa nº/ })
      .map((b) => b.getAttribute('aria-label'));
    expect(rotulos).toEqual([
      'Baixar Termo de Baixa nº 1',
      'Baixar Termo de Baixa nº 2',
      'Baixar Termo de Baixa nº 3',
    ]);
  });

  it('o resultado de Outras saídas não aparece na aba Transferências', async () => {
    decidirLotePedidosMock.mockResolvedValue(LOTE_COM_TERMOS());
    const user = userEvent.setup();
    await montarEConectar();
    await irParaOutrasSaidas(user);
    await user.click(screen.getByRole('checkbox', { name: 'Selecionar todos' }));
    await user.click(screen.getByRole('button', { name: /Aprovar selecionados/ }));
    await user.click(await screen.findByRole('button', { name: 'Aprovar' }));
    await screen.findByText('2 Termos de Baixa gerados (um por motivo).');

    await user.click(screen.getByRole('tab', { name: /Transferências/ }));
    expect(screen.queryByText(/Termos? de Baixa gerados?/)).not.toBeInTheDocument();
  });
});

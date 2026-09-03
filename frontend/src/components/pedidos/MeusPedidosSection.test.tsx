import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { act, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MeusPedidosSection } from './MeusPedidosSection';
import type { EventoRealtime, StatusRealtime } from '@/lib/realtime/client';

const toastInfo = vi.hoisted(() => vi.fn());
const toastError = vi.hoisted(() => vi.fn());
vi.mock('sonner', () => ({ toast: { info: toastInfo, error: toastError } }));

const listarPedidosMock = vi.hoisted(() => vi.fn());
const buscarPedidoMock = vi.hoisted(() => vi.fn());
const buscarReciboPedidoBlobMock = vi.hoisted(() => vi.fn());
vi.mock('@/lib/pedidos', () => ({
  listarPedidos: listarPedidosMock,
  buscarPedido: buscarPedidoMock,
  buscarReciboPedidoBlob: buscarReciboPedidoBlobMock,
  MENSAGEM_ERRO_RECIBO: 'Não foi possível baixar o recibo agora. Tente novamente em instantes.',
}));

// conectarRealtime mockado (molde de MovimentacoesSection.test.tsx): captura
// os dois callbacks para disparar cada cenário direto.
const conectarRealtimeMock = vi.hoisted(() => vi.fn());
vi.mock('@/lib/realtime/client', () => ({
  conectarRealtime: conectarRealtimeMock,
}));

let aoReceberEvento: (evento: EventoRealtime) => void;
let aoMudarStatus: (status: StatusRealtime) => void;
const desconectarMock = vi.fn();

const PEDIDOS = [
  {
    id: 'p-1',
    usuarioId: 'u-1',
    solicitante: 'Ana Silva',
    obraCentroCusto: 'Obra Norte',
    observacao: null,
    status: 'pendente',
    criadoEm: '2026-09-02T12:00:00Z',
    qtdItens: 2,
  },
  {
    id: 'p-2',
    usuarioId: 'u-1',
    solicitante: 'Ana Silva',
    obraCentroCusto: 'Obra Sul',
    observacao: null,
    status: 'aprovado',
    criadoEm: '2026-09-01T09:00:00Z',
    qtdItens: 1,
  },
];

beforeEach(() => {
  conectarRealtimeMock.mockImplementation(
    (receber: (evento: EventoRealtime) => void, mudar: (status: StatusRealtime) => void) => {
      aoReceberEvento = receber;
      aoMudarStatus = mudar;
      return desconectarMock;
    },
  );
  listarPedidosMock.mockResolvedValue(PEDIDOS);
  buscarPedidoMock.mockResolvedValue({ ...PEDIDOS[0], itens: [] });
});

afterEach(() => {
  vi.clearAllMocks();
});

describe('MeusPedidosSection', () => {
  it('carrega a lista SÓ quando conectarRealtime chama aoMudarStatus("conectado")', async () => {
    render(<MeusPedidosSection />);

    expect(listarPedidosMock).not.toHaveBeenCalled();
    expect(screen.getByText('Carregando pedidos...')).toBeInTheDocument();

    act(() => {
      aoMudarStatus('conectado');
    });

    expect(await screen.findByText('Obra Norte')).toBeInTheDocument();
    expect(listarPedidosMock).toHaveBeenCalledWith(undefined);
    expect(screen.queryByText('Carregando pedidos...')).not.toBeInTheDocument();
  });

  it('renderiza o badge de status de cada linha com ícone + texto', async () => {
    render(<MeusPedidosSection />);
    act(() => {
      aoMudarStatus('conectado');
    });
    await screen.findByText('Obra Norte');

    const pendente = screen.getByText('Pendente');
    const aprovado = screen.getByText('Aprovado');
    expect(pendente).toBeInTheDocument();
    expect(aprovado).toBeInTheDocument();
    // "nunca só cor": cada badge tem um ícone SVG junto do texto.
    expect(pendente.closest('span')?.querySelector('svg')).toBeInTheDocument();
    expect(aprovado.closest('span')?.querySelector('svg')).toBeInTheDocument();
  });

  it('escolher um status no filtro refaz a busca com ?status=', async () => {
    render(<MeusPedidosSection />);
    act(() => {
      aoMudarStatus('conectado');
    });
    await screen.findByText('Obra Norte');

    listarPedidosMock.mockClear();
    listarPedidosMock.mockResolvedValue([PEDIDOS[1]]);

    const user = userEvent.setup();
    await user.click(screen.getByRole('combobox', { name: 'Status' }));
    await user.click(await screen.findByRole('option', { name: 'Aprovado' }));

    await waitFor(() => expect(listarPedidosMock).toHaveBeenCalledWith('aprovado'));
  });

  it('um evento SSE resource="pedidos" dispara toast + refetch, sem recarregar a tela', async () => {
    render(<MeusPedidosSection />);
    act(() => {
      aoMudarStatus('conectado');
    });
    await screen.findByText('Obra Norte');

    listarPedidosMock.mockClear();
    act(() => {
      aoReceberEvento({ resource: 'pedidos', id: 'p-9', change: 'created' });
    });

    await waitFor(() => expect(listarPedidosMock).toHaveBeenCalledTimes(1));
    expect(toastInfo).toHaveBeenCalledWith('Meus Pedidos atualizados.');
    // A tela não se desmontou: as linhas antigas continuam visíveis.
    expect(screen.getByText('Obra Norte')).toBeInTheDocument();
  });

  it('um evento SSE de outro canal é ignorado', async () => {
    render(<MeusPedidosSection />);
    act(() => {
      aoMudarStatus('conectado');
    });
    await screen.findByText('Obra Norte');

    listarPedidosMock.mockClear();
    act(() => {
      aoReceberEvento({ resource: 'movimentacoes', id: 'm-1', change: 'created' });
    });

    expect(listarPedidosMock).not.toHaveBeenCalled();
    expect(toastInfo).not.toHaveBeenCalled();
  });

  it('mostra o estado vazio orientador quando não há pedidos', async () => {
    listarPedidosMock.mockResolvedValue([]);
    render(<MeusPedidosSection />);
    act(() => {
      aoMudarStatus('conectado');
    });

    expect(await screen.findByText(/ainda não enviou nenhum pedido/i)).toBeInTheDocument();
  });

  it('mostra a mensagem de erro do servidor (role="alert") quando a busca falha', async () => {
    listarPedidosMock.mockRejectedValue(new Error('sessão expirada'));
    render(<MeusPedidosSection />);
    act(() => {
      aoMudarStatus('conectado');
    });

    const alerta = await screen.findByRole('alert');
    expect(alerta).toHaveTextContent('sessão expirada');
  });

  it('cai na mensagem genérica quando a falha não é um Error', async () => {
    listarPedidosMock.mockRejectedValue('falha crua, sem Error');
    render(<MeusPedidosSection />);
    act(() => {
      aoMudarStatus('conectado');
    });

    const alerta = await screen.findByRole('alert');
    expect(alerta).toHaveTextContent(/não foi possível carregar seus pedidos/i);
  });

  it('mostra "Reconectando..." enquanto a conexão está reconectando', async () => {
    render(<MeusPedidosSection />);
    act(() => {
      aoMudarStatus('reconectando');
    });

    const aviso = await screen.findByText('Reconectando...');
    expect(aviso).toHaveAttribute('aria-live', 'polite');
  });

  it('"Ver itens" abre o diálogo e lista os itens em snapshot do pedido', async () => {
    buscarPedidoMock.mockResolvedValue({
      ...PEDIDOS[0],
      itens: [
        {
          produtoId: 'pr-1',
          produtoNome: 'Cabo Flexível 4mm',
          categoriaNome: 'Fios e Cabos',
          estoqueId: 'e-1',
          estoqueNome: 'Almoxarifado Central',
          quantidade: 5,
        },
      ],
    });

    render(<MeusPedidosSection />);
    act(() => {
      aoMudarStatus('conectado');
    });
    await screen.findByText('Obra Norte');

    const user = userEvent.setup();
    await user.click(
      screen.getByRole('button', { name: /^Ver itens do pedido de Ana Silva — Obra Norte/ }),
    );

    expect(await screen.findByRole('dialog')).toBeInTheDocument();
    expect(await screen.findByText('Cabo Flexível 4mm')).toBeInTheDocument();
    expect(buscarPedidoMock).toHaveBeenCalledWith('p-1');
  });

  it('mostra a mensagem de erro do servidor (role="alert") no diálogo quando buscarPedido falha', async () => {
    buscarPedidoMock.mockRejectedValue(new Error('pedido não encontrado'));

    render(<MeusPedidosSection />);
    act(() => {
      aoMudarStatus('conectado');
    });
    await screen.findByText('Obra Norte');

    const user = userEvent.setup();
    await user.click(
      screen.getByRole('button', { name: /^Ver itens do pedido de Ana Silva — Obra Norte/ }),
    );

    const alerta = await screen.findByRole('alert');
    expect(alerta).toHaveTextContent('pedido não encontrado');
    expect(screen.queryByText('Carregando itens...')).not.toBeInTheDocument();
  });

  it('cai na mensagem genérica de itens quando a falha de buscarPedido não é um Error', async () => {
    buscarPedidoMock.mockRejectedValue('falha crua, sem Error');

    render(<MeusPedidosSection />);
    act(() => {
      aoMudarStatus('conectado');
    });
    await screen.findByText('Obra Norte');

    const user = userEvent.setup();
    await user.click(
      screen.getByRole('button', { name: /^Ver itens do pedido de Ana Silva — Obra Norte/ }),
    );

    const alerta = await screen.findByRole('alert');
    expect(alerta).toHaveTextContent(/não foi possível carregar os itens/i);
  });

  it('uma resposta obsoleta de buscarPedido (Pedido A) não sobrescreve os itens já abertos do Pedido B', async () => {
    // Abrir A dispara um buscarPedido que fica pendurado (nunca resolve
    // sozinho); abrir B em seguida precisa exibir SÓ os itens de B, mesmo
    // que a resposta de A chegue depois.
    let resolverA!: (valor: unknown) => void;
    buscarPedidoMock.mockImplementationOnce(
      () => new Promise((resolve) => { resolverA = resolve; }),
    );
    buscarPedidoMock.mockResolvedValueOnce({
      ...PEDIDOS[1],
      itens: [
        {
          produtoId: 'pr-b',
          produtoNome: 'Conduíte 3/4',
          categoriaNome: 'Elétrica',
          estoqueId: 'e-2',
          estoqueNome: 'Obra Sul',
          quantidade: 3,
        },
      ],
    });

    render(<MeusPedidosSection />);
    act(() => {
      aoMudarStatus('conectado');
    });
    await screen.findByText('Obra Norte');

    const user = userEvent.setup();
    const botaoA = screen.getByRole('button', { name: /^Ver itens do pedido de Ana Silva — Obra Norte/ });
    const botaoB = screen.getByRole('button', { name: /^Ver itens do pedido de Ana Silva — Obra Sul/ });
    await user.click(botaoA); // abre A (p-1) — fica pendurado
    expect(await screen.findByRole('dialog')).toBeInTheDocument();
    await user.keyboard('{Escape}'); // fecha A ANTES da resposta chegar (overlay do Dialog bloqueia clique direto em B)
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    await user.click(botaoB); // abre B (p-2) antes de A resolver

    expect(await screen.findByText('Conduíte 3/4')).toBeInTheDocument();

    // A resposta tardia de A chega DEPOIS que B já está aberto — não pode
    // sobrescrever os itens de B nem reintroduzir o item de A no diálogo.
    act(() => {
      resolverA({
        ...PEDIDOS[0],
        itens: [
          {
            produtoId: 'pr-a',
            produtoNome: 'Cabo Flexível 4mm',
            categoriaNome: 'Fios e Cabos',
            estoqueId: 'e-1',
            estoqueNome: 'Almoxarifado Central',
            quantidade: 5,
          },
        ],
      });
    });

    await waitFor(() => expect(buscarPedidoMock).toHaveBeenCalledTimes(2));
    expect(screen.getByText('Conduíte 3/4')).toBeInTheDocument();
    expect(screen.queryByText('Cabo Flexível 4mm')).not.toBeInTheDocument();
  });

  it('uma resposta obsoleta da 1ª abertura do MESMO Pedido não sobrescreve o resultado da reabertura', async () => {
    // Abrir A dispara um buscarPedido que fica pendurado (nunca resolve
    // sozinho); fechar e reabrir o MESMO A precisa exibir só o resultado da
    // reabertura, mesmo que a resposta (com erro) da 1ª chamada chegue
    // depois — o guard não pode depender só do id do Pedido, já que id é o
    // mesmo nas duas chamadas.
    let rejeitarPrimeira!: (erro: unknown) => void;
    buscarPedidoMock.mockImplementationOnce(
      () => new Promise((_resolve, reject) => { rejeitarPrimeira = reject; }),
    );
    buscarPedidoMock.mockResolvedValueOnce({
      ...PEDIDOS[0],
      itens: [
        {
          produtoId: 'pr-a',
          produtoNome: 'Cabo Flexível 4mm',
          categoriaNome: 'Fios e Cabos',
          estoqueId: 'e-1',
          estoqueNome: 'Almoxarifado Central',
          quantidade: 5,
        },
      ],
    });

    render(<MeusPedidosSection />);
    act(() => {
      aoMudarStatus('conectado');
    });
    await screen.findByText('Obra Norte');

    const user = userEvent.setup();
    const botaoA = screen.getByRole('button', { name: /^Ver itens do pedido de Ana Silva — Obra Norte/ });
    await user.click(botaoA); // 1ª abertura de p-1 — fica pendurada
    expect(await screen.findByRole('dialog')).toBeInTheDocument();
    await user.keyboard('{Escape}'); // fecha ANTES da resposta chegar
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    await user.click(botaoA); // reabre o MESMO p-1 — a 2ª chamada resolve com sucesso

    expect(await screen.findByText('Cabo Flexível 4mm')).toBeInTheDocument();

    // A rejeição tardia da 1ª chamada chega DEPOIS que a reabertura já
    // mostrou os itens — não pode substituir o resultado por um erro.
    act(() => {
      rejeitarPrimeira(new Error('falha da 1ª chamada, já obsoleta'));
    });

    await waitFor(() => expect(buscarPedidoMock).toHaveBeenCalledTimes(2));
    expect(screen.getByText('Cabo Flexível 4mm')).toBeInTheDocument();
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('mostra a mensagem orientadora específica do filtro quando não há pedidos naquele status', async () => {
    render(<MeusPedidosSection />);
    act(() => {
      aoMudarStatus('conectado');
    });
    await screen.findByText('Obra Norte');

    listarPedidosMock.mockResolvedValue([]);
    const user = userEvent.setup();
    await user.click(screen.getByRole('combobox', { name: 'Status' }));
    await user.click(await screen.findByRole('option', { name: 'Rejeitado' }));

    expect(await screen.findByText('Nenhum pedido seu neste status.')).toBeInTheDocument();
    expect(screen.queryByText(/ainda não enviou nenhum pedido/i)).not.toBeInTheDocument();
  });

  it('voltar o filtro para "Todos" refaz a busca sem status', async () => {
    render(<MeusPedidosSection />);
    act(() => {
      aoMudarStatus('conectado');
    });
    await screen.findByText('Obra Norte');

    const user = userEvent.setup();
    await user.click(screen.getByRole('combobox', { name: 'Status' }));
    await user.click(await screen.findByRole('option', { name: 'Aprovado' }));
    await waitFor(() => expect(listarPedidosMock).toHaveBeenCalledWith('aprovado'));

    listarPedidosMock.mockClear();
    listarPedidosMock.mockResolvedValue(PEDIDOS);
    await user.click(screen.getByRole('combobox', { name: 'Status' }));
    await user.click(await screen.findByRole('option', { name: 'Todos' }));

    await waitFor(() => expect(listarPedidosMock).toHaveBeenCalledWith(undefined));
  });
});

describe('MeusPedidosSection — recibo (Story 7.6)', () => {
  // jsdom não implementa URL.createObjectURL/revokeObjectURL (mesmo padrão
  // de CatalogoListagem.test.tsx).
  beforeEach(() => {
    URL.createObjectURL = vi.fn(() => 'blob:recibo-teste');
    URL.revokeObjectURL = vi.fn();
  });

  it('"Baixar recibo" aparece só para um pedido já decidido (aprovado), nunca para um pendente', async () => {
    render(<MeusPedidosSection />);
    act(() => {
      aoMudarStatus('conectado');
    });
    await screen.findByText('Obra Norte');

    const user = userEvent.setup();
    await user.click(
      screen.getByRole('button', { name: /^Ver itens do pedido de Ana Silva — Obra Norte/ }),
    );
    await screen.findByRole('dialog');
    expect(screen.queryByRole('button', { name: 'Baixar recibo' })).not.toBeInTheDocument();
    await user.keyboard('{Escape}');
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());

    await user.click(
      screen.getByRole('button', { name: /^Ver itens do pedido de Ana Silva — Obra Sul/ }),
    );
    expect(await screen.findByRole('button', { name: 'Baixar recibo' })).toBeInTheDocument();
  });

  it('clicar em "Baixar recibo" baixa o PDF via buscarReciboPedidoBlob e cria/clica/remove o <a download>', async () => {
    const blob = new Blob(['%PDF-fake'], { type: 'application/pdf' });
    buscarReciboPedidoBlobMock.mockResolvedValue(blob);
    const cliqueSpy = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {});

    render(<MeusPedidosSection />);
    act(() => {
      aoMudarStatus('conectado');
    });
    await screen.findByText('Obra Norte');

    const user = userEvent.setup();
    await user.click(
      screen.getByRole('button', { name: /^Ver itens do pedido de Ana Silva — Obra Sul/ }),
    );
    await user.click(await screen.findByRole('button', { name: 'Baixar recibo' }));

    await waitFor(() => expect(buscarReciboPedidoBlobMock).toHaveBeenCalledWith('p-2'));
    await waitFor(() => expect(cliqueSpy).toHaveBeenCalled());
    expect(URL.createObjectURL).toHaveBeenCalledWith(blob);
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:recibo-teste');
    expect(toastError).not.toHaveBeenCalled();

    cliqueSpy.mockRestore();
  });

  it('falha de buscarReciboPedidoBlob mostra toast.error com a mensagem do servidor, sem baixar nada', async () => {
    buscarReciboPedidoBlobMock.mockRejectedValue(new Error('pedido ainda não foi decidido'));
    const cliqueSpy = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {});

    render(<MeusPedidosSection />);
    act(() => {
      aoMudarStatus('conectado');
    });
    await screen.findByText('Obra Norte');

    const user = userEvent.setup();
    await user.click(
      screen.getByRole('button', { name: /^Ver itens do pedido de Ana Silva — Obra Sul/ }),
    );
    await user.click(await screen.findByRole('button', { name: 'Baixar recibo' }));

    await waitFor(() => expect(toastError).toHaveBeenCalledWith('pedido ainda não foi decidido'));
    expect(cliqueSpy).not.toHaveBeenCalled();

    cliqueSpy.mockRestore();
  });

  it('cai na mensagem genérica de recibo quando a falha não é um Error', async () => {
    buscarReciboPedidoBlobMock.mockRejectedValue('falha crua, sem Error');

    render(<MeusPedidosSection />);
    act(() => {
      aoMudarStatus('conectado');
    });
    await screen.findByText('Obra Norte');

    const user = userEvent.setup();
    await user.click(
      screen.getByRole('button', { name: /^Ver itens do pedido de Ana Silva — Obra Sul/ }),
    );
    await user.click(await screen.findByRole('button', { name: 'Baixar recibo' }));

    await waitFor(() =>
      expect(toastError).toHaveBeenCalledWith(
        'Não foi possível baixar o recibo agora. Tente novamente em instantes.',
      ),
    );
  });
});

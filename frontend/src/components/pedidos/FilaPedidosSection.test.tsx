import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { act, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { FilaPedidosSection } from './FilaPedidosSection';
import type { EventoRealtime, StatusRealtime } from '@/lib/realtime/client';

const toastInfo = vi.hoisted(() => vi.fn());
vi.mock('sonner', () => ({ toast: { info: toastInfo } }));

const listarFilaPedidosMock = vi.hoisted(() => vi.fn());
const buscarPedidoMock = vi.hoisted(() => vi.fn());
vi.mock('@/lib/pedidos', () => ({
  listarFilaPedidos: listarFilaPedidosMock,
  buscarPedido: buscarPedidoMock,
}));

// conectarRealtime mockado (molde de MeusPedidosSection.test.tsx): captura
// os dois callbacks para disparar cada cenário direto.
const conectarRealtimeMock = vi.hoisted(() => vi.fn());
vi.mock('@/lib/realtime/client', () => ({
  conectarRealtime: conectarRealtimeMock,
}));

let aoReceberEvento: (evento: EventoRealtime) => void;
let aoMudarStatus: (status: StatusRealtime) => void;
const desconectarMock = vi.fn();

// Pedidos de DOIS solicitantes diferentes — a Fila mostra Pedidos de VÁRIOS
// usuários (Story 7.4), ao contrário de "Meus Pedidos".
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
    usuarioId: 'u-2',
    solicitante: 'Bruno Costa',
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
  listarFilaPedidosMock.mockResolvedValue(PEDIDOS);
  buscarPedidoMock.mockResolvedValue({ ...PEDIDOS[0], itens: [] });
});

afterEach(() => {
  vi.clearAllMocks();
});

describe('FilaPedidosSection', () => {
  it('carrega a lista SÓ quando conectarRealtime chama aoMudarStatus("conectado")', async () => {
    render(<FilaPedidosSection />);

    expect(listarFilaPedidosMock).not.toHaveBeenCalled();
    expect(screen.getByText('Carregando pedidos...')).toBeInTheDocument();

    act(() => {
      aoMudarStatus('conectado');
    });

    expect(await screen.findByText('Obra Norte')).toBeInTheDocument();
    expect(listarFilaPedidosMock).toHaveBeenCalledWith(undefined);
    expect(screen.queryByText('Carregando pedidos...')).not.toBeInTheDocument();
  });

  it('mostra Pedidos de VÁRIOS solicitantes', async () => {
    render(<FilaPedidosSection />);
    act(() => {
      aoMudarStatus('conectado');
    });

    expect(await screen.findByText('Ana Silva')).toBeInTheDocument();
    expect(screen.getByText('Bruno Costa')).toBeInTheDocument();
  });

  it('renderiza o badge de status de cada linha com ícone + texto', async () => {
    render(<FilaPedidosSection />);
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
    render(<FilaPedidosSection />);
    act(() => {
      aoMudarStatus('conectado');
    });
    await screen.findByText('Obra Norte');

    listarFilaPedidosMock.mockClear();
    listarFilaPedidosMock.mockResolvedValue([PEDIDOS[1]]);

    const user = userEvent.setup();
    await user.click(screen.getByRole('combobox', { name: 'Status' }));
    await user.click(await screen.findByRole('option', { name: 'Aprovado' }));

    await waitFor(() => expect(listarFilaPedidosMock).toHaveBeenCalledWith('aprovado'));
  });

  it('um evento SSE resource="pedidos" dispara toast + refetch, sem recarregar a tela', async () => {
    render(<FilaPedidosSection />);
    act(() => {
      aoMudarStatus('conectado');
    });
    await screen.findByText('Obra Norte');

    listarFilaPedidosMock.mockClear();
    act(() => {
      aoReceberEvento({ resource: 'pedidos', id: 'p-9', change: 'created' });
    });

    await waitFor(() => expect(listarFilaPedidosMock).toHaveBeenCalledTimes(1));
    expect(toastInfo).toHaveBeenCalledWith('Fila de Pedidos atualizada.');
    // A tela não se desmontou: as linhas antigas continuam visíveis.
    expect(screen.getByText('Obra Norte')).toBeInTheDocument();
  });

  it('um evento SSE de outro canal é ignorado', async () => {
    render(<FilaPedidosSection />);
    act(() => {
      aoMudarStatus('conectado');
    });
    await screen.findByText('Obra Norte');

    listarFilaPedidosMock.mockClear();
    act(() => {
      aoReceberEvento({ resource: 'movimentacoes', id: 'm-1', change: 'created' });
    });

    expect(listarFilaPedidosMock).not.toHaveBeenCalled();
    expect(toastInfo).not.toHaveBeenCalled();
  });

  it('mostra o estado vazio orientador quando não há pedidos na fila', async () => {
    listarFilaPedidosMock.mockResolvedValue([]);
    render(<FilaPedidosSection />);
    act(() => {
      aoMudarStatus('conectado');
    });

    expect(await screen.findByText('Nenhum pedido na fila.')).toBeInTheDocument();
  });

  it('mostra a mensagem de erro do servidor (role="alert") quando a busca falha', async () => {
    listarFilaPedidosMock.mockRejectedValue(new Error('sessão expirada'));
    render(<FilaPedidosSection />);
    act(() => {
      aoMudarStatus('conectado');
    });

    const alerta = await screen.findByRole('alert');
    expect(alerta).toHaveTextContent('sessão expirada');
  });

  it('cai na mensagem genérica quando a falha não é um Error', async () => {
    listarFilaPedidosMock.mockRejectedValue('falha crua, sem Error');
    render(<FilaPedidosSection />);
    act(() => {
      aoMudarStatus('conectado');
    });

    const alerta = await screen.findByRole('alert');
    expect(alerta).toHaveTextContent(/não foi possível carregar a fila de pedidos/i);
  });

  it('mostra "Reconectando..." enquanto a conexão está reconectando', async () => {
    render(<FilaPedidosSection />);
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

    render(<FilaPedidosSection />);
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

  it('"Ver itens" desambigua por solicitante — obra — data/hora entre Pedidos de usuários diferentes', async () => {
    render(<FilaPedidosSection />);
    act(() => {
      aoMudarStatus('conectado');
    });
    await screen.findByText('Obra Norte');

    expect(
      screen.getByRole('button', { name: /^Ver itens do pedido de Ana Silva — Obra Norte/ }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole('button', { name: /^Ver itens do pedido de Bruno Costa — Obra Sul/ }),
    ).toBeInTheDocument();
  });

  it('mostra a mensagem de erro do servidor (role="alert") no diálogo quando buscarPedido falha', async () => {
    buscarPedidoMock.mockRejectedValue(new Error('pedido não encontrado'));

    render(<FilaPedidosSection />);
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

    render(<FilaPedidosSection />);
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

    render(<FilaPedidosSection />);
    act(() => {
      aoMudarStatus('conectado');
    });
    await screen.findByText('Obra Norte');

    const user = userEvent.setup();
    const botaoA = screen.getByRole('button', { name: /^Ver itens do pedido de Ana Silva — Obra Norte/ });
    const botaoB = screen.getByRole('button', { name: /^Ver itens do pedido de Bruno Costa — Obra Sul/ });
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

    render(<FilaPedidosSection />);
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
    render(<FilaPedidosSection />);
    act(() => {
      aoMudarStatus('conectado');
    });
    await screen.findByText('Obra Norte');

    listarFilaPedidosMock.mockResolvedValue([]);
    const user = userEvent.setup();
    await user.click(screen.getByRole('combobox', { name: 'Status' }));
    await user.click(await screen.findByRole('option', { name: 'Rejeitado' }));

    expect(await screen.findByText('Nenhum pedido na fila neste status.')).toBeInTheDocument();
    expect(screen.queryByText('Nenhum pedido na fila.')).not.toBeInTheDocument();
  });

  it('voltar o filtro para "Todos" refaz a busca sem status', async () => {
    render(<FilaPedidosSection />);
    act(() => {
      aoMudarStatus('conectado');
    });
    await screen.findByText('Obra Norte');

    const user = userEvent.setup();
    await user.click(screen.getByRole('combobox', { name: 'Status' }));
    await user.click(await screen.findByRole('option', { name: 'Aprovado' }));
    await waitFor(() => expect(listarFilaPedidosMock).toHaveBeenCalledWith('aprovado'));

    listarFilaPedidosMock.mockClear();
    listarFilaPedidosMock.mockResolvedValue(PEDIDOS);
    await user.click(screen.getByRole('combobox', { name: 'Status' }));
    await user.click(await screen.findByRole('option', { name: 'Todos' }));

    await waitFor(() => expect(listarFilaPedidosMock).toHaveBeenCalledWith(undefined));
  });
});

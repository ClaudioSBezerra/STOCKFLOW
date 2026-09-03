import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { CarrinhoPage } from './CarrinhoPage';

const toastSuccess = vi.hoisted(() => vi.fn());
const toastError = vi.hoisted(() => vi.fn());
vi.mock('sonner', () => ({ toast: { success: toastSuccess, error: toastError, info: vi.fn() } }));

// useAuth() fornece usuario.nome como valor inicial (editável) do campo
// "Solicitante" no diálogo "Enviar Pedido" (Story 7.2).
vi.mock('@/lib/auth', () => ({
  useAuth: () => ({
    estado: 'autenticado',
    usuario: { id: 'u1', nome: 'Maria Operária', email: 'maria@empresa.com', papel: 'usuario' },
    definirSessao: vi.fn(),
    atualizarUsuario: vi.fn(),
    logout: vi.fn(),
  }),
}));

// useCarrinho() é mockado diretamente (molde de ProdutoDetalhePage.test.tsx
// e AppShell.test.tsx) — CarrinhoPage é só uma view sobre o estado global,
// a busca/limpeza em si já tem cobertura dedicada em lib/carrinho.test.tsx.
const carrinhoState = vi.hoisted(() => ({
  itens: [] as Array<{
    produtoId: string;
    produtoNome: string;
    estoqueId: string;
    estoqueNome: string;
    quantidade: number;
  }>,
  carregando: false,
  erro: false,
}));
const refreshMock = vi.hoisted(() => vi.fn());
const removerItemMock = vi.hoisted(() => vi.fn());
const enviarPedidoMock = vi.hoisted(() => vi.fn());
vi.mock('@/lib/carrinho', () => ({
  useCarrinho: () => ({
    itens: carrinhoState.itens,
    count: carrinhoState.itens.length,
    carregando: carrinhoState.carregando,
    erro: carrinhoState.erro,
    refresh: refreshMock,
    adicionarItem: vi.fn(),
    removerItem: removerItemMock,
    enviarPedido: enviarPedidoMock,
  }),
}));

const ITEM_1 = {
  produtoId: 'p1',
  produtoNome: 'Cabo Flexível 4mm',
  estoqueId: 'e1',
  estoqueNome: 'Almoxarifado Central',
  quantidade: 3,
};
const ITEM_2 = {
  produtoId: 'p2',
  produtoNome: 'Conduíte 3/4',
  estoqueId: 'e2',
  estoqueNome: 'Obra Norte',
  quantidade: 10,
};

beforeEach(() => {
  carrinhoState.itens = [];
  carrinhoState.carregando = false;
  carrinhoState.erro = false;
  refreshMock.mockReset();
  removerItemMock.mockReset();
  removerItemMock.mockResolvedValue({ ok: true });
  enviarPedidoMock.mockReset();
  enviarPedidoMock.mockResolvedValue({ ok: true });
  toastSuccess.mockClear();
  toastError.mockClear();
});

afterEach(() => {
  vi.clearAllMocks();
});

describe('CarrinhoPage', () => {
  it('chama refresh() ao montar', () => {
    render(<CarrinhoPage />);
    expect(refreshMock).toHaveBeenCalledTimes(1);
  });

  it('carrinho vazio mostra a mensagem orientando buscar produto ou usar a câmera', () => {
    render(<CarrinhoPage />);
    expect(
      screen.getByText('Seu carrinho está vazio. Busque um produto ou aponte a câmera para um código.'),
    ).toBeInTheDocument();
  });

  it('mostra "Carregando carrinho..." enquanto carregando=true e itens ainda vazios', () => {
    carrinhoState.carregando = true;
    render(<CarrinhoPage />);
    expect(screen.getByText('Carregando carrinho...')).toBeInTheDocument();
    expect(
      screen.queryByText('Seu carrinho está vazio. Busque um produto ou aponte a câmera para um código.'),
    ).not.toBeInTheDocument();
  });

  it('erro=true mostra uma mensagem distinta de erro (role="alert"), nunca a de carrinho vazio', () => {
    carrinhoState.erro = true;
    render(<CarrinhoPage />);

    expect(screen.getByRole('alert')).toHaveTextContent(
      'Não foi possível carregar o carrinho agora. Tente novamente em instantes.',
    );
    expect(
      screen.queryByText('Seu carrinho está vazio. Busque um produto ou aponte a câmera para um código.'),
    ).not.toBeInTheDocument();
    expect(screen.queryByText('Carregando carrinho...')).not.toBeInTheDocument();
  });

  it('erro=true nunca mostra a mensagem de "Carregando carrinho..." mesmo com carregando=true', () => {
    carrinhoState.erro = true;
    carrinhoState.carregando = true;
    render(<CarrinhoPage />);

    expect(screen.getByRole('alert')).toBeInTheDocument();
    expect(screen.queryByText('Carregando carrinho...')).not.toBeInTheDocument();
  });

  it('lista os itens com nome do produto, estoque e quantidade', () => {
    carrinhoState.itens = [ITEM_1, ITEM_2];
    render(<CarrinhoPage />);

    expect(screen.getByText('Cabo Flexível 4mm')).toBeInTheDocument();
    expect(screen.getByText('Almoxarifado Central')).toBeInTheDocument();
    expect(screen.getByText('3')).toBeInTheDocument();
    expect(screen.getByText('Conduíte 3/4')).toBeInTheDocument();
    expect(screen.getByText('Obra Norte')).toBeInTheDocument();
    expect(screen.getByText('10')).toBeInTheDocument();
    expect(
      screen.queryByText('Seu carrinho está vazio. Busque um produto ou aponte a câmera para um código.'),
    ).not.toBeInTheDocument();
  });

  it('clicar em "Remover" abre o ConfirmDialog citando o nome do produto', async () => {
    carrinhoState.itens = [ITEM_1];
    const user = userEvent.setup();
    render(<CarrinhoPage />);

    await user.click(screen.getByRole('button', { name: 'Remover Cabo Flexível 4mm do carrinho' }));

    const dialog = await screen.findByRole('alertdialog');
    expect(within(dialog).getByText('Remover "Cabo Flexível 4mm" do carrinho?')).toBeInTheDocument();
  });

  it('cancelar o ConfirmDialog não chama removerItem', async () => {
    carrinhoState.itens = [ITEM_1];
    const user = userEvent.setup();
    render(<CarrinhoPage />);

    await user.click(screen.getByRole('button', { name: 'Remover Cabo Flexível 4mm do carrinho' }));
    const dialog = await screen.findByRole('alertdialog');
    await user.click(within(dialog).getByRole('button', { name: 'Cancelar' }));

    await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument());
    expect(removerItemMock).not.toHaveBeenCalled();
  });

  it('confirmar remoção: chama removerItem(produtoId, estoqueId) e mostra toast.success ao suceder', async () => {
    carrinhoState.itens = [ITEM_1];
    const user = userEvent.setup();
    render(<CarrinhoPage />);

    await user.click(screen.getByRole('button', { name: 'Remover Cabo Flexível 4mm do carrinho' }));
    const dialog = await screen.findByRole('alertdialog');
    await user.click(within(dialog).getByRole('button', { name: 'Remover' }));

    await waitFor(() => expect(removerItemMock).toHaveBeenCalledWith('p1', 'e1'));
    await waitFor(() => expect(toastSuccess).toHaveBeenCalledWith('Item removido do carrinho.'));
    expect(toastError).not.toHaveBeenCalled();
  });

  it('confirmar remoção com falha: mostra toast.error com a mensagem devolvida', async () => {
    carrinhoState.itens = [ITEM_1];
    removerItemMock.mockResolvedValue({ ok: false, mensagem: 'item não encontrado no carrinho' });
    const user = userEvent.setup();
    render(<CarrinhoPage />);

    await user.click(screen.getByRole('button', { name: 'Remover Cabo Flexível 4mm do carrinho' }));
    const dialog = await screen.findByRole('alertdialog');
    await user.click(within(dialog).getByRole('button', { name: 'Remover' }));

    await waitFor(() => expect(toastError).toHaveBeenCalledWith('item não encontrado no carrinho'));
    expect(toastSuccess).not.toHaveBeenCalled();
  });

  describe('Enviar Pedido (Story 7.2)', () => {
    it('carrinho vazio não mostra o botão "Enviar Pedido"', () => {
      render(<CarrinhoPage />);
      expect(screen.queryByRole('button', { name: 'Enviar Pedido' })).not.toBeInTheDocument();
    });

    it('carrinho com erro de carga não mostra o botão "Enviar Pedido"', () => {
      carrinhoState.erro = true;
      carrinhoState.itens = [ITEM_1];
      render(<CarrinhoPage />);
      expect(screen.queryByRole('button', { name: 'Enviar Pedido' })).not.toBeInTheDocument();
    });

    it('carrinho com itens mostra o botão e abre o diálogo com o solicitante pré-preenchido', async () => {
      carrinhoState.itens = [ITEM_1, ITEM_2];
      const user = userEvent.setup();
      render(<CarrinhoPage />);

      await user.click(screen.getByRole('button', { name: 'Enviar Pedido' }));

      const dialog = await screen.findByRole('dialog');
      expect(within(dialog).getByLabelText('Solicitante')).toHaveValue('Maria Operária');
      expect(within(dialog).getByLabelText('Obra / centro de custo')).toHaveValue('');
    });

    it('envio feliz: chama enviarPedido com os campos preenchidos, mostra toast.success e fecha o diálogo', async () => {
      carrinhoState.itens = [ITEM_1];
      const user = userEvent.setup();
      render(<CarrinhoPage />);

      await user.click(screen.getByRole('button', { name: 'Enviar Pedido' }));
      const dialog = await screen.findByRole('dialog');
      await user.type(within(dialog).getByLabelText('Obra / centro de custo'), 'Obra Sul 42');
      await user.type(within(dialog).getByLabelText('Observação (opcional)'), 'retirar pela manhã');
      await user.click(within(dialog).getByRole('button', { name: 'Confirmar' }));

      await waitFor(() =>
        expect(enviarPedidoMock).toHaveBeenCalledWith('Maria Operária', 'Obra Sul 42', 'retirar pela manhã'),
      );
      await waitFor(() => expect(toastSuccess).toHaveBeenCalledWith('Pedido enviado.'));
      await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
      expect(toastError).not.toHaveBeenCalled();
    });

    it('botão "Confirmar" fica desabilitado sem obra/centro de custo', async () => {
      carrinhoState.itens = [ITEM_1];
      const user = userEvent.setup();
      render(<CarrinhoPage />);

      await user.click(screen.getByRole('button', { name: 'Enviar Pedido' }));
      const dialog = await screen.findByRole('dialog');
      expect(within(dialog).getByRole('button', { name: 'Confirmar' })).toBeDisabled();

      await user.type(within(dialog).getByLabelText('Obra / centro de custo'), 'Obra Sul 42');
      expect(within(dialog).getByRole('button', { name: 'Confirmar' })).toBeEnabled();
    });

    it('erro do servidor: mostra toast.error com a mensagem e mantém o diálogo aberto', async () => {
      carrinhoState.itens = [ITEM_1];
      enviarPedidoMock.mockResolvedValue({
        ok: false,
        mensagem: 'disponibilidade insuficiente para: Cabo Flexível 4mm',
      });
      const user = userEvent.setup();
      render(<CarrinhoPage />);

      await user.click(screen.getByRole('button', { name: 'Enviar Pedido' }));
      const dialog = await screen.findByRole('dialog');
      await user.type(within(dialog).getByLabelText('Obra / centro de custo'), 'Obra Sul 42');
      await user.click(within(dialog).getByRole('button', { name: 'Confirmar' }));

      await waitFor(() =>
        expect(toastError).toHaveBeenCalledWith('disponibilidade insuficiente para: Cabo Flexível 4mm'),
      );
      expect(toastSuccess).not.toHaveBeenCalled();
      expect(screen.getByRole('dialog')).toBeInTheDocument();
    });
  });
});

import { describe, expect, it, vi, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { DepositosDaFilial } from './DepositosDaFilial';

const toastSuccess = vi.hoisted(() => vi.fn());
vi.mock('sonner', () => ({ toast: { success: toastSuccess } }));
vi.mock('@/lib/session', () => ({ getAccessToken: () => 'token-de-teste' }));

type Resp = { ok: boolean; status: number; json: () => Promise<unknown> };
function resp(status: number, body: unknown = {}): Promise<Resp> {
  return Promise.resolve({ ok: status < 400, status, json: async () => body });
}
function stub(impl: (url: string, init?: RequestInit) => Promise<Resp>) {
  const fn = vi.fn(impl);
  vi.stubGlobal('fetch', fn);
  return fn;
}

const DEPS = [{ id: 'e-1', nome: 'Almoxarifado Central', filial_id: 'f-1' }];

function montar(depositos = DEPS, onMudou = vi.fn()) {
  render(
    <DepositosDaFilial
      filialId="f-1"
      filialNome="Recife"
      depositos={depositos}
      onMudou={onMudou}
    />,
  );
  return onMudou;
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe('DepositosDaFilial', () => {
  it('estado vazio', () => {
    montar([]);
    expect(screen.getByText('Nenhum depósito nesta filial ainda.')).toBeInTheDocument();
  });

  it('adicionar: POST com {nome, filial_id}, toast, input limpo e onMudou', async () => {
    const user = userEvent.setup();
    const fn = stub(() => resp(201, { estoque: { id: 'e-9' } }));
    const onMudou = montar();

    const campo = screen.getByLabelText('Nome do depósito em Recife');
    await user.type(campo, 'Canteiro B');
    await user.click(screen.getByRole('button', { name: 'Adicionar depósito' }));

    await waitFor(() => expect(onMudou).toHaveBeenCalled());
    const [url, init] = fn.mock.calls[0];
    expect(url).toBe('/api/estoques');
    expect(init?.method).toBe('POST');
    expect(JSON.parse(String(init?.body))).toEqual({
      nome: 'Canteiro B',
      filial_id: 'f-1',
    });
    expect(toastSuccess).toHaveBeenCalledWith('Depósito criado.');
    expect(campo).toHaveValue('');
  });

  it('409 mostra "Já existe um depósito" e preserva o texto', async () => {
    const user = userEvent.setup();
    stub(() => resp(409));
    const onMudou = montar();
    const campo = screen.getByLabelText('Nome do depósito em Recife');
    await user.type(campo, 'Dup');
    await user.click(screen.getByRole('button', { name: 'Adicionar depósito' }));

    expect(await screen.findByText('Já existe um depósito com esse nome.')).toBeInTheDocument();
    expect(campo).toHaveValue('Dup');
    expect(onMudou).not.toHaveBeenCalled();
  });

  it('botão desabilitado com nome em branco', async () => {
    const user = userEvent.setup();
    montar();
    const botao = screen.getByRole('button', { name: 'Adicionar depósito' });
    expect(botao).toBeDisabled();
    await user.type(screen.getByLabelText('Nome do depósito em Recife'), '   ');
    expect(botao).toBeDisabled();
  });

  it('excluir: confirma no diálogo, DELETE, toast e onMudou; cancelar não dispara', async () => {
    const user = userEvent.setup();
    const fn = stub(() => resp(204));
    const onMudou = montar();

    await user.click(
      screen.getByRole('button', {
        name: 'Excluir depósito Almoxarifado Central',
      }),
    );
    await user.click(await screen.findByRole('button', { name: 'Cancelar' }));
    expect(fn).not.toHaveBeenCalled();

    await user.click(
      screen.getByRole('button', {
        name: 'Excluir depósito Almoxarifado Central',
      }),
    );
    const dialogo = await screen.findByRole('alertdialog');
    await user.click(
      screen.getAllByRole('button', { name: 'Excluir' }).find((b) => dialogo.contains(b))!,
    );

    await waitFor(() => expect(onMudou).toHaveBeenCalled());
    expect(fn.mock.calls[0][0]).toBe('/api/estoques/e-1');
    expect(fn.mock.calls[0][1]?.method).toBe('DELETE');
    expect(toastSuccess).toHaveBeenCalledWith('Depósito excluído.');
  });

  it('excluir 409 mostra a mensagem do servidor (saldo/pedido pendente)', async () => {
    const user = userEvent.setup();
    stub(() =>
      resp(409, {
        error: { message: 'estoque possui quantidade residual de: Tubo PVC' },
      }),
    );
    montar();
    await user.click(
      screen.getByRole('button', {
        name: 'Excluir depósito Almoxarifado Central',
      }),
    );
    const dialogo = await screen.findByRole('alertdialog');
    await user.click(
      screen.getAllByRole('button', { name: 'Excluir' }).find((b) => dialogo.contains(b))!,
    );
    expect(
      await screen.findByText('estoque possui quantidade residual de: Tubo PVC'),
    ).toBeInTheDocument();
  });

  it('excluir com erro genérico mostra a mensagem fixa', async () => {
    const user = userEvent.setup();
    stub(() => resp(500));
    montar();
    await user.click(
      screen.getByRole('button', {
        name: 'Excluir depósito Almoxarifado Central',
      }),
    );
    const dialogo = await screen.findByRole('alertdialog');
    await user.click(
      screen.getAllByRole('button', { name: 'Excluir' }).find((b) => dialogo.contains(b))!,
    );
    expect(
      await screen.findByText(
        'Não foi possível excluir o depósito agora. Tente novamente em instantes.',
      ),
    ).toBeInTheDocument();
  });

  it('cadastro ok mas onMudou rejeita: sem erro de cadastro, toast e campo limpo', async () => {
    const user = userEvent.setup();
    stub(() => resp(201));
    const onMudou = vi.fn().mockRejectedValue(new Error('x'));
    montar(DEPS, onMudou);
    const campo = screen.getByLabelText('Nome do depósito em Recife');
    await user.type(campo, 'Novo');
    await user.click(screen.getByRole('button', { name: 'Adicionar depósito' }));
    await waitFor(() => expect(toastSuccess).toHaveBeenCalledWith('Depósito criado.'));
    expect(campo).toHaveValue('');
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('envia o nome sem espaços nas pontas e limita o campo a 255', async () => {
    const user = userEvent.setup();
    const fn = stub(() => resp(201));
    montar();
    const campo = screen.getByLabelText('Nome do depósito em Recife');
    expect(campo).toHaveAttribute('maxlength', '255');
    await user.type(campo, '  Canteiro  ');
    await user.click(screen.getByRole('button', { name: 'Adicionar depósito' }));
    await waitFor(() => expect(fn).toHaveBeenCalled());
    expect(JSON.parse(String(fn.mock.calls[0][1]?.body)).nome).toBe('Canteiro');
  });

  it.each([
    [404, 'Este depósito já foi removido.'],
    [403, 'Você não tem permissão para excluir depósitos.'],
  ])('excluir %i mostra a mensagem própria e recarrega', async (status, msg) => {
    const user = userEvent.setup();
    stub(() => resp(status));
    const onMudou = montar();
    await user.click(screen.getByRole('button', { name: 'Excluir depósito Almoxarifado Central' }));
    const dialogo = await screen.findByRole('alertdialog');
    await user.click(
      screen.getAllByRole('button', { name: 'Excluir' }).find((b) => dialogo.contains(b))!,
    );
    expect(await screen.findByText(msg)).toBeInTheDocument();
    expect(onMudou).toHaveBeenCalled();
  });

  it('Excluir fica desabilitado enquanto um cadastro está em andamento', async () => {
    const user = userEvent.setup();
    stub(() => new Promise<Resp>(() => {}));
    montar();
    await user.type(screen.getByLabelText('Nome do depósito em Recife'), 'Novo');
    await user.click(screen.getByRole('button', { name: 'Adicionar depósito' }));
    expect(
      await screen.findByRole('button', { name: 'Excluir depósito Almoxarifado Central' }),
    ).toBeDisabled();
  });

  it('400 com message do servidor mostra essa mensagem em role="alert"', async () => {
    const user = userEvent.setup();
    stub(() => resp(400, { error: { message: 'Nome inválido' } }));
    const onMudou = montar();
    await user.type(screen.getByLabelText('Nome do depósito em Recife'), 'X');
    await user.click(screen.getByRole('button', { name: 'Adicionar depósito' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Nome inválido');
    expect(onMudou).not.toHaveBeenCalled();
  });

  it('400 sem corpo/message mostra a mensagem fixa de cadastro', async () => {
    const user = userEvent.setup();
    stub(() =>
      Promise.resolve({
        ok: false,
        status: 400,
        json: async () => Promise.reject(new Error('sem corpo')),
      }),
    );
    montar();
    await user.type(screen.getByLabelText('Nome do depósito em Recife'), 'X');
    await user.click(screen.getByRole('button', { name: 'Adicionar depósito' }));
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível cadastrar o depósito agora. Tente novamente em instantes.',
    );
  });
});

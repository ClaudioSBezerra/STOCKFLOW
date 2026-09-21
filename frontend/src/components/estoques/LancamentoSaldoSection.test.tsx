import { describe, expect, it, vi, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { LancamentoSaldoSection } from './LancamentoSaldoSection';

const toastSuccess = vi.hoisted(() => vi.fn());
vi.mock('sonner', () => ({ toast: { success: toastSuccess } }));

vi.mock('@/lib/session', () => ({
  getAccessToken: () => 'token-de-teste',
}));

type FetchImpl = (
  url: string,
  init?: RequestInit,
) => Promise<{ ok: boolean; status?: number; json: () => Promise<unknown> }>;

function stubFetch(impl: FetchImpl) {
  const fn = vi.fn(impl);
  vi.stubGlobal('fetch', fn);
  return fn;
}

function jsonOk(body: unknown, status = 200) {
  return Promise.resolve({ ok: true, status, json: async () => body });
}

const ESTOQUES = [
  { id: 'e-1', nome: 'Almoxarifado Central' },
  { id: 'e-2', nome: 'Canteiro A' },
];

const PRODUTOS = [
  {
    id: 'p-1',
    nome: 'Cimento CP-II 50kg',
    codigo: '000001',
    categoria: { id: 'c-1', codigo: '01.001', nome: 'Cimentos' },
  },
];

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

function corpoDoPost(fetchMock: ReturnType<typeof stubFetch>) {
  const chamada = fetchMock.mock.calls.find(([url, init]) => url === '/api/lotes' && init?.method === 'POST');
  return chamada ? JSON.parse(String(chamada[1]?.body)) : undefined;
}

async function preencherAteProduto(user: ReturnType<typeof userEvent.setup>) {
  await screen.findByRole('option', { name: 'Canteiro A' });
  await user.type(screen.getByLabelText('Produto'), 'cimento');
  await user.click(await screen.findByRole('button', { name: /Cimento CP-II 50kg/ }));
}

describe('LancamentoSaldoSection', () => {
  it('lança saldo com validade: POST /api/lotes com o corpo esperado, toast e campos limpos', async () => {
    const fetchMock = stubFetch((url, init) => {
      if (url === '/api/estoques') return jsonOk({ estoques: ESTOQUES });
      if (url.startsWith('/api/produtos/busca')) return jsonOk({ produtos: PRODUTOS });
      if (url === '/api/lotes' && init?.method === 'POST') return jsonOk({ lote: { id: 'l-1' } }, 201);
      throw new Error(`URL inesperada: ${url}`);
    });
    const user = userEvent.setup();
    render(<LancamentoSaldoSection />);

    await preencherAteProduto(user);
    await user.selectOptions(screen.getByLabelText('Estoque'), 'e-2');
    await user.type(screen.getByLabelText('Quantidade'), '10,5');
    await user.type(screen.getByLabelText('Data de validade (opcional)'), '2027-03-01');
    await user.click(screen.getByRole('button', { name: 'Lançar saldo' }));

    await waitFor(() => expect(toastSuccess).toHaveBeenCalledWith('Saldo lançado.'));
    expect(corpoDoPost(fetchMock)).toEqual({
      produtoId: 'p-1',
      estoqueId: 'e-2',
      quantidade: 10.5,
      dataValidade: '2027-03-01',
    });
    expect(screen.getByLabelText('Quantidade')).toHaveValue('');
    expect(screen.getByLabelText('Data de validade (opcional)')).toHaveValue('');
  });

  it('validade vazia envia dataValidade null', async () => {
    const fetchMock = stubFetch((url, init) => {
      if (url === '/api/estoques') return jsonOk({ estoques: ESTOQUES });
      if (url.startsWith('/api/produtos/busca')) return jsonOk({ produtos: PRODUTOS });
      if (url === '/api/lotes' && init?.method === 'POST') return jsonOk({ lote: { id: 'l-1' } }, 201);
      throw new Error(`URL inesperada: ${url}`);
    });
    const user = userEvent.setup();
    render(<LancamentoSaldoSection />);

    await preencherAteProduto(user);
    await user.selectOptions(screen.getByLabelText('Estoque'), 'e-1');
    await user.type(screen.getByLabelText('Quantidade'), '3');
    await user.click(screen.getByRole('button', { name: 'Lançar saldo' }));

    await waitFor(() => expect(toastSuccess).toHaveBeenCalled());
    expect(corpoDoPost(fetchMock).dataValidade).toBeNull();
  });

  it('botão desabilitado sem Produto, Estoque ou quantidade > 0', async () => {
    stubFetch((url) => {
      if (url === '/api/estoques') return jsonOk({ estoques: ESTOQUES });
      if (url.startsWith('/api/produtos/busca')) return jsonOk({ produtos: PRODUTOS });
      throw new Error(`URL inesperada: ${url}`);
    });
    const user = userEvent.setup();
    render(<LancamentoSaldoSection />);

    const botao = screen.getByRole('button', { name: 'Lançar saldo' });
    expect(botao).toBeDisabled();
    await preencherAteProduto(user);
    expect(botao).toBeDisabled();
    await user.selectOptions(screen.getByLabelText('Estoque'), 'e-1');
    expect(botao).toBeDisabled();
    await user.type(screen.getByLabelText('Quantidade'), '0');
    expect(botao).toBeDisabled();
    await user.clear(screen.getByLabelText('Quantidade'));
    await user.type(screen.getByLabelText('Quantidade'), '2');
    expect(botao).toBeEnabled();
  });

  it('erro 400 do servidor aparece inline em role="alert"', async () => {
    stubFetch((url, init) => {
      if (url === '/api/estoques') return jsonOk({ estoques: ESTOQUES });
      if (url.startsWith('/api/produtos/busca')) return jsonOk({ produtos: PRODUTOS });
      if (url === '/api/lotes' && init?.method === 'POST') {
        return Promise.resolve({
          ok: false,
          status: 400,
          json: async () => ({ error: { code: 'VALIDATION_ERROR', message: 'quantidade deve ser maior que zero' } }),
        });
      }
      throw new Error(`URL inesperada: ${url}`);
    });
    const user = userEvent.setup();
    render(<LancamentoSaldoSection />);

    await preencherAteProduto(user);
    await user.selectOptions(screen.getByLabelText('Estoque'), 'e-1');
    await user.type(screen.getByLabelText('Quantidade'), '2');
    await user.click(screen.getByRole('button', { name: 'Lançar saldo' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('quantidade deve ser maior que zero');
    expect(toastSuccess).not.toHaveBeenCalled();
  });

  it('falha genérica (500) mostra mensagem genérica inline', async () => {
    stubFetch((url, init) => {
      if (url === '/api/estoques') return jsonOk({ estoques: ESTOQUES });
      if (url.startsWith('/api/produtos/busca')) return jsonOk({ produtos: PRODUTOS });
      if (url === '/api/lotes' && init?.method === 'POST') {
        return Promise.resolve({ ok: false, status: 500, json: async () => ({}) });
      }
      throw new Error(`URL inesperada: ${url}`);
    });
    const user = userEvent.setup();
    render(<LancamentoSaldoSection />);

    await preencherAteProduto(user);
    await user.selectOptions(screen.getByLabelText('Estoque'), 'e-1');
    await user.type(screen.getByLabelText('Quantidade'), '2');
    await user.click(screen.getByRole('button', { name: 'Lançar saldo' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('Não foi possível lançar o saldo agora.');
  });

  it('falha ao carregar estoques mostra alerta', async () => {
    stubFetch((url) => {
      if (url === '/api/estoques') return Promise.resolve({ ok: false, status: 500, json: async () => ({}) });
      throw new Error(`URL inesperada: ${url}`);
    });
    render(<LancamentoSaldoSection />);
    expect(await screen.findByRole('alert')).toHaveTextContent('Não foi possível carregar a lista de estoques.');
  });
});

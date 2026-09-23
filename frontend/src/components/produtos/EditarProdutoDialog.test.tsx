import { afterEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { EditarProdutoDialog, type ProdutoEditavel } from './EditarProdutoDialog';

vi.mock('@/lib/session', () => ({ getAccessToken: () => 'token-de-teste' }));
const toastSuccess = vi.hoisted(() => vi.fn());
vi.mock('sonner', () => ({ toast: { success: toastSuccess, info: vi.fn() } }));

const PRODUTO: ProdutoEditavel = {
  id: 'p1',
  nome: 'Cabo Flexível 4mm',
  codigo: '000007',
  categoria: { id: 'c1' },
  dimensoes: {
    comprimento: { valor: 100, unidade: 'm' },
    largura: null,
    diametro: null,
    altura: null,
    espessura: null,
  },
  unidadeMedida: 'm',
  embalagem: 'Rolo 100m',
  codigoFornecedor: null,
  ean13: null,
  templateId: 't1',
  observacoes: 'obs',
};

function resposta(ok: boolean, body: unknown, status = 200) {
  return Promise.resolve({ ok, status, json: async () => body });
}

function stub(put: () => Promise<unknown>) {
  const fn = vi.fn((url: string, init?: RequestInit) => {
    if (url === '/api/categorias') {
      return resposta(true, { categorias: [{ id: 'c1', codigo: '05.002', nome: 'Elétricos' }] });
    }
    if (url === '/api/nomenclatura-templates') {
      return resposta(true, { templates: [{ id: 't1', subtipo: 'Cabo', template: 'CABO [TIPO] [BITOLA]' }] });
    }
    if (url === '/api/produtos/p1' && init?.method === 'PUT') return put();
    throw new Error(`URL inesperada: ${url}`);
  });
  vi.stubGlobal('fetch', fn);
  return fn;
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe('EditarProdutoDialog', () => {
  it('pré-preenche, mostra Código somente-leitura e o formato do template', async () => {
    stub(() => resposta(true, {}));
    render(<EditarProdutoDialog produto={PRODUTO} open onOpenChange={vi.fn()} onSalvo={vi.fn()} />);
    expect(screen.getByLabelText('Nome')).toHaveValue('Cabo Flexível 4mm');
    expect(screen.getByLabelText('Código')).toBeDisabled();
    expect(screen.getByLabelText('Código')).toHaveValue('000007');
    expect(screen.getByLabelText('Embalagem')).toHaveValue('Rolo 100m');
    expect(await screen.findByText(/Formato: CABO \[TIPO\] \[BITOLA\]/)).toBeInTheDocument();
  });

  it('sucesso: envia PUT, fecha, avisa e recarrega', async () => {
    const fn = stub(() => resposta(true, { produto: { id: 'p1' } }));
    const onOpenChange = vi.fn();
    const onSalvo = vi.fn();
    render(<EditarProdutoDialog produto={PRODUTO} open onOpenChange={onOpenChange} onSalvo={onSalvo} />);
    const nome = screen.getByLabelText('Nome');
    await userEvent.clear(nome);
    await userEvent.type(nome, 'Cabo Flexível 6mm');
    await userEvent.click(screen.getByRole('button', { name: 'Salvar' }));
    await waitFor(() => expect(onSalvo).toHaveBeenCalled());
    const put = fn.mock.calls.find(([, init]) => init?.method === 'PUT');
    const corpo = JSON.parse(String(put?.[1]?.body)) as Record<string, unknown>;
    expect(corpo).toMatchObject({
      nome: 'Cabo Flexível 6mm',
      categoria_id: 'c1',
      template_id: 't1',
      unidade_medida: 'm',
      comprimento: { valor: 100, unidade: 'm' },
    });
    expect(onOpenChange).toHaveBeenCalledWith(false);
    expect(toastSuccess).toHaveBeenCalledWith('Produto atualizado.');
  });

  // O PUT regrava tudo: um opcional que o pré-preenchimento perca some do
  // corpo e o servidor o grava NULL. Salvar sem editar tem de devolver todos.
  it('salvar sem editar reenvia todos os opcionais pré-preenchidos', async () => {
    const fn = stub(() => resposta(true, { produto: { id: 'p1' } }));
    const onSalvo = vi.fn();
    const completo: ProdutoEditavel = {
      ...PRODUTO,
      codigoFornecedor: 'FORN-123',
      ean13: '7891000100103',
      observacoes: 'uso externo',
    };
    render(<EditarProdutoDialog produto={completo} open onOpenChange={vi.fn()} onSalvo={onSalvo} />);
    await screen.findByText(/Formato: CABO \[TIPO\] \[BITOLA\]/);
    await userEvent.click(screen.getByRole('button', { name: 'Salvar' }));
    await waitFor(() => expect(onSalvo).toHaveBeenCalled());
    const put = fn.mock.calls.find(([, init]) => init?.method === 'PUT');
    const corpo = JSON.parse(String(put?.[1]?.body)) as Record<string, unknown>;
    expect(corpo).toEqual({
      nome: 'Cabo Flexível 4mm',
      categoria_id: 'c1',
      template_id: 't1',
      unidade_medida: 'm',
      embalagem: 'Rolo 100m',
      codigo_fornecedor: 'FORN-123',
      ean13: '7891000100103',
      observacoes: 'uso externo',
      comprimento: { valor: 100, unidade: 'm' },
    });
  });

  it('400: mostra a mensagem do servidor em role=alert e não fecha', async () => {
    stub(() => resposta(false, { error: { message: 'nome fora do formato' } }, 400));
    const onOpenChange = vi.fn();
    render(<EditarProdutoDialog produto={PRODUTO} open onOpenChange={onOpenChange} onSalvo={vi.fn()} />);
    await userEvent.click(screen.getByRole('button', { name: 'Salvar' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('nome fora do formato');
    expect(onOpenChange).not.toHaveBeenCalledWith(false);
  });
});

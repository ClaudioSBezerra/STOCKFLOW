import { describe, expect, it, vi, afterEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { LocaisEstoqueSection } from './LocaisEstoqueSection';

const toastSuccess = vi.hoisted(() => vi.fn());
vi.mock('sonner', () => ({ toast: { success: toastSuccess } }));

vi.mock('@/lib/session', () => ({
  getAccessToken: () => 'token-de-teste',
}));

type FetchImpl = (
  url: string,
  init?: RequestInit,
) => Promise<{ ok: boolean; status?: number; json: () => Promise<unknown> }>;

const FILIAIS = [
  { id: 'f-1', nome: 'Matriz' },
  { id: 'f-2', nome: 'Recife' },
];

// GET /api/filiais (Story 12.1) é respondido por padrão; `filiaisImpl` deixa
// um teste sobrescrever a resposta.
let filiaisImpl: FetchImpl | null = null;

function stubFetch(impl: FetchImpl) {
  const fn = vi.fn((url: string, init?: RequestInit) => {
    if (url === '/api/filiais') {
      return filiaisImpl ? filiaisImpl(url, init) : jsonOk({ filiais: FILIAIS });
    }
    return impl(url, init);
  });
  vi.stubGlobal('fetch', fn);
  return fn;
}

function jsonOk(body: unknown) {
  return Promise.resolve({ ok: true, status: 200, json: async () => body });
}

const ESTOQUES = [
  { id: 'e-1', nome: 'Almoxarifado Central', filial_id: 'f-1', filial_nome: 'Matriz' },
  { id: 'e-2', nome: 'Canteiro A', filial_id: 'f-2', filial_nome: 'Recife' },
];

afterEach(() => {
  filiaisImpl = null;
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe('LocaisEstoqueSection', () => {
  it('lista os estoques recebidos de GET /api/estoques', async () => {
    stubFetch((url) => {
      if (url === '/api/estoques') return jsonOk({ estoques: ESTOQUES });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<LocaisEstoqueSection />);

    expect(await screen.findByText('Almoxarifado Central')).toBeInTheDocument();
    expect(screen.getByText('Canteiro A')).toBeInTheDocument();
    expect(fetch).toHaveBeenCalledWith('/api/estoques', expect.anything());
  });

  it('lista vazia mostra a mensagem "Nenhum estoque cadastrado ainda."', async () => {
    stubFetch((url) => {
      if (url === '/api/estoques') return jsonOk({ estoques: [] });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<LocaisEstoqueSection />);

    expect(await screen.findByText('Nenhum estoque cadastrado ainda.')).toBeInTheDocument();
  });

  it('cadastro 201: POST com {nome}, toast.success, GET refeito e input limpo', async () => {
    let gets = 0;
    const fetchMock = stubFetch((url, init) => {
      if (url === '/api/estoques' && (init?.method ?? 'GET') === 'GET') {
        gets += 1;
        return jsonOk({ estoques: gets === 1 ? [] : ESTOQUES });
      }
      if (url === '/api/estoques' && init?.method === 'POST') {
        return Promise.resolve({
          ok: true,
          status: 201,
          json: async () => ({ estoque: { id: 'e-9', nome: 'Depósito Novo' } }),
        });
      }
      throw new Error(`URL inesperada: ${url} (${init?.method ?? 'GET'})`);
    });

    const user = userEvent.setup();
    render(<LocaisEstoqueSection />);
    await screen.findByText('Nenhum estoque cadastrado ainda.');

    const input = screen.getByLabelText('Nome do estoque');
    await user.type(input, 'Depósito Novo');
    await user.selectOptions(await screen.findByLabelText('Filial'), 'f-2');
    await user.click(screen.getByRole('button', { name: 'Adicionar estoque' }));

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/estoques',
        expect.objectContaining({
          method: 'POST',
          body: JSON.stringify({ nome: 'Depósito Novo', filial_id: 'f-2' }),
        }),
      ),
    );
    await waitFor(() => expect(toastSuccess).toHaveBeenCalledWith('Estoque criado.'));
    await waitFor(() => expect(gets).toBe(2));
    expect(input).toHaveValue('');
  });

  it('cadastro 409: mostra o role="alert" específico e não limpa o input', async () => {
    const fetchMock = stubFetch((url, init) => {
      if (url === '/api/estoques' && (init?.method ?? 'GET') === 'GET') {
        return jsonOk({ estoques: [] });
      }
      if (url === '/api/estoques' && init?.method === 'POST') {
        return Promise.resolve({
          ok: false,
          status: 409,
          json: async () => ({ error: { code: 'CONFLICT' } }),
        });
      }
      throw new Error(`URL inesperada: ${url} (${init?.method ?? 'GET'})`);
    });

    const user = userEvent.setup();
    render(<LocaisEstoqueSection />);
    await screen.findByText('Nenhum estoque cadastrado ainda.');

    const input = screen.getByLabelText('Nome do estoque');
    await user.type(input, 'Canteiro A');
    await user.selectOptions(await screen.findByLabelText('Filial'), 'f-1');
    await user.click(screen.getByRole('button', { name: 'Adicionar estoque' }));

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Já existe um estoque com esse nome.',
    );
    expect(toastSuccess).not.toHaveBeenCalled();
    expect(input).toHaveValue('Canteiro A');
    // Só o GET de mount + o POST — nenhum GET extra após o 409.
    expect(
      fetchMock.mock.calls.filter(
        ([url, init]) => url === '/api/estoques' && (init?.method ?? 'GET') === 'GET',
      ),
    ).toHaveLength(1);
  });

  it('GET !ok no mount mostra um role="alert" genérico', async () => {
    stubFetch((url) => {
      if (url === '/api/estoques') {
        return Promise.resolve({
          ok: false,
          status: 500,
          json: async () => ({ error: { code: 'INTERNAL_ERROR' } }),
        });
      }
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<LocaisEstoqueSection />);

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível carregar a lista de estoques. Recarregue a página.',
    );
  });

  it('"Excluir" abre o ConfirmDialog e confirmar dispara DELETE + toast + GET refeito sem o item', async () => {
    let gets = 0;
    const fetchMock = stubFetch((url, init) => {
      if (url === '/api/estoques' && (init?.method ?? 'GET') === 'GET') {
        gets += 1;
        return jsonOk({ estoques: gets === 1 ? ESTOQUES : [ESTOQUES[1]] });
      }
      if (url === '/api/estoques/e-1' && init?.method === 'DELETE') {
        return Promise.resolve({ ok: true, status: 204, json: async () => ({}) });
      }
      throw new Error(`URL inesperada: ${url} (${init?.method ?? 'GET'})`);
    });

    const user = userEvent.setup();
    render(<LocaisEstoqueSection />);
    await screen.findByText('Almoxarifado Central');

    await user.click(
      screen.getByRole('button', { name: 'Excluir estoque Almoxarifado Central' }),
    );
    const dialog = await screen.findByRole('alertdialog');
    await user.click(within(dialog).getByRole('button', { name: 'Excluir' }));

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/estoques/e-1',
        expect.objectContaining({ method: 'DELETE' }),
      ),
    );
    await waitFor(() => expect(toastSuccess).toHaveBeenCalledWith('Estoque excluído.'));
    await waitFor(() => expect(gets).toBe(2));
    expect(screen.queryByText('Almoxarifado Central')).not.toBeInTheDocument();
    expect(screen.getByText('Canteiro A')).toBeInTheDocument();
  });

  it('cancelar o ConfirmDialog não dispara nenhum DELETE e mantém a lista', async () => {
    const fetchMock = stubFetch((url, init) => {
      if (url === '/api/estoques' && (init?.method ?? 'GET') === 'GET') {
        return jsonOk({ estoques: ESTOQUES });
      }
      throw new Error(`URL inesperada: ${url} (${init?.method ?? 'GET'})`);
    });

    const user = userEvent.setup();
    render(<LocaisEstoqueSection />);
    await screen.findByText('Almoxarifado Central');

    await user.click(
      screen.getByRole('button', { name: 'Excluir estoque Almoxarifado Central' }),
    );
    const dialog = await screen.findByRole('alertdialog');
    await user.click(within(dialog).getByRole('button', { name: 'Cancelar' }));

    await waitFor(() =>
      expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument(),
    );
    expect(
      fetchMock.mock.calls.filter(([, init]) => init?.method === 'DELETE'),
    ).toHaveLength(0);
    expect(screen.getByText('Almoxarifado Central')).toBeInTheDocument();
  });

  it('DELETE !ok mostra o role="alert" genérico e recarrega a lista', async () => {
    let gets = 0;
    stubFetch((url, init) => {
      if (url === '/api/estoques' && (init?.method ?? 'GET') === 'GET') {
        gets += 1;
        return jsonOk({ estoques: ESTOQUES });
      }
      if (url === '/api/estoques/e-1' && init?.method === 'DELETE') {
        return Promise.resolve({
          ok: false,
          status: 500,
          json: async () => ({ error: { code: 'INTERNAL_ERROR' } }),
        });
      }
      throw new Error(`URL inesperada: ${url} (${init?.method ?? 'GET'})`);
    });

    const user = userEvent.setup();
    render(<LocaisEstoqueSection />);
    await screen.findByText('Almoxarifado Central');

    await user.click(
      screen.getByRole('button', { name: 'Excluir estoque Almoxarifado Central' }),
    );
    const dialog = await screen.findByRole('alertdialog');
    await user.click(within(dialog).getByRole('button', { name: 'Excluir' }));

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível excluir o estoque agora. Tente novamente em instantes.',
    );
    expect(toastSuccess).not.toHaveBeenCalled();
    await waitFor(() => expect(gets).toBe(2));
  });

  it('DELETE 409 (guard de quantidade residual) mostra a mensagem específica do servidor, não a genérica', async () => {
    let gets = 0;
    stubFetch((url, init) => {
      if (url === '/api/estoques' && (init?.method ?? 'GET') === 'GET') {
        gets += 1;
        return jsonOk({ estoques: ESTOQUES });
      }
      if (url === '/api/estoques/e-1' && init?.method === 'DELETE') {
        return Promise.resolve({
          ok: false,
          status: 409,
          json: async () => ({
            error: {
              code: 'CONFLICT',
              message: 'estoque possui quantidade residual de: Tubo PVC 100mm',
            },
          }),
        });
      }
      throw new Error(`URL inesperada: ${url} (${init?.method ?? 'GET'})`);
    });

    const user = userEvent.setup();
    render(<LocaisEstoqueSection />);
    await screen.findByText('Almoxarifado Central');

    await user.click(
      screen.getByRole('button', { name: 'Excluir estoque Almoxarifado Central' }),
    );
    const dialog = await screen.findByRole('alertdialog');
    await user.click(within(dialog).getByRole('button', { name: 'Excluir' }));

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'estoque possui quantidade residual de: Tubo PVC 100mm',
    );
    expect(toastSuccess).not.toHaveBeenCalled();
    await waitFor(() => expect(gets).toBe(2));
  });

  it('nome de estoque muito longo (~255 chars) não remove nem esconde o botão "Excluir" da linha', async () => {
    const nomeLongo = 'L'.repeat(255);
    stubFetch((url, init) => {
      if (url === '/api/estoques' && (init?.method ?? 'GET') === 'GET') {
        return jsonOk({ estoques: [{ id: 'e-long', nome: nomeLongo }] });
      }
      throw new Error(`URL inesperada: ${url} (${init?.method ?? 'GET'})`);
    });

    render(<LocaisEstoqueSection />);

    const botao = await screen.findByRole('button', {
      name: `Excluir estoque ${nomeLongo}`,
    });
    expect(botao).toBeInTheDocument();
  });

  it('botão desabilitado enquanto o nome está em branco', async () => {
    stubFetch((url) => {
      if (url === '/api/estoques') return jsonOk({ estoques: [] });
      throw new Error(`URL inesperada: ${url}`);
    });

    const user = userEvent.setup();
    render(<LocaisEstoqueSection />);
    await screen.findByText('Nenhum estoque cadastrado ainda.');

    const botao = screen.getByRole('button', { name: 'Adicionar estoque' });
    expect(botao).toBeDisabled();

    await user.type(screen.getByLabelText('Nome do estoque'), 'Canteiro B');
    // Filial é obrigatória: só com o nome, continua desabilitado.
    expect(botao).toBeDisabled();
    await user.selectOptions(await screen.findByLabelText('Filial'), 'f-1');
    expect(botao).toBeEnabled();
  });

  it('mostra a Filial em cada linha e "—" para Estoque legado sem Filial', async () => {
    stubFetch((url) => {
      if (url === '/api/estoques') {
        return jsonOk({
          estoques: [
            ...ESTOQUES,
            { id: 'e-3', nome: 'Legado', filial_id: null, filial_nome: null },
          ],
        });
      }
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<LocaisEstoqueSection />);

    expect(await screen.findByText('Filial: Matriz')).toBeInTheDocument();
    expect(screen.getByText('Filial: Recife')).toBeInTheDocument();
    expect(screen.getByText('Filial: —')).toBeInTheDocument();
  });

  it('carrega GET /api/filiais e uma única Filial já vem selecionada', async () => {
    filiaisImpl = () => jsonOk({ filiais: [FILIAIS[0]] });
    const fetchMock = stubFetch((url) => {
      if (url === '/api/estoques') return jsonOk({ estoques: [] });
      throw new Error(`URL inesperada: ${url}`);
    });

    const user = userEvent.setup();
    render(<LocaisEstoqueSection />);

    await waitFor(() => expect(screen.getByLabelText('Filial')).toHaveValue('f-1'));
    expect(fetchMock).toHaveBeenCalledWith('/api/filiais', expect.anything());
    await user.type(screen.getByLabelText('Nome do estoque'), 'Depósito');
    expect(screen.getByRole('button', { name: 'Adicionar estoque' })).toBeEnabled();
  });

  it('sem filiais cadastradas: mostra o aviso role="alert" e mantém o botão desabilitado', async () => {
    filiaisImpl = () => jsonOk({ filiais: [] });
    stubFetch((url) => {
      if (url === '/api/estoques') return jsonOk({ estoques: [] });
      throw new Error(`URL inesperada: ${url}`);
    });

    const user = userEvent.setup();
    render(<LocaisEstoqueSection />);

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Nenhuma filial cadastrada. Peça a um administrador para cadastrar uma filial antes de criar estoques.',
    );
    await user.type(screen.getByLabelText('Nome do estoque'), 'Depósito');
    expect(screen.getByRole('button', { name: 'Adicionar estoque' })).toBeDisabled();
  });

  it('falha ao carregar filiais: mostra o erro de carga (não o aviso de sem filiais) e bloqueia o cadastro', async () => {
    filiaisImpl = async () => ({ ok: false, status: 500, json: async () => ({}) });
    stubFetch((url) => {
      if (url === '/api/estoques') return jsonOk({ estoques: [] });
      throw new Error(`URL inesperada: ${url}`);
    });

    const user = userEvent.setup();
    render(<LocaisEstoqueSection />);

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível carregar as filiais. Recarregue a página.',
    );
    expect(screen.queryByText(/Nenhuma filial cadastrada/)).not.toBeInTheDocument();
    await user.type(screen.getByLabelText('Nome do estoque'), 'Depósito');
    expect(screen.getByRole('button', { name: 'Adicionar estoque' })).toBeDisabled();
  });

  it('cadastro 400: mostra a mensagem do servidor; sem mensagem usa a genérica', async () => {
    let corpo400: unknown = { error: { code: 'VALIDATION_ERROR', message: 'a filial é obrigatória e deve pertencer à empresa' } };
    stubFetch((url, init) => {
      if (url === '/api/estoques' && (init?.method ?? 'GET') === 'GET') return jsonOk({ estoques: [] });
      if (url === '/api/estoques' && init?.method === 'POST') {
        return Promise.resolve({ ok: false, status: 400, json: async () => corpo400 });
      }
      throw new Error(`URL inesperada: ${url} (${init?.method ?? 'GET'})`);
    });

    const user = userEvent.setup();
    render(<LocaisEstoqueSection />);
    await user.type(screen.getByLabelText('Nome do estoque'), 'Depósito');
    await user.selectOptions(await screen.findByLabelText('Filial'), 'f-1');
    await user.click(screen.getByRole('button', { name: 'Adicionar estoque' }));
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'a filial é obrigatória e deve pertencer à empresa',
    );

    corpo400 = {};
    await user.click(screen.getByRole('button', { name: 'Adicionar estoque' }));
    await waitFor(() =>
      expect(screen.getByRole('alert')).toHaveTextContent(
        'Não foi possível cadastrar o estoque agora. Tente novamente em instantes.',
      ),
    );
  });
});

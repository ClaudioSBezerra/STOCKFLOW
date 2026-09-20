import { describe, expect, it, vi, afterEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { CategoriasSection } from './CategoriasSection';

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

function jsonOk(body: unknown) {
  return Promise.resolve({ ok: true, status: 200, json: async () => body });
}

function jsonErro(status: number, message: string) {
  return Promise.resolve({
    ok: false,
    status,
    json: async () => ({ error: { code: 'X', message } }),
  });
}

const CATEGORIAS = [
  { id: 'c-1', codigo: '04.001', nome: 'Materiais Civis' },
  { id: 'c-2', codigo: '05.001', nome: 'EPI/EPC' },
];

const metodo = (init?: RequestInit) => init?.method ?? 'GET';

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe('CategoriasSection', () => {
  it('lista as categorias de GET /api/categorias', async () => {
    stubFetch((url) => {
      if (url === '/api/categorias') return jsonOk({ categorias: CATEGORIAS });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<CategoriasSection />);

    expect(await screen.findByText('Materiais Civis')).toBeInTheDocument();
    expect(screen.getByText('EPI/EPC')).toBeInTheDocument();
  });

  it('lista vazia mostra a mensagem', async () => {
    stubFetch(() => jsonOk({ categorias: [] }));
    render(<CategoriasSection />);
    expect(await screen.findByText('Nenhuma categoria cadastrada ainda.')).toBeInTheDocument();
  });

  it('GET !ok no mount mostra role="alert" genérico', async () => {
    stubFetch(() => jsonErro(500, 'x'));
    render(<CategoriasSection />);
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível carregar as categorias.',
    );
  });

  it('inputs espelham os limites do banco (maxLength 8 e 50)', async () => {
    stubFetch(() => jsonOk({ categorias: [] }));
    render(<CategoriasSection />);
    await screen.findByText('Nenhuma categoria cadastrada ainda.');
    expect(screen.getByLabelText('Código da categoria')).toHaveAttribute('maxlength', '8');
    expect(screen.getByLabelText('Nome da categoria')).toHaveAttribute('maxlength', '50');
  });

  it('cadastro 201: POST {codigo,nome}, toast, GET refeito e inputs limpos', async () => {
    let gets = 0;
    const fetchMock = stubFetch((url, init) => {
      if (url === '/api/categorias' && metodo(init) === 'GET') {
        gets += 1;
        return jsonOk({ categorias: gets === 1 ? [] : CATEGORIAS });
      }
      if (url === '/api/categorias' && init?.method === 'POST') {
        return Promise.resolve({
          ok: true,
          status: 201,
          json: async () => ({ categoria: CATEGORIAS[0] }),
        });
      }
      throw new Error(`URL inesperada: ${url} (${metodo(init)})`);
    });

    const user = userEvent.setup();
    render(<CategoriasSection />);
    await screen.findByText('Nenhuma categoria cadastrada ainda.');

    await user.type(screen.getByLabelText('Código da categoria'), '14.001');
    await user.type(screen.getByLabelText('Nome da categoria'), 'Brindes');
    await user.click(screen.getByRole('button', { name: 'Adicionar categoria' }));

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/categorias',
        expect.objectContaining({
          method: 'POST',
          body: JSON.stringify({ codigo: '14.001', nome: 'Brindes' }),
        }),
      ),
    );
    await waitFor(() => expect(toastSuccess).toHaveBeenCalledWith('Categoria criada.'));
    await waitFor(() => expect(gets).toBe(2));
    expect(screen.getByLabelText('Código da categoria')).toHaveValue('');
    expect(screen.getByLabelText('Nome da categoria')).toHaveValue('');
  });

  it.each([
    [409, 'já existe uma categoria com esse código'],
    [400, 'o código e o nome são obrigatórios'],
  ])('cadastro %i: mostra a mensagem do servidor e mantém os inputs', async (status, mensagem) => {
    stubFetch((_url, init) => {
      if (metodo(init) === 'GET') return jsonOk({ categorias: [] });
      return jsonErro(status, mensagem);
    });

    const user = userEvent.setup();
    render(<CategoriasSection />);
    await screen.findByText('Nenhuma categoria cadastrada ainda.');

    await user.type(screen.getByLabelText('Código da categoria'), 'X1');
    await user.type(screen.getByLabelText('Nome da categoria'), 'Nome');
    await user.click(screen.getByRole('button', { name: 'Adicionar categoria' }));

    expect(await screen.findByRole('alert')).toHaveTextContent(mensagem);
    expect(toastSuccess).not.toHaveBeenCalled();
    expect(screen.getByLabelText('Nome da categoria')).toHaveValue('Nome');
  });

  it('cadastro 500: mostra role="alert" genérico', async () => {
    stubFetch((_url, init) => {
      if (metodo(init) === 'GET') return jsonOk({ categorias: [] });
      return jsonErro(500, 'interno');
    });

    const user = userEvent.setup();
    render(<CategoriasSection />);
    await screen.findByText('Nenhuma categoria cadastrada ainda.');
    await user.type(screen.getByLabelText('Código da categoria'), 'X1');
    await user.type(screen.getByLabelText('Nome da categoria'), 'Nome');
    await user.click(screen.getByRole('button', { name: 'Adicionar categoria' }));

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível cadastrar a categoria agora.',
    );
  });

  it('editar inline: PUT com código/nome novos, toast e GET refeito', async () => {
    let gets = 0;
    const fetchMock = stubFetch((url, init) => {
      if (url === '/api/categorias' && metodo(init) === 'GET') {
        gets += 1;
        return jsonOk({ categorias: CATEGORIAS });
      }
      if (url === '/api/categorias/c-1' && init?.method === 'PUT') {
        return jsonOk({ categoria: { id: 'c-1', codigo: '04.009', nome: 'Civis' } });
      }
      throw new Error(`URL inesperada: ${url} (${metodo(init)})`);
    });

    const user = userEvent.setup();
    render(<CategoriasSection />);
    await screen.findByText('Materiais Civis');

    await user.click(screen.getByRole('button', { name: 'Editar categoria Materiais Civis' }));
    const codigo = screen.getByLabelText('Código');
    const nome = screen.getByLabelText('Nome');
    expect(codigo).toHaveValue('04.001');
    expect(nome).toHaveValue('Materiais Civis');
    await user.clear(codigo);
    await user.type(codigo, '04.009');
    await user.clear(nome);
    await user.type(nome, 'Civis');
    await user.click(screen.getByRole('button', { name: 'Salvar' }));

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/categorias/c-1',
        expect.objectContaining({
          method: 'PUT',
          body: JSON.stringify({ codigo: '04.009', nome: 'Civis' }),
        }),
      ),
    );
    await waitFor(() => expect(toastSuccess).toHaveBeenCalledWith('Categoria atualizada.'));
    await waitFor(() => expect(gets).toBe(2));
    expect(screen.queryByRole('button', { name: 'Salvar' })).not.toBeInTheDocument();
  });

  it('editar 409: mostra a mensagem do servidor e mantém a edição aberta', async () => {
    stubFetch((_url, init) => {
      if (metodo(init) === 'GET') return jsonOk({ categorias: CATEGORIAS });
      return jsonErro(409, 'já existe uma categoria com esse nome');
    });

    const user = userEvent.setup();
    render(<CategoriasSection />);
    await screen.findByText('Materiais Civis');
    await user.click(screen.getByRole('button', { name: 'Editar categoria Materiais Civis' }));
    await user.click(screen.getByRole('button', { name: 'Salvar' }));

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'já existe uma categoria com esse nome',
    );
    expect(screen.getByRole('button', { name: 'Salvar' })).toBeInTheDocument();
  });

  it('editar 404 (linha removida em outra sessão): fecha a edição, avisa e recarrega', async () => {
    let gets = 0;
    stubFetch((_url, init) => {
      if (metodo(init) === 'GET') {
        gets += 1;
        return jsonOk({ categorias: CATEGORIAS });
      }
      return jsonErro(404, 'categoria não encontrada');
    });

    const user = userEvent.setup();
    render(<CategoriasSection />);
    await screen.findByText('Materiais Civis');
    await user.click(screen.getByRole('button', { name: 'Editar categoria Materiais Civis' }));
    await user.click(screen.getByRole('button', { name: 'Salvar' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('Categoria não encontrada.');
    expect(screen.queryByRole('button', { name: 'Salvar' })).not.toBeInTheDocument();
    await waitFor(() => expect(gets).toBe(2));
  });

  it('cancelar edição volta à linha sem chamar PUT', async () => {
    const fetchMock = stubFetch(() => jsonOk({ categorias: CATEGORIAS }));
    const user = userEvent.setup();
    render(<CategoriasSection />);
    await screen.findByText('Materiais Civis');
    await user.click(screen.getByRole('button', { name: 'Editar categoria Materiais Civis' }));
    await user.click(screen.getByRole('button', { name: 'Cancelar' }));

    expect(screen.getByText('Materiais Civis')).toBeInTheDocument();
    expect(fetchMock.mock.calls.some(([, init]) => init?.method === 'PUT')).toBe(false);
  });

  it('"Excluir" abre o ConfirmDialog e confirmar dispara DELETE + toast + GET refeito', async () => {
    let gets = 0;
    const fetchMock = stubFetch((url, init) => {
      if (url === '/api/categorias' && metodo(init) === 'GET') {
        gets += 1;
        return jsonOk({ categorias: gets === 1 ? CATEGORIAS : [CATEGORIAS[1]] });
      }
      if (url === '/api/categorias/c-1' && init?.method === 'DELETE') {
        return Promise.resolve({ ok: true, status: 204, json: async () => ({}) });
      }
      throw new Error(`URL inesperada: ${url} (${metodo(init)})`);
    });

    const user = userEvent.setup();
    render(<CategoriasSection />);
    await screen.findByText('Materiais Civis');

    await user.click(screen.getByRole('button', { name: 'Excluir categoria Materiais Civis' }));
    const dialog = await screen.findByRole('alertdialog');
    expect(fetchMock.mock.calls.some(([, init]) => init?.method === 'DELETE')).toBe(false);
    await user.click(within(dialog).getByRole('button', { name: 'Excluir' }));

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/categorias/c-1',
        expect.objectContaining({ method: 'DELETE' }),
      ),
    );
    await waitFor(() => expect(toastSuccess).toHaveBeenCalledWith('Categoria excluída.'));
    await waitFor(() => expect(gets).toBe(2));
    expect(screen.queryByText('Materiais Civis')).not.toBeInTheDocument();
  });

  it('cancelar o ConfirmDialog não dispara DELETE', async () => {
    const fetchMock = stubFetch(() => jsonOk({ categorias: CATEGORIAS }));
    const user = userEvent.setup();
    render(<CategoriasSection />);
    await screen.findByText('Materiais Civis');

    await user.click(screen.getByRole('button', { name: 'Excluir categoria Materiais Civis' }));
    const dialog = await screen.findByRole('alertdialog');
    await user.click(within(dialog).getByRole('button', { name: 'Cancelar' }));

    await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument());
    expect(fetchMock.mock.calls.some(([, init]) => init?.method === 'DELETE')).toBe(false);
    expect(screen.getByText('Materiais Civis')).toBeInTheDocument();
  });

  it('excluir 409 (em uso): mostra a mensagem do servidor com a contagem e mantém a linha', async () => {
    stubFetch((_url, init) => {
      if (metodo(init) === 'GET') return jsonOk({ categorias: CATEGORIAS });
      return jsonErro(409, 'a categoria não pode ser excluída: está em uso por 3 produtos');
    });

    const user = userEvent.setup();
    render(<CategoriasSection />);
    await screen.findByText('Materiais Civis');
    await user.click(screen.getByRole('button', { name: 'Excluir categoria Materiais Civis' }));
    const dialog = await screen.findByRole('alertdialog');
    await user.click(within(dialog).getByRole('button', { name: 'Excluir' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('está em uso por 3 produtos');
    expect(toastSuccess).not.toHaveBeenCalled();
    expect(screen.getByText('Materiais Civis')).toBeInTheDocument();
  });

  it('excluir 404 (linha removida em outra sessão): avisa e recarrega a lista', async () => {
    let gets = 0;
    stubFetch((_url, init) => {
      if (metodo(init) === 'GET') {
        gets += 1;
        return jsonOk({ categorias: CATEGORIAS });
      }
      return jsonErro(404, 'categoria não encontrada');
    });

    const user = userEvent.setup();
    render(<CategoriasSection />);
    await screen.findByText('Materiais Civis');
    await user.click(screen.getByRole('button', { name: 'Excluir categoria Materiais Civis' }));
    const dialog = await screen.findByRole('alertdialog');
    await user.click(within(dialog).getByRole('button', { name: 'Excluir' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('Categoria não encontrada.');
    await waitFor(() => expect(gets).toBe(2));
  });

  it('excluir 500: mostra role="alert" genérico', async () => {
    stubFetch((_url, init) => {
      if (metodo(init) === 'GET') return jsonOk({ categorias: CATEGORIAS });
      return jsonErro(500, 'interno');
    });

    const user = userEvent.setup();
    render(<CategoriasSection />);
    await screen.findByText('Materiais Civis');
    await user.click(screen.getByRole('button', { name: 'Excluir categoria Materiais Civis' }));
    const dialog = await screen.findByRole('alertdialog');
    await user.click(within(dialog).getByRole('button', { name: 'Excluir' }));

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível excluir a categoria agora.',
    );
  });
});

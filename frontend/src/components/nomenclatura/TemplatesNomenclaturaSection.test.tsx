import { describe, expect, it, vi, afterEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { TemplatesNomenclaturaSection } from './TemplatesNomenclaturaSection';

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

const TEMPLATES = [
  { id: 't-1', subtipo: 'Cabos — Rede', template: 'CABO REDE [CAT] [COR]' },
  { id: 't-2', subtipo: 'Genérico', template: '[NOME LIVRE]' },
];

const URL_LISTA = '/api/nomenclatura-templates';
const metodo = (init?: RequestInit) => init?.method ?? 'GET';

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe('TemplatesNomenclaturaSection', () => {
  it('lista os templates de GET /api/nomenclatura-templates', async () => {
    stubFetch((url) => {
      if (url === URL_LISTA) return jsonOk({ templates: TEMPLATES });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<TemplatesNomenclaturaSection />);

    expect(await screen.findByText('Cabos — Rede')).toBeInTheDocument();
    expect(screen.getByText('CABO REDE [CAT] [COR]')).toBeInTheDocument();
    expect(screen.getByText('Genérico')).toBeInTheDocument();
  });

  it('lista vazia mostra a mensagem', async () => {
    stubFetch(() => jsonOk({ templates: [] }));
    render(<TemplatesNomenclaturaSection />);
    expect(await screen.findByText('Nenhum template cadastrado ainda.')).toBeInTheDocument();
  });

  it('GET !ok no mount mostra role="alert" genérico', async () => {
    stubFetch(() => jsonErro(500, 'x'));
    render(<TemplatesNomenclaturaSection />);
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível carregar os templates de nomenclatura.',
    );
  });

  it('inputs espelham o limite do banco (maxLength 255)', async () => {
    stubFetch(() => jsonOk({ templates: [] }));
    render(<TemplatesNomenclaturaSection />);
    await screen.findByText('Nenhum template cadastrado ainda.');
    expect(screen.getByLabelText('Subtipo do template')).toHaveAttribute('maxlength', '255');
    expect(screen.getByLabelText('Estrutura do template')).toHaveAttribute('maxlength', '255');
  });

  it('cadastro 201: POST {subtipo,template}, toast, GET refeito e inputs limpos', async () => {
    let gets = 0;
    const fetchMock = stubFetch((url, init) => {
      if (url === URL_LISTA && metodo(init) === 'GET') {
        gets += 1;
        return jsonOk({ templates: gets === 1 ? [] : [TEMPLATES[0]] });
      }
      if (url === URL_LISTA && init?.method === 'POST') {
        return Promise.resolve({
          ok: true,
          status: 201,
          json: async () => ({ template: TEMPLATES[0] }),
        });
      }
      throw new Error(`URL inesperada: ${url} (${metodo(init)})`);
    });

    const user = userEvent.setup();
    render(<TemplatesNomenclaturaSection />);
    await screen.findByText('Nenhum template cadastrado ainda.');

    await user.type(screen.getByLabelText('Subtipo do template'), 'Cabos — Rede');
    // `[` é caractere especial do user-event: `[[` digita um `[` literal.
    await user.type(screen.getByLabelText('Estrutura do template'), 'CABO [[TIPO]');
    await user.click(screen.getByRole('button', { name: 'Adicionar template' }));

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        URL_LISTA,
        expect.objectContaining({
          method: 'POST',
          body: JSON.stringify({ subtipo: 'Cabos — Rede', template: 'CABO [TIPO]' }),
        }),
      ),
    );
    await waitFor(() => expect(toastSuccess).toHaveBeenCalledWith('Template criado.'));
    await waitFor(() => expect(gets).toBe(2));
    expect(screen.getByLabelText('Subtipo do template')).toHaveValue('');
    expect(screen.getByLabelText('Estrutura do template')).toHaveValue('');
  });

  it.each([
    [409, 'já existe um template com esse subtipo'],
    [400, 'o template deve conter ao menos um campo entre colchetes'],
  ])('cadastro %i: mostra a mensagem do servidor e mantém os inputs', async (status, mensagem) => {
    stubFetch((_url, init) => {
      if (metodo(init) === 'GET') return jsonOk({ templates: [] });
      return jsonErro(status, mensagem);
    });

    const user = userEvent.setup();
    render(<TemplatesNomenclaturaSection />);
    await screen.findByText('Nenhum template cadastrado ainda.');

    await user.type(screen.getByLabelText('Subtipo do template'), 'X1');
    await user.type(screen.getByLabelText('Estrutura do template'), 'SEM TOKEN');
    await user.click(screen.getByRole('button', { name: 'Adicionar template' }));

    expect(await screen.findByRole('alert')).toHaveTextContent(mensagem);
    expect(toastSuccess).not.toHaveBeenCalled();
    expect(screen.getByLabelText('Subtipo do template')).toHaveValue('X1');
  });

  it('cadastro 500: mostra role="alert" genérico', async () => {
    stubFetch((_url, init) => {
      if (metodo(init) === 'GET') return jsonOk({ templates: [] });
      return jsonErro(500, 'interno');
    });

    const user = userEvent.setup();
    render(<TemplatesNomenclaturaSection />);
    await screen.findByText('Nenhum template cadastrado ainda.');
    await user.type(screen.getByLabelText('Subtipo do template'), 'X1');
    await user.type(screen.getByLabelText('Estrutura do template'), 'SEM TOKEN');
    await user.click(screen.getByRole('button', { name: 'Adicionar template' }));

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível cadastrar o template agora.',
    );
  });

  it('editar inline: PUT com subtipo/template novos, toast e GET refeito', async () => {
    let gets = 0;
    const fetchMock = stubFetch((url, init) => {
      if (url === URL_LISTA && metodo(init) === 'GET') {
        gets += 1;
        return jsonOk({ templates: TEMPLATES });
      }
      if (url === `${URL_LISTA}/t-1` && init?.method === 'PUT') {
        return jsonOk({ template: { id: 't-1', subtipo: 'Rede', template: 'REDE [CAT]' } });
      }
      throw new Error(`URL inesperada: ${url} (${metodo(init)})`);
    });

    const user = userEvent.setup();
    render(<TemplatesNomenclaturaSection />);
    await screen.findByText('Cabos — Rede');

    await user.click(screen.getByRole('button', { name: 'Editar template Cabos — Rede' }));
    const subtipo = screen.getByLabelText('Subtipo');
    const texto = screen.getByLabelText('Template');
    expect(subtipo).toHaveValue('Cabos — Rede');
    expect(texto).toHaveValue('CABO REDE [CAT] [COR]');
    await user.clear(subtipo);
    await user.type(subtipo, 'Rede');
    await user.clear(texto);
    await user.type(texto, 'REDE [[CAT]');
    await user.click(screen.getByRole('button', { name: 'Salvar' }));

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        `${URL_LISTA}/t-1`,
        expect.objectContaining({
          method: 'PUT',
          body: JSON.stringify({ subtipo: 'Rede', template: 'REDE [CAT]' }),
        }),
      ),
    );
    await waitFor(() => expect(toastSuccess).toHaveBeenCalledWith('Template atualizado.'));
    await waitFor(() => expect(gets).toBe(2));
    expect(screen.queryByRole('button', { name: 'Salvar' })).not.toBeInTheDocument();
  });

  it('editar 409: mostra a mensagem do servidor e mantém a edição aberta', async () => {
    stubFetch((_url, init) => {
      if (metodo(init) === 'GET') return jsonOk({ templates: TEMPLATES });
      return jsonErro(409, 'já existe um template com esse subtipo');
    });

    const user = userEvent.setup();
    render(<TemplatesNomenclaturaSection />);
    await screen.findByText('Cabos — Rede');
    await user.click(screen.getByRole('button', { name: 'Editar template Cabos — Rede' }));
    await user.click(screen.getByRole('button', { name: 'Salvar' }));

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'já existe um template com esse subtipo',
    );
    expect(screen.getByRole('button', { name: 'Salvar' })).toBeInTheDocument();
  });

  it('editar 404 (linha removida em outra sessão): fecha a edição, avisa e recarrega', async () => {
    let gets = 0;
    stubFetch((_url, init) => {
      if (metodo(init) === 'GET') {
        gets += 1;
        return jsonOk({ templates: TEMPLATES });
      }
      return jsonErro(404, 'template não encontrado');
    });

    const user = userEvent.setup();
    render(<TemplatesNomenclaturaSection />);
    await screen.findByText('Cabos — Rede');
    await user.click(screen.getByRole('button', { name: 'Editar template Cabos — Rede' }));
    await user.click(screen.getByRole('button', { name: 'Salvar' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('Template não encontrado.');
    expect(screen.queryByRole('button', { name: 'Salvar' })).not.toBeInTheDocument();
    await waitFor(() => expect(gets).toBe(2));
  });

  it('cancelar edição volta à linha sem chamar PUT', async () => {
    const fetchMock = stubFetch(() => jsonOk({ templates: TEMPLATES }));
    const user = userEvent.setup();
    render(<TemplatesNomenclaturaSection />);
    await screen.findByText('Cabos — Rede');
    await user.click(screen.getByRole('button', { name: 'Editar template Cabos — Rede' }));
    await user.click(screen.getByRole('button', { name: 'Cancelar' }));

    expect(screen.getByText('Cabos — Rede')).toBeInTheDocument();
    expect(fetchMock.mock.calls.some(([, init]) => init?.method === 'PUT')).toBe(false);
  });

  it('"Excluir" abre o ConfirmDialog e confirmar dispara DELETE + toast + GET refeito', async () => {
    let gets = 0;
    const fetchMock = stubFetch((url, init) => {
      if (url === URL_LISTA && metodo(init) === 'GET') {
        gets += 1;
        return jsonOk({ templates: gets === 1 ? TEMPLATES : [TEMPLATES[1]] });
      }
      if (url === `${URL_LISTA}/t-1` && init?.method === 'DELETE') {
        return Promise.resolve({ ok: true, status: 204, json: async () => ({}) });
      }
      throw new Error(`URL inesperada: ${url} (${metodo(init)})`);
    });

    const user = userEvent.setup();
    render(<TemplatesNomenclaturaSection />);
    await screen.findByText('Cabos — Rede');

    await user.click(screen.getByRole('button', { name: 'Excluir template Cabos — Rede' }));
    const dialog = await screen.findByRole('alertdialog');
    expect(fetchMock.mock.calls.some(([, init]) => init?.method === 'DELETE')).toBe(false);
    await user.click(within(dialog).getByRole('button', { name: 'Excluir' }));

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        `${URL_LISTA}/t-1`,
        expect.objectContaining({ method: 'DELETE' }),
      ),
    );
    await waitFor(() => expect(toastSuccess).toHaveBeenCalledWith('Template excluído.'));
    await waitFor(() => expect(gets).toBe(2));
    expect(screen.queryByText('Cabos — Rede')).not.toBeInTheDocument();
  });

  it('cancelar o ConfirmDialog não dispara DELETE', async () => {
    const fetchMock = stubFetch(() => jsonOk({ templates: TEMPLATES }));
    const user = userEvent.setup();
    render(<TemplatesNomenclaturaSection />);
    await screen.findByText('Cabos — Rede');

    await user.click(screen.getByRole('button', { name: 'Excluir template Cabos — Rede' }));
    const dialog = await screen.findByRole('alertdialog');
    await user.click(within(dialog).getByRole('button', { name: 'Cancelar' }));

    await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument());
    expect(fetchMock.mock.calls.some(([, init]) => init?.method === 'DELETE')).toBe(false);
  });

  it('excluir 409 (em uso): mostra a mensagem do servidor com a contagem e mantém a linha', async () => {
    stubFetch((_url, init) => {
      if (metodo(init) === 'GET') return jsonOk({ templates: TEMPLATES });
      return jsonErro(409, 'o template não pode ser excluído: está em uso por 3 produtos');
    });

    const user = userEvent.setup();
    render(<TemplatesNomenclaturaSection />);
    await screen.findByText('Cabos — Rede');
    await user.click(screen.getByRole('button', { name: 'Excluir template Cabos — Rede' }));
    const dialog = await screen.findByRole('alertdialog');
    await user.click(within(dialog).getByRole('button', { name: 'Excluir' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('está em uso por 3 produtos');
    expect(toastSuccess).not.toHaveBeenCalled();
    expect(screen.getByText('Cabos — Rede')).toBeInTheDocument();
  });

  it('excluir 404 (linha removida em outra sessão): avisa e recarrega a lista', async () => {
    let gets = 0;
    stubFetch((_url, init) => {
      if (metodo(init) === 'GET') {
        gets += 1;
        return jsonOk({ templates: TEMPLATES });
      }
      return jsonErro(404, 'template não encontrado');
    });

    const user = userEvent.setup();
    render(<TemplatesNomenclaturaSection />);
    await screen.findByText('Cabos — Rede');
    await user.click(screen.getByRole('button', { name: 'Excluir template Cabos — Rede' }));
    const dialog = await screen.findByRole('alertdialog');
    await user.click(within(dialog).getByRole('button', { name: 'Excluir' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('Template não encontrado.');
    await waitFor(() => expect(gets).toBe(2));
  });

  it('excluir 500: mostra role="alert" genérico', async () => {
    stubFetch((_url, init) => {
      if (metodo(init) === 'GET') return jsonOk({ templates: TEMPLATES });
      return jsonErro(500, 'interno');
    });

    const user = userEvent.setup();
    render(<TemplatesNomenclaturaSection />);
    await screen.findByText('Cabos — Rede');
    await user.click(screen.getByRole('button', { name: 'Excluir template Cabos — Rede' }));
    const dialog = await screen.findByRole('alertdialog');
    await user.click(within(dialog).getByRole('button', { name: 'Excluir' }));

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível excluir o template agora.',
    );
  });

  it('template-marcador ÚNICO: Excluir desabilitado com a explicação "fallback obrigatório"', async () => {
    const fetchMock = stubFetch(() => jsonOk({ templates: TEMPLATES }));
    const user = userEvent.setup();
    render(<TemplatesNomenclaturaSection />);
    await screen.findByText('Genérico');

    const excluir = screen.getByRole('button', { name: 'Excluir template Genérico' });
    expect(excluir).toBeDisabled();
    expect(screen.getByText(/Fallback obrigatório/)).toBeInTheDocument();
    expect(excluir).toHaveAccessibleDescription(/Fallback obrigatório/);
    await user.click(excluir);
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(fetchMock.mock.calls.some(([, init]) => init?.method === 'DELETE')).toBe(false);
    // Os demais templates continuam excluíveis.
    expect(screen.getByRole('button', { name: 'Excluir template Cabos — Rede' })).toBeEnabled();
  });

  it('com DOIS templates-marcador, ambos podem ser excluídos', async () => {
    stubFetch(() =>
      jsonOk({
        templates: [...TEMPLATES, { id: 't-3', subtipo: 'Livre 2', template: '[NOME LIVRE]' }],
      }),
    );
    render(<TemplatesNomenclaturaSection />);
    await screen.findByText('Livre 2');

    expect(screen.getByRole('button', { name: 'Excluir template Genérico' })).toBeEnabled();
    expect(screen.getByRole('button', { name: 'Excluir template Livre 2' })).toBeEnabled();
    expect(screen.queryByText(/Fallback obrigatório/)).not.toBeInTheDocument();
  });

  it('fallback único renomeado (subtipo diferente) continua protegido', async () => {
    stubFetch(() =>
      jsonOk({ templates: [{ id: 't-9', subtipo: 'Livre', template: '[NOME LIVRE]' }] }),
    );
    render(<TemplatesNomenclaturaSection />);
    await screen.findByText('Livre');
    expect(screen.getByRole('button', { name: 'Excluir template Livre' })).toBeDisabled();
  });
});

import { describe, expect, it, vi, afterEach, onTestFinished } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { FiliaisSection } from './FiliaisSection';

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

const FILIAIS = [
  { id: 'f-1', nome: 'Matriz', cnpj: null, inscricaoEstadual: null, endereco: null },
  { id: 'f-2', nome: 'Recife', cnpj: null, inscricaoEstadual: null, endereco: null },
];

const RECIFE_FISCAL = {
  id: 'f-2',
  nome: 'Recife',
  cnpj: '11222333000181',
  inscricaoEstadual: 'ISENTO',
  endereco: {
    logradouro: 'Av. Recife',
    numero: '1000',
    complemento: '',
    bairro: 'Areias',
    cidade: 'Recife',
    cep: '50780000',
    uf: 'PE',
  },
};

const metodo = (init?: RequestInit) => init?.method ?? 'GET';

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe('FiliaisSection', () => {
  it('lista as filiais de GET /api/filiais', async () => {
    stubFetch((url) => {
      if (url === '/api/filiais') return jsonOk({ filiais: FILIAIS });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<FiliaisSection />);

    expect(await screen.findByText('Matriz')).toBeInTheDocument();
    expect(screen.getByText('Recife')).toBeInTheDocument();
  });

  it('lista vazia mostra a mensagem', async () => {
    stubFetch(() => jsonOk({ filiais: [] }));
    render(<FiliaisSection />);
    expect(await screen.findByText('Nenhuma filial cadastrada ainda.')).toBeInTheDocument();
  });

  it('GET !ok no mount mostra role="alert" genérico', async () => {
    stubFetch(() => jsonErro(500, 'x'));
    render(<FiliaisSection />);
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível carregar as filiais.',
    );
  });

  it('cadastro 201: POST com {nome}, toast, input limpo e lista recarregada', async () => {
    let gets = 0;
    const fetchMock = stubFetch((url, init) => {
      if (url === '/api/filiais' && metodo(init) === 'GET') {
        gets += 1;
        return jsonOk({ filiais: gets === 1 ? [FILIAIS[0]] : FILIAIS });
      }
      if (url === '/api/filiais' && init?.method === 'POST') {
        return Promise.resolve({
          ok: true,
          status: 201,
          json: async () => ({ filial: FILIAIS[1] }),
        });
      }
      throw new Error(`URL inesperada: ${url}`);
    });

    const user = userEvent.setup();
    render(<FiliaisSection />);
    await screen.findByText('Matriz');

    const input = screen.getByLabelText('Nome da filial');
    await user.type(input, 'Recife');
    await user.click(screen.getByRole('button', { name: 'Adicionar filial' }));

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/filiais',
        expect.objectContaining({
          method: 'POST',
          body: JSON.stringify({
            nome: 'Recife',
            cnpj: '',
            inscricaoEstadual: '',
            endereco: {
              logradouro: '',
              numero: '',
              complemento: '',
              bairro: '',
              cidade: '',
              cep: '',
              uf: '',
            },
          }),
        }),
      ),
    );
    await waitFor(() => expect(toastSuccess).toHaveBeenCalledWith('Filial criada.'));
    expect(await screen.findByText('Recife')).toBeInTheDocument();
    expect(input).toHaveValue('');
  });

  it('cadastro 409: mostra a mensagem do servidor e não limpa o input', async () => {
    stubFetch((url, init) => {
      if (url === '/api/filiais' && metodo(init) === 'GET') return jsonOk({ filiais: FILIAIS });
      if (url === '/api/filiais' && init?.method === 'POST') {
        return jsonErro(409, 'já existe uma filial com esse nome');
      }
      throw new Error(`URL inesperada: ${url}`);
    });

    const user = userEvent.setup();
    render(<FiliaisSection />);
    await screen.findByText('Matriz');

    const input = screen.getByLabelText('Nome da filial');
    await user.type(input, 'matriz');
    await user.click(screen.getByRole('button', { name: 'Adicionar filial' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('já existe uma filial com esse nome');
    expect(toastSuccess).not.toHaveBeenCalled();
    expect(input).toHaveValue('matriz');
  });

  it('botão desabilitado com o nome em branco e maxLength 255', async () => {
    stubFetch(() => jsonOk({ filiais: [] }));
    const user = userEvent.setup();
    render(<FiliaisSection />);
    await screen.findByText('Nenhuma filial cadastrada ainda.');

    const botao = screen.getByRole('button', { name: 'Adicionar filial' });
    expect(botao).toBeDisabled();
    expect(screen.getByLabelText('Nome da filial')).toHaveAttribute('maxlength', '255');
    await user.type(screen.getByLabelText('Nome da filial'), 'Nova');
    expect(botao).toBeEnabled();
  });

  it('lista mostra CNPJ formatado e Cidade/UF ("—" quando ausentes)', async () => {
    stubFetch(() => jsonOk({ filiais: [FILIAIS[0], RECIFE_FISCAL] }));
    render(<FiliaisSection />);
    expect(await screen.findByText('11.222.333/0001-81')).toBeInTheDocument();
    expect(screen.getByText('Recife/PE')).toBeInTheDocument();
    expect(screen.getByRole('columnheader', { name: 'CNPJ' })).toBeInTheDocument();
    expect(screen.getByRole('columnheader', { name: 'Cidade/UF' })).toBeInTheDocument();
    expect(screen.getAllByRole('cell', { name: '—' })).toHaveLength(2); // Matriz sem CNPJ e sem cidade
  });

  it('cadastro com dados fiscais envia CNPJ, IE e endereço no POST', async () => {
    const fetchMock = stubFetch((url, init) => {
      if (url === '/api/filiais' && metodo(init) === 'GET') return jsonOk({ filiais: [] });
      if (url === '/api/filiais' && init?.method === 'POST') {
        return Promise.resolve({ ok: true, status: 201, json: async () => ({ filial: RECIFE_FISCAL }) });
      }
      throw new Error(`URL inesperada: ${url}`);
    });
    const user = userEvent.setup();
    render(<FiliaisSection />);
    await screen.findByText('Nenhuma filial cadastrada ainda.');

    await user.type(screen.getByLabelText('Nome da filial'), 'Recife');
    await user.type(screen.getByLabelText('CNPJ (opcional)'), '11.222.333/0001-81');
    await user.type(screen.getByLabelText('Inscrição Estadual (opcional)'), 'ISENTO');
    await user.type(screen.getByLabelText('Logradouro'), 'Av. Recife');
    await user.type(screen.getByLabelText('Número'), '1000');
    await user.type(screen.getByLabelText('Bairro'), 'Areias');
    await user.type(screen.getByLabelText('Cidade'), 'Recife');
    await user.type(screen.getByLabelText('CEP'), '50780-000');
    await user.selectOptions(screen.getByLabelText('UF'), 'PE');
    await user.click(screen.getByRole('button', { name: 'Adicionar filial' }));

    await waitFor(() => expect(toastSuccess).toHaveBeenCalledWith('Filial criada.'));
    const post = fetchMock.mock.calls.find(([, init]) => init?.method === 'POST');
    expect(JSON.parse(String(post?.[1]?.body))).toEqual({
      nome: 'Recife',
      cnpj: '11.222.333/0001-81',
      inscricaoEstadual: 'ISENTO',
      endereco: {
        logradouro: 'Av. Recife',
        numero: '1000',
        complemento: '',
        bairro: 'Areias',
        cidade: 'Recife',
        cep: '50780-000',
        uf: 'PE',
      },
    });
  });

  it('Editar abre o formulário preenchido e salva com PUT /api/filiais/{id}', async () => {
    let gets = 0;
    const fetchMock = stubFetch((url, init) => {
      if (url === '/api/filiais' && metodo(init) === 'GET') {
        gets += 1;
        return jsonOk({ filiais: gets === 1 ? FILIAIS : [FILIAIS[0], RECIFE_FISCAL] });
      }
      if (url === '/api/filiais/f-2' && init?.method === 'PUT') {
        return jsonOk({ filial: RECIFE_FISCAL });
      }
      throw new Error(`URL inesperada: ${metodo(init)} ${url}`);
    });
    const user = userEvent.setup();
    render(<FiliaisSection />);
    await screen.findByText('Recife');

    await user.click(screen.getByRole('button', { name: 'Editar Recife' }));
    expect(screen.getByLabelText('Nome da filial')).toHaveValue('Recife');
    expect(screen.getByRole('heading', { name: 'Editar filial' })).toBeInTheDocument();

    await user.type(screen.getByLabelText('CNPJ (opcional)'), '11222333000181');
    await user.click(screen.getByRole('button', { name: 'Salvar alterações' }));

    await waitFor(() => expect(toastSuccess).toHaveBeenCalledWith('Filial atualizada.'));
    const put = fetchMock.mock.calls.find(([, init]) => init?.method === 'PUT');
    expect(JSON.parse(String(put?.[1]?.body))).toMatchObject({ nome: 'Recife', cnpj: '11222333000181' });
    expect(await screen.findByText('11.222.333/0001-81')).toBeInTheDocument();
    // Volta para o modo de cadastro, com o formulário limpo.
    expect(screen.getByRole('button', { name: 'Adicionar filial' })).toBeInTheDocument();
    expect(screen.getByLabelText('Nome da filial')).toHaveValue('');
  });

  it('Editar preenche CNPJ, IE e endereço já gravados; Cancelar volta ao cadastro', async () => {
    stubFetch(() => jsonOk({ filiais: [RECIFE_FISCAL] }));
    const user = userEvent.setup();
    render(<FiliaisSection />);
    await screen.findByText('Recife/PE');

    await user.click(screen.getByRole('button', { name: 'Editar Recife' }));
    expect(screen.getByLabelText('CNPJ (opcional)')).toHaveValue('11.222.333/0001-81');
    expect(screen.getByLabelText('Inscrição Estadual (opcional)')).toHaveValue('ISENTO');
    expect(screen.getByLabelText('Logradouro')).toHaveValue('Av. Recife');
    expect(screen.getByLabelText('CEP')).toHaveValue('50780-000');
    expect(screen.getByLabelText('UF')).toHaveValue('PE');

    await user.click(screen.getByRole('button', { name: 'Cancelar' }));
    expect(screen.getByLabelText('Nome da filial')).toHaveValue('');
    expect(screen.getByRole('button', { name: 'Adicionar filial' })).toBeInTheDocument();
  });

  it('edição 400: mostra a mensagem do servidor e mantém o formulário', async () => {
    stubFetch((url, init) => {
      if (url === '/api/filiais' && metodo(init) === 'GET') return jsonOk({ filiais: FILIAIS });
      if (url === '/api/filiais/f-1' && init?.method === 'PUT') {
        return jsonErro(400, 'endereço incompleto: preencha também logradouro, número, bairro, CEP, UF');
      }
      throw new Error(`URL inesperada: ${url}`);
    });
    const user = userEvent.setup();
    render(<FiliaisSection />);
    await screen.findByText('Matriz');

    await user.click(screen.getByRole('button', { name: 'Editar Matriz' }));
    await user.type(screen.getByLabelText('Cidade'), 'Recife');
    await user.click(screen.getByRole('button', { name: 'Salvar alterações' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('endereço incompleto');
    expect(toastSuccess).not.toHaveBeenCalled();
    expect(screen.getByLabelText('Cidade')).toHaveValue('Recife');
  });

  it('Editar com alterações não salvas pergunta antes de descartar', async () => {
    stubFetch(() => jsonOk({ filiais: [FILIAIS[0], RECIFE_FISCAL] }));
    const user = userEvent.setup();
    render(<FiliaisSection />);
    await screen.findByText('Recife/PE');

    await user.type(screen.getByLabelText('Nome da filial'), 'Rascunho');
    await user.click(screen.getByRole('button', { name: 'Editar Recife' }));

    // Diálogo aberto; o rascunho continua no formulário.
    expect(await screen.findByText('Descartar alterações não salvas?')).toBeInTheDocument();
    expect(screen.getByLabelText('Nome da filial')).toHaveValue('Rascunho');

    await user.click(screen.getByRole('button', { name: 'Continuar editando' }));
    await waitFor(() =>
      expect(screen.queryByText('Descartar alterações não salvas?')).not.toBeInTheDocument(),
    );
    expect(screen.getByLabelText('Nome da filial')).toHaveValue('Rascunho');

    await user.click(screen.getByRole('button', { name: 'Editar Recife' }));
    await user.click(await screen.findByRole('button', { name: 'Descartar e editar' }));
    await waitFor(() => expect(screen.getByLabelText('Nome da filial')).toHaveValue('Recife'));
    expect(screen.getByLabelText('CNPJ (opcional)')).toHaveValue('11.222.333/0001-81');
  });

  it('Editar sem alterações não pergunta, rola até o formulário e foca o nome', async () => {
    const scroll = vi.fn();
    const original = HTMLElement.prototype.scrollIntoView;
    HTMLElement.prototype.scrollIntoView = scroll;
    onTestFinished(() => {
      HTMLElement.prototype.scrollIntoView = original;
    });
    stubFetch(() => jsonOk({ filiais: FILIAIS }));
    const user = userEvent.setup();
    render(<FiliaisSection />);
    await screen.findByText('Matriz');

    await user.click(screen.getByRole('button', { name: 'Editar Matriz' }));
    expect(screen.queryByText('Descartar alterações não salvas?')).not.toBeInTheDocument();
    await waitFor(() => expect(screen.getByLabelText('Nome da filial')).toHaveFocus());
    expect(scroll).toHaveBeenCalled();
  });

  it('"Editar" fica desabilitado enquanto um salvamento está em andamento', async () => {
    let liberar: ((v: unknown) => void) | undefined;
    stubFetch((url, init) => {
      if (url === '/api/filiais' && metodo(init) === 'GET') return jsonOk({ filiais: FILIAIS });
      if (url === '/api/filiais' && init?.method === 'POST') {
        return new Promise((resolve) => {
          liberar = () => resolve({ ok: true, status: 201, json: async () => ({ filial: FILIAIS[1] }) });
        });
      }
      throw new Error(`URL inesperada: ${url}`);
    });
    const user = userEvent.setup();
    render(<FiliaisSection />);
    await screen.findByText('Matriz');

    await user.type(screen.getByLabelText('Nome da filial'), 'Nova');
    await user.click(screen.getByRole('button', { name: 'Adicionar filial' }));
    expect(await screen.findByRole('button', { name: 'Adicionando...' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Editar Matriz' })).toBeDisabled();

    liberar?.(undefined);
    await waitFor(() => expect(screen.getByRole('button', { name: 'Editar Matriz' })).toBeEnabled());
  });
});

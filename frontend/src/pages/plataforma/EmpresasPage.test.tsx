import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { EmpresasPage } from './EmpresasPage';
import { limparTokenPlataforma, setTokenPlataforma, type EmpresaResumo } from '@/lib/plataforma';

const navigateMock = vi.fn();

vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom');
  return { ...actual, useNavigate: () => navigateMock };
});

const { toastSuccess, toastError } = vi.hoisted(() => ({
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
}));
vi.mock('sonner', () => ({ toast: { success: toastSuccess, error: toastError } }));

const EMPRESA: EmpresaResumo = {
  id: 'emp-1',
  nomeFantasia: 'Acme Obras',
  razaoSocial: 'Acme Obras LTDA',
  cnpj: '11222333000181',
  endereco: {
    logradouro: 'Rua A',
    numero: '10',
    complemento: '',
    bairro: 'Centro',
    cidade: 'Recife',
    cep: '50000000',
    uf: 'PE',
  },
  slug: 'acme-obras',
  status: 'ativa',
  criadoEm: '2026-09-10T12:00:00Z',
  adm: { nome: 'Ana Adm', email: 'ana@acme.com' },
  treinamento: { id: 'emp-1-t', slug: 'acme-obras-treinamento', status: 'ativa' },
};

let empresas: EmpresaResumo[];
let criarResp: () => Promise<unknown>;
const fetchMock = vi.fn();

function ok(body: unknown, status = 200) {
  return Promise.resolve({ ok: true, status, json: async () => body });
}

function erro(status: number, code: string, message: string) {
  return Promise.resolve({ ok: false, status, json: async () => ({ error: { code, message } }) });
}

function chamadas(metodo: string, url: string) {
  return fetchMock.mock.calls.filter(
    ([u, init]) => u === url && ((init as RequestInit | undefined)?.method ?? 'GET') === metodo,
  );
}

function renderPage() {
  return render(
    <MemoryRouter>
      <EmpresasPage />
    </MemoryRouter>,
  );
}

// Cola cada valor de uma vez (um único evento de input por campo): digitar
// tecla a tecla em onze campos estoura o timeout de 5s sob a suíte paralela.
async function preencherFormulario(user: ReturnType<typeof userEvent.setup>) {
  const campos: Array<[string, string]> = [
    ['Nome Fantasia', 'Nova Construtora'],
    ['Razão Social', 'Nova Construtora LTDA'],
    ['CNPJ', '12345678000195'],
    ['Logradouro', 'Rua B'],
    ['Número', '20'],
    ['Bairro', 'Centro'],
    ['Cidade', 'Recife'],
    ['CEP', '50000000'],
    ['UF', 'pe'],
    ['Nome do primeiro administrador', 'Bia Adm'],
    ['E-mail do primeiro administrador', 'bia@nova.com'],
  ];
  for (const [rotulo, valor] of campos) {
    await user.click(screen.getByLabelText(rotulo));
    await user.paste(valor);
  }
}

describe('EmpresasPage (Story 9.2)', () => {
  beforeEach(() => {
    navigateMock.mockClear();
    toastSuccess.mockClear();
    toastError.mockClear();
    setTokenPlataforma('tk-dono');
    empresas = [EMPRESA];
    criarResp = () => ok({ empresa: { id: 'nova' }, treinamento: { id: 'nova-t' } }, 201);
    fetchMock.mockReset();
    fetchMock.mockImplementation((url: string, init?: RequestInit) => {
      const metodo = init?.method ?? 'GET';
      if (url === '/api/plataforma/empresas' && metodo === 'GET') {
        return ok({ empresas });
      }
      if (url === '/api/plataforma/empresas' && metodo === 'POST') {
        return criarResp();
      }
      if (url === '/api/plataforma/empresas/emp-1/desativacao') {
        empresas = [
          { ...EMPRESA, status: 'inativa', treinamento: { ...EMPRESA.treinamento!, status: 'inativa' } },
        ];
        return ok({ empresa: { id: 'emp-1', status: 'inativa' } });
      }
      if (url === '/api/plataforma/auth/logout') {
        return ok({}, 204);
      }
      return Promise.reject(new Error(`fetch não stubado: ${metodo} ${url}`));
    });
    vi.stubGlobal('fetch', fetchMock);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    limparTokenPlataforma();
  });

  it('mostra "Carregando..." e depois a lista só com metadado e os dois endereços de acesso', async () => {
    renderPage();

    expect(screen.getByText('Carregando...')).toBeInTheDocument();
    expect(await screen.findByText('Acme Obras')).toBeInTheDocument();

    expect(screen.getByText(/Acme Obras LTDA · CNPJ 11\.222\.333\/0001-81/)).toBeInTheDocument();
    expect(screen.getByText(/Rua A, 10 · Centro, Recife\/PE · CEP 50000-000/)).toBeInTheDocument();
    expect(screen.getByText(/Ana Adm \(ana@acme\.com\)/)).toBeInTheDocument();
    expect(screen.getByText('/e/acme-obras')).toBeInTheDocument();
    expect(screen.getByText('/e/acme-obras-treinamento')).toBeInTheDocument();
    expect(screen.getByText('Ativa')).toBeInTheDocument();
    expect(screen.getByText(/Criada em/)).toBeInTheDocument();
    expect(chamadas('GET', '/api/plataforma/empresas')[0][1].headers.Authorization).toBe('Bearer tk-dono');
  });

  it('pré-preenche o endereço de acesso a partir do Nome Fantasia até ser editado à mão', async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByText('Acme Obras');

    await user.type(screen.getByLabelText('Nome Fantasia'), 'Construtora Ávila');
    expect(screen.getByLabelText('Endereço de acesso')).toHaveValue('construtora-avila');
    expect(screen.getByText(/Treinamento: \/e\/construtora-avila-treinamento/)).toBeInTheDocument();

    await user.clear(screen.getByLabelText('Endereço de acesso'));
    await user.type(screen.getByLabelText('Endereço de acesso'), 'avila');
    await user.type(screen.getByLabelText('Nome Fantasia'), ' Filhos');

    expect(screen.getByLabelText('Endereço de acesso')).toHaveValue('avila');
  });

  it('cria a Empresa: envia o payload sem senha, mostra o toast e recarrega a lista', async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByText('Acme Obras');

    await preencherFormulario(user);
    await user.click(screen.getByRole('button', { name: 'Criar empresa' }));

    await waitFor(() =>
      expect(toastSuccess).toHaveBeenCalledWith(
        'Empresa criada. O administrador receberá um e-mail para definir a senha.',
      ),
    );
    const [, init] = chamadas('POST', '/api/plataforma/empresas')[0];
    const corpo = JSON.parse(init.body as string);
    expect(corpo).toEqual({
      nomeFantasia: 'Nova Construtora',
      razaoSocial: 'Nova Construtora LTDA',
      cnpj: '12345678000195',
      slug: 'nova-construtora',
      endereco: {
        logradouro: 'Rua B',
        numero: '20',
        complemento: '',
        bairro: 'Centro',
        cidade: 'Recife',
        cep: '50000000',
        uf: 'PE',
      },
      admNome: 'Bia Adm',
      admEmail: 'bia@nova.com',
    });
    await waitFor(() => expect(chamadas('GET', '/api/plataforma/empresas')).toHaveLength(2));
    expect(screen.getByLabelText('Nome Fantasia')).toHaveValue('');
  });

  it('409 mostra a mensagem do servidor num alerta, sem toast de sucesso', async () => {
    const user = userEvent.setup();
    criarResp = () => erro(409, 'CONFLICT', 'Já existe uma empresa com este CNPJ.');
    renderPage();
    await screen.findByText('Acme Obras');

    await preencherFormulario(user);
    await user.click(screen.getByRole('button', { name: 'Criar empresa' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('Já existe uma empresa com este CNPJ.');
    expect(toastSuccess).not.toHaveBeenCalled();
  });

  it('400 mostra a mensagem do campo vinda do servidor', async () => {
    const user = userEvent.setup();
    criarResp = () => erro(400, 'VALIDATION_ERROR', 'UF deve ter 2 letras');
    renderPage();
    await screen.findByText('Acme Obras');

    await preencherFormulario(user);
    await user.click(screen.getByRole('button', { name: 'Criar empresa' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('UF deve ter 2 letras');
  });

  it('desativar pede confirmação num diálogo destrutivo e recarrega a lista', async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByText('Acme Obras');

    await user.click(screen.getByRole('button', { name: 'Desativar Acme Obras' }));
    const dialog = await screen.findByRole('alertdialog');
    expect(dialog).toHaveTextContent('Ambiente de Treinamento');
    expect(dialog).toHaveTextContent('Nenhum dado é apagado');
    expect(chamadas('POST', '/api/plataforma/empresas/emp-1/desativacao')).toHaveLength(0);

    await user.click(within(dialog).getByRole('button', { name: 'Desativar' }));

    await waitFor(() =>
      expect(chamadas('POST', '/api/plataforma/empresas/emp-1/desativacao')).toHaveLength(1),
    );
    expect(await screen.findByRole('button', { name: 'Reativar Acme Obras' })).toBeInTheDocument();
    expect(screen.getByText('Inativa')).toBeInTheDocument();
  });

  it('cancelar o diálogo não desativa nada', async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByText('Acme Obras');

    await user.click(screen.getByRole('button', { name: 'Desativar Acme Obras' }));
    const dialog = await screen.findByRole('alertdialog');
    await user.click(within(dialog).getByRole('button', { name: 'Cancelar' }));

    await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument());
    expect(chamadas('POST', '/api/plataforma/empresas/emp-1/desativacao')).toHaveLength(0);
  });

  it('"Sair" encerra a sessão e volta ao login da Plataforma', async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByText('Acme Obras');

    await user.click(screen.getByRole('button', { name: 'Sair' }));

    await waitFor(() =>
      expect(navigateMock).toHaveBeenCalledWith('/plataforma/login', { replace: true }),
    );
    expect(chamadas('POST', '/api/plataforma/auth/logout')).toHaveLength(1);
  });

  it('sessão encerrada (401 sem refresh possível) leva ao login da Plataforma', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (url === '/api/plataforma/empresas' || url === '/api/plataforma/auth/refresh') {
        return erro(401, 'TOKEN_EXPIRED', 'sessão expirada');
      }
      return Promise.reject(new Error(`fetch não stubado: ${url}`));
    });
    renderPage();

    await waitFor(() =>
      expect(navigateMock).toHaveBeenCalledWith('/plataforma/login', { replace: true }),
    );
  });
});

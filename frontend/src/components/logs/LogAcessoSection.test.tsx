import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { LogAcessoSection } from './LogAcessoSection';

// useAuth() fornece o papel do ator — configurável por teste.
const authState = vi.hoisted(() => ({ papel: 'adm' as string }));

vi.mock('@/lib/auth', () => ({
  useAuth: () => ({
    estado: 'autenticado',
    usuario: { id: 'ator-1', nome: 'Adm', email: 'adm@empresa.com', papel: authState.papel },
    definirSessao: vi.fn(),
    logout: vi.fn(),
  }),
}));

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
  return Promise.resolve({ ok: true, json: async () => body });
}

const LOGS = [
  {
    id: 'l-1',
    usuarioId: 'u-1',
    usuarioNome: 'Ana Usuária',
    emailInformado: 'ana@empresa.com',
    metodo: 'senha',
    sucesso: true,
    ip: '203.0.113.7',
    criadoEm: '2026-08-15T14:30:00Z',
  },
  {
    id: 'l-2',
    usuarioId: null,
    usuarioNome: null,
    emailInformado: 'fantasma@empresa.com',
    metodo: 'sso',
    sucesso: false,
    ip: '198.51.100.9',
    criadoEm: '2026-08-14T09:00:00Z',
  },
];

beforeEach(() => {
  authState.papel = 'adm';
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe('LogAcessoSection', () => {
  it('mostra a tabela com as colunas e os valores mapeados', async () => {
    stubFetch((url) => {
      if (url === '/api/logs-acesso') return jsonOk({ logs: LOGS });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<LogAcessoSection />);

    expect(await screen.findByText('ana@empresa.com')).toBeInTheDocument();
    // Colunas do cabeçalho (EXPERIENCE.md: usuário/timestamp/IP/método).
    for (const coluna of ['Data/Hora', 'Usuário', 'E-mail informado', 'IP', 'Método', 'Resultado']) {
      expect(screen.getByRole('columnheader', { name: coluna })).toBeInTheDocument();
    }
    // Linha 1: conta identificável, IP, senha, sucesso.
    expect(screen.getByText('Ana Usuária')).toBeInTheDocument();
    expect(screen.getByText('203.0.113.7')).toBeInTheDocument();
    expect(screen.getByText('Senha')).toBeInTheDocument();
    expect(screen.getByText('Sucesso')).toBeInTheDocument();
    // Linha 2: sem conta -> travessão; SSO; falha.
    expect(screen.getByText('—')).toBeInTheDocument();
    expect(screen.getByText('198.51.100.9')).toBeInTheDocument();
    expect(screen.getByText('SSO')).toBeInTheDocument();
    expect(screen.getByText('Falha')).toBeInTheDocument();
  });

  it('não tem nenhum botão de editar/excluir em nenhuma linha', async () => {
    stubFetch((url) => {
      if (url === '/api/logs-acesso') return jsonOk({ logs: LOGS });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<LogAcessoSection />);
    await screen.findByText('ana@empresa.com');

    const botoes = screen.getAllByRole('button').map((b) => b.textContent);
    // O único botão da seção é "Filtrar".
    expect(botoes).toEqual(['Filtrar']);
    for (const rotulo of [/editar/i, /excluir/i, /remover/i, /apagar/i]) {
      expect(screen.queryByRole('button', { name: rotulo })).not.toBeInTheDocument();
    }
  });

  it('preencher datas + "Filtrar" refaz o fetch com os limites do dia LOCAL em RFC3339', async () => {
    const fetchMock = stubFetch((url) => {
      if (url.startsWith('/api/logs-acesso')) return jsonOk({ logs: LOGS });
      throw new Error(`URL inesperada: ${url}`);
    });

    const user = userEvent.setup();
    render(<LogAcessoSection />);
    await screen.findByText('ana@empresa.com');

    // Carga inicial: sem query string.
    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/logs-acesso', expect.anything());

    fireEvent.change(screen.getByLabelText('Início'), { target: { value: '2026-08-01' } });
    fireEvent.change(screen.getByLabelText('Fim'), { target: { value: '2026-08-15' } });
    await user.click(screen.getByRole('button', { name: 'Filtrar' }));

    // Expectativa computada com a MESMA lógica do componente -> agnóstica ao
    // fuso da máquina que roda o teste.
    const esperado = new URLSearchParams();
    esperado.set('inicio', new Date('2026-08-01T00:00:00').toISOString());
    esperado.set('fim', new Date('2026-08-15T23:59:59.999').toISOString());

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        `/api/logs-acesso?${esperado.toString()}`,
        expect.anything(),
      ),
    );
  });

  it('res.ok === false no mount mostra um role="alert"', async () => {
    stubFetch((url) => {
      if (url === '/api/logs-acesso') {
        return Promise.resolve({
          ok: false,
          status: 500,
          json: async () => ({ error: { code: 'INTERNAL_ERROR' } }),
        });
      }
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<LogAcessoSection />);

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível carregar o log de acesso. Recarregue a página.',
    );
  });

  it('resultado no teto de 500 mostra o aviso de resultado capado', async () => {
    const logs500 = Array.from({ length: 500 }, (_, i) => ({
      id: `l-${i}`,
      usuarioId: null,
      usuarioNome: null,
      emailInformado: `u${i}@x.com`,
      metodo: 'senha',
      sucesso: true,
      ip: '203.0.113.7',
      criadoEm: '2026-08-15T14:30:00Z',
    }));
    stubFetch((url) => {
      if (url.startsWith('/api/logs-acesso')) return jsonOk({ logs: logs500 });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<LogAcessoSection />);

    expect(
      await screen.findByText(/Cada consulta mostra no máximo 500 registros/),
    ).toBeInTheDocument();
  });

  it('resultado abaixo de 500 NÃO mostra o aviso de resultado capado', async () => {
    stubFetch((url) => {
      if (url.startsWith('/api/logs-acesso')) return jsonOk({ logs: LOGS });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<LogAcessoSection />);
    await screen.findByText('ana@empresa.com');

    expect(
      screen.queryByText(/Cada consulta mostra no máximo 500 registros/),
    ).not.toBeInTheDocument();
  });

  it('descarta a resposta de uma carga antiga que chega depois de uma nova (out-of-order)', async () => {
    // Holder (não `let`) para o TS não estreitar o tipo a `null` no ponto de
    // uso — a atribuição acontece dentro do executor do Promise.
    const ctrl: { resolver: (() => void) | null } = { resolver: null };
    let chamada = 0;
    stubFetch((url) => {
      if (!url.startsWith('/api/logs-acesso')) {
        throw new Error(`URL inesperada: ${url}`);
      }
      chamada += 1;
      if (chamada === 1) {
        // Carga do mount: fica pendente até ctrl.resolver() ser chamado.
        return new Promise((resolve) => {
          ctrl.resolver = () => resolve({ ok: true, json: async () => ({ logs: LOGS }) });
        });
      }
      // 2ª chamada (Filtrar): responde já, com outro conjunto.
      return jsonOk({
        logs: [{ ...LOGS[0], id: 'novo-1', emailInformado: 'novo@empresa.com' }],
      });
    });

    const user = userEvent.setup();
    render(<LogAcessoSection />);

    await user.click(screen.getByRole('button', { name: 'Filtrar' }));
    expect(await screen.findByText('novo@empresa.com')).toBeInTheDocument();

    // A carga antiga (mount) resolve agora — não pode sobrescrever a nova.
    ctrl.resolver?.();
    await waitFor(() =>
      expect(screen.queryByText('ana@empresa.com')).not.toBeInTheDocument(),
    );
    expect(screen.getByText('novo@empresa.com')).toBeInTheDocument();
  });

  it('não monta nem chama GET /api/logs-acesso quando o papel é abaixo de adm', async () => {
    authState.papel = 'gestor';
    const fetchMock = stubFetch((url) => {
      throw new Error(`URL inesperada: ${url}`);
    });

    const { container } = render(<LogAcessoSection />);

    expect(container).toBeEmptyDOMElement();
    expect(fetchMock).not.toHaveBeenCalled();
  });
});

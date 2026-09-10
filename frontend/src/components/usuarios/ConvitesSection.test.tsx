import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { ConvitesSection } from './ConvitesSection';

// useAuth() fornece o papel do ator — configurável por teste.
const authState = vi.hoisted(() => ({ papel: 'gestor' as string }));

vi.mock('@/lib/auth', () => ({
  useAuth: () => ({
    estado: 'autenticado',
    usuario: { id: 'ator-1', nome: 'Ator', email: 'ator@empresa.com', papel: authState.papel },
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

function jsonErro(status: number, code: string) {
  return Promise.resolve({
    ok: false,
    status,
    json: async () => ({ error: { code, message: 'x' } }),
  });
}

const LINK_PENDENTE = 'http://app.local/e/acme/cadastro?token=abc123';

const CONVITES = [
  {
    id: 'c-1',
    email: 'pendente@x.com',
    situacao: 'pendente',
    expiraEm: '2026-09-17T12:00:00Z',
    criadoEm: '2026-09-10T12:00:00Z',
    criadoPorNome: 'Maria',
    link: LINK_PENDENTE,
  },
  {
    id: 'c-2',
    email: 'usado@x.com',
    situacao: 'usado',
    expiraEm: '2026-09-17T12:00:00Z',
    criadoEm: '2026-09-09T12:00:00Z',
    criadoPorNome: 'Maria',
  },
];

beforeEach(() => {
  authState.papel = 'gestor';
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe('ConvitesSection', () => {
  it('não monta nada para papel abaixo de gestor', () => {
    authState.papel = 'almoxarife';
    const fn = stubFetch(() => jsonOk({ convites: [] }));

    const { container } = render(<ConvitesSection />);

    expect(container).toBeEmptyDOMElement();
    expect(fn).not.toHaveBeenCalled();
  });

  it('lista os convites de GET /api/convites com a situação de cada um', async () => {
    stubFetch((url) => {
      if (url === '/api/convites') return jsonOk({ convites: CONVITES });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<ConvitesSection />);

    expect(await screen.findByText('pendente@x.com')).toBeInTheDocument();
    expect(screen.getByText('usado@x.com')).toBeInTheDocument();
    expect(screen.getByText(/Pendente/)).toBeInTheDocument();
    expect(screen.getByText('Utilizado')).toBeInTheDocument();
  });

  it('mostra o link em campo somente-leitura só para os convites pendentes', async () => {
    stubFetch((url) => {
      if (url === '/api/convites') return jsonOk({ convites: CONVITES });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<ConvitesSection />);

    const campo = await screen.findByLabelText('Link do convite de pendente@x.com');
    expect(campo).toHaveValue(LINK_PENDENTE);
    expect(campo).toHaveAttribute('readonly');
    expect(screen.queryByLabelText('Link do convite de usado@x.com')).not.toBeInTheDocument();
  });

  it('emite o convite com POST /api/convites e exibe o link devolvido, recarregando a lista', async () => {
    const user = userEvent.setup();
    const fn = stubFetch((url, init) => {
      if (url === '/api/convites' && init?.method === 'POST') {
        return jsonOk({
          convite: { ...CONVITES[0], email: 'novo@x.com', link: LINK_PENDENTE },
        });
      }
      if (url === '/api/convites') return jsonOk({ convites: [] });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<ConvitesSection />);
    await screen.findByText('Nenhum convite emitido.');

    await user.type(screen.getByLabelText('E-mail da pessoa convidada'), 'novo@x.com');
    await user.click(screen.getByRole('button', { name: 'Convidar' }));

    const campo = await screen.findByLabelText('Link do convite');
    expect(campo).toHaveValue(LINK_PENDENTE);

    const post = fn.mock.calls.find(([, init]) => (init as RequestInit | undefined)?.method === 'POST');
    if (!post) {
      throw new Error('nenhum POST /api/convites foi disparado');
    }
    expect(JSON.parse((post[1] as RequestInit).body as string)).toEqual({ email: 'novo@x.com' });
    // Sucesso também refaz o GET: 1 do mount + 1 POST + 1 recarga.
    expect(fn).toHaveBeenCalledTimes(3);
  });

  it('traduz o 409 CONFLICT da emissão em "já tem conta" e não mostra link nenhum', async () => {
    const user = userEvent.setup();
    stubFetch((url, init) => {
      if (url === '/api/convites' && init?.method === 'POST') return jsonErro(409, 'CONFLICT');
      if (url === '/api/convites') return jsonOk({ convites: [] });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<ConvitesSection />);
    await screen.findByText('Nenhum convite emitido.');

    await user.type(screen.getByLabelText('E-mail da pessoa convidada'), 'jatem@x.com');
    await user.click(screen.getByRole('button', { name: 'Convidar' }));

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Este e-mail já tem conta nesta empresa.',
    );
    expect(screen.queryByLabelText('Link do convite')).not.toBeInTheDocument();
  });

  it('traduz o 400 VALIDATION_ERROR da emissão em "e-mail válido"', async () => {
    const user = userEvent.setup();
    stubFetch((url, init) => {
      if (url === '/api/convites' && init?.method === 'POST') {
        return jsonErro(400, 'VALIDATION_ERROR');
      }
      if (url === '/api/convites') return jsonOk({ convites: [] });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<ConvitesSection />);
    await screen.findByText('Nenhum convite emitido.');

    await user.type(screen.getByLabelText('E-mail da pessoa convidada'), 'invalido');
    await user.click(screen.getByRole('button', { name: 'Convidar' }));

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Informe um e-mail válido para convidar.',
    );
  });

  it('não chama a API quando o campo de e-mail está vazio', async () => {
    const user = userEvent.setup();
    const fn = stubFetch((url) => {
      if (url === '/api/convites') return jsonOk({ convites: [] });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<ConvitesSection />);
    await screen.findByText('Nenhum convite emitido.');

    await user.click(screen.getByRole('button', { name: 'Convidar' }));

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Informe um e-mail válido para convidar.',
    );
    // Só o GET do mount.
    expect(fn).toHaveBeenCalledTimes(1);
  });

  it('só revoga depois da confirmação no ConfirmDialog, e refaz a lista', async () => {
    const user = userEvent.setup();
    const fn = stubFetch((url, init) => {
      if (url === '/api/convites/c-1/revogacao' && init?.method === 'POST') {
        return jsonOk({ mensagem: 'Convite cancelado.' });
      }
      if (url === '/api/convites') return jsonOk({ convites: CONVITES });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<ConvitesSection />);
    await screen.findByText('pendente@x.com');

    await user.click(screen.getByRole('button', { name: 'Cancelar convite de pendente@x.com' }));
    // Enquanto o diálogo está aberto, nenhuma chamada de revogação aconteceu.
    expect(fn).toHaveBeenCalledTimes(1);

    await user.click(screen.getByRole('button', { name: 'Cancelar convite' }));

    await waitFor(() =>
      expect(fn).toHaveBeenCalledWith('/api/convites/c-1/revogacao', expect.anything()),
    );
    // 1 GET do mount + 1 POST + 1 recarga.
    await waitFor(() => expect(fn).toHaveBeenCalledTimes(3));
  });

  it('não revoga nada quando o ConfirmDialog é cancelado', async () => {
    const user = userEvent.setup();
    const fn = stubFetch((url) => {
      if (url === '/api/convites') return jsonOk({ convites: CONVITES });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<ConvitesSection />);
    await screen.findByText('pendente@x.com');

    await user.click(screen.getByRole('button', { name: 'Cancelar convite de pendente@x.com' }));
    await user.click(screen.getByRole('button', { name: 'Voltar' }));

    expect(fn).toHaveBeenCalledTimes(1);
  });

  it('mostra erro inline quando a carga da lista falha', async () => {
    stubFetch(() => jsonErro(500, 'INTERNAL_ERROR'));

    render(<ConvitesSection />);

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível carregar a lista de convites. Recarregue a página.',
    );
  });
});

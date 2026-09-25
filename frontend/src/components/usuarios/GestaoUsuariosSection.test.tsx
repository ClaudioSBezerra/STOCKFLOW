import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { GestaoUsuariosSection } from './GestaoUsuariosSection';

// useAuth() fornece o id/papel do ator — configuráveis por teste.
const authState = vi.hoisted(() => ({
  id: 'ator-1' as string,
  papel: 'gestor' as string,
  mfaObrigatorio: false as boolean,
}));

vi.mock('@/lib/auth', () => ({
  useAuth: () => ({
    estado: 'autenticado',
    usuario: { id: authState.id, nome: 'Ator', email: 'ator@empresa.com', papel: authState.papel, empresa: { mfaObrigatorio: authState.mfaObrigatorio } },
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

const CONTAS = [
  { id: 'u-1', nome: 'Ana Usuária', email: 'ana@empresa.com', papel: 'usuario', ativo: true },
  { id: 'a-1', nome: 'Bruno Almox', email: 'bruno@empresa.com', papel: 'almoxarife', ativo: true },
];

beforeEach(() => {
  authState.id = 'ator-1';
  authState.papel = 'gestor';
  authState.mfaObrigatorio = false;
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

const valorDe = (rotulo: string) =>
  screen.getByText(rotulo).parentElement?.lastElementChild as HTMLElement;

describe('GestaoUsuariosSection', () => {
  it('lista as contas recebidas de GET /api/usuarios', async () => {
    stubFetch((url) => {
      if (url === '/api/usuarios') return jsonOk({ usuarios: CONTAS });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<GestaoUsuariosSection />);

    expect(await screen.findByText('Ana Usuária')).toBeInTheDocument();
    expect(screen.getByText('Bruno Almox')).toBeInTheDocument();
    expect(fetch).toHaveBeenCalledWith('/api/usuarios', expect.anything());
  });

  it('não mostra botões de ação na linha do próprio ator', async () => {
    stubFetch((url) => {
      if (url === '/api/usuarios') {
        return jsonOk({
          usuarios: [
            { id: 'ator-1', nome: 'Eu Mesmo', email: 'ator@empresa.com', papel: 'gestor', ativo: true },
            ...CONTAS,
          ],
        });
      }
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<GestaoUsuariosSection />);

    expect(await screen.findByText('Eu Mesmo')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Desativar conta de Eu Mesmo/ })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Rebaixar Eu Mesmo/ })).not.toBeInTheDocument();
    // As demais linhas seguem com ação.
    expect(screen.getByRole('button', { name: 'Desativar conta de Ana Usuária' })).toBeInTheDocument();
  });

  it('não mostra "Rebaixar" numa linha de papel usuario (sem papel abaixo)', async () => {
    stubFetch((url) => {
      if (url === '/api/usuarios') return jsonOk({ usuarios: CONTAS });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<GestaoUsuariosSection />);

    await screen.findByText('Ana Usuária');
    expect(screen.queryByRole('button', { name: /Rebaixar Ana Usuária/ })).not.toBeInTheDocument();
    // O almoxarife pode ser rebaixado para Usuário.
    expect(
      screen.getByRole('button', { name: 'Rebaixar Bruno Almox para Usuário' }),
    ).toBeInTheDocument();
  });

  it('"Desativar" confirma no ConfirmDialog e chama POST .../desativacao {ativo:false} + refaz o GET', async () => {
    let listaChamadas = 0;
    const fetchMock = stubFetch((url, init) => {
      if (url === '/api/usuarios') {
        listaChamadas += 1;
        return jsonOk({ usuarios: CONTAS });
      }
      if (url === '/api/usuarios/u-1/desativacao' && init?.method === 'POST') {
        return jsonOk({ usuario: { ...CONTAS[0], ativo: false } });
      }
      throw new Error(`URL inesperada: ${url} (${init?.method ?? 'GET'})`);
    });

    const user = userEvent.setup();
    render(<GestaoUsuariosSection />);

    await user.click(await screen.findByRole('button', { name: 'Desativar conta de Ana Usuária' }));
    // ConfirmDialog aberto — nunca window.confirm().
    const dialog = await screen.findByRole('alertdialog');
    await user.click(within(dialog).getByRole('button', { name: 'Desativar' }));

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/usuarios/u-1/desativacao',
        expect.objectContaining({ method: 'POST', body: JSON.stringify({ ativo: false }) }),
      ),
    );
    await waitFor(() => expect(listaChamadas).toBe(2));
  });

  it('"Rebaixar" confirma no ConfirmDialog e chama POST .../rebaixamento + refaz o GET', async () => {
    let listaChamadas = 0;
    const fetchMock = stubFetch((url, init) => {
      if (url === '/api/usuarios') {
        listaChamadas += 1;
        return jsonOk({ usuarios: CONTAS });
      }
      if (url === '/api/usuarios/a-1/rebaixamento' && init?.method === 'POST') {
        return jsonOk({ usuario: { ...CONTAS[1], papel: 'usuario' } });
      }
      throw new Error(`URL inesperada: ${url} (${init?.method ?? 'GET'})`);
    });

    const user = userEvent.setup();
    render(<GestaoUsuariosSection />);

    await user.click(
      await screen.findByRole('button', { name: 'Rebaixar Bruno Almox para Usuário' }),
    );
    const dialog = await screen.findByRole('alertdialog');
    await user.click(within(dialog).getByRole('button', { name: 'Rebaixar' }));

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/usuarios/a-1/rebaixamento',
        expect.objectContaining({ method: 'POST' }),
      ),
    );
    const rebaixarInit = fetchMock.mock.calls.find(
      ([u]) => u === '/api/usuarios/a-1/rebaixamento',
    )?.[1] as RequestInit | undefined;
    expect(rebaixarInit?.body).toBeUndefined();
    await waitFor(() => expect(listaChamadas).toBe(2));
  });

  it('"Reativar" numa conta inativa é direto (sem ConfirmDialog)', async () => {
    const fetchMock = stubFetch((url, init) => {
      if (url === '/api/usuarios') {
        return jsonOk({
          usuarios: [{ ...CONTAS[1], ativo: false }],
        });
      }
      if (url === '/api/usuarios/a-1/desativacao' && init?.method === 'POST') {
        return jsonOk({ usuario: { ...CONTAS[1], ativo: true } });
      }
      throw new Error(`URL inesperada: ${url} (${init?.method ?? 'GET'})`);
    });

    const user = userEvent.setup();
    render(<GestaoUsuariosSection />);

    await user.click(await screen.findByRole('button', { name: 'Reativar conta de Bruno Almox' }));

    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/usuarios/a-1/desativacao',
        expect.objectContaining({ method: 'POST', body: JSON.stringify({ ativo: true }) }),
      ),
    );
  });

  it('falha ao carregar a lista mostra um role="alert"', async () => {
    stubFetch((url) => {
      if (url === '/api/usuarios') {
        return Promise.resolve({
          ok: false,
          status: 500,
          json: async () => ({ error: { code: 'INTERNAL_ERROR' } }),
        });
      }
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<GestaoUsuariosSection />);

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível carregar a lista de contas. Recarregue a página.',
    );
  });

  it('falha de ação (409) mostra role="alert" e refaz o GET /api/usuarios', async () => {
    let listaChamadas = 0;
    stubFetch((url, init) => {
      if (url === '/api/usuarios') {
        listaChamadas += 1;
        return jsonOk({ usuarios: CONTAS });
      }
      if (url === '/api/usuarios/a-1/rebaixamento' && init?.method === 'POST') {
        return Promise.resolve({
          ok: false,
          status: 409,
          json: async () => ({ error: { code: 'CONFLICT' } }),
        });
      }
      throw new Error(`URL inesperada: ${url} (${init?.method ?? 'GET'})`);
    });

    const user = userEvent.setup();
    render(<GestaoUsuariosSection />);

    await user.click(
      await screen.findByRole('button', { name: 'Rebaixar Bruno Almox para Usuário' }),
    );
    const dialog = await screen.findByRole('alertdialog');
    await user.click(within(dialog).getByRole('button', { name: 'Rebaixar' }));

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível concluir a ação na conta.',
    );
    await waitFor(() => expect(listaChamadas).toBe(2));
  });

  it('não monta nem chama GET /api/usuarios quando o papel é abaixo de gestor', async () => {
    authState.papel = 'almoxarife';
    const fetchMock = stubFetch((url) => {
      throw new Error(`URL inesperada: ${url}`);
    });

    const { container } = render(<GestaoUsuariosSection />);

    expect(container).toBeEmptyDOMElement();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  describe('Resetar MFA (Story 14.4)', () => {
    const CONTAS_MFA = [
      { id: 'adm-1', nome: 'Eu Adm', email: 'adm@empresa.com', papel: 'adm', ativo: true, mfaHabilitado: true },
      { id: 'g-1', nome: 'Gil Gestor', email: 'gil@empresa.com', papel: 'gestor', ativo: true, mfaHabilitado: true },
      { id: 'g-2', nome: 'Gabi Sem MFA', email: 'gabi@empresa.com', papel: 'gestor', ativo: true, mfaHabilitado: false },
      { id: 'adm-2', nome: 'Outro Adm', email: 'outro@empresa.com', papel: 'adm', ativo: true, mfaHabilitado: true },
    ];

    it('aparece para o adm numa conta gestor com MFA, e só nela', async () => {
      authState.id = 'adm-1';
      authState.papel = 'adm';
      stubFetch((url) => {
        if (url === '/api/usuarios') return jsonOk({ usuarios: CONTAS_MFA });
        throw new Error(`URL inesperada: ${url}`);
      });

      render(<GestaoUsuariosSection />);

      expect(await screen.findByRole('button', { name: 'Resetar MFA de Gil Gestor' })).toBeInTheDocument();
      expect(screen.queryByRole('button', { name: /Resetar MFA de Gabi Sem MFA/ })).not.toBeInTheDocument();
      expect(screen.queryByRole('button', { name: /Resetar MFA de Outro Adm/ })).not.toBeInTheDocument();
      expect(screen.queryByRole('button', { name: /Resetar MFA de Eu Adm/ })).not.toBeInTheDocument();
    });

    it('não aparece para o ator gestor', async () => {
      authState.id = 'g-9';
      authState.papel = 'gestor';
      stubFetch((url) => {
        if (url === '/api/usuarios') {
          return jsonOk({
            usuarios: [{ id: 'a-9', nome: 'Almox MFA', email: 'x@empresa.com', papel: 'almoxarife', ativo: true, mfaHabilitado: true }],
          });
        }
        throw new Error(`URL inesperada: ${url}`);
      });

      render(<GestaoUsuariosSection />);

      await screen.findByText('Almox MFA');
      expect(screen.queryByRole('button', { name: /Resetar MFA/ })).not.toBeInTheDocument();
    });

    it('confirmar faz POST .../mfa-reset sem corpo e recarrega a lista', async () => {
      authState.id = 'adm-1';
      authState.papel = 'adm';
      let listaChamadas = 0;
      const fetchMock = stubFetch((url, init) => {
        if (url === '/api/usuarios') {
          listaChamadas += 1;
          const lista =
            listaChamadas === 1
              ? CONTAS_MFA
              : CONTAS_MFA.map((c) => (c.id === 'g-1' ? { ...c, mfaHabilitado: false } : c));
          return jsonOk({ usuarios: lista });
        }
        if (url === '/api/usuarios/g-1/mfa-reset' && init?.method === 'POST') {
          return jsonOk({ usuario: { ...CONTAS_MFA[1], mfaHabilitado: false } });
        }
        throw new Error(`URL inesperada: ${url} (${init?.method ?? 'GET'})`);
      });

      const user = userEvent.setup();
      render(<GestaoUsuariosSection />);

      await user.click(await screen.findByRole('button', { name: 'Resetar MFA de Gil Gestor' }));
      const dialog = await screen.findByRole('alertdialog');
      expect(within(dialog).getByText('Resetar a dupla autenticação de Gil Gestor?')).toBeInTheDocument();
      expect(
        within(dialog).getByText(
          'As sessões da conta são encerradas e ela precisará configurar um novo MFA se a Empresa exigir.',
        ),
      ).toBeInTheDocument();
      await user.click(within(dialog).getByRole('button', { name: 'Resetar MFA' }));

      await waitFor(() =>
        expect(fetchMock).toHaveBeenCalledWith(
          '/api/usuarios/g-1/mfa-reset',
          expect.objectContaining({ method: 'POST' }),
        ),
      );
      const chamada = fetchMock.mock.calls.find(([u]) => u === '/api/usuarios/g-1/mfa-reset');
      expect(chamada?.[1]?.body).toBeUndefined();
      await waitFor(() => expect(listaChamadas).toBe(2));
      await waitFor(() =>
        expect(screen.queryByRole('button', { name: /Resetar MFA de Gil Gestor/ })).not.toBeInTheDocument(),
      );
    });

    it('cancelar não chama a API', async () => {
      authState.id = 'adm-1';
      authState.papel = 'adm';
      const fetchMock = stubFetch((url) => {
        if (url === '/api/usuarios') return jsonOk({ usuarios: CONTAS_MFA });
        throw new Error(`URL inesperada: ${url}`);
      });

      const user = userEvent.setup();
      render(<GestaoUsuariosSection />);

      await user.click(await screen.findByRole('button', { name: 'Resetar MFA de Gil Gestor' }));
      const dialog = await screen.findByRole('alertdialog');
      await user.click(within(dialog).getByRole('button', { name: /Cancelar/ }));

      await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument());
      expect(fetchMock).toHaveBeenCalledTimes(1);
      expect(fetchMock.mock.calls.some(([u]) => String(u).includes('mfa-reset'))).toBe(false);
    });
  });

  it('faixa: Ativos e Sem MFA sobre a lista completa; alerta só se a Empresa exige MFA', async () => {
    const contas = [
      { id: 'u-1', nome: 'Ana Usuária', email: 'ana@empresa.com', papel: 'usuario', ativo: true, mfaHabilitado: false },
      { id: 'u-2', nome: 'Caio Sem', email: 'caio@empresa.com', papel: 'usuario', ativo: true },
      { id: 'a-1', nome: 'Bruno Almox', email: 'bruno@empresa.com', papel: 'almoxarife', ativo: true, mfaHabilitado: true },
      { id: 'u-3', nome: 'Duda Inativa', email: 'duda@empresa.com', papel: 'usuario', ativo: false, mfaHabilitado: false },
    ];
    stubFetch(() => jsonOk({ usuarios: contas }));
    const { unmount } = render(<GestaoUsuariosSection />);
    await screen.findByText('Ana Usuária');
    expect(valorDe('Ativos')).toHaveTextContent('3');
    expect(valorDe('Sem MFA')).toHaveTextContent('2');
    expect(valorDe('Sem MFA')).not.toHaveClass('text-destructive');

    // Filtrar não muda os indicadores.
    await userEvent.setup().type(screen.getByLabelText('Buscar por nome ou e-mail'), 'ana');
    expect(screen.queryByText('Bruno Almox')).not.toBeInTheDocument();
    expect(valorDe('Ativos')).toHaveTextContent('3');
    unmount();

    authState.mfaObrigatorio = true;
    stubFetch(() => jsonOk({ usuarios: contas }));
    render(<GestaoUsuariosSection />);
    await screen.findByText('Ana Usuária');
    expect(valorDe('Sem MFA')).toHaveClass('text-destructive');
  });

  it('filtros por papel e situação e "Limpar filtros"', async () => {
    const contas = [
      { id: 'u-1', nome: 'Ana Usuária', email: 'ana@empresa.com', papel: 'usuario', ativo: true },
      { id: 'a-1', nome: 'Bruno Almox', email: 'bruno@empresa.com', papel: 'almoxarife', ativo: true },
      { id: 'u-3', nome: 'Duda Inativa', email: 'duda@empresa.com', papel: 'usuario', ativo: false },
    ];
    stubFetch(() => jsonOk({ usuarios: contas }));
    const user = userEvent.setup();
    render(<GestaoUsuariosSection />);
    await screen.findByText('Ana Usuária');

    // Só os papéis presentes aparecem no filtro.
    expect(within(screen.getByLabelText('Papel')).queryByRole('option', { name: 'Gestor' })).toBeNull();

    await user.selectOptions(screen.getByLabelText('Papel'), 'almoxarife');
    expect(screen.queryByText('Ana Usuária')).not.toBeInTheDocument();
    expect(screen.getByText('Bruno Almox')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Limpar filtros' }));
    await user.selectOptions(screen.getByLabelText('Situação'), 'inativas');
    expect(screen.getByText('Duda Inativa')).toBeInTheDocument();
    expect(screen.queryByText('Ana Usuária')).not.toBeInTheDocument();
    expect(screen.getByText('Inativa')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Limpar filtros' }));
    expect(screen.getByText('Ana Usuária')).toBeInTheDocument();
    expect(screen.getByText('Bruno Almox')).toBeInTheDocument();
  });

  it('antes da carga (e em erro) a faixa mostra "—" e não mostra o vazio', async () => {
    const resolvers: Array<(v: { ok: boolean; status?: number; json: () => Promise<unknown> }) => void> = [];
    stubFetch(
      () =>
        new Promise<{ ok: boolean; status?: number; json: () => Promise<unknown> }>((resolve) => {
          resolvers.push(resolve);
        }),
    );
    const { unmount } = render(<GestaoUsuariosSection />);
    expect(valorDe('Ativos')).toHaveTextContent('—');
    expect(valorDe('Sem MFA')).toHaveTextContent('—');
    expect(screen.queryByText('Nenhuma conta para gerir.')).not.toBeInTheDocument();
    resolvers[0]({ ok: true, json: async () => ({ usuarios: [] }) });
    expect(await screen.findByText('Nenhuma conta para gerir.')).toBeInTheDocument();
    expect(valorDe('Ativos')).toHaveTextContent('0');
    unmount();

    stubFetch(() => Promise.resolve({ ok: false, status: 500, json: async () => ({}) }));
    render(<GestaoUsuariosSection />);
    await screen.findByRole('alert');
    expect(valorDe('Ativos')).toHaveTextContent('—');
  });

  it('papel filtrado que some da lista após recarga volta a "todos"', async () => {
    let contas = [
      { id: 'u-1', nome: 'Ana Usuária', email: 'ana@empresa.com', papel: 'usuario', ativo: true },
      { id: 'a-1', nome: 'Bruno Almox', email: 'bruno@empresa.com', papel: 'almoxarife', ativo: true },
    ];
    stubFetch((_url, init) => {
      if (init?.method === 'POST') return jsonOk({});
      return jsonOk({ usuarios: contas });
    });
    const user = userEvent.setup();
    render(<GestaoUsuariosSection />);
    await screen.findByText('Ana Usuária');
    await user.selectOptions(screen.getByLabelText('Papel'), 'almoxarife');
    expect(screen.queryByText('Ana Usuária')).not.toBeInTheDocument();

    contas = [contas[0]];
    await user.click(screen.getByRole('button', { name: 'Desativar conta de Bruno Almox' }));
    await user.click(await screen.findByRole('button', { name: 'Desativar' }));
    expect(await screen.findByText('Ana Usuária')).toBeInTheDocument();
    expect(screen.getByLabelText('Papel')).toHaveValue('');
  });
});

import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { ConfiguracoesPage } from './ConfiguracoesPage';

// useAuth() fornece a identidade e o papel — configuráveis por teste.
const authState = vi.hoisted(() => ({
  papel: 'usuario' as string,
  nome: 'Ana Usuária',
  email: 'ana@empresa.com',
}));

vi.mock('@/lib/auth', () => ({
  useAuth: () => ({
    estado: 'autenticado',
    usuario: { id: '1', nome: authState.nome, email: authState.email, papel: authState.papel },
    definirSessao: vi.fn(),
  }),
}));

type FetchImpl = (url: string, init?: RequestInit) => Promise<{ ok: boolean; status?: number; json: () => Promise<unknown> }>;

function stubFetch(impl: FetchImpl) {
  const fn = vi.fn(impl);
  vi.stubGlobal('fetch', fn);
  return fn;
}

function jsonOk(body: unknown) {
  return Promise.resolve({ ok: true, json: async () => body });
}

beforeEach(() => {
  authState.papel = 'usuario';
  authState.nome = 'Ana Usuária';
  authState.email = 'ana@empresa.com';
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe('ConfiguracoesPage — Meu Perfil', () => {
  it('mostra a identidade da conta', async () => {
    stubFetch((url) => {
      if (url === '/api/promocoes/minha') return jsonOk({ solicitacao: null });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<ConfiguracoesPage />);

    expect(screen.getByRole('heading', { name: 'Meu Perfil' })).toBeInTheDocument();
    expect(screen.getByText('Ana Usuária')).toBeInTheDocument();
    expect(screen.getByText('ana@empresa.com')).toBeInTheDocument();
    await waitFor(() => expect(fetch).toHaveBeenCalledWith('/api/promocoes/minha', expect.anything()));
  });

  it('usuario sem pendente: botão habilitado com o alvo correto (Almoxarife)', async () => {
    stubFetch((url) => {
      if (url === '/api/promocoes/minha') return jsonOk({ solicitacao: null });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<ConfiguracoesPage />);

    const botao = await screen.findByRole('button', { name: 'Solicitar promoção para Almoxarife' });
    expect(botao).toBeEnabled();
  });

  it('almoxarife sem pendente: o alvo do botão é Gestor', async () => {
    authState.papel = 'almoxarife';
    stubFetch((url) => {
      if (url === '/api/promocoes/minha') return jsonOk({ solicitacao: null });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<ConfiguracoesPage />);

    expect(
      await screen.findByRole('button', { name: 'Solicitar promoção para Gestor' }),
    ).toBeInTheDocument();
  });

  it('com solicitação pendente: botão desabilitado e texto explicativo', async () => {
    stubFetch((url) => {
      if (url === '/api/promocoes/minha') {
        return jsonOk({
          solicitacao: {
            id: 's1',
            papel_alvo: 'almoxarife',
            status: 'pendente',
            criado_em: '2026-08-29T12:00:00Z',
            decidido_em: null,
          },
        });
      }
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<ConfiguracoesPage />);

    await waitFor(() =>
      expect(screen.getByText('Solicitação pendente de aprovação.')).toBeInTheDocument(),
    );
    expect(screen.getByRole('button', { name: /Solicitar promoção para/ })).toBeDisabled();
  });

  it('após solicitar (POST 201) refaz o fetch de /minha e desabilita o botão', async () => {
    let minhaChamadas = 0;
    const fetchMock = stubFetch((url, init) => {
      if (url === '/api/promocoes/minha') {
        minhaChamadas += 1;
        // 1ª chamada (mount): nunca solicitou. 2ª chamada (após POST): pendente.
        return jsonOk({
          solicitacao:
            minhaChamadas === 1
              ? null
              : {
                  id: 's1',
                  papel_alvo: 'almoxarife',
                  status: 'pendente',
                  criado_em: '2026-08-29T12:00:00Z',
                  decidido_em: null,
                },
        });
      }
      if (url === '/api/promocoes' && init?.method === 'POST') {
        return Promise.resolve({ ok: true, json: async () => ({ solicitacao: { id: 's1' } }) });
      }
      throw new Error(`URL inesperada: ${url} (${init?.method ?? 'GET'})`);
    });

    const user = userEvent.setup();
    render(<ConfiguracoesPage />);

    const botao = await screen.findByRole('button', { name: 'Solicitar promoção para Almoxarife' });
    await user.click(botao);

    await waitFor(() =>
      expect(screen.getByText('Solicitação pendente de aprovação.')).toBeInTheDocument(),
    );
    expect(screen.getByRole('button', { name: /Solicitar promoção/ })).toBeDisabled();
    expect(fetchMock).toHaveBeenCalledWith('/api/promocoes', expect.objectContaining({ method: 'POST' }));
    expect(minhaChamadas).toBe(2);
  });

  it('após rejeição: botão volta a habilitar com a nota de recusa', async () => {
    stubFetch((url) => {
      if (url === '/api/promocoes/minha') {
        return jsonOk({
          solicitacao: {
            id: 's1',
            papel_alvo: 'almoxarife',
            status: 'rejeitada',
            criado_em: '2026-08-29T12:00:00Z',
            decidido_em: '2026-08-29T13:00:00Z',
          },
        });
      }
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<ConfiguracoesPage />);

    expect(await screen.findByText('Sua última solicitação foi recusada.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Solicitar promoção para Almoxarife' })).toBeEnabled();
  });

  it('gestor: sem botão de solicitar e com a seção "Decidir promoções"', async () => {
    authState.papel = 'gestor';
    stubFetch((url) => {
      if (url === '/api/promocoes/minha') return jsonOk({ solicitacao: null });
      if (url === '/api/promocoes') return jsonOk({ solicitacoes: [] });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<ConfiguracoesPage />);

    expect(
      await screen.findByText('Não há promoção disponível para o seu papel.'),
    ).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Solicitar promoção/ })).not.toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Decidir promoções' })).toBeInTheDocument();
  });

  it('erro HTTP ao solicitar promoção mostra um role="alert"', async () => {
    stubFetch((url, init) => {
      if (url === '/api/promocoes/minha') return jsonOk({ solicitacao: null });
      if (url === '/api/promocoes' && init?.method === 'POST') {
        return Promise.resolve({ ok: false, status: 500, json: async () => ({ error: { code: 'INTERNAL_ERROR' } }) });
      }
      throw new Error(`URL inesperada: ${url}`);
    });

    const user = userEvent.setup();
    render(<ConfiguracoesPage />);

    await user.click(await screen.findByRole('button', { name: 'Solicitar promoção para Almoxarife' }));

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível solicitar a promoção agora. Tente novamente em instantes.',
    );
  });

  it('falha ao carregar /minha: alerta inline e botão desabilitado (não oferece ação sem verificar a pré-condição)', async () => {
    stubFetch((url) => {
      if (url === '/api/promocoes/minha') {
        return Promise.resolve({ ok: false, status: 500, json: async () => ({ error: { code: 'INTERNAL_ERROR' } }) });
      }
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<ConfiguracoesPage />);

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível verificar o estado da sua solicitação. Recarregue a página.',
    );
    expect(screen.getByRole('button', { name: 'Solicitar promoção para Almoxarife' })).toBeDisabled();
  });
});

describe('ConfiguracoesPage — Decidir promoções', () => {
  beforeEach(() => {
    authState.papel = 'gestor';
  });

  it('lista itens pendentes e remove um após aprovar (refetch)', async () => {
    let pendentesChamadas = 0;
    const fetchMock = stubFetch((url, init) => {
      if (url === '/api/promocoes/minha') return jsonOk({ solicitacao: null });
      if (url === '/api/promocoes' && (!init || init.method === undefined)) {
        pendentesChamadas += 1;
        if (pendentesChamadas === 1) {
          return jsonOk({
            solicitacoes: [
              {
                id: 'p1',
                solicitante_nome: 'Bruno',
                solicitante_email: 'bruno@empresa.com',
                papel_atual: 'usuario',
                papel_alvo: 'almoxarife',
                criado_em: '2026-08-29T10:00:00Z',
              },
              {
                id: 'p2',
                solicitante_nome: 'Carla',
                solicitante_email: 'carla@empresa.com',
                papel_atual: 'usuario',
                papel_alvo: 'almoxarife',
                criado_em: '2026-08-29T11:00:00Z',
              },
            ],
          });
        }
        return jsonOk({
          solicitacoes: [
            {
              id: 'p2',
              solicitante_nome: 'Carla',
              solicitante_email: 'carla@empresa.com',
              papel_atual: 'usuario',
              papel_alvo: 'almoxarife',
              criado_em: '2026-08-29T11:00:00Z',
            },
          ],
        });
      }
      if (url === '/api/promocoes/p1/decisao' && init?.method === 'POST') {
        return Promise.resolve({ ok: true, json: async () => ({ solicitacao: { id: 'p1', status: 'aprovada' } }) });
      }
      throw new Error(`URL inesperada: ${url} (${init?.method ?? 'GET'})`);
    });

    const user = userEvent.setup();
    render(<ConfiguracoesPage />);

    expect(await screen.findByText('Bruno')).toBeInTheDocument();
    expect(screen.getByText('Carla')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Aprovar promoção de Bruno' }));

    await waitFor(() => expect(screen.queryByText('Bruno')).not.toBeInTheDocument());
    expect(screen.getByText('Carla')).toBeInTheDocument();
    expect(screen.getByText('Promoção aprovada.')).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/promocoes/p1/decisao',
      expect.objectContaining({ method: 'POST' }),
    );
  });

  it('decisão que falha (409) recarrega a fila: item obsoleto some e o alerta aparece', async () => {
    let pendentesChamadas = 0;
    stubFetch((url, init) => {
      if (url === '/api/promocoes/minha') return jsonOk({ solicitacao: null });
      if (url === '/api/promocoes' && (!init || init.method === undefined)) {
        pendentesChamadas += 1;
        // 1ª carga: Bruno pendente. Após a decisão falhada: fila vazia (já
        // decidida por outro gestor).
        return jsonOk({
          solicitacoes:
            pendentesChamadas === 1
              ? [
                  {
                    id: 'p1',
                    solicitante_nome: 'Bruno',
                    solicitante_email: 'bruno@empresa.com',
                    papel_atual: 'usuario',
                    papel_alvo: 'almoxarife',
                    criado_em: '2026-08-29T10:00:00Z',
                  },
                ]
              : [],
        });
      }
      if (url === '/api/promocoes/p1/decisao' && init?.method === 'POST') {
        return Promise.resolve({ ok: false, status: 409, json: async () => ({ error: { code: 'CONFLICT' } }) });
      }
      throw new Error(`URL inesperada: ${url} (${init?.method ?? 'GET'})`);
    });

    const user = userEvent.setup();
    render(<ConfiguracoesPage />);

    await user.click(await screen.findByRole('button', { name: 'Recusar promoção de Bruno' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('Não foi possível concluir a decisão.');
    await waitFor(() => expect(screen.queryByText('Bruno')).not.toBeInTheDocument());
    expect(pendentesChamadas).toBe(2);
  });

  it('falha ao carregar a fila mostra um alerta e NÃO o falso "nada a fazer"', async () => {
    stubFetch((url, init) => {
      if (url === '/api/promocoes/minha') return jsonOk({ solicitacao: null });
      if (url === '/api/promocoes' && (!init || init.method === undefined)) {
        return Promise.resolve({ ok: false, status: 500, json: async () => ({ error: { code: 'INTERNAL_ERROR' } }) });
      }
      throw new Error(`URL inesperada: ${url} (${init?.method ?? 'GET'})`);
    });

    render(<ConfiguracoesPage />);

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível carregar as solicitações pendentes.',
    );
    expect(screen.queryByText('Nenhuma solicitação pendente.')).not.toBeInTheDocument();
  });

  it('decisão que lança erro de rede também recarrega a fila (paridade com o ramo !res.ok)', async () => {
    let pendentesChamadas = 0;
    stubFetch((url, init) => {
      if (url === '/api/promocoes/minha') return jsonOk({ solicitacao: null });
      if (url === '/api/promocoes' && (!init || init.method === undefined)) {
        pendentesChamadas += 1;
        return jsonOk({
          solicitacoes:
            pendentesChamadas === 1
              ? [
                  {
                    id: 'p1',
                    solicitante_nome: 'Bruno',
                    solicitante_email: 'bruno@empresa.com',
                    papel_atual: 'usuario',
                    papel_alvo: 'almoxarife',
                    criado_em: '2026-08-29T10:00:00Z',
                  },
                ]
              : [],
        });
      }
      if (url === '/api/promocoes/p1/decisao' && init?.method === 'POST') {
        return Promise.reject(new Error('falha de rede'));
      }
      throw new Error(`URL inesperada: ${url} (${init?.method ?? 'GET'})`);
    });

    const user = userEvent.setup();
    render(<ConfiguracoesPage />);

    await user.click(await screen.findByRole('button', { name: 'Aprovar promoção de Bruno' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('Não foi possível concluir a decisão.');
    await waitFor(() => expect(screen.queryByText('Bruno')).not.toBeInTheDocument());
    expect(pendentesChamadas).toBe(2);
  });
});

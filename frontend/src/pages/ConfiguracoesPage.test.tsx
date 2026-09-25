import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { ConfiguracoesPage } from './ConfiguracoesPage';
import { CategoriasPage } from './cadastros/CategoriasPage';
import { CentrosCustoPage } from './cadastros/CentrosCustoPage';
import { FiliaisPage } from './cadastros/FiliaisPage';
import { LgpdPage } from './admin/LgpdPage';
import { LogAcessoPage } from './admin/LogAcessoPage';
import { PromocoesPage } from './admin/PromocoesPage';
import { SegurancaEmpresaPage } from './admin/SegurancaEmpresaPage';
import { TemplatesPage } from './cadastros/TemplatesPage';
import { UsuariosPage } from './admin/UsuariosPage';

// useAuth() fornece a identidade e o papel — configuráveis por teste.
const authState = vi.hoisted(() => ({
  papel: 'usuario' as string,
  nome: 'Ana Usuária',
  email: 'ana@empresa.com',
  mfaHabilitado: false,
  origem: 'senha' as string,
  // Story 14.1: `empresa.mfaObrigatorio` da sessão. Padrão `true` (Empresa
  // exige) para os casos do gate herdados da Story 1.11; os casos "Empresa
  // não exige" sobrescrevem.
  empresaMfaObrigatorio: true,
}));

const atualizarUsuarioMock = vi.hoisted(() => vi.fn());

// Só `useAuth` é substituído: `mfaSetupPendente` (Story 14.1) roda a
// implementação REAL, a mesma usada por RotaProtegida.
vi.mock('@/lib/auth', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/auth')>()),
  useAuth: () => ({
    estado: 'autenticado',
    usuario: {
      id: '1',
      nome: authState.nome,
      email: authState.email,
      papel: authState.papel,
      mfaHabilitado: authState.mfaHabilitado,
      origem: authState.origem,
      empresa: { mfaObrigatorio: authState.empresaMfaObrigatorio },
    },
    definirSessao: vi.fn(),
    atualizarUsuario: atualizarUsuarioMock,
    logout: vi.fn(),
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
  authState.mfaHabilitado = false;
  authState.origem = 'senha';
  authState.empresaMfaObrigatorio = true;
  atualizarUsuarioMock.mockReset();
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

  it('gestor: sem botão de solicitar e sem seções administrativas (Story 17.2)', async () => {
    authState.papel = 'gestor';
    stubFetch((url) => {
      if (url === '/api/promocoes/minha') return jsonOk({ solicitacao: null });
      if (url === '/api/promocoes') return jsonOk({ solicitacoes: [] });
      if (url === '/api/usuarios') return jsonOk({ usuarios: [] });
      if (url === '/api/convites') return jsonOk({ convites: [] });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<ConfiguracoesPage />);

    expect(
      await screen.findByText('Não há promoção disponível para o seu papel.'),
    ).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Solicitar promoção/ })).not.toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: 'Decidir promoções' })).not.toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: 'Gestão de Usuários' })).not.toBeInTheDocument();
  });

  it('UsuariosPage: gestor vê "Usuários" e a Gestão de Usuários; usuario recebe a recusa sem fetch', async () => {
    authState.papel = 'gestor';
    stubFetch((url) => {
      if (url === '/api/promocoes/minha') return jsonOk({ solicitacao: null });
      if (url === '/api/promocoes') return jsonOk({ solicitacoes: [] });
      if (url === '/api/usuarios') return jsonOk({ usuarios: [] });
      if (url === '/api/convites') return jsonOk({ convites: [] });
      throw new Error(`URL inesperada: ${url}`);
    });

    const { unmount } = render(<UsuariosPage />);
    expect(screen.getByRole('heading', { level: 1, name: 'Usuários' })).toBeInTheDocument();
    // Sem h2 interno (Story 17.5): a seção mostra a busca e a faixa de indicadores.
    expect(await screen.findByLabelText('Buscar por nome ou e-mail')).toBeInTheDocument();
    expect(screen.getByText('Ativos')).toBeInTheDocument();
    unmount();

    authState.papel = 'usuario';
    vi.clearAllMocks();
    const fetchMock = stubFetch((url) => {
      if (url === '/api/promocoes/minha') return jsonOk({ solicitacao: null });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<UsuariosPage />);
    expect(screen.getByText('Você não tem acesso a Usuários.')).toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: 'Gestão de Usuários' })).not.toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalled();
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

describe('PromocoesPage — Decidir promoções', () => {
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
      if (url === '/api/usuarios') return jsonOk({ usuarios: [] });
      if (url === '/api/convites') return jsonOk({ convites: [] });
      throw new Error(`URL inesperada: ${url} (${init?.method ?? 'GET'})`);
    });

    const user = userEvent.setup();
    render(<PromocoesPage />);

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
      if (url === '/api/usuarios') return jsonOk({ usuarios: [] });
      if (url === '/api/convites') return jsonOk({ convites: [] });
      throw new Error(`URL inesperada: ${url} (${init?.method ?? 'GET'})`);
    });

    const user = userEvent.setup();
    render(<PromocoesPage />);

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
      if (url === '/api/usuarios') return jsonOk({ usuarios: [] });
      if (url === '/api/convites') return jsonOk({ convites: [] });
      throw new Error(`URL inesperada: ${url} (${init?.method ?? 'GET'})`);
    });

    render(<PromocoesPage />);

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
      if (url === '/api/usuarios') return jsonOk({ usuarios: [] });
      if (url === '/api/convites') return jsonOk({ convites: [] });
      throw new Error(`URL inesperada: ${url} (${init?.method ?? 'GET'})`);
    });

    const user = userEvent.setup();
    render(<PromocoesPage />);

    await user.click(await screen.findByRole('button', { name: 'Aprovar promoção de Bruno' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('Não foi possível concluir a decisão.');
    await waitFor(() => expect(screen.queryByText('Bruno')).not.toBeInTheDocument());
    expect(pendentesChamadas).toBe(2);
  });
});

describe('ConfiguracoesPage — Segurança (MFA, Story 1.11)', () => {
  function stubFetchBase(extra: FetchImpl) {
    return stubFetch((url, init) => {
      if (url === '/api/promocoes/minha') return jsonOk({ solicitacao: null });
      if (url === '/api/promocoes' && (!init || init.method === undefined)) return jsonOk({ solicitacoes: [] });
      if (url === '/api/usuarios') return jsonOk({ usuarios: [] });
      if (url === '/api/convites') return jsonOk({ convites: [] });
      return extra(url, init);
    });
  }

  it('gestor sem MFA (origem=senha): mensagem "obrigatório para o seu papel"', async () => {
    authState.papel = 'gestor';
    authState.origem = 'senha';
    authState.mfaHabilitado = false;
    stubFetchBase(() => {
      throw new Error('URL inesperada');
    });

    render(<ConfiguracoesPage />);

    expect(await screen.findByRole('heading', { level: 2, name: 'Segurança' })).toBeInTheDocument();
    expect(
      screen.getByText('Obrigatório para o seu papel. Configure para continuar acessando ações restritas.'),
    ).toBeInTheDocument();
  });

  it('gestor sem MFA (origem=senha) numa Empresa que NÃO exige: mensagem "opcional" (Story 14.1)', async () => {
    authState.papel = 'gestor';
    authState.origem = 'senha';
    authState.mfaHabilitado = false;
    authState.empresaMfaObrigatorio = false;
    stubFetchBase(() => {
      throw new Error('URL inesperada');
    });

    render(<ConfiguracoesPage />);

    expect(await screen.findByText('Opcional para o seu papel.')).toBeInTheDocument();
    expect(
      screen.queryByText('Obrigatório para o seu papel. Configure para continuar acessando ações restritas.'),
    ).not.toBeInTheDocument();
  });

  it('adm sem MFA (origem=senha) numa Empresa que exige: mensagem "obrigatório" (Story 14.1)', async () => {
    authState.papel = 'adm';
    authState.origem = 'senha';
    authState.mfaHabilitado = false;
    authState.empresaMfaObrigatorio = true;
    stubFetchBase(() => jsonOk([]));

    render(<ConfiguracoesPage />);

    expect(
      await screen.findByText('Obrigatório para o seu papel. Configure para continuar acessando ações restritas.'),
    ).toBeInTheDocument();
  });

  it('usuario sem MFA: mensagem "opcional"', async () => {
    authState.papel = 'usuario';
    authState.mfaHabilitado = false;
    stubFetchBase(() => {
      throw new Error('URL inesperada');
    });

    render(<ConfiguracoesPage />);

    expect(await screen.findByText('Opcional para o seu papel.')).toBeInTheDocument();
  });

  it('gestor via SSO sem MFA: mensagem "opcional", nunca "obrigatório"', async () => {
    authState.papel = 'gestor';
    authState.origem = 'sso';
    authState.mfaHabilitado = false;
    stubFetchBase(() => {
      throw new Error('URL inesperada');
    });

    render(<ConfiguracoesPage />);

    expect(await screen.findByText('Opcional para o seu papel.')).toBeInTheDocument();
    expect(
      screen.queryByText('Obrigatório para o seu papel. Configure para continuar acessando ações restritas.'),
    ).not.toBeInTheDocument();
  });

  it('MFA já habilitado: mensagem "ativa", sem botão de configurar', async () => {
    authState.mfaHabilitado = true;
    stubFetchBase(() => {
      throw new Error('URL inesperada');
    });

    render(<ConfiguracoesPage />);

    expect(await screen.findByText('Autenticação em duas etapas ativa.')).toBeInTheDocument();
    expect(
      screen.queryByRole('button', { name: 'Configurar autenticação em duas etapas' }),
    ).not.toBeInTheDocument();
  });

  it('fluxo de confirmação feliz: iniciar -> QR + segredo -> confirmar com sucesso', async () => {
    authState.mfaHabilitado = false;
    const fetchMock = stubFetchBase((url, init) => {
      if (url === '/api/auth/mfa/iniciar' && init?.method === 'POST') {
        return jsonOk({ segredo: 'JBSWY3DPEHPK3PXP', otpauthUrl: 'otpauth://totp/StockFlow:ana@empresa.com?secret=JBSWY3DPEHPK3PXP' });
      }
      if (url === '/api/auth/mfa/confirmar' && init?.method === 'POST') {
        return jsonOk({});
      }
      throw new Error(`URL inesperada: ${url}`);
    });

    const user = userEvent.setup();
    render(<ConfiguracoesPage />);

    await user.click(
      await screen.findByRole('button', { name: 'Configurar autenticação em duas etapas' }),
    );

    expect(await screen.findByText('JBSWY3DPEHPK3PXP')).toBeInTheDocument();
    await user.type(screen.getByLabelText('Senha atual'), 'senha-correta-123');
    const inputCodigo = screen.getByLabelText('Código de verificação');
    await user.type(inputCodigo, '123456');
    await user.click(screen.getByRole('button', { name: 'Confirmar' }));

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/auth/mfa/confirmar',
        expect.objectContaining({ method: 'POST' }),
      ),
    );
    const confirmarCall = fetchMock.mock.calls.find(([u]) => u === '/api/auth/mfa/confirmar');
    const confirmarInit = confirmarCall?.[1] as RequestInit | undefined;
    expect(JSON.parse(confirmarInit?.body as string)).toEqual({
      segredo: 'JBSWY3DPEHPK3PXP',
      codigo: '123456',
      senhaAtual: 'senha-correta-123',
    });

    await waitFor(() =>
      expect(atualizarUsuarioMock).toHaveBeenCalledWith(
        expect.objectContaining({ mfaHabilitado: true }),
      ),
    );
  });

  it('código de confirmação errado: mostra erro inline e mantém o QR para nova tentativa', async () => {
    authState.mfaHabilitado = false;
    stubFetchBase((url, init) => {
      if (url === '/api/auth/mfa/iniciar' && init?.method === 'POST') {
        return jsonOk({ segredo: 'JBSWY3DPEHPK3PXP', otpauthUrl: 'otpauth://totp/StockFlow:ana@empresa.com?secret=JBSWY3DPEHPK3PXP' });
      }
      if (url === '/api/auth/mfa/confirmar' && init?.method === 'POST') {
        return Promise.resolve({
          ok: false,
          json: async () => ({ error: { code: 'MFA_CODIGO_INVALIDO' } }),
        });
      }
      throw new Error(`URL inesperada: ${url}`);
    });

    const user = userEvent.setup();
    render(<ConfiguracoesPage />);

    await user.click(
      await screen.findByRole('button', { name: 'Configurar autenticação em duas etapas' }),
    );
    await screen.findByText('JBSWY3DPEHPK3PXP');

    await user.type(screen.getByLabelText('Senha atual'), 'senha-correta-123');
    await user.type(screen.getByLabelText('Código de verificação'), '000000');
    await user.click(screen.getByRole('button', { name: 'Confirmar' }));

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Código de autenticação inválido. Confira o código no seu aplicativo e tente novamente.',
    );
    // O QR/segredo continua visível para nova tentativa — nada foi gravado.
    expect(screen.getByText('JBSWY3DPEHPK3PXP')).toBeInTheDocument();
    expect(atualizarUsuarioMock).not.toHaveBeenCalled();
  });

  it('senha atual incorreta: mostra erro inline específico, mantém o QR para nova tentativa', async () => {
    authState.mfaHabilitado = false;
    stubFetchBase((url, init) => {
      if (url === '/api/auth/mfa/iniciar' && init?.method === 'POST') {
        return jsonOk({ segredo: 'JBSWY3DPEHPK3PXP', otpauthUrl: 'otpauth://totp/StockFlow:ana@empresa.com?secret=JBSWY3DPEHPK3PXP' });
      }
      if (url === '/api/auth/mfa/confirmar' && init?.method === 'POST') {
        return Promise.resolve({
          ok: false,
          json: async () => ({ error: { code: 'INVALID_CREDENTIALS' } }),
        });
      }
      throw new Error(`URL inesperada: ${url}`);
    });

    const user = userEvent.setup();
    render(<ConfiguracoesPage />);

    await user.click(
      await screen.findByRole('button', { name: 'Configurar autenticação em duas etapas' }),
    );
    await screen.findByText('JBSWY3DPEHPK3PXP');

    await user.type(screen.getByLabelText('Senha atual'), 'senha-errada');
    await user.type(screen.getByLabelText('Código de verificação'), '123456');
    await user.click(screen.getByRole('button', { name: 'Confirmar' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('Senha atual incorreta.');
    expect(screen.getByText('JBSWY3DPEHPK3PXP')).toBeInTheDocument();
    expect(atualizarUsuarioMock).not.toHaveBeenCalled();
  });
});

describe('ConfiguracoesPage — Desligar meu MFA (Story 14.4)', () => {
  function stubFetchBase(extra: FetchImpl) {
    return stubFetch((url, init) => {
      if (url === '/api/promocoes/minha') return jsonOk({ solicitacao: null });
      if (url === '/api/promocoes' && (!init || init.method === undefined)) return jsonOk({ solicitacoes: [] });
      if (url === '/api/usuarios') return jsonOk({ usuarios: [] });
      if (url === '/api/convites') return jsonOk({ convites: [] });
      return extra(url, init);
    });
  }

  async function preencherEDesligar(senha: string, codigo: string) {
    const user = userEvent.setup();
    await user.click(await screen.findByRole('button', { name: 'Desligar meu MFA' }));
    await user.type(screen.getByLabelText('Senha atual'), senha);
    await user.type(screen.getByLabelText('Código de verificação'), codigo);
    await user.click(screen.getByRole('button', { name: 'Desligar' }));
  }

  it('envia {senhaAtual, codigo} e atualiza a sessão para mfaHabilitado:false', async () => {
    authState.papel = 'usuario';
    authState.mfaHabilitado = true;
    const fetchMock = stubFetchBase((url, init) => {
      if (url === '/api/auth/mfa/desligar' && init?.method === 'POST') return jsonOk({});
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<ConfiguracoesPage />);
    await preencherEDesligar('senha-correta-123', '123456');

    await waitFor(() =>
      expect(atualizarUsuarioMock).toHaveBeenCalledWith(expect.objectContaining({ mfaHabilitado: false })),
    );
    const chamada = fetchMock.mock.calls.find(([u]) => u === '/api/auth/mfa/desligar');
    expect(JSON.parse((chamada?.[1] as RequestInit).body as string)).toEqual({
      senhaAtual: 'senha-correta-123',
      codigo: '123456',
    });
  });

  it('401 vira alerta "Senha ou código inválido." e não atualiza a sessão', async () => {
    authState.papel = 'usuario';
    authState.mfaHabilitado = true;
    stubFetchBase((url, init) => {
      if (url === '/api/auth/mfa/desligar' && init?.method === 'POST') {
        return Promise.resolve({
          ok: false,
          status: 401,
          json: async () => ({ error: { code: 'INVALID_CREDENTIALS', message: 'Senha ou código inválido.' } }),
        });
      }
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<ConfiguracoesPage />);
    await preencherEDesligar('senha-errada', '000000');

    expect(await screen.findByRole('alert')).toHaveTextContent('Senha ou código inválido.');
    expect(atualizarUsuarioMock).not.toHaveBeenCalled();
  });

  it('409 MFA_EXIGIDO_PELA_EMPRESA do servidor vira alerta com a mensagem dele', async () => {
    authState.papel = 'gestor';
    authState.mfaHabilitado = true;
    authState.empresaMfaObrigatorio = false; // a sessão ainda não viu a mudança
    const mensagem = 'A Empresa exige dupla autenticação para o seu papel; ela não pode ser desligada.';
    stubFetchBase((url, init) => {
      if (url === '/api/auth/mfa/desligar' && init?.method === 'POST') {
        return Promise.resolve({
          ok: false,
          status: 409,
          json: async () => ({ error: { code: 'MFA_EXIGIDO_PELA_EMPRESA', message: mensagem } }),
        });
      }
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<ConfiguracoesPage />);
    await preencherEDesligar('senha-correta-123', '123456');

    expect(await screen.findByRole('alert')).toHaveTextContent(mensagem);
    expect(atualizarUsuarioMock).not.toHaveBeenCalled();
  });

  it('gestor numa Empresa que exige não vê o botão e vê a explicação', async () => {
    authState.papel = 'gestor';
    authState.mfaHabilitado = true;
    authState.empresaMfaObrigatorio = true;
    stubFetchBase(() => {
      throw new Error('URL inesperada');
    });

    render(<ConfiguracoesPage />);

    expect(await screen.findByText('Autenticação em duas etapas ativa.')).toBeInTheDocument();
    expect(
      screen.getByText('A Empresa exige dupla autenticação para o seu papel; ela não pode ser desligada.'),
    ).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Desligar meu MFA' })).not.toBeInTheDocument();
  });

  it('usuario numa Empresa que exige ainda pode desligar (exigência é só de gestor/adm)', async () => {
    authState.papel = 'usuario';
    authState.mfaHabilitado = true;
    authState.empresaMfaObrigatorio = true;
    stubFetchBase(() => {
      throw new Error('URL inesperada');
    });

    render(<ConfiguracoesPage />);

    expect(await screen.findByRole('button', { name: 'Desligar meu MFA' })).toBeInTheDocument();
  });
});

describe('LogAcessoPage — Log de Acesso (Story 1.12)', () => {
  it('adm vê a seção "Log de Acesso"', async () => {
    authState.papel = 'adm';
    stubFetch((url) => {
      if (url === '/api/promocoes/minha') return jsonOk({ solicitacao: null });
      if (url === '/api/promocoes') return jsonOk({ solicitacoes: [] });
      if (url === '/api/usuarios') return jsonOk({ usuarios: [] });
      if (url === '/api/convites') return jsonOk({ convites: [] });
      if (url.startsWith('/api/logs-acesso')) return jsonOk({ logs: [] });
      if (url === '/api/categorias') return jsonOk({ categorias: [] });
      if (url === '/api/nomenclatura-templates') return jsonOk({ templates: [] });
      if (url === '/api/solicitacoes-exclusao') return jsonOk({ solicitacoes: [] });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<LogAcessoPage />);

    expect(
      await screen.findByRole('heading', { level: 2, name: 'Log de Acesso' }),
    ).toBeInTheDocument();
  });

  it('gestor NÃO vê a seção "Log de Acesso" e nunca chama GET /api/logs-acesso', async () => {
    authState.papel = 'gestor';
    const fetchMock = stubFetch((url) => {
      if (url === '/api/promocoes/minha') return jsonOk({ solicitacao: null });
      if (url === '/api/promocoes') return jsonOk({ solicitacoes: [] });
      if (url === '/api/usuarios') return jsonOk({ usuarios: [] });
      if (url === '/api/convites') return jsonOk({ convites: [] });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<LogAcessoPage />);

    await screen.findByText(/Você não tem acesso a/);
    expect(screen.queryByRole('heading', { name: 'Log de Acesso' })).not.toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalledWith(
      expect.stringContaining('/api/logs-acesso'),
      expect.anything(),
    );
  });

  it('usuario NÃO vê a seção "Log de Acesso"', async () => {
    authState.papel = 'usuario';
    stubFetch((url) => {
      if (url === '/api/promocoes/minha') return jsonOk({ solicitacao: null });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<LogAcessoPage />);

    await screen.findByText(/Você não tem acesso a/);
    expect(screen.queryByRole('heading', { name: 'Log de Acesso' })).not.toBeInTheDocument();
  });
});

describe('FiliaisPage — Filiais (Story 12.1)', () => {
  it('adm vê a seção "Filiais" e carrega GET /api/filiais', async () => {
    authState.papel = 'adm';
    const fetchMock = stubFetch((url) => {
      if (url === '/api/promocoes/minha') return jsonOk({ solicitacao: null });
      if (url === '/api/promocoes') return jsonOk({ solicitacoes: [] });
      if (url === '/api/usuarios') return jsonOk({ usuarios: [] });
      if (url === '/api/convites') return jsonOk({ convites: [] });
      if (url.startsWith('/api/logs-acesso')) return jsonOk({ logs: [] });
      if (url === '/api/filiais') return jsonOk({ filiais: [] });
      if (url === '/api/categorias') return jsonOk({ categorias: [] });
      if (url === '/api/nomenclatura-templates') return jsonOk({ templates: [] });
      if (url === '/api/solicitacoes-exclusao') return jsonOk({ solicitacoes: [] });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<FiliaisPage />);

    expect(await screen.findByRole('heading', { level: 2, name: 'Filiais' })).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith('/api/filiais', expect.anything());
  });

  it.each(['usuario', 'almoxarife', 'gestor'])(
    'papel %s NÃO vê a seção "Filiais" e nunca chama GET /api/filiais',
    async (papel) => {
      authState.papel = papel;
      const fetchMock = stubFetch((url) => {
        if (url === '/api/promocoes/minha') return jsonOk({ solicitacao: null });
        if (url === '/api/promocoes') return jsonOk({ solicitacoes: [] });
        if (url === '/api/usuarios') return jsonOk({ usuarios: [] });
        if (url === '/api/convites') return jsonOk({ convites: [] });
        throw new Error(`URL inesperada: ${url}`);
      });

      render(<FiliaisPage />);

      await screen.findByText(/Você não tem acesso a/);
      expect(screen.queryByRole('heading', { name: 'Filiais' })).not.toBeInTheDocument();
      expect(fetchMock).not.toHaveBeenCalledWith(
        expect.stringContaining('/api/filiais'),
        expect.anything(),
      );
    },
  );
});

describe('CentrosCustoPage — Centros de custo (Story 12.3)', () => {
  it('adm vê a seção "Centros de custo" e carrega GET /api/centros-custo', async () => {
    authState.papel = 'adm';
    const fetchMock = stubFetch((url) => {
      if (url === '/api/promocoes/minha') return jsonOk({ solicitacao: null });
      if (url === '/api/promocoes') return jsonOk({ solicitacoes: [] });
      if (url === '/api/usuarios') return jsonOk({ usuarios: [] });
      if (url === '/api/convites') return jsonOk({ convites: [] });
      if (url.startsWith('/api/logs-acesso')) return jsonOk({ logs: [] });
      if (url === '/api/centros-custo') return jsonOk({ centrosCusto: [] });
      if (url === '/api/filiais') return jsonOk({ filiais: [] });
      if (url === '/api/categorias') return jsonOk({ categorias: [] });
      if (url === '/api/nomenclatura-templates') return jsonOk({ templates: [] });
      if (url === '/api/solicitacoes-exclusao') return jsonOk({ solicitacoes: [] });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<CentrosCustoPage />);

    expect(await screen.findByRole('heading', { level: 2, name: 'Centros de custo' })).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith('/api/centros-custo', expect.anything());
  });

  it.each(['usuario', 'almoxarife', 'gestor'])(
    'papel %s NÃO vê a seção "Centros de custo" e nunca chama GET /api/centros-custo',
    async (papel) => {
      authState.papel = papel;
      const fetchMock = stubFetch((url) => {
        if (url === '/api/promocoes/minha') return jsonOk({ solicitacao: null });
        if (url === '/api/promocoes') return jsonOk({ solicitacoes: [] });
        if (url === '/api/usuarios') return jsonOk({ usuarios: [] });
        if (url === '/api/convites') return jsonOk({ convites: [] });
        throw new Error(`URL inesperada: ${url}`);
      });

      render(<CentrosCustoPage />);

      await screen.findByText(/Você não tem acesso a/);
      expect(screen.queryByRole('heading', { name: 'Centros de custo' })).not.toBeInTheDocument();
      expect(fetchMock).not.toHaveBeenCalledWith(
        expect.stringContaining('/api/centros-custo'),
        expect.anything(),
      );
    },
  );
});

describe('SegurancaEmpresaPage — Dupla autenticação da Empresa (Story 14.3)', () => {
  it('adm vê a seção e carrega a exigência e o histórico', async () => {
    authState.papel = 'adm';
    authState.mfaHabilitado = true;
    const fetchMock = stubFetch((url) => {
      if (url === '/api/promocoes/minha') return jsonOk({ solicitacao: null });
      if (url === '/api/promocoes') return jsonOk({ solicitacoes: [] });
      if (url === '/api/usuarios') return jsonOk({ usuarios: [] });
      if (url === '/api/convites') return jsonOk({ convites: [] });
      if (url.startsWith('/api/logs-acesso')) return jsonOk({ logs: [] });
      if (url === '/api/seguranca/mfa-empresa') return jsonOk({ mfaObrigatorio: false, contasSemMfa: 0 });
      if (url === '/api/seguranca/auditoria') return jsonOk({ eventos: [] });
      if (url === '/api/centros-custo') return jsonOk({ centrosCusto: [] });
      if (url === '/api/filiais') return jsonOk({ filiais: [] });
      if (url === '/api/categorias') return jsonOk({ categorias: [] });
      if (url === '/api/nomenclatura-templates') return jsonOk({ templates: [] });
      if (url === '/api/solicitacoes-exclusao') return jsonOk({ solicitacoes: [] });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<SegurancaEmpresaPage />);

    expect(
      await screen.findByRole('heading', { level: 2, name: 'Dupla autenticação da Empresa' }),
    ).toBeInTheDocument();
    expect(await screen.findByRole('button', { name: 'Passar a exigir' })).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith('/api/seguranca/mfa-empresa', expect.anything());
    expect(fetchMock).toHaveBeenCalledWith('/api/seguranca/auditoria', expect.anything());
  });

  it.each(['usuario', 'almoxarife', 'gestor'])(
    'papel %s NÃO vê a seção e nunca chama /api/seguranca',
    async (papel) => {
      authState.papel = papel;
      authState.mfaHabilitado = true;
      const fetchMock = stubFetch((url) => {
        if (url === '/api/promocoes/minha') return jsonOk({ solicitacao: null });
        if (url === '/api/promocoes') return jsonOk({ solicitacoes: [] });
        if (url === '/api/usuarios') return jsonOk({ usuarios: [] });
        if (url === '/api/convites') return jsonOk({ convites: [] });
        throw new Error(`URL inesperada: ${url}`);
      });

      render(<SegurancaEmpresaPage />);

      await screen.findByText(/Você não tem acesso a/);
      expect(
        screen.queryByRole('heading', { name: 'Dupla autenticação da Empresa' }),
      ).not.toBeInTheDocument();
      expect(fetchMock).not.toHaveBeenCalledWith(
        expect.stringContaining('/api/seguranca'),
        expect.anything(),
      );
    },
  );
});

describe('CategoriasPage — Categorias (Story 10.5)', () => {
  it('adm vê a seção "Categorias" e carrega GET /api/categorias', async () => {
    authState.papel = 'adm';
    const fetchMock = stubFetch((url) => {
      if (url === '/api/promocoes/minha') return jsonOk({ solicitacao: null });
      if (url === '/api/promocoes') return jsonOk({ solicitacoes: [] });
      if (url === '/api/usuarios') return jsonOk({ usuarios: [] });
      if (url === '/api/convites') return jsonOk({ convites: [] });
      if (url.startsWith('/api/logs-acesso')) return jsonOk({ logs: [] });
      if (url === '/api/categorias') return jsonOk({ categorias: [] });
      if (url === '/api/nomenclatura-templates') return jsonOk({ templates: [] });
      if (url === '/api/solicitacoes-exclusao') return jsonOk({ solicitacoes: [] });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<CategoriasPage />);

    expect(await screen.findByRole('heading', { level: 2, name: 'Categorias' })).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith('/api/categorias', expect.anything());
  });

  it.each(['usuario', 'almoxarife', 'gestor'])(
    'papel %s NÃO vê a seção "Categorias" e nunca chama GET /api/categorias',
    async (papel) => {
      authState.papel = papel;
      const fetchMock = stubFetch((url) => {
        if (url === '/api/promocoes/minha') return jsonOk({ solicitacao: null });
        if (url === '/api/promocoes') return jsonOk({ solicitacoes: [] });
        if (url === '/api/usuarios') return jsonOk({ usuarios: [] });
        if (url === '/api/convites') return jsonOk({ convites: [] });
        throw new Error(`URL inesperada: ${url}`);
      });

      render(<CategoriasPage />);

      await screen.findByText(/Você não tem acesso a/);
      expect(screen.queryByRole('heading', { name: 'Categorias' })).not.toBeInTheDocument();
      expect(fetchMock).not.toHaveBeenCalledWith(
        expect.stringContaining('/api/categorias'),
        expect.anything(),
      );
    },
  );
});

describe('TemplatesPage — Templates de Nomenclatura (Story 10.6)', () => {
  it('adm vê a seção "Templates de Nomenclatura" e carrega GET /api/nomenclatura-templates', async () => {
    authState.papel = 'adm';
    const fetchMock = stubFetch((url) => {
      if (url === '/api/promocoes/minha') return jsonOk({ solicitacao: null });
      if (url === '/api/promocoes') return jsonOk({ solicitacoes: [] });
      if (url === '/api/usuarios') return jsonOk({ usuarios: [] });
      if (url === '/api/convites') return jsonOk({ convites: [] });
      if (url.startsWith('/api/logs-acesso')) return jsonOk({ logs: [] });
      if (url === '/api/categorias') return jsonOk({ categorias: [] });
      if (url === '/api/nomenclatura-templates') return jsonOk({ templates: [] });
      if (url === '/api/solicitacoes-exclusao') return jsonOk({ solicitacoes: [] });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<TemplatesPage />);

    expect(
      await screen.findByRole('heading', { level: 2, name: 'Templates de Nomenclatura' }),
    ).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith('/api/nomenclatura-templates', expect.anything());
  });

  it.each(['usuario', 'almoxarife', 'gestor'])(
    'papel %s NÃO vê a seção "Templates de Nomenclatura" e nunca chama GET /api/nomenclatura-templates',
    async (papel) => {
      authState.papel = papel;
      const fetchMock = stubFetch((url) => {
        if (url === '/api/promocoes/minha') return jsonOk({ solicitacao: null });
        if (url === '/api/promocoes') return jsonOk({ solicitacoes: [] });
        if (url === '/api/usuarios') return jsonOk({ usuarios: [] });
        if (url === '/api/convites') return jsonOk({ convites: [] });
        throw new Error(`URL inesperada: ${url}`);
      });

      render(<TemplatesPage />);

      await screen.findByText(/Você não tem acesso a/);
      expect(
        screen.queryByRole('heading', { name: 'Templates de Nomenclatura' }),
      ).not.toBeInTheDocument();
      expect(fetchMock).not.toHaveBeenCalledWith(
        expect.stringContaining('/api/nomenclatura-templates'),
        expect.anything(),
      );
    },
  );
});

describe('ConfiguracoesPage — Privacidade (Story 8.1)', () => {
  it.each(['usuario', 'almoxarife', 'gestor', 'adm'])(
    'papel %s vê a seção "Privacidade" com o botão "Baixar meus dados", sem gate de papel',
    async (papel) => {
      authState.papel = papel;
      stubFetch((url) => {
        if (url === '/api/promocoes/minha') return jsonOk({ solicitacao: null });
        if (url === '/api/promocoes') return jsonOk({ solicitacoes: [] });
        if (url === '/api/usuarios') return jsonOk({ usuarios: [] });
        if (url === '/api/convites') return jsonOk({ convites: [] });
        if (url.startsWith('/api/logs-acesso')) return jsonOk({ logs: [] });
        if (url === '/api/categorias') return jsonOk({ categorias: [] });
        if (url === '/api/nomenclatura-templates') return jsonOk({ templates: [] });
        if (url === '/api/solicitacoes-exclusao') return jsonOk({ solicitacoes: [] });
        throw new Error(`URL inesperada: ${url}`);
      });

      render(<ConfiguracoesPage />);

      expect(await screen.findByRole('heading', { level: 2, name: 'Privacidade' })).toBeInTheDocument();
      expect(screen.getByRole('button', { name: 'Baixar meus dados' })).toBeInTheDocument();
    },
  );

  it('montar a seção NÃO chama fetch nenhum (só o clique no botão baixa os dados)', async () => {
    authState.papel = 'usuario';
    const fetchMock = stubFetch((url) => {
      if (url === '/api/promocoes/minha') return jsonOk({ solicitacao: null });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<ConfiguracoesPage />);

    await screen.findByRole('button', { name: 'Baixar meus dados' });
    expect(fetchMock).not.toHaveBeenCalledWith(
      expect.stringContaining('/api/usuarios/me/exportar-dados'),
      expect.anything(),
    );
  });
});

describe('LgpdPage — Solicitações de exclusão (Story 8.2)', () => {
  it('adm vê a seção "Solicitações de exclusão"', async () => {
    authState.papel = 'adm';
    stubFetch((url) => {
      if (url === '/api/promocoes/minha') return jsonOk({ solicitacao: null });
      if (url === '/api/promocoes') return jsonOk({ solicitacoes: [] });
      if (url === '/api/usuarios') return jsonOk({ usuarios: [] });
      if (url === '/api/convites') return jsonOk({ convites: [] });
      if (url.startsWith('/api/logs-acesso')) return jsonOk({ logs: [] });
      if (url === '/api/categorias') return jsonOk({ categorias: [] });
      if (url === '/api/nomenclatura-templates') return jsonOk({ templates: [] });
      if (url === '/api/solicitacoes-exclusao') return jsonOk({ solicitacoes: [] });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<LgpdPage />);

    expect(
      await screen.findByRole('heading', { level: 2, name: 'Solicitações de exclusão' }),
    ).toBeInTheDocument();
  });

  it.each(['usuario', 'gestor'])(
    'papel %s NÃO vê a seção "Solicitações de exclusão" e nunca chama GET /api/solicitacoes-exclusao',
    async (papel) => {
      authState.papel = papel;
      const fetchMock = stubFetch((url) => {
        if (url === '/api/promocoes/minha') return jsonOk({ solicitacao: null });
        if (url === '/api/promocoes') return jsonOk({ solicitacoes: [] });
        if (url === '/api/usuarios') return jsonOk({ usuarios: [] });
        if (url === '/api/convites') return jsonOk({ convites: [] });
        throw new Error(`URL inesperada: ${url}`);
      });

      render(<LgpdPage />);

      await screen.findByText(/Você não tem acesso a/);
      expect(
        screen.queryByRole('heading', { name: 'Solicitações de exclusão' }),
      ).not.toBeInTheDocument();
      expect(fetchMock).not.toHaveBeenCalledWith(
        expect.stringContaining('/api/solicitacoes-exclusao'),
        expect.anything(),
      );
    },
  );
});

import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { CriarUsuarioSection } from './CriarUsuarioSection';

// useAuth() fornece o papel do ator — configurável por teste.
const authState = vi.hoisted(() => ({ papel: 'adm' as string }));

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
  return Promise.resolve({ ok: true, status: 201, json: async () => body });
}

function jsonErro(status: number, code: string, message = 'x') {
  return Promise.resolve({
    ok: false,
    status,
    json: async () => ({ error: { code, message } }),
  });
}

beforeEach(() => {
  authState.papel = 'adm';
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe('CriarUsuarioSection', () => {
  it('não monta nada para papel diferente de adm', () => {
    for (const papel of ['usuario', 'almoxarife', 'gestor']) {
      authState.papel = papel;
      const fn = stubFetch(() => jsonOk({}));

      const { container, unmount } = render(<CriarUsuarioSection />);

      expect(container).toBeEmptyDOMElement();
      expect(fn).not.toHaveBeenCalled();
      unmount();
    }
  });

  it('monta para adm, com papel "usuario" selecionado por padrão', () => {
    render(<CriarUsuarioSection />);
    expect(screen.getByLabelText('Papel')).toHaveValue('usuario');
    expect(
      screen.getByLabelText('Exigir autenticação em duas etapas (2FA) para esta conta'),
    ).not.toBeChecked();
  });

  it('cria a conta com POST /api/usuarios, mostra sucesso, limpa o formulário e chama onCriada', async () => {
    const user = userEvent.setup();
    const onCriada = vi.fn();
    const fn = stubFetch((url, init) => {
      if (url === '/api/usuarios' && init?.method === 'POST') {
        return jsonOk({ usuario: { id: 'u-novo', nome: 'Karla Tenório', papel: 'gestor' } });
      }
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<CriarUsuarioSection onCriada={onCriada} />);

    await user.type(screen.getByLabelText('Nome'), 'Karla Tenório');
    await user.type(screen.getByLabelText('E-mail'), 'karla@empresa.com');
    await user.type(screen.getByLabelText('Senha inicial'), 'SenhaForte123');
    await user.selectOptions(screen.getByLabelText('Papel'), 'gestor');
    await user.click(screen.getByLabelText('Exigir autenticação em duas etapas (2FA) para esta conta'));
    await user.click(screen.getByRole('button', { name: 'Criar conta' }));

    expect(await screen.findByText('Conta de Karla Tenório criada.')).toBeInTheDocument();
    expect(onCriada).toHaveBeenCalledTimes(1);
    expect(screen.getByLabelText('Nome')).toHaveValue('');
    expect(screen.getByLabelText('Papel')).toHaveValue('usuario');

    const post = fn.mock.calls.find(([, init]) => (init as RequestInit | undefined)?.method === 'POST');
    if (!post) {
      throw new Error('nenhum POST /api/usuarios foi disparado');
    }
    expect(JSON.parse((post[1] as RequestInit).body as string)).toEqual({
      nome: 'Karla Tenório',
      email: 'karla@empresa.com',
      senha: 'SenhaForte123',
      papel: 'gestor',
      mfaObrigatorioIndividual: true,
    });
  });

  it('traduz o 409 CONFLICT em "este e-mail já tem conta"', async () => {
    const user = userEvent.setup();
    stubFetch((url, init) => {
      if (url === '/api/usuarios' && init?.method === 'POST') return jsonErro(409, 'CONFLICT');
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<CriarUsuarioSection />);
    await user.type(screen.getByLabelText('Nome'), 'Nome');
    await user.type(screen.getByLabelText('E-mail'), 'dup@empresa.com');
    await user.type(screen.getByLabelText('Senha inicial'), 'SenhaForte123');
    await user.click(screen.getByRole('button', { name: 'Criar conta' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('Este e-mail já tem conta.');
  });

  it('repassa a mensagem do servidor para um 400 VALIDATION_ERROR (ex.: senha fraca)', async () => {
    const user = userEvent.setup();
    stubFetch((url, init) => {
      if (url === '/api/usuarios' && init?.method === 'POST') {
        return jsonErro(400, 'VALIDATION_ERROR', 'a senha precisa ter 8+ caracteres com letra e dígito.');
      }
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<CriarUsuarioSection />);
    await user.type(screen.getByLabelText('Nome'), 'Nome');
    await user.type(screen.getByLabelText('E-mail'), 'x@empresa.com');
    await user.type(screen.getByLabelText('Senha inicial'), 'curta1');
    await user.click(screen.getByRole('button', { name: 'Criar conta' }));

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'a senha precisa ter 8+ caracteres com letra e dígito.',
    );
  });

  it('botão fica desabilitado até nome/e-mail/senha estarem preenchidos', async () => {
    const user = userEvent.setup();
    stubFetch(() => jsonOk({}));
    render(<CriarUsuarioSection />);

    expect(screen.getByRole('button', { name: 'Criar conta' })).toBeDisabled();
    await user.type(screen.getByLabelText('Nome'), 'Nome');
    await user.type(screen.getByLabelText('E-mail'), 'x@empresa.com');
    expect(screen.getByRole('button', { name: 'Criar conta' })).toBeDisabled();
    await user.type(screen.getByLabelText('Senha inicial'), 'SenhaForte123');
    expect(screen.getByRole('button', { name: 'Criar conta' })).toBeEnabled();
  });
});

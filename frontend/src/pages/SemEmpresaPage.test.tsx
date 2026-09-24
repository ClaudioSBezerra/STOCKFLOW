import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { SemEmpresaPage } from './SemEmpresaPage';
import { CHAVE_MFA_PENDENTE } from '@/lib/entrada';

// Story 15.2 (AD-36): a raiz do domínio vira o login pela conta.

const fetchMock = vi.fn();
const assignMock = vi.fn();

function resposta(status: number, body: unknown) {
  return Promise.resolve({ ok: status >= 200 && status < 300, status, json: async () => body });
}

async function preencherEEnviar(user: ReturnType<typeof userEvent.setup>) {
  await user.type(screen.getByLabelText('E-mail'), 'fulano@empresa.com');
  await user.type(screen.getByLabelText('Senha'), 'senha-123456');
  await user.click(screen.getByRole('button', { name: 'Entrar' }));
}

describe('SemEmpresaPage — login pela conta (Story 15.2)', () => {
  beforeEach(() => {
    fetchMock.mockReset();
    assignMock.mockReset();
    vi.stubGlobal('fetch', fetchMock);
    vi.stubGlobal('location', { ...window.location, origin: 'http://localhost', assign: assignMock });
    window.sessionStorage.clear();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    window.sessionStorage.clear();
  });

  it('mostra e-mail, senha, Entrar e "Esqueci a senha", sem a página explicativa', () => {
    render(<SemEmpresaPage />);

    expect(screen.getByLabelText('E-mail')).toBeInTheDocument();
    expect(screen.getByLabelText('Senha')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Entrar' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Esqueci a senha' })).toBeInTheDocument();
    expect(screen.queryByText('Acesse pelo endereço da sua empresa')).not.toBeInTheDocument();
    expect(document.body.textContent ?? '').not.toMatch(/plataforma/i);
  });

  it('"Esqueci a senha" mostra a orientação inline', async () => {
    const user = userEvent.setup();
    render(<SemEmpresaPage />);

    await user.click(screen.getByRole('button', { name: 'Esqueci a senha' }));

    expect(
      screen.getByText("Para redefinir a senha, use 'Esqueci minha senha' no endereço de acesso da sua empresa."),
    ).toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('uma conta sem MFA: chama /api/auth/entrar (sem prefixo) e vai para /e/{slug}/', async () => {
    const user = userEvent.setup();
    fetchMock.mockImplementation(() => resposta(200, { slug: 'acme' }));
    render(<SemEmpresaPage />);

    await preencherEEnviar(user);

    await waitFor(() => expect(assignMock).toHaveBeenCalledWith('/e/acme/'));
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('/api/auth/entrar');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body as string)).toEqual({ email: 'fulano@empresa.com', senha: 'senha-123456' });
  });

  it('uma conta com MFA: grava o repasse e vai para /e/{slug}/login', async () => {
    const user = userEvent.setup();
    fetchMock.mockImplementation(() => resposta(200, { slug: 'acme', mfaRequerido: true, mfaToken: 'mfa-tok' }));
    render(<SemEmpresaPage />);

    await preencherEEnviar(user);

    await waitFor(() => expect(assignMock).toHaveBeenCalledWith('/e/acme/login'));
    expect(JSON.parse(window.sessionStorage.getItem(CHAVE_MFA_PENDENTE) ?? 'null')).toEqual({
      slug: 'acme',
      mfaToken: 'mfa-tok',
    });
  });

  it('real + Treinamento: pergunta, e a escolha conclui em /api/auth/entrar/escolha', async () => {
    const user = userEvent.setup();
    fetchMock.mockImplementation((url: string) => {
      if (url === '/api/auth/entrar') {
        return resposta(200, {
          escolha: [
            { slug: 'acme', nomeFantasia: 'Acme', treinamento: false },
            { slug: 'acme-treinamento', nomeFantasia: 'Acme', treinamento: true },
          ],
          escolhaToken: 'esc-tok',
        });
      }
      return resposta(200, { slug: 'acme-treinamento' });
    });
    render(<SemEmpresaPage />);

    await preencherEEnviar(user);

    expect(await screen.findByText('Ambiente real ou Treinamento?')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Acme\s*Ambiente real/ })).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: /Acme\s*Treinamento/ }));

    await waitFor(() => expect(assignMock).toHaveBeenCalledWith('/e/acme-treinamento/'));
    const [url, init] = fetchMock.mock.calls[1] as [string, RequestInit];
    expect(url).toBe('/api/auth/entrar/escolha');
    expect(JSON.parse(init.body as string)).toEqual({ escolhaToken: 'esc-tok', slug: 'acme-treinamento' });
  });

  it('escolha expirada volta ao formulário com a mensagem', async () => {
    const user = userEvent.setup();
    fetchMock.mockImplementation((url: string) => {
      if (url === '/api/auth/entrar') {
        return resposta(200, {
          escolha: [
            { slug: 'acme', nomeFantasia: 'Acme', treinamento: false },
            { slug: 'acme-treinamento', nomeFantasia: 'Acme', treinamento: true },
          ],
          escolhaToken: 'esc-tok',
        });
      }
      return resposta(401, { error: { code: 'ESCOLHA_INVALIDA', message: 'x' } });
    });
    render(<SemEmpresaPage />);

    await preencherEEnviar(user);
    await user.click(await screen.findByRole('button', { name: /Acme\s*Ambiente real/ }));

    expect(await screen.findByRole('alert')).toHaveTextContent('A escolha expirou. Faça login novamente.');
    expect(screen.getByLabelText('Senha')).toHaveValue('');
    expect(screen.getByRole('button', { name: 'Entrar' })).toBeEnabled();
    expect(assignMock).not.toHaveBeenCalled();
  });

  it('401 INVALID_CREDENTIALS mostra a mensagem genérica', async () => {
    const user = userEvent.setup();
    fetchMock.mockImplementation(() => resposta(401, { error: { code: 'INVALID_CREDENTIALS', message: 'x' } }));
    render(<SemEmpresaPage />);

    await preencherEEnviar(user);

    expect(await screen.findByRole('alert')).toHaveTextContent('E-mail ou senha inválidos.');
    expect(assignMock).not.toHaveBeenCalled();
  });

  it('429 ACCOUNT_LOCKED mostra a mensagem de bloqueio', async () => {
    const user = userEvent.setup();
    fetchMock.mockImplementation(() => resposta(429, { error: { code: 'ACCOUNT_LOCKED', message: 'x' } }));
    render(<SemEmpresaPage />);

    await preencherEEnviar(user);

    expect(await screen.findByRole('alert')).toHaveTextContent(/Muitas tentativas de login sem sucesso/);
  });

  it('erro de rede mostra a mensagem genérica', async () => {
    const user = userEvent.setup();
    fetchMock.mockImplementation(() => Promise.reject(new Error('rede')));
    render(<SemEmpresaPage />);

    await preencherEEnviar(user);

    expect(await screen.findByRole('alert')).toHaveTextContent('Não foi possível entrar. Tente novamente em instantes.');
  });
});

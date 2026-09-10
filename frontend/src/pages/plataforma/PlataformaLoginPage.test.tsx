import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { PlataformaLoginPage } from './PlataformaLoginPage';
import { getTokenPlataforma, limparTokenPlataforma } from '@/lib/plataforma';

const navigateMock = vi.fn();

vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom');
  return { ...actual, useNavigate: () => navigateMock };
});

let loginResp: () => Promise<unknown>;
const fetchMock = vi.fn();

function renderPage() {
  return render(
    <MemoryRouter>
      <PlataformaLoginPage />
    </MemoryRouter>,
  );
}

async function preencherEEnviar(user: ReturnType<typeof userEvent.setup>) {
  await user.type(screen.getByLabelText('E-mail'), 'dona@plataforma.com');
  await user.type(screen.getByLabelText('Senha'), 'senha-dono-123');
  await user.type(screen.getByLabelText('Código de verificação'), '123456');
  await user.click(screen.getByRole('button', { name: 'Entrar' }));
}

function erroHTTP(status: number, code: string) {
  return () =>
    Promise.resolve({ ok: false, status, json: async () => ({ error: { code, message: 'x' } }) });
}

describe('PlataformaLoginPage (Story 9.2)', () => {
  beforeEach(() => {
    navigateMock.mockClear();
    limparTokenPlataforma();
    loginResp = () => Promise.reject(new Error('loginResp não configurado neste teste'));
    fetchMock.mockReset();
    fetchMock.mockImplementation((url: string) => {
      if (url === '/api/plataforma/auth/login') {
        return loginResp();
      }
      return Promise.reject(new Error(`fetch não stubado para ${url}`));
    });
    vi.stubGlobal('fetch', fetchMock);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    limparTokenPlataforma();
  });

  it('pede e-mail, senha e o código de 6 dígitos do autenticador', () => {
    renderPage();

    expect(screen.getByLabelText('E-mail')).toBeInTheDocument();
    expect(screen.getByLabelText('Senha')).toHaveAttribute('type', 'password');
    const codigo = screen.getByLabelText('Código de verificação');
    expect(codigo).toHaveAttribute('autocomplete', 'one-time-code');
    expect(codigo).toHaveAttribute('inputmode', 'numeric');
    expect(codigo).toHaveAttribute('maxlength', '6');
  });

  it('só aceita dígitos no código', async () => {
    const user = userEvent.setup();
    renderPage();

    await user.type(screen.getByLabelText('Código de verificação'), '12a3b4');

    expect(screen.getByLabelText('Código de verificação')).toHaveValue('1234');
  });

  it('no sucesso envia os três fatores juntos, guarda o token e navega para /plataforma', async () => {
    const user = userEvent.setup();
    loginResp = () =>
      Promise.resolve({
        ok: true,
        status: 200,
        json: async () => ({ token: 'tk-dono', dono: { id: '1', nome: 'Dona', email: 'dona@plataforma.com' } }),
      });
    renderPage();

    await preencherEEnviar(user);

    await waitFor(() => expect(navigateMock).toHaveBeenCalledWith('/plataforma'));
    expect(JSON.parse(fetchMock.mock.calls[0][1].body as string)).toEqual({
      email: 'dona@plataforma.com',
      senha: 'senha-dono-123',
      codigo: '123456',
    });
    expect(getTokenPlataforma()).toBe('tk-dono');
  });

  it('401 INVALID_CREDENTIALS mostra a mesma mensagem para qualquer fator errado', async () => {
    const user = userEvent.setup();
    loginResp = erroHTTP(401, 'INVALID_CREDENTIALS');
    renderPage();

    await preencherEEnviar(user);

    expect(await screen.findByRole('alert')).toHaveTextContent('E-mail, senha ou código inválidos.');
    expect(navigateMock).not.toHaveBeenCalled();
  });

  it('400 VALIDATION_ERROR pede os três campos', async () => {
    const user = userEvent.setup();
    loginResp = erroHTTP(400, 'VALIDATION_ERROR');
    renderPage();

    await preencherEEnviar(user);

    expect(await screen.findByRole('alert')).toHaveTextContent('Preencha e-mail, senha e código.');
  });

  it('campo em branco é barrado no cliente, sem chamar a API', async () => {
    const user = userEvent.setup();
    renderPage();

    await user.type(screen.getByLabelText('E-mail'), 'dona@plataforma.com');
    await user.click(screen.getByRole('button', { name: 'Entrar' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('Preencha e-mail, senha e código.');
    expect(fetchMock).not.toHaveBeenCalled();
  });
});

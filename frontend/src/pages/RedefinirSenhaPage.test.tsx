import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { RedefinirSenhaPage } from './RedefinirSenhaPage';

function renderPage(path = '/redefinir-senha?token=tok-123') {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/redefinir-senha" element={<RedefinirSenhaPage />} />
        <Route path="/login" element={<div>tela de login</div>} />
        <Route path="/esqueci-senha" element={<div>tela de esqueci senha</div>} />
      </Routes>
    </MemoryRouter>,
  );
}

const okGet = { ok: true, json: async () => ({}) };
function erroResp(code: string) {
  return { ok: false, json: async () => ({ error: { code } }) };
}

async function digitarESubmeter(user: ReturnType<typeof userEvent.setup>, senha: string) {
  await user.type(await screen.findByLabelText('Nova senha'), senha);
  await user.click(screen.getByRole('button', { name: 'Redefinir senha' }));
}

describe('RedefinirSenhaPage', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn());
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('sem token na URL: estado de link inválido, sem chamar a API', async () => {
    renderPage('/redefinir-senha');

    expect(await screen.findByText('Este link de redefinição é inválido.')).toBeInTheDocument();
    expect(fetch).not.toHaveBeenCalled();
    expect(screen.getByRole('link', { name: 'Solicitar novo link' })).toHaveAttribute(
      'href',
      '/esqueci-senha',
    );
  });

  it('mount com GET 200: valida o token e mostra o formulário de nova senha', async () => {
    (fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(okGet);
    renderPage();

    expect(await screen.findByLabelText('Nova senha')).toBeInTheDocument();
    expect(fetch).toHaveBeenCalledWith('/api/auth/redefinir-senha?token=tok-123');
    expect(screen.getByLabelText('Nova senha')).toHaveAttribute('autocomplete', 'new-password');
  });

  it('mount com GET 400 TOKEN_EXPIRED: estado explicativo com link para /esqueci-senha', async () => {
    (fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(erroResp('TOKEN_EXPIRED'));
    renderPage();

    expect(
      await screen.findByText(
        'Este link expirou ou já foi utilizado. Solicite um novo para redefinir a senha.',
      ),
    ).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Solicitar novo link' })).toHaveAttribute(
      'href',
      '/esqueci-senha',
    );
  });

  it('mount com GET 404 NOT_FOUND: estado de link inválido', async () => {
    (fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(erroResp('NOT_FOUND'));
    renderPage();

    expect(await screen.findByText('Este link de redefinição é inválido.')).toBeInTheDocument();
  });

  it('submit de senha fraca: erro inline, sem um segundo fetch (POST)', async () => {
    const user = userEvent.setup();
    (fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(okGet);
    renderPage();

    await digitarESubmeter(user, 'fraca1');

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'A senha deve ter ao menos 8 caracteres, incluindo uma letra e um número.',
    );
    // Só o GET do mount — nenhum POST foi disparado.
    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it('submit com TOKEN_EXPIRED (corrida entre mount e envio): estado explicativo', async () => {
    const user = userEvent.setup();
    (fetch as ReturnType<typeof vi.fn>)
      .mockResolvedValueOnce(okGet)
      .mockResolvedValueOnce(erroResp('TOKEN_EXPIRED'));
    renderPage();

    await digitarESubmeter(user, 'nova-senha1');

    expect(
      await screen.findByText(
        'Este link expirou ou já foi utilizado. Solicite um novo para redefinir a senha.',
      ),
    ).toBeInTheDocument();
    const [postUrl, postInit] = (fetch as ReturnType<typeof vi.fn>).mock.calls[1];
    expect(postUrl).toBe('/api/auth/redefinir-senha');
    expect(postInit.method).toBe('POST');
    expect(JSON.parse(postInit.body as string)).toEqual({ token: 'tok-123', senha: 'nova-senha1' });
  });

  it('mantém o estado de sucesso quando uma resposta perdedora resolve depois com TOKEN_EXPIRED', async () => {
    const user = userEvent.setup();
    let resolvePrimeiro!: (v: unknown) => void;
    let resolveSegundo!: (v: unknown) => void;
    const primeiroPost = new Promise((r) => {
      resolvePrimeiro = r;
    });
    const segundoPost = new Promise((r) => {
      resolveSegundo = r;
    });
    (fetch as ReturnType<typeof vi.fn>)
      .mockResolvedValueOnce(okGet) // GET do mount
      .mockReturnValueOnce(primeiroPost) // POST #1 — pendente
      .mockReturnValueOnce(segundoPost); // POST #2 — pendente
    renderPage();

    await user.type(await screen.findByLabelText('Nova senha'), 'nova-senha1');
    const form = screen.getByRole('button', { name: 'Redefinir senha' }).closest('form');
    if (!form) {
      throw new Error('formulário não encontrado');
    }

    // Dois submits despachados no MESMO tick, antes de qualquer re-render que
    // reflita `enviando` — ambos os handlers usam o closure com `enviando`
    // ainda false e disparam um POST. Reproduz o duplo-clique muito rápido em
    // navegador real (onde React não faz flush síncrono entre os dois eventos).
    await act(async () => {
      form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
      form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    });
    expect(fetch).toHaveBeenCalledTimes(3);

    // POST #1 conclui com sucesso.
    await act(async () => {
      resolvePrimeiro({ ok: true, json: async () => ({ mensagem: 'ok' }) });
    });
    expect(
      screen.getByText('Senha redefinida com sucesso. Você já pode entrar com a nova senha.'),
    ).toBeInTheDocument();

    // POST #2 (perdedor) resolve depois com TOKEN_EXPIRED — não pode reverter
    // a tela de sucesso já estabelecida.
    await act(async () => {
      resolveSegundo(erroResp('TOKEN_EXPIRED'));
    });
    expect(
      screen.queryByText(
        'Este link expirou ou já foi utilizado. Solicite um novo para redefinir a senha.',
      ),
    ).not.toBeInTheDocument();
    expect(
      screen.getByText('Senha redefinida com sucesso. Você já pode entrar com a nova senha.'),
    ).toBeInTheDocument();
  });

  it('submit bem-sucedido: estado final com link para /login', async () => {
    const user = userEvent.setup();
    (fetch as ReturnType<typeof vi.fn>)
      .mockResolvedValueOnce(okGet)
      .mockResolvedValueOnce({ ok: true, json: async () => ({ mensagem: 'ok' }) });
    renderPage();

    await digitarESubmeter(user, 'nova-senha1');

    expect(
      await screen.findByText('Senha redefinida com sucesso. Você já pode entrar com a nova senha.'),
    ).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Ir para o login' })).toHaveAttribute('href', '/login');
  });
});

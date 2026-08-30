import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { EsqueciSenhaPage } from './EsqueciSenhaPage';

function renderPage() {
  return render(
    <MemoryRouter>
      <EsqueciSenhaPage />
    </MemoryRouter>,
  );
}

async function preencherEEnviar(user: ReturnType<typeof userEvent.setup>) {
  await user.type(screen.getByLabelText('E-mail'), 'fulano@empresa.com');
  await user.click(screen.getByRole('button', { name: 'Enviar link de redefinição' }));
}

describe('EsqueciSenhaPage', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn());
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('renderiza o campo de e-mail e o botão de enviar', () => {
    renderPage();
    expect(screen.getByLabelText('E-mail')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Enviar link de redefinição' })).toBeInTheDocument();
  });

  it('envia POST /api/auth/esqueci-senha com o e-mail preenchido', async () => {
    const user = userEvent.setup();
    (fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({ ok: true, json: async () => ({}) });
    renderPage();

    await preencherEEnviar(user);

    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(1));
    const [url, init] = (fetch as ReturnType<typeof vi.fn>).mock.calls[0];
    expect(url).toBe('/api/auth/esqueci-senha');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body as string)).toEqual({ email: 'fulano@empresa.com' });
  });

  it('mostra o estado de sucesso genérico e esconde o formulário em qualquer 2xx', async () => {
    const user = userEvent.setup();
    (fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({ ok: true, json: async () => ({}) });
    renderPage();

    await preencherEEnviar(user);

    expect(
      await screen.findByText('Se o e-mail existir, você receberá um link.'),
    ).toBeInTheDocument();
    expect(screen.queryByLabelText('E-mail')).not.toBeInTheDocument();
  });

  it('mostra a mensagem de nova tentativa quando a resposta não é ok', async () => {
    const user = userEvent.setup();
    (fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: false,
      json: async () => ({ error: { code: 'INTERNAL_ERROR' } }),
    });
    renderPage();

    await preencherEEnviar(user);

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível enviar o link agora. Tente novamente em instantes.',
    );
    expect(screen.getByLabelText('E-mail')).toBeInTheDocument();
  });

  it('mostra a mensagem de nova tentativa em erro de rede', async () => {
    const user = userEvent.setup();
    (fetch as ReturnType<typeof vi.fn>).mockRejectedValueOnce(new Error('network down'));
    renderPage();

    await preencherEEnviar(user);

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível enviar o link agora. Tente novamente em instantes.',
    );
  });
});

import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { CadastroPage } from './CadastroPage';

function renderPage() {
  return render(
    <MemoryRouter>
      <CadastroPage />
    </MemoryRouter>,
  );
}

async function preencherEEnviar(user: ReturnType<typeof userEvent.setup>) {
  await user.type(screen.getByLabelText('Nome'), 'Fulano de Tal');
  await user.type(screen.getByLabelText('E-mail'), 'fulano@empresa.com');
  await user.type(screen.getByLabelText('Senha'), 'senha-123456');
  await user.click(screen.getByRole('button', { name: 'Criar conta' }));
}

describe('CadastroPage', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn());
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('renderiza os campos nome/e-mail/senha e o botão de enviar', () => {
    renderPage();

    expect(screen.getByLabelText('Nome')).toBeInTheDocument();
    expect(screen.getByLabelText('E-mail')).toBeInTheDocument();
    expect(screen.getByLabelText('Senha')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Criar conta' })).toBeInTheDocument();
  });

  it('envia POST /api/auth/cadastro com o payload preenchido e nunca inclui papel', async () => {
    const user = userEvent.setup();
    (fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({ ok: true, json: async () => ({}) });
    renderPage();

    await preencherEEnviar(user);

    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(1));
    const [url, init] = (fetch as ReturnType<typeof vi.fn>).mock.calls[0];
    expect(url).toBe('/api/auth/cadastro');
    expect(init.method).toBe('POST');
    const body = JSON.parse(init.body as string);
    expect(body).toEqual({ nome: 'Fulano de Tal', email: 'fulano@empresa.com', senha: 'senha-123456' });
  });

  it('mostra a mensagem de sucesso e esconde o formulário quando o cadastro é aceito', async () => {
    const user = userEvent.setup();
    (fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({ ok: true, json: async () => ({}) });
    renderPage();

    await preencherEEnviar(user);

    expect(await screen.findByText('Verifique seu e-mail para confirmar a conta.')).toBeInTheDocument();
    expect(screen.queryByLabelText('Nome')).not.toBeInTheDocument();
  });

  it('mostra erro inline "Este e-mail já está cadastrado." no 409 CONFLICT', async () => {
    const user = userEvent.setup();
    (fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: false,
      json: async () => ({ error: { code: 'CONFLICT', message: 'dup' } }),
    });
    renderPage();

    await preencherEEnviar(user);

    expect(await screen.findByRole('alert')).toHaveTextContent('Este e-mail já está cadastrado.');
    expect(screen.getByLabelText('Nome')).toBeInTheDocument();
  });

  it('mostra erro inline de validação no 400 VALIDATION_ERROR', async () => {
    const user = userEvent.setup();
    (fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: false,
      json: async () => ({ error: { code: 'VALIDATION_ERROR', message: 'obrigatorio' } }),
    });
    renderPage();

    await preencherEEnviar(user);

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Preencha nome, e-mail e senha para continuar.',
    );
  });

  it('mostra mensagem genérica quando a requisição falha por erro de rede', async () => {
    const user = userEvent.setup();
    (fetch as ReturnType<typeof vi.fn>).mockRejectedValueOnce(new Error('network down'));
    renderPage();

    await preencherEEnviar(user);

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível concluir o cadastro. Tente novamente em instantes.',
    );
  });

  // Reproduz o cenário do guard `if (enviando) return;`: dois `submit`
  // disparados sem nenhum `await` entre eles executam o prefixo síncrono de
  // handleSubmit duas vezes sob o mesmo valor de `enviando` (o atributo
  // `disabled` do botão só reflete o novo estado após o próximo repaint).
  // Sem o guard, isso dispararia dois POSTs.
  it('nunca envia um segundo POST quando dois submits chegam antes do repaint do botão desabilitado', async () => {
    const user = userEvent.setup();
    (fetch as ReturnType<typeof vi.fn>).mockResolvedValue({ ok: true, json: async () => ({}) });
    renderPage();

    await user.type(screen.getByLabelText('Nome'), 'Fulano de Tal');
    await user.type(screen.getByLabelText('E-mail'), 'fulano@empresa.com');
    await user.type(screen.getByLabelText('Senha'), 'senha-123456');

    const form = screen.getByRole('button', { name: 'Criar conta' }).closest('form');
    if (!form) {
      throw new Error('formulário não encontrado');
    }
    fireEvent.submit(form);
    fireEvent.submit(form);

    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(1));
  });
});

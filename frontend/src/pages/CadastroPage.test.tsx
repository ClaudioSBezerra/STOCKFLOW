import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { CadastroPage } from './CadastroPage';

const TOKEN = 'token-de-convite';
const EMAIL_CONVIDADO = 'fulano@empresa.com';
const URL_VALIDACAO = `/api/auth/convite?token=${TOKEN}`;

function renderPage(query = `?token=${TOKEN}`) {
  return render(
    <MemoryRouter initialEntries={[`/cadastro${query}`]}>
      <CadastroPage />
    </MemoryRouter>,
  );
}

/**
 * Responde ao GET de validação do convite com 200 + e-mail convidado, e deixa
 * o POST de cadastro para o `mockResolvedValueOnce` de cada teste — a ordem é
 * sempre GET (mount) e depois POST (submit).
 */
function stubValidacaoOk() {
  (fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
    ok: true,
    json: async () => ({ email: EMAIL_CONVIDADO }),
  });
}

function stubValidacaoErro(code: string) {
  (fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
    ok: false,
    json: async () => ({ error: { code, message: 'x' } }),
  });
}

/** Espera o formulário aparecer (o GET de validação terminou com sucesso). */
async function aguardarFormulario() {
  expect(await screen.findByLabelText('Nome')).toBeInTheDocument();
}

async function preencherEEnviar(user: ReturnType<typeof userEvent.setup>) {
  await user.type(screen.getByLabelText('Nome'), 'Fulano de Tal');
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

  it('valida o convite no mount e só então mostra o formulário, com o e-mail convidado somente-leitura', async () => {
    stubValidacaoOk();
    renderPage();

    await waitFor(() => expect(fetch).toHaveBeenCalledWith(URL_VALIDACAO));
    await aguardarFormulario();

    const campoEmail = screen.getByLabelText('E-mail');
    expect(campoEmail).toHaveValue(EMAIL_CONVIDADO);
    expect(campoEmail).toHaveAttribute('readonly');
    expect(screen.getByLabelText('Senha')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Criar conta' })).toBeInTheDocument();
  });

  it('sem ?token= não valida nada nem mostra o formulário — explica que o convite é necessário', async () => {
    renderPage('');

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Para criar uma conta você precisa de um convite da empresa. Peça o link a um gestor.',
    );
    expect(screen.queryByLabelText('Nome')).not.toBeInTheDocument();
    expect(fetch).not.toHaveBeenCalled();
  });

  it('envia POST /api/auth/cadastro com o token do convite e nunca inclui papel', async () => {
    const user = userEvent.setup();
    stubValidacaoOk();
    (fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({ ok: true, json: async () => ({}) });
    renderPage();
    await aguardarFormulario();

    await preencherEEnviar(user);

    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(2));
    const [url, init] = (fetch as ReturnType<typeof vi.fn>).mock.calls[1];
    expect(url).toBe('/api/auth/cadastro');
    expect(init.method).toBe('POST');
    const body = JSON.parse(init.body as string);
    expect(body).toEqual({
      token: TOKEN,
      nome: 'Fulano de Tal',
      email: EMAIL_CONVIDADO,
      senha: 'senha-123456',
    });
    expect(body).not.toHaveProperty('papel');
  });

  it('mostra a mensagem de sucesso e esconde o formulário quando o cadastro é aceito', async () => {
    const user = userEvent.setup();
    stubValidacaoOk();
    (fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({ ok: true, json: async () => ({}) });
    renderPage();
    await aguardarFormulario();

    await preencherEEnviar(user);

    expect(
      await screen.findByText('Verifique seu e-mail para confirmar a conta.'),
    ).toBeInTheDocument();
    expect(screen.queryByLabelText('Nome')).not.toBeInTheDocument();
  });

  it.each([
    ['NOT_FOUND', 'Este convite não é válido. Peça um novo link a um gestor da empresa.'],
    ['TOKEN_EXPIRED', 'Este convite expirou. Peça um novo link a um gestor da empresa.'],
    ['CONFLICT', 'Este convite já foi utilizado. Se a conta já existe, entre pelo login.'],
    ['FORBIDDEN', 'Este convite foi cancelado pela empresa.'],
  ])('traduz %s do GET de validação em uma explicação própria, sem formulário', async (code, mensagem) => {
    stubValidacaoErro(code);
    renderPage();

    expect(await screen.findByRole('alert')).toHaveTextContent(mensagem);
    expect(screen.queryByLabelText('Nome')).not.toBeInTheDocument();
  });

  it('troca o formulário pela explicação quando o convite morre entre o mount e o envio', async () => {
    const user = userEvent.setup();
    stubValidacaoOk();
    (fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: false,
      json: async () => ({ error: { code: 'CONFLICT', message: 'usado' } }),
    });
    renderPage();
    await aguardarFormulario();

    await preencherEEnviar(user);

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Este convite já foi utilizado. Se a conta já existe, entre pelo login.',
    );
    expect(screen.queryByLabelText('Nome')).not.toBeInTheDocument();
  });

  it('mapeia um 400 VALIDATION_ERROR do servidor para o critério de senha, mantendo o formulário', async () => {
    const user = userEvent.setup();
    stubValidacaoOk();
    (fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: false,
      json: async () => ({ error: { code: 'VALIDATION_ERROR', message: 'obrigatorio' } }),
    });
    renderPage();
    await aguardarFormulario();

    await preencherEEnviar(user);

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'A senha deve ter ao menos 8 caracteres, incluindo uma letra e um número.',
    );
    expect(screen.getByLabelText('Nome')).toBeInTheDocument();
  });

  it('barra campos obrigatórios vazios no cliente com mensagem própria, sem chamar a API', async () => {
    const user = userEvent.setup();
    stubValidacaoOk();
    renderPage();
    await aguardarFormulario();

    // Só o nome preenchido; senha vazia.
    await user.type(screen.getByLabelText('Nome'), 'Fulano de Tal');
    await user.click(screen.getByRole('button', { name: 'Criar conta' }));

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Preencha nome e senha para continuar.',
    );
    // Só o GET de validação do mount.
    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it('barra o submit com senha fraca: erro inline com o critério, sem chamar a API', async () => {
    const user = userEvent.setup();
    stubValidacaoOk();
    renderPage();
    await aguardarFormulario();

    await user.type(screen.getByLabelText('Nome'), 'Fulano de Tal');
    await user.type(screen.getByLabelText('Senha'), 'abc');
    await user.click(screen.getByRole('button', { name: 'Criar conta' }));

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'A senha deve ter ao menos 8 caracteres, incluindo uma letra e um número.',
    );
    expect(fetch).toHaveBeenCalledTimes(1);
    // O formulário continua visível para correção.
    expect(screen.getByLabelText('Senha')).toBeInTheDocument();
  });

  it('mostra mensagem genérica quando o POST falha por erro de rede', async () => {
    const user = userEvent.setup();
    stubValidacaoOk();
    (fetch as ReturnType<typeof vi.fn>).mockRejectedValueOnce(new Error('network down'));
    renderPage();
    await aguardarFormulario();

    await preencherEEnviar(user);

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível concluir o cadastro. Tente novamente em instantes.',
    );
  });

  // Reproduz o cenário do guard `if (enviando) return;`: dois `submit`
  // disparados sem nenhum `await` entre eles executam o prefixo síncrono de
  // handleSubmit duas vezes sob o mesmo valor de `enviando` (o atributo
  // `disabled` do botão só reflete o novo estado após o próximo repaint).
  // Sem o guard, isso dispararia dois POSTs — e um convite é de uso único, o
  // segundo queimaria em CONFLICT e trocaria a tela de sucesso por um erro.
  it('nunca envia um segundo POST quando dois submits chegam antes do repaint do botão desabilitado', async () => {
    const user = userEvent.setup();
    stubValidacaoOk();
    (fetch as ReturnType<typeof vi.fn>).mockResolvedValue({ ok: true, json: async () => ({}) });
    renderPage();
    await aguardarFormulario();

    await user.type(screen.getByLabelText('Nome'), 'Fulano de Tal');
    await user.type(screen.getByLabelText('Senha'), 'senha-123456');

    const form = screen.getByRole('button', { name: 'Criar conta' }).closest('form');
    if (!form) {
      throw new Error('formulário não encontrado');
    }
    fireEvent.submit(form);
    fireEvent.submit(form);

    // 1 GET de validação + exatamente 1 POST.
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(2));
  });
});

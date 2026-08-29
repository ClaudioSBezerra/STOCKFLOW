import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes, useNavigate } from 'react-router-dom';
import { VerificarEmailPage } from './VerificarEmailPage';

// Navega para um segundo token sem desmontar VerificarEmailPage — simula
// dois links de verificação abertos na mesma aba via navegação client-side
// (ex. botão voltar, ou um segundo link clicado sem recarregar a página).
function TrocaDeToken({ proximoToken }: { proximoToken: string }) {
  const navigate = useNavigate();
  return (
    <>
      <button onClick={() => navigate(`/verificar-email?token=${proximoToken}`)}>trocar token</button>
      <VerificarEmailPage />
    </>
  );
}

function renderPage(path = '/verificar-email?token=abc123') {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/verificar-email" element={<VerificarEmailPage />} />
      </Routes>
    </MemoryRouter>,
  );
}

describe('VerificarEmailPage', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn());
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('chama GET /api/auth/verificar-email com o token da URL', async () => {
    (fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({ ok: true, json: async () => ({}) });
    renderPage('/verificar-email?token=meu-token');

    await screen.findByText('E-mail verificado com sucesso. Sua conta já está pronta para uso.');

    expect(fetch).toHaveBeenCalledWith('/api/auth/verificar-email?token=meu-token');
  });

  it('mostra mensagem de sucesso quando o token é válido', async () => {
    (fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({ ok: true, json: async () => ({}) });
    renderPage();

    expect(
      await screen.findByText('E-mail verificado com sucesso. Sua conta já está pronta para uso.'),
    ).toBeInTheDocument();
  });

  it('mostra mensagem de expirado no TOKEN_EXPIRED', async () => {
    (fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: false,
      json: async () => ({ error: { code: 'TOKEN_EXPIRED' } }),
    });
    renderPage();

    expect(
      await screen.findByText(
        'Este link expirou ou já foi utilizado. Solicite um novo cadastro para gerar outro link.',
      ),
    ).toBeInTheDocument();
  });

  it('mostra mensagem de link inválido no NOT_FOUND', async () => {
    (fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: false,
      json: async () => ({ error: { code: 'NOT_FOUND' } }),
    });
    renderPage();

    expect(await screen.findByText('Link de verificação inválido.')).toBeInTheDocument();
  });

  it('mostra link inválido sem sequer chamar a API quando não há token na URL', async () => {
    renderPage('/verificar-email');

    expect(await screen.findByText('Link de verificação inválido.')).toBeInTheDocument();
    expect(fetch).not.toHaveBeenCalled();
  });

  it('mostra mensagem genérica de erro em falha de rede', async () => {
    (fetch as ReturnType<typeof vi.fn>).mockRejectedValueOnce(new Error('network down'));
    renderPage();

    expect(
      await screen.findByText('Não foi possível verificar seu e-mail agora. Tente novamente em instantes.'),
    ).toBeInTheDocument();
  });

  // Cobre o fallback genérico com uma resposta HTTP real (não uma rejeição de
  // rede) carregando um código não mapeado — distinto do teste de falha de
  // rede acima, que nunca alcança o branch de decodificação do corpo JSON.
  it('mostra mensagem genérica de erro numa resposta 500 com código não mapeado', async () => {
    (fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: false,
      json: async () => ({ error: { code: 'INTERNAL_ERROR' } }),
    });
    renderPage();

    expect(
      await screen.findByText('Não foi possível verificar seu e-mail agora. Tente novamente em instantes.'),
    ).toBeInTheDocument();
  });

  it('reverifica quando o token da URL muda sem desmontar a página', async () => {
    const user = userEvent.setup();
    (fetch as ReturnType<typeof vi.fn>)
      .mockResolvedValueOnce({ ok: true, json: async () => ({}) })
      .mockResolvedValueOnce({ ok: false, json: async () => ({ error: { code: 'NOT_FOUND' } }) });

    render(
      <MemoryRouter initialEntries={['/verificar-email?token=primeiro-token']}>
        <Routes>
          <Route path="/verificar-email" element={<TrocaDeToken proximoToken="segundo-token" />} />
        </Routes>
      </MemoryRouter>,
    );

    await screen.findByText('E-mail verificado com sucesso. Sua conta já está pronta para uso.');

    await user.click(screen.getByRole('button', { name: 'trocar token' }));

    await screen.findByText('Link de verificação inválido.');
    expect(fetch).toHaveBeenNthCalledWith(1, '/api/auth/verificar-email?token=primeiro-token');
    expect(fetch).toHaveBeenNthCalledWith(2, '/api/auth/verificar-email?token=segundo-token');
  });

  // Reproduz a janela de corrida: o usuário troca de token antes da resposta
  // do primeiro token chegar. Se a resposta obsoleta do primeiro token não
  // for descartada, ela sobrescreveria o resultado já exibido para o
  // segundo token quando finalmente resolvesse.
  it('descarta resposta obsoleta de um token anterior quando o token muda antes dela resolver', async () => {
    const user = userEvent.setup();
    let resolvePrimeiroToken!: (value: { ok: boolean; json: () => Promise<unknown> }) => void;
    const respostaPrimeiroToken = new Promise<{ ok: boolean; json: () => Promise<unknown> }>((resolve) => {
      resolvePrimeiroToken = resolve;
    });

    (fetch as ReturnType<typeof vi.fn>)
      .mockReturnValueOnce(respostaPrimeiroToken)
      .mockResolvedValueOnce({ ok: false, json: async () => ({ error: { code: 'NOT_FOUND' } }) });

    render(
      <MemoryRouter initialEntries={['/verificar-email?token=primeiro-token']}>
        <Routes>
          <Route path="/verificar-email" element={<TrocaDeToken proximoToken="segundo-token" />} />
        </Routes>
      </MemoryRouter>,
    );

    await user.click(screen.getByRole('button', { name: 'trocar token' }));
    await screen.findByText('Link de verificação inválido.');

    resolvePrimeiroToken({ ok: true, json: async () => ({}) });

    // Dá tempo para a promise resolvida do primeiro token ser processada,
    // se o componente ainda estivesse escutando por ela.
    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(screen.getByText('Link de verificação inválido.')).toBeInTheDocument();
    expect(
      screen.queryByText('E-mail verificado com sucesso. Sua conta já está pronta para uso.'),
    ).not.toBeInTheDocument();
  });
});

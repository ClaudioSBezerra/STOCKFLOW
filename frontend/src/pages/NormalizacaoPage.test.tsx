import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom';
import { NormalizacaoPage } from './NormalizacaoPage';
import { DuplicatasPage } from './DuplicatasPage';

// useAuth() fornece o papel do ator — configurável por teste.
const authState = vi.hoisted(() => ({ papel: 'almoxarife' as string }));

vi.mock('@/lib/auth', () => ({
  useAuth: () => ({
    estado: 'autenticado',
    usuario: { id: '1', nome: 'Ator', email: 'ator@empresa.com', papel: authState.papel },
    definirSessao: vi.fn(),
    atualizarUsuario: vi.fn(),
    logout: vi.fn(),
  }),
}));

vi.mock('@/lib/session', () => ({
  getAccessToken: () => 'token-de-teste',
}));

// Rotas reais desta story (`/normalizacao` e `/normalizacao/duplicatas`) num
// MemoryRouter — a página de Inconsistências redireciona o link antigo.
function LocalizacaoAtual() {
  const { pathname, search } = useLocation();
  return <output data-testid="local">{pathname + search}</output>;
}

function renderPage(path = '/normalizacao') {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <LocalizacaoAtual />
      <Routes>
        <Route path="/normalizacao" element={<NormalizacaoPage />} />
        <Route path="/normalizacao/duplicatas" element={<DuplicatasPage />} />
      </Routes>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  authState.papel = 'almoxarife';
  // Nem InconsistenciasSection nem DuplicatasSection (sem autoAnalisar)
  // carregam nada no mount (a análise é só ao clique) — o stub existe só
  // para provar que nenhum fetch automático acontece.
  vi.stubGlobal('fetch', vi.fn());
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe('NormalizacaoPage (/normalizacao) — Inconsistências', () => {
  it.each(['almoxarife', 'gestor', 'adm'])('papel %s vê Inconsistências direto, sem abas', (papel) => {
    authState.papel = papel;
    renderPage();

    expect(screen.getByRole('heading', { level: 1, name: 'Inconsistências' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Analisar todos os produtos' })).toBeInTheDocument();
    expect(screen.queryByRole('tab')).not.toBeInTheDocument();
    expect(
      screen.queryByText('Você não tem acesso à área de Normalização.'),
    ).not.toBeInTheDocument();
    expect(fetch).not.toHaveBeenCalled();
  });

  it('papel usuario vê a mensagem de acesso restrito', () => {
    authState.papel = 'usuario';
    renderPage();

    expect(screen.getByText('Você não tem acesso à área de Normalização.')).toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: 'Inconsistências' })).not.toBeInTheDocument();
    expect(
      screen.queryByRole('button', { name: 'Analisar todos os produtos' }),
    ).not.toBeInTheDocument();
    expect(fetch).not.toHaveBeenCalled();
  });
});

describe('DuplicatasPage (/normalizacao/duplicatas) — gate de papel', () => {
  it.each(['almoxarife', 'gestor', 'adm'])('papel %s abre Duplicatas direto, sem análise automática', (papel) => {
    authState.papel = papel;
    renderPage('/normalizacao/duplicatas');

    expect(screen.getByRole('heading', { level: 1, name: 'Duplicatas' })).toBeInTheDocument();
    expect(screen.queryByRole('tab')).not.toBeInTheDocument();
    expect(fetch).not.toHaveBeenCalled();
  });

  it('papel usuario vê a mensagem de acesso restrito', () => {
    authState.papel = 'usuario';
    renderPage('/normalizacao/duplicatas');

    expect(screen.getByText('Você não tem acesso à área de Normalização.')).toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: 'Duplicatas' })).not.toBeInTheDocument();
    expect(fetch).not.toHaveBeenCalled();
  });
});

describe('link antigo /normalizacao?verificarDuplicatas=1', () => {
  it('redireciona para /normalizacao/duplicatas?verificarDuplicatas=1 e analisa uma única vez', async () => {
    (fetch as ReturnType<typeof vi.fn>).mockResolvedValue({ ok: true, json: async () => ({ grupos: [] }) });

    renderPage('/normalizacao?verificarDuplicatas=1');

    expect(screen.getByTestId('local')).toHaveTextContent(
      '/normalizacao/duplicatas?verificarDuplicatas=1',
    );
    expect(screen.getByRole('heading', { level: 1, name: 'Duplicatas' })).toBeInTheDocument();
    expect(fetch).toHaveBeenCalledWith(
      '/api/normalizacao/duplicatas',
      expect.objectContaining({ headers: { Authorization: 'Bearer token-de-teste' } }),
    );
    expect(await screen.findByText('Nenhuma duplicata encontrada.')).toBeInTheDocument();
    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it('abrir /normalizacao/duplicatas?verificarDuplicatas=1 direto também analisa uma vez', async () => {
    (fetch as ReturnType<typeof vi.fn>).mockResolvedValue({ ok: true, json: async () => ({ grupos: [] }) });

    renderPage('/normalizacao/duplicatas?verificarDuplicatas=1');

    expect(await screen.findByText('Nenhuma duplicata encontrada.')).toBeInTheDocument();
    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it('papel usuario com o link antigo cai na recusa de Duplicatas, sem fetch', () => {
    authState.papel = 'usuario';
    renderPage('/normalizacao?verificarDuplicatas=1');

    expect(screen.getByText('Você não tem acesso à área de Normalização.')).toBeInTheDocument();
    expect(fetch).not.toHaveBeenCalled();
  });
});

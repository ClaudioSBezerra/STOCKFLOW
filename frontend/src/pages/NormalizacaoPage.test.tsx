import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { NormalizacaoPage } from './NormalizacaoPage';

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

// renderPage monta NormalizacaoPage dentro de um MemoryRouter — necessário
// desde a Story 6.3 (spec-6-3), que passou a ler `?verificarDuplicatas=1`
// via useSearchParams (precisa de contexto de Router, ao contrário da
// versão de página única da Story 6.1).
function renderPage(path = '/normalizacao') {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/normalizacao" element={<NormalizacaoPage />} />
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

describe('NormalizacaoPage — gate de papel', () => {
  it.each(['almoxarife', 'gestor', 'adm'])('papel %s vê as abas Inconsistências/Duplicatas', (papel) => {
    authState.papel = papel;
    renderPage();

    expect(screen.getByRole('tab', { name: 'Inconsistências' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Duplicatas' })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Inconsistências' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Analisar todos os produtos' })).toBeInTheDocument();
    expect(
      screen.queryByText('Você não tem acesso à área de Normalização.'),
    ).not.toBeInTheDocument();
    // Nenhum fetch automático no mount — a aba inicial (Inconsistências) só
    // analisa ao clique, e a URL não pede `verificarDuplicatas=1`.
    expect(fetch).not.toHaveBeenCalled();
  });

  it('papel usuario vê a mensagem de acesso restrito e nenhuma aba', () => {
    authState.papel = 'usuario';
    renderPage();

    expect(screen.getByText('Você não tem acesso à área de Normalização.')).toBeInTheDocument();
    expect(screen.queryByRole('tab', { name: 'Inconsistências' })).not.toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: 'Inconsistências' })).not.toBeInTheDocument();
    expect(
      screen.queryByRole('button', { name: 'Analisar todos os produtos' }),
    ).not.toBeInTheDocument();
    expect(fetch).not.toHaveBeenCalled();
  });
});

describe('NormalizacaoPage — CTA "Verificar duplicatas agora" (?verificarDuplicatas=1)', () => {
  it('abre direto na aba Duplicatas com a análise já em andamento', async () => {
    (fetch as ReturnType<typeof vi.fn>).mockResolvedValue({ ok: true, json: async () => ({ grupos: [] }) });

    renderPage('/normalizacao?verificarDuplicatas=1');

    expect(screen.getByRole('tab', { name: 'Duplicatas', selected: true })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Duplicatas' })).toBeInTheDocument();
    // autoAnalisar dispara o fetch sozinho, sem exigir clique.
    expect(fetch).toHaveBeenCalledWith(
      '/api/normalizacao/duplicatas',
      expect.objectContaining({ headers: { Authorization: 'Bearer token-de-teste' } }),
    );
    expect(await screen.findByText('Nenhuma duplicata encontrada.')).toBeInTheDocument();
  });

  // Fix do code review de spec-6-3: TabsContent (Radix) desmonta o conteúdo
  // da aba inativa por padrão — trocar para Inconsistências e voltar para
  // Duplicatas remonta DuplicatasSection do zero. Um guard local dentro dela
  // (useRef) resetaria a cada remontagem e disparia o fetch automático de
  // novo; o guard correto mora em NormalizacaoPage (que não desmonta ao
  // trocar de aba) e nunca deve deixar autoAnalisar voltar a `true` depois do
  // primeiro disparo desta visita.
  it('trocar de aba e voltar para Duplicatas NÃO dispara o fetch automático de novo', async () => {
    const user = userEvent.setup();
    (fetch as ReturnType<typeof vi.fn>).mockResolvedValue({ ok: true, json: async () => ({ grupos: [] }) });

    renderPage('/normalizacao?verificarDuplicatas=1');

    // Disparo automático inicial, ao cair na aba Duplicatas.
    await screen.findByText('Nenhuma duplicata encontrada.');
    expect(fetch).toHaveBeenCalledTimes(1);

    // Troca para Inconsistências (desmonta DuplicatasSection) e volta para
    // Duplicatas (remonta DuplicatasSection).
    await user.click(screen.getByRole('tab', { name: 'Inconsistências' }));
    expect(screen.getByRole('tab', { name: 'Inconsistências', selected: true })).toBeInTheDocument();
    await user.click(screen.getByRole('tab', { name: 'Duplicatas' }));
    expect(screen.getByRole('tab', { name: 'Duplicatas', selected: true })).toBeInTheDocument();

    // A remontagem não deve ter disparado um segundo fetch automático.
    expect(fetch).toHaveBeenCalledTimes(1);
  });
});

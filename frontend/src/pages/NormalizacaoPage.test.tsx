import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
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

beforeEach(() => {
  authState.papel = 'almoxarife';
  // InconsistenciasSection NÃO carrega nada no mount (a análise é só ao
  // clique) — o stub existe só para provar que ele nunca é chamado.
  vi.stubGlobal('fetch', vi.fn());
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe('NormalizacaoPage — gate de papel', () => {
  it.each(['almoxarife', 'gestor', 'adm'])('papel %s vê a seção "Inconsistências"', (papel) => {
    authState.papel = papel;
    render(<NormalizacaoPage />);

    expect(screen.getByRole('heading', { name: 'Inconsistências' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Analisar todos os produtos' })).toBeInTheDocument();
    expect(
      screen.queryByText('Você não tem acesso à área de Normalização.'),
    ).not.toBeInTheDocument();
    // Nenhum fetch automático no mount — a análise é só ao clique.
    expect(fetch).not.toHaveBeenCalled();
  });

  it('papel usuario vê a mensagem de acesso restrito e nenhuma seção', () => {
    authState.papel = 'usuario';
    render(<NormalizacaoPage />);

    expect(screen.getByText('Você não tem acesso à área de Normalização.')).toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: 'Inconsistências' })).not.toBeInTheDocument();
    expect(
      screen.queryByRole('button', { name: 'Analisar todos os produtos' }),
    ).not.toBeInTheDocument();
    expect(fetch).not.toHaveBeenCalled();
  });
});

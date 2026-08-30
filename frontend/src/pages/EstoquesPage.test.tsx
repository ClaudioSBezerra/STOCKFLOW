import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { EstoquesPage } from './EstoquesPage';

// useAuth() fornece o papel — configurável por teste.
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
  // LocaisEstoqueSection busca a lista no mount — resposta vazia basta.
  vi.stubGlobal(
    'fetch',
    vi.fn(() => Promise.resolve({ ok: true, json: async () => ({ estoques: [] }) })),
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe('EstoquesPage — gate de papel', () => {
  it.each(['almoxarife', 'gestor', 'adm'])('papel %s vê a seção "Locais"', async (papel) => {
    authState.papel = papel;
    render(<EstoquesPage />);

    expect(await screen.findByRole('heading', { name: 'Locais' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Adicionar estoque' })).toBeInTheDocument();
    expect(
      screen.queryByText('Você não tem acesso à área de Estoques.'),
    ).not.toBeInTheDocument();
  });

  it('papel usuario vê a mensagem de acesso restrito e nenhum formulário', () => {
    authState.papel = 'usuario';
    render(<EstoquesPage />);

    expect(screen.getByText('Você não tem acesso à área de Estoques.')).toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: 'Locais' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Adicionar estoque' })).not.toBeInTheDocument();
    expect(fetch).not.toHaveBeenCalled();
  });
});

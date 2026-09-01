import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { act, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { EstoquesPage } from './EstoquesPage';
import type { EventoRealtime, StatusRealtime } from '@/lib/realtime/client';

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

vi.mock('sonner', () => ({ toast: { info: vi.fn(), success: vi.fn() } }));

// conectarRealtime mockado (molde de ProdutoDetalhePage.test.tsx): captura os
// dois callbacks para que o teste dispare 'conectado' quando a aba
// "Movimentações" é aberta.
const conectarRealtimeMock = vi.hoisted(() => vi.fn());
vi.mock('@/lib/realtime/client', () => ({
  conectarRealtime: conectarRealtimeMock,
}));

let aoMudarStatus: (status: StatusRealtime) => void;
const desconectarMock = vi.fn();

beforeEach(() => {
  authState.papel = 'almoxarife';
  conectarRealtimeMock.mockImplementation(
    (_receber: (evento: EventoRealtime) => void, mudar: (status: StatusRealtime) => void) => {
      aoMudarStatus = mudar;
      return desconectarMock;
    },
  );
  // LocaisEstoqueSection busca a lista no mount; MovimentacoesSection busca
  // GET /api/movimentacoes ao "conectar" — respostas vazias bastam.
  vi.stubGlobal(
    'fetch',
    vi.fn((url: string) =>
      Promise.resolve({
        ok: true,
        json: async () => (url.startsWith('/api/movimentacoes') ? { movimentacoes: [] } : { estoques: [] }),
      }),
    ),
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe('EstoquesPage — gate de papel', () => {
  it.each(['almoxarife', 'gestor', 'adm'])('papel %s vê as abas "Locais" e "Movimentações"', async (papel) => {
    authState.papel = papel;
    render(<EstoquesPage />);

    expect(await screen.findByRole('heading', { name: 'Locais' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Adicionar estoque' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Locais' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Movimentações' })).toBeInTheDocument();
    expect(
      screen.queryByText('Você não tem acesso à área de Estoques.'),
    ).not.toBeInTheDocument();
  });

  it('papel usuario vê a mensagem de acesso restrito e nenhuma aba', () => {
    authState.papel = 'usuario';
    render(<EstoquesPage />);

    expect(screen.getByText('Você não tem acesso à área de Estoques.')).toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: 'Locais' })).not.toBeInTheDocument();
    expect(screen.queryByRole('tab', { name: 'Movimentações' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Adicionar estoque' })).not.toBeInTheDocument();
    expect(fetch).not.toHaveBeenCalled();
  });

  it('clicar na aba "Movimentações" monta a MovimentacoesSection', async () => {
    const user = userEvent.setup();
    render(<EstoquesPage />);

    await user.click(await screen.findByRole('tab', { name: 'Movimentações' }));

    // A seção só carrega a partir de aoMudarStatus('conectado').
    act(() => {
      aoMudarStatus('conectado');
    });

    expect(await screen.findByRole('heading', { name: 'Movimentações' })).toBeInTheDocument();
    expect(
      await screen.findByText('Nenhuma movimentação registrada.'),
    ).toBeInTheDocument();
    expect(fetch).toHaveBeenCalledWith('/api/movimentacoes', expect.anything());
  });
});

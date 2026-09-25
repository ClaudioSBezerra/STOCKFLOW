import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { act, render, screen } from '@testing-library/react';
import { EstoquesPage } from './EstoquesPage';
import { LancarSaldoPage } from './LancarSaldoPage';
import { MovimentacoesPage } from './MovimentacoesPage';
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

describe('EstoquesPage — Locais (/estoques)', () => {
  it.each(['almoxarife', 'gestor', 'adm'])('papel %s vê a tela Locais direto, sem abas', async (papel) => {
    authState.papel = papel;
    render(<EstoquesPage />);

    expect(await screen.findByRole('heading', { name: 'Locais' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Adicionar estoque' })).toBeInTheDocument();
    expect(screen.queryByRole('tab')).not.toBeInTheDocument();
    expect(
      screen.queryByText('Você não tem acesso à área de Estoques.'),
    ).not.toBeInTheDocument();
  });

  it('papel usuario vê a mensagem de acesso restrito', () => {
    authState.papel = 'usuario';
    render(<EstoquesPage />);

    expect(screen.getByText('Você não tem acesso à área de Estoques.')).toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: 'Locais' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Adicionar estoque' })).not.toBeInTheDocument();
    expect(fetch).not.toHaveBeenCalled();
  });
});

describe('LancarSaldoPage (/estoques/lancar-saldo)', () => {
  it('almoxarife+ cai direto na LancamentoSaldoSection', async () => {
    render(<LancarSaldoPage />);

    expect(await screen.findByRole('heading', { name: 'Lançar saldo' })).toBeInTheDocument();
    expect(screen.getByLabelText('Quantidade')).toBeInTheDocument();
    expect(screen.queryByRole('tab')).not.toBeInTheDocument();
  });

  it('papel usuario vê a mensagem de acesso restrito', () => {
    authState.papel = 'usuario';
    render(<LancarSaldoPage />);

    expect(screen.getByText('Você não tem acesso à área de Estoques.')).toBeInTheDocument();
    expect(screen.queryByLabelText('Quantidade')).not.toBeInTheDocument();
    expect(fetch).not.toHaveBeenCalled();
  });
});

describe('MovimentacoesPage (/estoques/movimentacoes)', () => {
  it('almoxarife+ cai direto na MovimentacoesSection, com h1', async () => {
    render(<MovimentacoesPage />);

    // A seção só carrega a partir de aoMudarStatus('conectado').
    act(() => {
      aoMudarStatus('conectado');
    });

    expect(await screen.findByRole('heading', { level: 1, name: 'Movimentações' })).toBeInTheDocument();
    expect(await screen.findByText('Nenhuma movimentação registrada.')).toBeInTheDocument();
    expect(fetch).toHaveBeenCalledWith('/api/movimentacoes', expect.anything());
    expect(screen.queryByRole('tab')).not.toBeInTheDocument();
  });

  it('papel usuario vê a mensagem de acesso restrito e nenhuma chamada', () => {
    authState.papel = 'usuario';
    render(<MovimentacoesPage />);

    expect(screen.getByText('Você não tem acesso à área de Estoques.')).toBeInTheDocument();
    expect(conectarRealtimeMock).not.toHaveBeenCalled();
    expect(fetch).not.toHaveBeenCalled();
  });
});

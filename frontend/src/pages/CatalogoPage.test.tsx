import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { CatalogoPage } from './CatalogoPage';

// useAuth() fornece o papel — configurável por teste.
const authState = vi.hoisted(() => ({ papel: 'usuario' as string }));

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
  authState.papel = 'usuario';
  // CadastroProdutoSection busca categorias/estoques no mount (quando
  // montada) — resposta vazia basta para os testes desta página.
  vi.stubGlobal(
    'fetch',
    vi.fn((url: string) => {
      if (url === '/api/categorias') return Promise.resolve({ ok: true, json: async () => ({ categorias: [] }) });
      if (url === '/api/estoques') return Promise.resolve({ ok: true, json: async () => ({ estoques: [] }) });
      throw new Error(`URL inesperada: ${url}`);
    }),
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe('CatalogoPage — gate de papel', () => {
  it('papel usuario vê só o aviso de "busca em breve", sem o formulário de cadastro', () => {
    authState.papel = 'usuario';
    render(<CatalogoPage />);

    expect(
      screen.getByText('Busca e visualização do catálogo chegam em breve.'),
    ).toBeInTheDocument();
    expect(screen.queryByText('Cadastrar Produto')).not.toBeInTheDocument();
    expect(fetch).not.toHaveBeenCalled();
  });

  it.each(['almoxarife', 'gestor', 'adm'])(
    'papel %s vê o aviso E a seção de cadastro',
    async (papel) => {
      authState.papel = papel;
      render(<CatalogoPage />);

      expect(
        screen.getByText('Busca e visualização do catálogo chegam em breve.'),
      ).toBeInTheDocument();
      expect(await screen.findByText('Cadastrar Produto')).toBeInTheDocument();
    },
  );
});

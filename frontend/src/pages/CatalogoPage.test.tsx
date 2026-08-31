import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
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
  // ImportacaoProdutosSection só monta quando a aba "Importação" é
  // selecionada (Radix Tabs desmonta o conteúdo inativo por padrão) — busca
  // GET /api/importacoes/ultima no próprio mount.
  vi.stubGlobal(
    'fetch',
    vi.fn((url: string) => {
      if (url === '/api/categorias') return Promise.resolve({ ok: true, json: async () => ({ categorias: [] }) });
      if (url === '/api/estoques') return Promise.resolve({ ok: true, json: async () => ({ estoques: [] }) });
      if (url === '/api/importacoes/ultima')
        return Promise.resolve({ ok: true, json: async () => ({ importacao: null }) });
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

describe('CatalogoPage — abas Cadastro/Importação (Story 3.3)', () => {
  it('almoxarife+ vê as abas "Cadastro"/"Importação", com Cadastro ativa por padrão', async () => {
    authState.papel = 'almoxarife';
    render(<CatalogoPage />);

    expect(await screen.findByText('Cadastrar Produto')).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Cadastro' })).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByRole('tab', { name: 'Importação' })).toHaveAttribute('aria-selected', 'false');
    // Radix Tabs desmonta o conteúdo da aba inativa por padrão — a seção de
    // Importação não deveria estar no DOM antes de a aba ser selecionada.
    expect(screen.queryByText('Importar Produtos')).not.toBeInTheDocument();
  });

  it('selecionar a aba "Importação" monta ImportacaoProdutosSection', async () => {
    authState.papel = 'almoxarife';
    const user = userEvent.setup();
    render(<CatalogoPage />);

    await screen.findByText('Cadastrar Produto');
    await user.click(screen.getByRole('tab', { name: 'Importação' }));

    expect(await screen.findByText('Importar Produtos')).toBeInTheDocument();
  });
});

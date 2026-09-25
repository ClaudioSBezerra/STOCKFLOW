import { describe, expect, it, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { CadastrarProdutoPage } from './CadastrarProdutoPage';
import { ImportarProdutosPage } from './ImportarProdutosPage';

// Rotas próprias de /produtos/novo e /produtos/importar (Story 17.1): as
// seções são mockadas — esta suíte prova só o WIRING (rota direta, h1, gate).
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

vi.mock('@/components/produtos/CadastroProdutoSection', () => ({
  CadastroProdutoSection: () => <div data-testid="secao-cadastro">Cadastro (mock)</div>,
}));
vi.mock('@/components/produtos/ImportacaoProdutosSection', () => ({
  ImportacaoProdutosSection: () => <div data-testid="secao-importacao">Importação (mock)</div>,
}));

beforeEach(() => {
  authState.papel = 'almoxarife';
});

describe('CadastrarProdutoPage (/produtos/novo)', () => {
  it.each(['almoxarife', 'gestor', 'adm'])('papel %s cai direto no cadastro, com h1 e sem abas', (papel) => {
    authState.papel = papel;
    render(<CadastrarProdutoPage />);

    expect(screen.getByRole('heading', { level: 1, name: 'Cadastrar produto' })).toBeInTheDocument();
    expect(screen.getByTestId('secao-cadastro')).toBeInTheDocument();
    expect(screen.queryByRole('tab')).not.toBeInTheDocument();
  });

  it('papel usuario vê a recusa e a seção não é montada', () => {
    authState.papel = 'usuario';
    render(<CadastrarProdutoPage />);

    expect(screen.getByText('Você não tem acesso ao cadastro de produtos.')).toBeInTheDocument();
    expect(screen.queryByTestId('secao-cadastro')).not.toBeInTheDocument();
  });
});

describe('ImportarProdutosPage (/produtos/importar)', () => {
  it.each(['almoxarife', 'gestor', 'adm'])('papel %s cai direto na importação, com h1 e sem abas', (papel) => {
    authState.papel = papel;
    render(<ImportarProdutosPage />);

    expect(screen.getByRole('heading', { level: 1, name: 'Importar planilha' })).toBeInTheDocument();
    expect(screen.getByTestId('secao-importacao')).toBeInTheDocument();
    expect(screen.queryByRole('tab')).not.toBeInTheDocument();
  });

  it('papel usuario vê a recusa e a seção não é montada', () => {
    authState.papel = 'usuario';
    render(<ImportarProdutosPage />);

    expect(screen.getByText('Você não tem acesso à importação de produtos.')).toBeInTheDocument();
    expect(screen.queryByTestId('secao-importacao')).not.toBeInTheDocument();
  });
});

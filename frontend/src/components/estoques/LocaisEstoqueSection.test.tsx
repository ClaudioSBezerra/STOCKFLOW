import { describe, expect, it, vi, afterEach } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { LocaisEstoqueSection } from './LocaisEstoqueSection';

const authState = vi.hoisted(() => ({ papel: 'almoxarife' as string }));
vi.mock('@/lib/auth', () => ({
  useAuth: () => ({
    estado: 'autenticado',
    usuario: {
      id: '1',
      nome: 'Ator',
      email: 'a@e.com',
      papel: authState.papel,
    },
  }),
}));
vi.mock('@/lib/session', () => ({ getAccessToken: () => 'token-de-teste' }));

const FILIAIS = [
  { id: 'f-1', nome: 'Matriz' },
  { id: 'f-2', nome: 'Recife' },
];

function stub(
  estoques: unknown[],
  filiais: unknown[] = FILIAIS,
  estoquesOk = true,
  filiaisOk = true,
) {
  const fn = vi.fn((url: string) => {
    const ok = (body: unknown) =>
      Promise.resolve({ ok: true, status: 200, json: async () => body });
    if (url === '/api/filiais') {
      return filiaisOk
        ? ok({ filiais })
        : Promise.resolve({ ok: false, status: 500, json: async () => ({}) });
    }
    if (url === '/api/estoques/indicadores')
      return ok({ locais: estoques.length, itensEmEstoque: 7 });
    if (url === '/api/estoques') {
      return estoquesOk
        ? ok({ estoques })
        : Promise.resolve({ ok: false, status: 500, json: async () => ({}) });
    }
    throw new Error(`URL inesperada: ${url}`);
  });
  vi.stubGlobal('fetch', fn);
  return fn;
}

function montar() {
  return render(
    <MemoryRouter>
      <LocaisEstoqueSection />
    </MemoryRouter>,
  );
}

afterEach(() => {
  authState.papel = 'almoxarife';
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe('LocaisEstoqueSection (Depósitos, consulta)', () => {
  it('agrupa por Filial, com "Sem filial" para depósito legado, sem formulário nem Excluir', async () => {
    stub([
      {
        id: 'e-1',
        nome: 'Almoxarifado Central',
        filial_id: 'f-1',
        totalItens: 3,
      },
      { id: 'e-2', nome: 'Canteiro A', filial_id: 'f-2', totalItens: 0 },
      { id: 'e-3', nome: 'Antigo', filial_id: null, totalItens: 1 },
    ]);
    montar();

    expect(await screen.findByRole('heading', { name: 'Depósitos' })).toBeInTheDocument();
    const matriz = await screen.findByRole('region', { name: 'Filial Matriz' });
    expect(within(matriz).getByText('Almoxarifado Central')).toBeInTheDocument();
    expect(
      within(screen.getByRole('region', { name: 'Filial Recife' })).getByText('Canteiro A'),
    ).toBeInTheDocument();
    expect(
      within(screen.getByRole('region', { name: 'Filial Sem filial' })).getByText('Antigo'),
    ).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Excluir/ })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Adicionar/ })).not.toBeInTheDocument();
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument();
    expect(await screen.findByText('Itens em estoque')).toBeInTheDocument();
  });

  it('almoxarife vê a orientação para pedir a um gestor, sem link', async () => {
    stub([]);
    montar();
    expect(
      await screen.findByText(
        'Peça a um gestor ou administrador para cadastrar ou excluir depósitos.',
      ),
    ).toBeInTheDocument();
    expect(screen.queryByRole('link')).not.toBeInTheDocument();
  });

  it.each(['gestor', 'adm'])('%s vê o link para Cadastros > Filiais', async (papel) => {
    authState.papel = papel;
    stub([]);
    montar();
    const link = await screen.findByRole('link', {
      name: 'Cadastros > Filiais',
    });
    expect(link).toHaveAttribute('href', '/cadastros/filiais');
  });

  it('sem Filial: explica que primeiro se cadastra uma Filial', async () => {
    stub([], []);
    montar();
    expect(await screen.findByText(/Primeiro cadastre uma filial/)).toBeInTheDocument();
  });

  it('Filial sem depósitos: "Nenhum depósito cadastrado ainda."', async () => {
    stub([]);
    montar();
    expect(await screen.findByText('Nenhum depósito cadastrado ainda.')).toBeInTheDocument();
  });

  it('GET !ok mostra role="alert" genérico', async () => {
    stub([], FILIAIS, false);
    montar();
    expect(
      await screen.findByText(
        'Não foi possível carregar a lista de depósitos. Recarregue a página.',
      ),
    ).toBeInTheDocument();
  });

  it('GET /api/filiais falha: lista plana com aviso, sem "Sem filial" nem "Nenhum depósito"', async () => {
    stub([{ id: 'e-1', nome: 'Central', filial_id: 'f-1', totalItens: 2 }], FILIAIS, true, false);
    montar();
    expect(await screen.findByText('Central')).toBeInTheDocument();
    expect(
      await screen.findByText('Não foi possível carregar as filiais. Recarregue a página.'),
    ).toBeInTheDocument();
    expect(screen.queryByRole('region')).not.toBeInTheDocument();
    expect(screen.queryByText('Nenhum depósito cadastrado ainda.')).not.toBeInTheDocument();
  });

  it('"Nenhum depósito cadastrado ainda." só depois de depósitos E filiais carregarem', async () => {
    let liberarFiliais: () => void = () => {};
    vi.stubGlobal(
      'fetch',
      vi.fn((url: string) => {
        const ok = (body: unknown) =>
          Promise.resolve({ ok: true, status: 200, json: async () => body });
        if (url === '/api/filiais') {
          return new Promise((r) => {
            liberarFiliais = () =>
              r({ ok: true, status: 200, json: async () => ({ filiais: FILIAIS }) });
          });
        }
        if (url === '/api/estoques/indicadores') return ok({ locais: 0, itensEmEstoque: 0 });
        return ok({ estoques: [] });
      }),
    );
    montar();
    await screen.findByRole('heading', { name: 'Depósitos' });
    await new Promise((r) => setTimeout(r, 50));
    expect(screen.queryByText('Nenhum depósito cadastrado ainda.')).not.toBeInTheDocument();
    liberarFiliais();
    expect(await screen.findByText('Nenhum depósito cadastrado ainda.')).toBeInTheDocument();
  });
});

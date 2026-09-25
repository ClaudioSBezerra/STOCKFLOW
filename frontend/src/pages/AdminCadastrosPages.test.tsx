import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import type { ComponentType } from 'react';
import { CategoriasPage } from './cadastros/CategoriasPage';
import { TemplatesPage } from './cadastros/TemplatesPage';
import { FiliaisPage } from './cadastros/FiliaisPage';
import { CentrosCustoPage } from './cadastros/CentrosCustoPage';
import { UsuariosPage } from './admin/UsuariosPage';
import { ConvitesPage } from './admin/ConvitesPage';
import { PromocoesPage } from './admin/PromocoesPage';
import { SegurancaEmpresaPage } from './admin/SegurancaEmpresaPage';
import { LogAcessoPage } from './admin/LogAcessoPage';
import { LgpdPage } from './admin/LgpdPage';
import { ConfiguracoesPage } from './ConfiguracoesPage';

const authState = vi.hoisted(() => ({ papel: 'usuario' as string }));

vi.mock('@/lib/auth', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/auth')>()),
  useAuth: () => ({
    estado: 'autenticado',
    usuario: {
      id: '1',
      nome: 'Ana',
      email: 'ana@empresa.com',
      papel: authState.papel,
      mfaHabilitado: true,
      origem: 'senha',
      empresa: { mfaObrigatorio: false },
    },
    definirSessao: vi.fn(),
    atualizarUsuario: vi.fn(),
    logout: vi.fn(),
  }),
}));

const paginas: Array<{ tela: string; Page: ComponentType; minimo: string; url: string }> = [
  { tela: 'Categorias', Page: CategoriasPage, minimo: 'adm', url: '/api/categorias' },
  { tela: 'Templates de nome', Page: TemplatesPage, minimo: 'adm', url: '/api/nomenclatura-templates' },
  { tela: 'Filiais', Page: FiliaisPage, minimo: 'adm', url: '/api/filiais' },
  { tela: 'Centros de custo', Page: CentrosCustoPage, minimo: 'adm', url: '/api/centros-custo' },
  { tela: 'Usuários', Page: UsuariosPage, minimo: 'gestor', url: '/api/usuarios' },
  { tela: 'Convites', Page: ConvitesPage, minimo: 'gestor', url: '/api/convites' },
  { tela: 'Promoções', Page: PromocoesPage, minimo: 'gestor', url: '/api/promocoes' },
  { tela: 'Segurança da empresa', Page: SegurancaEmpresaPage, minimo: 'adm', url: '/api/seguranca/mfa-empresa' },
  { tela: 'Log de acesso', Page: LogAcessoPage, minimo: 'adm', url: '/api/logs-acesso' },
  { tela: 'Solicitações LGPD', Page: LgpdPage, minimo: 'adm', url: '/api/solicitacoes-exclusao' },
];

let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  fetchMock = vi.fn(() => Promise.resolve({ ok: true, status: 200, json: async () => ({}) }));
  vi.stubGlobal('fetch', fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('páginas de Cadastros e Administração (Story 17.2)', () => {
  it.each(paginas)('$tela: com o papel mínimo mostra o h1 e carrega a seção', async ({ tela, Page, minimo, url }) => {
    authState.papel = minimo;
    render(<Page />);
    expect(screen.getByRole('heading', { level: 1, name: tela })).toBeInTheDocument();
    await vi.waitFor(() =>
      expect(fetchMock.mock.calls.some(([u]) => String(u).startsWith(url))).toBe(true),
    );
  });

  it.each(paginas)('$tela: abaixo do papel mínimo mostra a recusa e não chama a API', ({ tela, Page, minimo }) => {
    authState.papel = minimo === 'adm' ? 'gestor' : 'almoxarife';
    render(<Page />);
    expect(screen.getByText(`Você não tem acesso a ${tela}.`)).toBeInTheDocument();
    expect(screen.queryByRole('heading', { level: 1 })).not.toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalled();
  });
});

describe('ConfiguracoesPage sem seções administrativas (Story 17.2)', () => {
  it('adm vê só conta, Segurança, solicitar promoção e Privacidade', async () => {
    authState.papel = 'adm';
    render(<ConfiguracoesPage />);
    expect(await screen.findByRole('heading', { name: 'Segurança' })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Privacidade' })).toBeInTheDocument();
    for (const nome of [
      'Decidir promoções',
      'Gestão de Usuários',
      'Convites',
      'Log de Acesso',
      'Filiais',
      'Categorias',
    ]) {
      expect(screen.queryByRole('heading', { name: nome })).not.toBeInTheDocument();
    }
    const urls = fetchMock.mock.calls.map(([u]) => String(u));
    expect(urls.every((u) => u === '/api/promocoes/minha')).toBe(true);
  });
});

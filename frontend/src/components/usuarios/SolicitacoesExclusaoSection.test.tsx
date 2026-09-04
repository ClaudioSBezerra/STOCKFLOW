import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { SolicitacoesExclusaoSection } from './SolicitacoesExclusaoSection';

const authState = vi.hoisted(() => ({ papel: 'adm' as string }));
vi.mock('@/lib/auth', () => ({
  useAuth: () => ({
    estado: 'autenticado',
    usuario: { id: 'adm-1', nome: 'Adm', email: 'adm@empresa.com', papel: authState.papel },
    definirSessao: vi.fn(),
    logout: vi.fn(),
  }),
}));

const listarMock = vi.hoisted(() => vi.fn());
const processarMock = vi.hoisted(() => vi.fn());
vi.mock('@/lib/privacidade', async () => {
  const actual = await vi.importActual<typeof import('@/lib/privacidade')>('@/lib/privacidade');
  return {
    ...actual,
    listarSolicitacoesExclusao: listarMock,
    processarExclusaoConta: processarMock,
  };
});

const SOLICITACOES = [
  {
    id: 's-1',
    nome: 'Ana Usuária',
    email: 'ana@empresa.com',
    papel: 'usuario',
    criadoEm: '2026-09-04T10:00:00Z',
  },
  {
    id: 's-2',
    nome: 'Bruno Gestor',
    email: 'bruno@empresa.com',
    papel: 'gestor',
    criadoEm: '2026-09-04T11:00:00Z',
  },
];

beforeEach(() => {
  authState.papel = 'adm';
  listarMock.mockReset();
  processarMock.mockReset();
});

afterEach(() => {
  vi.clearAllMocks();
});

describe('SolicitacoesExclusaoSection', () => {
  it('não renderiza nada para papel abaixo de adm e nunca busca a lista', () => {
    authState.papel = 'gestor';
    listarMock.mockResolvedValue([]);

    const { container } = render(<SolicitacoesExclusaoSection />);

    expect(container).toBeEmptyDOMElement();
    expect(listarMock).not.toHaveBeenCalled();
  });

  it('para adm: carrega e lista as solicitações pendentes', async () => {
    listarMock.mockResolvedValue(SOLICITACOES);

    render(<SolicitacoesExclusaoSection />);

    expect(
      await screen.findByRole('heading', { name: 'Solicitações de exclusão' }),
    ).toBeInTheDocument();
    expect(await screen.findByText('Ana Usuária')).toBeInTheDocument();
    expect(screen.getByText('bruno@empresa.com')).toBeInTheDocument();
    expect(listarMock).toHaveBeenCalledTimes(1);
  });

  it('confirmar no ConfirmDialog dispara processarExclusaoConta com o id da linha e recarrega a lista', async () => {
    listarMock.mockResolvedValue(SOLICITACOES);
    processarMock.mockResolvedValue(undefined);
    const user = userEvent.setup();

    render(<SolicitacoesExclusaoSection />);

    const linha = (await screen.findByText('Ana Usuária')).closest('li') as HTMLElement;
    await user.click(
      within(linha).getByRole('button', { name: /Processar exclusão da conta de Ana Usuária/ }),
    );

    await user.click(await screen.findByRole('button', { name: 'Anonimizar' }));

    await waitFor(() => expect(processarMock).toHaveBeenCalledWith('s-1'));
    // mount + recarga após a ação.
    await waitFor(() => expect(listarMock).toHaveBeenCalledTimes(2));
  });

  it('409 do guard do último administrador mostra alerta inline com a mensagem do servidor e recarrega a lista', async () => {
    listarMock.mockResolvedValue(SOLICITACOES);
    processarMock.mockRejectedValue(
      new Error('ao menos um administrador ativo deve sempre existir'),
    );
    const user = userEvent.setup();

    render(<SolicitacoesExclusaoSection />);

    const linha = (await screen.findByText('Ana Usuária')).closest('li') as HTMLElement;
    await user.click(
      within(linha).getByRole('button', { name: /Processar exclusão da conta de Ana Usuária/ }),
    );
    await user.click(await screen.findByRole('button', { name: 'Anonimizar' }));

    expect(
      await screen.findByText('ao menos um administrador ativo deve sempre existir'),
    ).toHaveAttribute('role', 'alert');
    await waitFor(() => expect(listarMock).toHaveBeenCalledTimes(2));
  });

  it('falha ao carregar a lista mostra alerta inline', async () => {
    listarMock.mockRejectedValue(new Error('papel insuficiente'));

    render(<SolicitacoesExclusaoSection />);

    expect(await screen.findByText('papel insuficiente')).toHaveAttribute('role', 'alert');
  });
});

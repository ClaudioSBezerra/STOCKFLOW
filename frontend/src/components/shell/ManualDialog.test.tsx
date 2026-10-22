import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { ManualDialog } from './ManualDialog';

const authState = vi.hoisted(() => ({ papel: 'usuario' as string }));
vi.mock('@/lib/auth', () => ({
  useAuth: () => ({
    estado: 'autenticado',
    usuario: { id: '1', nome: 'Teste', email: 'teste@empresa.com', papel: authState.papel },
  }),
}));

const toastError = vi.hoisted(() => vi.fn());
vi.mock('sonner', () => ({ toast: { error: toastError } }));

const buscarManualMock = vi.hoisted(() => vi.fn());
const buscarManualPDFBlobMock = vi.hoisted(() => vi.fn());
vi.mock('@/lib/manual', () => ({
  buscarManual: buscarManualMock,
  buscarManualPDFBlob: buscarManualPDFBlobMock,
}));

const SECOES = [
  { id: 'visao-geral', titulo: 'Visão geral', papelMinimo: 'usuario', paragrafos: ['Parágrafo um.'] },
  { id: 'produtos', titulo: 'Catálogo — Produtos', papelMinimo: 'usuario', paragrafos: ['Parágrafo dois.'] },
];

beforeEach(() => {
  authState.papel = 'usuario';
  buscarManualMock.mockReset();
  buscarManualPDFBlobMock.mockReset();
  toastError.mockClear();
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('ManualDialog', () => {
  it('abre o diálogo e busca GET /api/manual só na primeira abertura', async () => {
    const user = userEvent.setup();
    buscarManualMock.mockResolvedValue(SECOES);
    render(<ManualDialog />);

    await user.click(screen.getByRole('button', { name: 'Ajuda' }));

    expect(await screen.findByText('Visão geral')).toBeInTheDocument();
    expect(screen.getByText('Parágrafo um.')).toBeInTheDocument();
    expect(screen.getByText('Catálogo — Produtos')).toBeInTheDocument();
    expect(buscarManualMock).toHaveBeenCalledTimes(1);

    await user.keyboard('{Escape}');
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());

    // Reabrir não refaz a busca — as seções já estão em cache local.
    await user.click(screen.getByRole('button', { name: 'Ajuda' }));
    expect(await screen.findByText('Visão geral')).toBeInTheDocument();
    expect(buscarManualMock).toHaveBeenCalledTimes(1);
  });

  it('mostra "Carregando manual..." enquanto a busca está em andamento', async () => {
    const user = userEvent.setup();
    let resolver: (v: typeof SECOES) => void = () => {};
    buscarManualMock.mockReturnValue(new Promise((r) => (resolver = r)));
    render(<ManualDialog />);

    await user.click(screen.getByRole('button', { name: 'Ajuda' }));
    expect(await screen.findByText('Carregando manual...')).toBeInTheDocument();

    resolver(SECOES);
    expect(await screen.findByText('Visão geral')).toBeInTheDocument();
  });

  it('mostra erro inline quando a busca falha', async () => {
    const user = userEvent.setup();
    buscarManualMock.mockRejectedValue(new Error('falhou'));
    render(<ManualDialog />);

    await user.click(screen.getByRole('button', { name: 'Ajuda' }));

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível carregar o manual agora. Tente novamente em instantes.',
    );
  });

  it('não mostra "Baixar manual em PDF" para papel abaixo de adm', async () => {
    const user = userEvent.setup();
    authState.papel = 'gestor';
    buscarManualMock.mockResolvedValue(SECOES);
    render(<ManualDialog />);

    await user.click(screen.getByRole('button', { name: 'Ajuda' }));
    await screen.findByText('Visão geral');

    expect(screen.queryByRole('button', { name: /Baixar manual em PDF/ })).not.toBeInTheDocument();
    expect(screen.getByText('Seções disponíveis para o seu papel de acesso.')).toBeInTheDocument();
  });

  it('adm vê "Baixar manual em PDF" e o texto de manual completo', async () => {
    const user = userEvent.setup();
    authState.papel = 'adm';
    buscarManualMock.mockResolvedValue(SECOES);
    render(<ManualDialog />);

    await user.click(screen.getByRole('button', { name: 'Ajuda' }));
    await screen.findByText('Visão geral');

    expect(screen.getByRole('button', { name: 'Baixar manual em PDF' })).toBeInTheDocument();
    expect(
      screen.getByText('Manual completo — como administrador, você vê todas as seções.'),
    ).toBeInTheDocument();
  });

  it('clicar em "Baixar manual em PDF" baixa o blob e cria/clica/remove o <a download>', async () => {
    const user = userEvent.setup();
    authState.papel = 'adm';
    buscarManualMock.mockResolvedValue(SECOES);
    const blob = new Blob(['%PDF-fake'], { type: 'application/pdf' });
    buscarManualPDFBlobMock.mockResolvedValue(blob);
    vi.stubGlobal('URL', {
      ...URL,
      createObjectURL: vi.fn(() => 'blob:manual-teste'),
      revokeObjectURL: vi.fn(),
    });
    render(<ManualDialog />);

    await user.click(screen.getByRole('button', { name: 'Ajuda' }));
    await screen.findByText('Visão geral');
    await user.click(screen.getByRole('button', { name: 'Baixar manual em PDF' }));

    await waitFor(() => expect(buscarManualPDFBlobMock).toHaveBeenCalledTimes(1));
    expect(URL.createObjectURL).toHaveBeenCalledWith(blob);
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:manual-teste');
  });

  it('falha ao baixar o PDF mostra toast.error com a mensagem', async () => {
    const user = userEvent.setup();
    authState.papel = 'adm';
    buscarManualMock.mockResolvedValue(SECOES);
    buscarManualPDFBlobMock.mockRejectedValue(new Error('sem permissão'));
    render(<ManualDialog />);

    await user.click(screen.getByRole('button', { name: 'Ajuda' }));
    await screen.findByText('Visão geral');
    await user.click(screen.getByRole('button', { name: 'Baixar manual em PDF' }));

    await waitFor(() => expect(toastError).toHaveBeenCalledWith('sem permissão'));
  });
});

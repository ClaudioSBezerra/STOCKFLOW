import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { PrivacidadeSection } from './PrivacidadeSection';

const toastError = vi.hoisted(() => vi.fn());
vi.mock('sonner', () => ({ toast: { error: toastError } }));

const baixarMeusDadosBlobMock = vi.hoisted(() => vi.fn());
vi.mock('@/lib/privacidade', async () => {
  const actual = await vi.importActual<typeof import('@/lib/privacidade')>('@/lib/privacidade');
  return {
    ...actual,
    baixarMeusDadosBlob: baixarMeusDadosBlobMock,
  };
});

// jsdom não implementa URL.createObjectURL/revokeObjectURL (mesmo padrão de
// MeusPedidosSection.test.tsx / CatalogoListagem.test.tsx).
beforeEach(() => {
  URL.createObjectURL = vi.fn(() => 'blob:meus-dados-teste');
  URL.revokeObjectURL = vi.fn();
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe('PrivacidadeSection', () => {
  it('renderiza o título e o botão "Baixar meus dados"', () => {
    render(<PrivacidadeSection />);

    expect(screen.getByRole('heading', { name: 'Privacidade' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Baixar meus dados' })).toBeInTheDocument();
  });

  it('clicar em "Baixar meus dados" baixa o JSON via baixarMeusDadosBlob e cria/clica/remove o <a download>', async () => {
    const blob = new Blob(['{"nome":"Ana"}'], { type: 'application/json' });
    baixarMeusDadosBlobMock.mockResolvedValue(blob);
    const cliqueSpy = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {});

    render(<PrivacidadeSection />);

    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: 'Baixar meus dados' }));

    await waitFor(() => expect(baixarMeusDadosBlobMock).toHaveBeenCalled());
    await waitFor(() => expect(cliqueSpy).toHaveBeenCalled());
    expect(URL.createObjectURL).toHaveBeenCalledWith(blob);
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:meus-dados-teste');
    expect(toastError).not.toHaveBeenCalled();

    cliqueSpy.mockRestore();
  });

  it('falha de baixarMeusDadosBlob mostra toast.error com a mensagem do servidor, sem baixar nada', async () => {
    baixarMeusDadosBlobMock.mockRejectedValue(new Error('falha ao exportar dados pessoais'));
    const cliqueSpy = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {});

    render(<PrivacidadeSection />);

    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: 'Baixar meus dados' }));

    await waitFor(() => expect(toastError).toHaveBeenCalledWith('falha ao exportar dados pessoais'));
    expect(cliqueSpy).not.toHaveBeenCalled();

    cliqueSpy.mockRestore();
  });

  it('cai na mensagem genérica quando a falha não é um Error', async () => {
    baixarMeusDadosBlobMock.mockRejectedValue('falha crua, sem Error');

    render(<PrivacidadeSection />);

    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: 'Baixar meus dados' }));

    await waitFor(() =>
      expect(toastError).toHaveBeenCalledWith(
        'Não foi possível baixar seus dados agora. Tente novamente em instantes.',
      ),
    );
  });
});

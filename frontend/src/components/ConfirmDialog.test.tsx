import { useState } from 'react';
import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { ConfirmDialog } from './ConfirmDialog';

function Wrapper({
  onConfirm,
  onCancel,
}: {
  onConfirm: () => void;
  onCancel?: () => void;
}) {
  const [open, setOpen] = useState(true);
  return (
    <ConfirmDialog
      open={open}
      onOpenChange={setOpen}
      onConfirm={onConfirm}
      onCancel={onCancel}
      title="Excluir item"
      description="Essa ação não pode ser desfeita."
    />
  );
}

describe('ConfirmDialog', () => {
  it('chama onConfirm uma única vez e fecha ao confirmar', async () => {
    const user = userEvent.setup();
    const onConfirm = vi.fn();
    render(<Wrapper onConfirm={onConfirm} />);

    expect(await screen.findByRole('alertdialog')).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Confirmar' }));

    expect(onConfirm).toHaveBeenCalledTimes(1);
    await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument());
  });

  it('nunca chama onCancel quando o usuário confirma', async () => {
    const user = userEvent.setup();
    const onConfirm = vi.fn();
    const onCancel = vi.fn();
    render(<Wrapper onConfirm={onConfirm} onCancel={onCancel} />);

    expect(await screen.findByRole('alertdialog')).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Confirmar' }));

    expect(onConfirm).toHaveBeenCalledTimes(1);
    expect(onCancel).not.toHaveBeenCalled();
    await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument());
  });

  it('nunca chama onConfirm e fecha ao cancelar', async () => {
    const user = userEvent.setup();
    const onConfirm = vi.fn();
    const onCancel = vi.fn();
    render(<Wrapper onConfirm={onConfirm} onCancel={onCancel} />);

    await screen.findByRole('alertdialog');
    await user.click(screen.getByRole('button', { name: 'Cancelar' }));

    expect(onConfirm).not.toHaveBeenCalled();
    expect(onCancel).toHaveBeenCalledTimes(1);
    await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument());
  });

  it('nunca chama onConfirm mais de uma vez em duplo clique rápido antes do fechamento', async () => {
    const onConfirm = vi.fn();
    render(<Wrapper onConfirm={onConfirm} />);
    const confirmButton = await screen.findByRole('button', { name: 'Confirmar' });

    fireEvent.click(confirmButton);
    fireEvent.click(confirmButton);

    expect(onConfirm).toHaveBeenCalledTimes(1);
    await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument());
  });

  it('nunca chama onCancel mais de uma vez em duplo clique rápido em Cancelar antes do fechamento', async () => {
    const onConfirm = vi.fn();
    const onCancel = vi.fn();
    render(<Wrapper onConfirm={onConfirm} onCancel={onCancel} />);
    const cancelButton = await screen.findByRole('button', { name: 'Cancelar' });

    fireEvent.click(cancelButton);
    fireEvent.click(cancelButton);

    expect(onConfirm).not.toHaveBeenCalled();
    expect(onCancel).toHaveBeenCalledTimes(1);
    await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument());
  });

  it('reseta o estado de confirmação quando onConfirm lança, permitindo onCancel num cancelamento real subsequente', async () => {
    const onCancel = vi.fn();
    const thrown = new Error('falha ao confirmar');
    const onConfirm = vi.fn(() => {
      throw thrown;
    });
    render(<Wrapper onConfirm={onConfirm} onCancel={onCancel} />);
    const confirmButton = await screen.findByRole('button', { name: 'Confirmar' });

    // React (dev mode) rethrows o erro do handler de forma assíncrona via um
    // evento `error` sintético no `window`, em vez de deixá-lo propagar de
    // volta pela chamada síncrona a `fireEvent.click`. Capturamos e
    // suprimimos esse evento para verificar que `onConfirm` de fato lançou,
    // sem deixar a exceção "não tratada" derrubar o teste.
    let caught: unknown;
    const captureError = (event: ErrorEvent) => {
      caught = event.error;
      event.preventDefault();
    };
    window.addEventListener('error', captureError);
    fireEvent.click(confirmButton);
    window.removeEventListener('error', captureError);

    expect(caught).toBe(thrown);
    expect(onConfirm).toHaveBeenCalledTimes(1);
    expect(onCancel).not.toHaveBeenCalled();
    expect(await screen.findByRole('alertdialog')).toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: 'Cancelar' }));
    expect(onCancel).toHaveBeenCalledTimes(1);
  });

  it('aplica o alvo de toque mínimo (48px) aos botões de confirmar/cancelar', async () => {
    render(<Wrapper onConfirm={vi.fn()} />);
    const confirmButton = await screen.findByRole('button', { name: 'Confirmar' });
    const cancelButton = screen.getByRole('button', { name: 'Cancelar' });

    for (const button of [confirmButton, cancelButton]) {
      expect(button.className).toContain('min-h-touch-target-min');
      expect(button.className).toContain('min-w-touch-target-min');
    }
  });

  it('exibe título e descrição informados', async () => {
    render(<Wrapper onConfirm={vi.fn()} />);
    expect(await screen.findByText('Excluir item')).toBeInTheDocument();
    expect(screen.getByText('Essa ação não pode ser desfeita.')).toBeInTheDocument();
  });
});

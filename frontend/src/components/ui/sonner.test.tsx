import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { toast } from 'sonner';
import { Toaster } from './sonner';

function ToastTrigger() {
  return (
    <>
      <button type="button" onClick={() => toast.success('Adicionado ao Carrinho.')}>
        Disparar toast
      </button>
      <Toaster />
    </>
  );
}

describe('Toaster (sonner)', () => {
  it('renderiza o toast disparado dentro de uma região aria-live="polite"', async () => {
    const user = userEvent.setup();
    render(<ToastTrigger />);

    await user.click(screen.getByRole('button', { name: 'Disparar toast' }));

    const message = await screen.findByText('Adicionado ao Carrinho.');
    expect(message).toBeInTheDocument();
    expect(message.closest('[aria-live="polite"]')).not.toBeNull();
  });
});

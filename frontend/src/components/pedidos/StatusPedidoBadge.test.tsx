import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { StatusPedidoBadge } from './StatusPedidoBadge';

describe('StatusPedidoBadge', () => {
  it.each([
    ['pendente', 'Pendente', 'text-on-tint-warning'],
    ['aprovado', 'Aprovado', 'text-on-tint-success'],
    ['parcialmente_aprovado', 'Parcialmente aprovado', 'text-on-tint-info'],
    ['rejeitado', 'Rejeitado', 'text-on-tint-destructive'],
  ])('status %j renderiza ícone + rótulo textual %j na variante de cor correta', (status, rotulo, tintEsperado) => {
    const { container } = render(<StatusPedidoBadge status={status} />);
    const pill = screen.getByText(rotulo);
    expect(pill).toBeInTheDocument();
    // "nunca só cor": há sempre um ícone SVG acompanhando o texto.
    expect(container.querySelector('svg')).toBeInTheDocument();
    // a variante de cor precisa corresponder ao status — nunca a de outro
    // status (ex.: "Aprovado" nunca pode sair com a classe destructive).
    expect(pill.className).toContain(tintEsperado);
  });

  it('status desconhecido cai no rótulo genérico, com ícone (nunca só cor)', () => {
    const { container } = render(<StatusPedidoBadge status="entregue" />);
    expect(screen.getByText('Desconhecido')).toBeInTheDocument();
    expect(screen.queryByText('entregue')).not.toBeInTheDocument();
    expect(container.querySelector('svg')).toBeInTheDocument();
  });
});

import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { TipoPedidoBadge } from './TipoPedidoBadge';

describe('TipoPedidoBadge', () => {
  it('tipo=transferencia renderiza ícone + rótulo "Transferência"', () => {
    const { container } = render(<TipoPedidoBadge tipo="transferencia" />);
    const pill = screen.getByText('Transferência');
    expect(pill).toBeInTheDocument();
    expect(container.querySelector('svg')).toBeInTheDocument();
  });

  it('tipo=baixa renderiza ícone + rótulo "Baixa"', () => {
    const { container } = render(<TipoPedidoBadge tipo="baixa" />);
    expect(screen.getByText('Baixa')).toBeInTheDocument();
    expect(container.querySelector('svg')).toBeInTheDocument();
  });

  it('tipo=retirada não renderiza nada — o Pedido de retirada continua visualmente idêntico a antes desta story', () => {
    const { container } = render(<TipoPedidoBadge tipo="retirada" />);
    expect(container).toBeEmptyDOMElement();
  });

  it('tipo ausente (fixtures anteriores a esta story) não renderiza nada', () => {
    const { container } = render(<TipoPedidoBadge />);
    expect(container).toBeEmptyDOMElement();
  });
});

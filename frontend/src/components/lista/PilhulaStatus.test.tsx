import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { PilhulaStatus } from './PilhulaStatus';

describe('PilhulaStatus', () => {
  it('renderiza o texto do status', () => {
    render(<PilhulaStatus status="Inativo" />);
    expect(screen.getByText('Inativo')).toBeInTheDocument();
  });

  it('renderiza texto arbitrário', () => {
    render(<PilhulaStatus status="Aprovado" />);
    expect(screen.getByText('Aprovado')).toBeInTheDocument();
  });

  it('aplica estilos de pílula suave', () => {
    render(<PilhulaStatus status="Pendente" />);
    const el = screen.getByText('Pendente');
    expect(el.className).toContain('rounded-full');
    expect(el.className).toContain('border');
  });
});

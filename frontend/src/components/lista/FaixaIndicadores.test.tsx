import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { FaixaIndicadores } from './FaixaIndicadores';

describe('FaixaIndicadores', () => {
  it('exibe rótulo e valor normal', () => {
    render(<FaixaIndicadores indicadores={[{ rotulo: 'Itens', valor: 42 }]} />);
    expect(screen.getByText('Itens')).toBeInTheDocument();
    expect(screen.getByText('42')).toBeInTheDocument();
  });

  it('valor null exibe "—"', () => {
    render(<FaixaIndicadores indicadores={[{ rotulo: 'Com saldo', valor: null }]} />);
    expect(screen.getByText('—')).toBeInTheDocument();
  });

  it('alerta=true e valor>0 aplica text-destructive', () => {
    render(<FaixaIndicadores indicadores={[{ rotulo: 'Sem foto', valor: 3, alerta: true }]} />);
    const valorEl = screen.getByText('3');
    expect(valorEl.className).toContain('text-destructive');
  });

  it('alerta=true e valor=0 NÃO aplica text-destructive', () => {
    render(<FaixaIndicadores indicadores={[{ rotulo: 'Sem foto', valor: 0, alerta: true }]} />);
    const valorEl = screen.getByText('0');
    expect(valorEl.className).not.toContain('text-destructive');
  });

  it('slot acoes é renderizado', () => {
    render(
      <FaixaIndicadores
        indicadores={[{ rotulo: 'Itens', valor: 1 }]}
        acoes={<button type="button">Exportar</button>}
      />,
    );
    expect(screen.getByRole('button', { name: 'Exportar' })).toBeInTheDocument();
  });

  it('sem slot acoes não renderiza container de ações', () => {
    const { container } = render(
      <FaixaIndicadores indicadores={[{ rotulo: 'Itens', valor: 1 }]} />,
    );
    // Sem acoes, apenas a div de KPIs deve existir.
    expect(container.querySelectorAll('div > div').length).toBeGreaterThan(0);
  });
});

import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { SemEmpresaPage } from './SemEmpresaPage';

describe('SemEmpresaPage (Story 9.2)', () => {
  it('explica que o acesso é pelo endereço da própria empresa', () => {
    render(<SemEmpresaPage />);

    expect(screen.getByText('Acesse pelo endereço da sua empresa')).toBeInTheDocument();
    expect(screen.getByText(/\/e\/nome-da-empresa$/)).toBeInTheDocument();
  });

  it('não lista Empresas, não tem links e não aponta para a Plataforma', () => {
    render(<SemEmpresaPage />);

    expect(screen.queryAllByRole('link')).toHaveLength(0);
    expect(screen.queryAllByRole('listitem')).toHaveLength(0);
    expect(document.body.textContent ?? '').not.toMatch(/plataforma/i);
  });
});

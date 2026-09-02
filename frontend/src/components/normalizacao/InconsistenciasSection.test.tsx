import { describe, expect, it, vi, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { InconsistenciasSection } from './InconsistenciasSection';

vi.mock('@/lib/session', () => ({
  getAccessToken: () => 'token-de-teste',
}));

type FetchImpl = (
  url: string,
  init?: RequestInit,
) => Promise<{ ok: boolean; status?: number; json: () => Promise<unknown> }>;

function stubFetch(impl: FetchImpl) {
  const fn = vi.fn(impl);
  vi.stubGlobal('fetch', fn);
  return fn;
}

function jsonOk(body: unknown) {
  return Promise.resolve({ ok: true, json: async () => body });
}

const SUGESTOES = [
  {
    produtoId: 'p-1',
    produtoNome: 'Tubo PVC 6m',
    campo: 'comprimento',
    valorSugerido: { valor: 6, unidade: 'm' },
    origem: 'nome',
  },
  {
    produtoId: 'p-2',
    produtoNome: 'Cabo Flexível',
    campo: 'diametro',
    valorSugerido: { valor: 3, unidade: 'mm' },
    origem: 'migracao',
  },
];

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe('InconsistenciasSection', () => {
  it('não busca nada no mount — só ao clicar em "Analisar todos os produtos"', () => {
    const fetchMock = stubFetch(() => jsonOk({ sugestoes: [] }));
    render(<InconsistenciasSection />);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('clique dispara o fetch e a tabela renderiza as sugestões', async () => {
    const user = userEvent.setup();
    const fetchMock = stubFetch((url) => {
      if (url === '/api/normalizacao/inconsistencias') return jsonOk({ sugestoes: SUGESTOES });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<InconsistenciasSection />);
    await user.click(screen.getByRole('button', { name: 'Analisar todos os produtos' }));

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/normalizacao/inconsistencias',
      expect.objectContaining({ headers: { Authorization: 'Bearer token-de-teste' } }),
    );

    expect(await screen.findByText('Tubo PVC 6m')).toBeInTheDocument();
    for (const coluna of ['Produto', 'Campo', 'Valor sugerido', 'Origem']) {
      expect(screen.getByRole('columnheader', { name: coluna })).toBeInTheDocument();
    }
    expect(screen.getByText('Comprimento')).toBeInTheDocument();
    expect(screen.getByText('6m')).toBeInTheDocument();
    expect(screen.getByText('Nome')).toBeInTheDocument();
    expect(screen.getByText('Cabo Flexível')).toBeInTheDocument();
    expect(screen.getByText('Diâmetro')).toBeInTheDocument();
    expect(screen.getByText('3mm')).toBeInTheDocument();
    expect(screen.getByText('Migração')).toBeInTheDocument();
  });

  it('lista vazia mostra "Nenhuma inconsistência encontrada."', async () => {
    const user = userEvent.setup();
    stubFetch((url) => {
      if (url === '/api/normalizacao/inconsistencias') return jsonOk({ sugestoes: [] });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<InconsistenciasSection />);
    await user.click(screen.getByRole('button', { name: 'Analisar todos os produtos' }));

    expect(await screen.findByText('Nenhuma inconsistência encontrada.')).toBeInTheDocument();
  });

  it('falha de rede mostra o alerta e nenhuma tabela', async () => {
    const user = userEvent.setup();
    stubFetch(() => Promise.reject(new Error('network down')));

    render(<InconsistenciasSection />);
    await user.click(screen.getByRole('button', { name: 'Analisar todos os produtos' }));

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível analisar os produtos. Tente novamente em instantes.',
    );
    expect(screen.queryByRole('table')).not.toBeInTheDocument();
  });

  it('resposta não-ok (ex. 500) mostra o mesmo alerta', async () => {
    const user = userEvent.setup();
    stubFetch(() => Promise.resolve({ ok: false, status: 500, json: async () => ({}) }));

    render(<InconsistenciasSection />);
    await user.click(screen.getByRole('button', { name: 'Analisar todos os produtos' }));

    await waitFor(() => expect(screen.getByRole('alert')).toBeInTheDocument());
  });

  it('um segundo clique que falha limpa a tabela da corrida anterior bem-sucedida', async () => {
    const user = userEvent.setup();
    let chamada = 0;
    stubFetch((url) => {
      if (url !== '/api/normalizacao/inconsistencias') throw new Error(`URL inesperada: ${url}`);
      chamada += 1;
      if (chamada === 1) return jsonOk({ sugestoes: SUGESTOES });
      return Promise.reject(new Error('network down'));
    });

    render(<InconsistenciasSection />);

    // Primeiro clique: sucesso, tabela aparece.
    await user.click(screen.getByRole('button', { name: 'Analisar todos os produtos' }));
    expect(await screen.findByText('Tubo PVC 6m')).toBeInTheDocument();

    // Segundo clique: falha — a tabela da corrida anterior NÃO pode
    // continuar visível ao lado do alerta de erro novo.
    await user.click(screen.getByRole('button', { name: 'Analisar todos os produtos' }));
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível analisar os produtos. Tente novamente em instantes.',
    );
    expect(screen.queryByText('Tubo PVC 6m')).not.toBeInTheDocument();
    expect(screen.queryByRole('table')).not.toBeInTheDocument();
  });
});

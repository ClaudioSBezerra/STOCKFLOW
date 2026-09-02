import { describe, expect, it, vi, afterEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
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

  it('"Aceitar" individual chama POST /correcoes com 1 item e remove só a linha confirmada', async () => {
    const user = userEvent.setup();
    const fetchMock = stubFetch((url) => {
      if (url === '/api/normalizacao/inconsistencias') return jsonOk({ sugestoes: SUGESTOES });
      if (url === '/api/normalizacao/correcoes') {
        return jsonOk({ aplicadas: [{ produtoId: 'p-1', campo: 'comprimento' }] });
      }
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<InconsistenciasSection />);
    await user.click(screen.getByRole('button', { name: 'Analisar todos os produtos' }));
    expect(await screen.findByText('Tubo PVC 6m')).toBeInTheDocument();

    const linhas = screen.getAllByRole('row');
    const linhaP1 = linhas.find((r) => r.textContent?.includes('Tubo PVC 6m'))!;
    await user.click(within(linhaP1).getByRole('button', { name: 'Aceitar Tubo PVC 6m - Comprimento' }));

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/normalizacao/correcoes',
        expect.objectContaining({
          method: 'POST',
          body: JSON.stringify({
            correcoes: [{ produtoId: 'p-1', campo: 'comprimento', valorSugerido: { valor: 6, unidade: 'm' } }],
          }),
        }),
      ),
    );

    await waitFor(() => expect(screen.queryByText('Tubo PVC 6m')).not.toBeInTheDocument());
    expect(screen.getByText('Cabo Flexível')).toBeInTheDocument();
  });

  it('seleção de checkboxes + "Aplicar selecionadas" envia o lote e remove as linhas confirmadas', async () => {
    const user = userEvent.setup();
    stubFetch((url) => {
      if (url === '/api/normalizacao/inconsistencias') return jsonOk({ sugestoes: SUGESTOES });
      if (url === '/api/normalizacao/correcoes') {
        return jsonOk({
          aplicadas: [
            { produtoId: 'p-1', campo: 'comprimento' },
            { produtoId: 'p-2', campo: 'diametro' },
          ],
        });
      }
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<InconsistenciasSection />);
    await user.click(screen.getByRole('button', { name: 'Analisar todos os produtos' }));
    expect(await screen.findByText('Tubo PVC 6m')).toBeInTheDocument();

    await user.click(screen.getByRole('checkbox', { name: 'Selecionar todas' }));
    expect(screen.getByRole('button', { name: 'Aplicar selecionadas (2)' })).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Aplicar selecionadas (2)' }));

    await waitFor(() => expect(screen.queryByText('Tubo PVC 6m')).not.toBeInTheDocument());
    await waitFor(() => expect(screen.queryByText('Cabo Flexível')).not.toBeInTheDocument());
    expect(screen.getByText('Nenhuma inconsistência encontrada.')).toBeInTheDocument();
  });

  it('"Ignorar" chama POST /ignoradas e remove a linha imediatamente', async () => {
    const user = userEvent.setup();
    const fetchMock = stubFetch((url) => {
      if (url === '/api/normalizacao/inconsistencias') return jsonOk({ sugestoes: SUGESTOES });
      if (url === '/api/normalizacao/ignoradas') return jsonOk({ ignorada: true });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<InconsistenciasSection />);
    await user.click(screen.getByRole('button', { name: 'Analisar todos os produtos' }));
    expect(await screen.findByText('Cabo Flexível')).toBeInTheDocument();

    const linhas = screen.getAllByRole('row');
    const linhaP2 = linhas.find((r) => r.textContent?.includes('Cabo Flexível'))!;
    await user.click(within(linhaP2).getByRole('button', { name: 'Ignorar Cabo Flexível - Diâmetro' }));

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/normalizacao/ignoradas',
        expect.objectContaining({
          method: 'POST',
          body: JSON.stringify({
            produtoId: 'p-2',
            campo: 'diametro',
            valorSugerido: { valor: 3, unidade: 'mm' },
          }),
        }),
      ),
    );

    await waitFor(() => expect(screen.queryByText('Cabo Flexível')).not.toBeInTheDocument());
    expect(screen.getByText('Tubo PVC 6m')).toBeInTheDocument();
  });

  it('falha de rede ao aplicar mostra o alerta e mantém a linha na tabela', async () => {
    const user = userEvent.setup();
    stubFetch((url) => {
      if (url === '/api/normalizacao/inconsistencias') return jsonOk({ sugestoes: SUGESTOES });
      if (url === '/api/normalizacao/correcoes') return Promise.reject(new Error('network down'));
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<InconsistenciasSection />);
    await user.click(screen.getByRole('button', { name: 'Analisar todos os produtos' }));
    expect(await screen.findByText('Tubo PVC 6m')).toBeInTheDocument();

    const linhas = screen.getAllByRole('row');
    const linhaP1 = linhas.find((r) => r.textContent?.includes('Tubo PVC 6m'))!;
    await user.click(within(linhaP1).getByRole('button', { name: 'Aceitar Tubo PVC 6m - Comprimento' }));

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível aplicar a(s) correção(ões) selecionada(s). Tente novamente em instantes.',
    );
    expect(screen.getByText('Tubo PVC 6m')).toBeInTheDocument();
  });

  it('falha de rede ao ignorar mostra o alerta e mantém a linha na tabela', async () => {
    const user = userEvent.setup();
    stubFetch((url) => {
      if (url === '/api/normalizacao/inconsistencias') return jsonOk({ sugestoes: SUGESTOES });
      if (url === '/api/normalizacao/ignoradas') return Promise.reject(new Error('network down'));
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<InconsistenciasSection />);
    await user.click(screen.getByRole('button', { name: 'Analisar todos os produtos' }));
    expect(await screen.findByText('Cabo Flexível')).toBeInTheDocument();

    const linhas = screen.getAllByRole('row');
    const linhaP2 = linhas.find((r) => r.textContent?.includes('Cabo Flexível'))!;
    await user.click(within(linhaP2).getByRole('button', { name: 'Ignorar Cabo Flexível - Diâmetro' }));

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível ignorar a sugestão. Tente novamente em instantes.',
    );
    expect(screen.getByText('Cabo Flexível')).toBeInTheDocument();
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

  it('"Analisar todos os produtos" fica desabilitado enquanto uma ação de aplicar/ignorar está em andamento', async () => {
    const user = userEvent.setup();
    let resolverCorrecoes: (value: { ok: boolean; json: () => Promise<unknown> }) => void;
    const correcoesPendente = new Promise<{ ok: boolean; json: () => Promise<unknown> }>((resolve) => {
      resolverCorrecoes = resolve;
    });
    stubFetch((url) => {
      if (url === '/api/normalizacao/inconsistencias') return jsonOk({ sugestoes: SUGESTOES });
      if (url === '/api/normalizacao/correcoes') return correcoesPendente;
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<InconsistenciasSection />);
    await user.click(screen.getByRole('button', { name: 'Analisar todos os produtos' }));
    expect(await screen.findByText('Tubo PVC 6m')).toBeInTheDocument();

    const linhas = screen.getAllByRole('row');
    const linhaP1 = linhas.find((r) => r.textContent?.includes('Tubo PVC 6m'))!;
    await user.click(within(linhaP1).getByRole('button', { name: 'Aceitar Tubo PVC 6m - Comprimento' }));

    // Corrida que o code review fechou: "Analisar" não pode ser clicável
    // enquanto uma ação de aplicar/ignorar (`processando`) ainda não voltou.
    expect(screen.getByRole('button', { name: 'Analisar todos os produtos' })).toBeDisabled();

    resolverCorrecoes!({ ok: true, json: async () => ({ aplicadas: [{ produtoId: 'p-1', campo: 'comprimento' }] }) });
    await waitFor(() => expect(screen.queryByText('Tubo PVC 6m')).not.toBeInTheDocument());
    expect(screen.getByRole('button', { name: 'Analisar todos os produtos' })).toBeEnabled();
  });
});

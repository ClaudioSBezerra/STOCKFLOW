import { describe, expect, it, vi, afterEach, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { ImportacaoProdutosSection } from './ImportacaoProdutosSection';

// `ImportacaoProdutosSection` usa `<Link>` (react-router-dom) no CTA
// "Verificar duplicatas agora" (Story 3.4, spec-3-4) — precisa de um Router
// no ar, mesmo padrão de EsqueciSenhaPage.test.tsx/RedefinirSenhaPage.test.tsx.
function renderComponente() {
  return render(
    <MemoryRouter>
      <ImportacaoProdutosSection />
    </MemoryRouter>,
  );
}

const toastSuccess = vi.hoisted(() => vi.fn());
vi.mock('sonner', () => ({ toast: { success: toastSuccess } }));

vi.mock('@/lib/session', () => ({
  getAccessToken: () => 'token-de-teste',
}));

type FetchImpl = (
  url: string,
  init?: RequestInit,
) => Promise<{ ok: boolean; status?: number; json: () => Promise<unknown> }>;

// Depósitos/Filiais servidos por padrão a todos os testes (a seção carrega os
// dois no mount); testes específicos sobrescrevem estas variáveis.
let estoquesDeTeste: unknown[] = [];
let filiaisDeTeste: unknown[] = [];

function stubFetch(impl: FetchImpl) {
  const fn = vi.fn((url: string, init?: RequestInit) => {
    if (url === '/api/estoques') return jsonOk({ estoques: estoquesDeTeste });
    if (url === '/api/filiais') return jsonOk({ filiais: filiaisDeTeste });
    return impl(url, init);
  });
  vi.stubGlobal('fetch', fn);
  return fn;
}

function jsonOk(body: unknown) {
  return Promise.resolve({ ok: true, status: 200, json: async () => body });
}

const SEM_IMPORTACAO_ANTERIOR = { importacao: null };

beforeEach(() => {
  estoquesDeTeste = [
    { id: 'est-1', nome: 'Almoxarifado Central', filial_id: 'fil-1' },
    { id: 'est-2', nome: 'Canteiro Norte', filial_id: 'fil-2' },
  ];
  filiaisDeTeste = [
    { id: 'fil-1', nome: 'Matriz' },
    { id: 'fil-2', nome: 'Filial Norte' },
  ];
});

async function escolherDeposito(user: ReturnType<typeof userEvent.setup>, nome = 'Almoxarifado Central') {
  const seletor = await screen.findByLabelText('Depósito de destino');
  await user.selectOptions(seletor, nome);
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

function chamada(fetchMock: ReturnType<typeof vi.fn>, url: string, metodo: string) {
  return fetchMock.mock.calls.find(
    (args: unknown[]) => args[0] === url && (args[1] as RequestInit)?.method === metodo,
  ) as [string, RequestInit] | undefined;
}

describe('ImportacaoProdutosSection — banner de retomada', () => {
  it('sem importação anterior: não mostra banner de retomada', async () => {
    const fetchMock = stubFetch((url) => {
      if (url === '/api/importacoes/ultima') return jsonOk(SEM_IMPORTACAO_ANTERIOR);
      throw new Error(`URL inesperada: ${url}`);
    });
    renderComponente();

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith('/api/importacoes/ultima', expect.anything()));
    expect(screen.queryByText(/Continuar de onde parou/)).not.toBeInTheDocument();
  });

  it('importação em_andamento: mostra "parou na linha N de M" usando proxima_linha_pendente (NÃO criados+rejeitados)', async () => {
    stubFetch((url) => {
      if (url === '/api/importacoes/ultima')
        // criados+rejeitados = 3+1 = 4 de propósito — se o componente algum
        // dia voltar a somá-los em vez de usar proxima_linha_pendente, este
        // teste falha (a mensagem citaria "linha 4", não "linha 7").
        return jsonOk({
          importacao: { id: 'imp-1', status: 'em_andamento', total_linhas: 10, proxima_linha_pendente: 7 },
          relatorio: { criados: 3, rejeitados: 1, linhas_rejeitadas: [] },
        });
      throw new Error(`URL inesperada: ${url}`);
    });
    renderComponente();

    expect(await screen.findByText(/Última importação parou na linha 7 de 10\./)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Continuar importação' })).toBeInTheDocument();
  });

  it('importação concluída (proxima_linha_pendente null): não mostra banner de retomada', async () => {
    stubFetch((url) => {
      if (url === '/api/importacoes/ultima')
        return jsonOk({
          importacao: { id: 'imp-1', status: 'concluida', total_linhas: 5, proxima_linha_pendente: null },
          relatorio: { criados: 5, rejeitados: 0, linhas_rejeitadas: [] },
        });
      throw new Error(`URL inesperada: ${url}`);
    });
    renderComponente();

    await waitFor(() => expect(fetch).toHaveBeenCalled());
    expect(screen.queryByText(/Continuar de onde parou/)).not.toBeInTheDocument();
  });

  it('em_andamento mas proxima_linha_pendente null (defensivo): não mostra banner', async () => {
    // Estado teoricamente só momentâneo (ex. outra chamada concorrente já
    // reivindicou a última linha pendente) — o componente não deve tentar
    // renderizar "parou na linha null de M".
    stubFetch((url) => {
      if (url === '/api/importacoes/ultima')
        return jsonOk({
          importacao: { id: 'imp-1', status: 'em_andamento', total_linhas: 5, proxima_linha_pendente: null },
          relatorio: { criados: 5, rejeitados: 0, linhas_rejeitadas: [] },
        });
      throw new Error(`URL inesperada: ${url}`);
    });
    renderComponente();

    await waitFor(() => expect(fetch).toHaveBeenCalled());
    expect(screen.queryByText(/Continuar de onde parou/)).not.toBeInTheDocument();
  });

  it('clicar em "Continuar importação" chama POST /continuar e atualiza o banner ao concluir', async () => {
    let chamadasUltima = 0;
    const fetchMock = stubFetch((url, init) => {
      if (url === '/api/importacoes/ultima') {
        chamadasUltima += 1;
        if (chamadasUltima === 1) {
          return jsonOk({
            importacao: { id: 'imp-1', status: 'em_andamento', total_linhas: 5, proxima_linha_pendente: 4 },
            relatorio: { criados: 2, rejeitados: 0, linhas_rejeitadas: [] },
          });
        }
        return jsonOk({
          importacao: { id: 'imp-1', status: 'concluida', total_linhas: 5, proxima_linha_pendente: null },
          relatorio: { criados: 5, rejeitados: 0, linhas_rejeitadas: [] },
        });
      }
      if (url === '/api/importacoes/imp-1/continuar' && init?.method === 'POST') {
        return jsonOk({
          importacao: { id: 'imp-1', status: 'concluida', total_linhas: 5, proxima_linha_pendente: null },
          relatorio: { criados: 4, atualizados: 1, rejeitados: 0, linhas_rejeitadas: [] },
        });
      }
      throw new Error(`URL inesperada: ${url} (${init?.method ?? 'GET'})`);
    });
    const user = userEvent.setup();
    renderComponente();

    await screen.findByText(/Última importação parou na linha 4 de 5\./);
    await user.click(screen.getByRole('button', { name: 'Continuar importação' }));

    await waitFor(() =>
      expect(toastSuccess).toHaveBeenCalledWith(
        'Importação concluída: 4 criado(s), 1 atualizado(s), 0 rejeitado(s).',
      ),
    );
    expect(screen.queryByText(/Continuar de onde parou/)).not.toBeInTheDocument();
    expect(chamada(fetchMock, '/api/importacoes/imp-1/continuar', 'POST')).toBeTruthy();
  });
});

describe('ImportacaoProdutosSection — botões mutuamente exclusivos', () => {
  it('enquanto "Continuar importação" está em voo, "Importar planilha" também fica desabilitado (e vice-versa)', async () => {
    let resolverContinuar: ((value: { ok: boolean; json: () => Promise<unknown> }) => void) | undefined;
    const continuarPromise = new Promise<{ ok: boolean; json: () => Promise<unknown> }>((resolve) => {
      resolverContinuar = resolve;
    });
    stubFetch((url, init) => {
      if (url === '/api/importacoes/ultima')
        return jsonOk({
          importacao: { id: 'imp-1', status: 'em_andamento', total_linhas: 5, proxima_linha_pendente: 3 },
          relatorio: { criados: 2, rejeitados: 0, linhas_rejeitadas: [] },
        });
      if (url === '/api/importacoes/imp-1/continuar' && init?.method === 'POST') {
        return continuarPromise;
      }
      throw new Error(`URL inesperada: ${url} (${init?.method ?? 'GET'})`);
    });
    const user = userEvent.setup();
    renderComponente();
    await screen.findByText(/Última importação parou na linha 3 de 5\./);

    const botaoImportar = screen.getByRole('button', { name: 'Importar planilha' });
    const arquivo = new File(['conteudo'], 'planilha.xlsx');
    await user.upload(screen.getByLabelText('Planilha (.xlsx)'), arquivo);
    await escolherDeposito(user);
    expect(botaoImportar).toBeEnabled();

    await user.click(screen.getByRole('button', { name: 'Continuar importação' }));

    // Com POST /continuar ainda pendente (a promise controlada acima não
    // resolveu), os DOIS botões devem estar desabilitados — não só o de
    // continuar.
    await waitFor(() => expect(screen.getByRole('button', { name: 'Continuando...' })).toBeDisabled());
    expect(botaoImportar).toBeDisabled();

    resolverContinuar?.({
      ok: true,
      json: async () => ({
        importacao: { id: 'imp-1', status: 'concluida', total_linhas: 5, proxima_linha_pendente: null },
        relatorio: { criados: 5, atualizados: 0, rejeitados: 0, linhas_rejeitadas: [] },
      }),
    });

    await waitFor(() => expect(toastSuccess).toHaveBeenCalled());
    // O banner (e o botão "continuar") somem com a importação concluída; o
    // botão de importar volta a ficar habilitado (o arquivo selecionado
    // continua no estado — só um envio bem-sucedido o limpa).
    expect(await screen.findByRole('button', { name: 'Importar planilha' })).toBeEnabled();
  });
});

describe('ImportacaoProdutosSection — upload da planilha', () => {
  it('botão "Importar planilha" começa desabilitado sem arquivo selecionado', async () => {
    stubFetch((url) => {
      if (url === '/api/importacoes/ultima') return jsonOk(SEM_IMPORTACAO_ANTERIOR);
      throw new Error(`URL inesperada: ${url}`);
    });
    renderComponente();

    expect(screen.getByRole('button', { name: 'Importar planilha' })).toBeDisabled();
  });

  it('upload bem-sucedido: envia FormData (campo planilha) sem Content-Type manual, mostra toast e a tabela de rejeitadas', async () => {
    const fetchMock = stubFetch((url, init) => {
      if (url === '/api/importacoes/ultima') return jsonOk(SEM_IMPORTACAO_ANTERIOR);
      if (url === '/api/importacoes' && init?.method === 'POST') {
        return jsonOk({
          importacao: {
            id: 'imp-2',
            status: 'concluida',
            total_linhas: 2,
            estoque_id: 'est-1',
            estoque_nome: 'Almoxarifado Central',
            filial_nome: 'Matriz',
          },
          relatorio: {
            criados: 1,
            atualizados: 0,
            rejeitados: 1,
            linhas_rejeitadas: [{ linha: 3, erro: 'categoria "Inexistente" não encontrada' }],
          },
        });
      }
      throw new Error(`URL inesperada: ${url} (${init?.method ?? 'GET'})`);
    });
    const user = userEvent.setup();
    renderComponente();
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith('/api/importacoes/ultima', expect.anything()));

    const arquivo = new File(['conteudo-fake'], 'planilha.xlsx', {
      type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
    });
    await user.upload(screen.getByLabelText('Planilha (.xlsx)'), arquivo);
    await escolherDeposito(user);
    expect(screen.getByRole('button', { name: 'Importar planilha' })).toBeEnabled();
    await user.click(screen.getByRole('button', { name: 'Importar planilha' }));

    await waitFor(() =>
      expect(toastSuccess).toHaveBeenCalledWith(
        'Importação concluída: 1 criado(s), 0 atualizado(s), 1 rejeitado(s).',
      ),
    );
    expect(screen.getByText('Depósito: Almoxarifado Central (Matriz)')).toBeInTheDocument();
    expect(screen.getByText('3')).toBeInTheDocument();
    expect(screen.getByText('categoria "Inexistente" não encontrada')).toBeInTheDocument();
    expect(screen.getByText(/1 criado\(s\), 0 atualizado\(s\), 1 rejeitado\(s\)\./)).toBeInTheDocument();

    const chamadaPost = chamada(fetchMock, '/api/importacoes', 'POST');
    expect(chamadaPost).toBeTruthy();
    const init = chamadaPost![1];
    expect(init.body).toBeInstanceOf(FormData);
    expect((init.body as FormData).get('planilha')).toBeInstanceOf(File);
    expect((init.body as FormData).get('estoque_id')).toBe('est-1');
    expect((init.headers as Record<string, string> | undefined)?.['Content-Type']).toBeUndefined();
    expect((init.headers as Record<string, string>).Authorization).toBe('Bearer token-de-teste');
  });

  it('400 do servidor: mostra a mensagem em role="alert"', async () => {
    stubFetch((url, init) => {
      if (url === '/api/importacoes/ultima') return jsonOk(SEM_IMPORTACAO_ANTERIOR);
      if (url === '/api/importacoes' && init?.method === 'POST') {
        return Promise.resolve({
          ok: false,
          status: 400,
          json: async () => ({ error: { code: 'VALIDATION_ERROR', message: 'cabeçalho da planilha inválido' } }),
        });
      }
      throw new Error(`URL inesperada: ${url} (${init?.method ?? 'GET'})`);
    });
    const user = userEvent.setup();
    renderComponente();
    await waitFor(() => expect(fetch).toHaveBeenCalled());

    const arquivo = new File(['ruim'], 'ruim.xlsx');
    await user.upload(screen.getByLabelText('Planilha (.xlsx)'), arquivo);
    await escolherDeposito(user);
    await user.click(screen.getByRole('button', { name: 'Importar planilha' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('cabeçalho da planilha inválido');
    expect(toastSuccess).not.toHaveBeenCalled();
  });

  it('erro de rede (fetch rejeita): mostra mensagem genérica em role="alert"', async () => {
    stubFetch((url, init) => {
      if (url === '/api/importacoes/ultima') return jsonOk(SEM_IMPORTACAO_ANTERIOR);
      if (url === '/api/importacoes' && init?.method === 'POST') {
        return Promise.reject(new Error('falha de rede'));
      }
      throw new Error(`URL inesperada: ${url} (${init?.method ?? 'GET'})`);
    });
    const user = userEvent.setup();
    renderComponente();
    await waitFor(() => expect(fetch).toHaveBeenCalled());

    const arquivo = new File(['x'], 'planilha.xlsx');
    await user.upload(screen.getByLabelText('Planilha (.xlsx)'), arquivo);
    await escolherDeposito(user);
    await user.click(screen.getByRole('button', { name: 'Importar planilha' }));

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível importar a planilha agora. Tente novamente em instantes.',
    );
  });
});

describe('ImportacaoProdutosSection — relatório com atualizados e CTA de duplicatas (Story 3.4)', () => {
  it('sem relatório ainda (nenhum envio feito): CTA "Verificar duplicatas agora" não aparece', async () => {
    stubFetch((url) => {
      if (url === '/api/importacoes/ultima') return jsonOk(SEM_IMPORTACAO_ANTERIOR);
      throw new Error(`URL inesperada: ${url}`);
    });
    renderComponente();

    await waitFor(() => expect(fetch).toHaveBeenCalled());
    expect(screen.queryByRole('link', { name: 'Verificar duplicatas agora' })).not.toBeInTheDocument();
  });

  it('após envio com sucesso: mostra criados/atualizados/rejeitados e o CTA aponta para /normalizacao/duplicatas', async () => {
    stubFetch((url, init) => {
      if (url === '/api/importacoes/ultima') return jsonOk(SEM_IMPORTACAO_ANTERIOR);
      if (url === '/api/importacoes' && init?.method === 'POST') {
        return jsonOk({
          importacao: { id: 'imp-3', status: 'concluida', total_linhas: 3 },
          relatorio: { criados: 1, atualizados: 2, rejeitados: 0, linhas_rejeitadas: [] },
        });
      }
      throw new Error(`URL inesperada: ${url} (${init?.method ?? 'GET'})`);
    });
    const user = userEvent.setup();
    renderComponente();
    await waitFor(() => expect(fetch).toHaveBeenCalled());

    const arquivo = new File(['conteudo'], 'planilha.xlsx');
    await user.upload(screen.getByLabelText('Planilha (.xlsx)'), arquivo);
    await escolherDeposito(user);
    await user.click(screen.getByRole('button', { name: 'Importar planilha' }));

    expect(await screen.findByText('1 criado(s), 2 atualizado(s), 0 rejeitado(s).')).toBeInTheDocument();
    const cta = screen.getByRole('link', { name: 'Verificar duplicatas agora' });
    expect(cta).toBeInTheDocument();
    expect(cta).toHaveAttribute('href', '/normalizacao/duplicatas?verificarDuplicatas=1');
  });
});

describe('ImportacaoProdutosSection — Depósito de destino (spec-importacao-por-deposito)', () => {
  const semUltima = (url: string) => {
    if (url === '/api/importacoes/ultima') return jsonOk(SEM_IMPORTACAO_ANTERIOR);
    throw new Error(`URL inesperada: ${url}`);
  };

  it('lista os depósitos agrupados por Filial', async () => {
    stubFetch(semUltima);
    renderComponente();

    const seletor = await screen.findByLabelText('Depósito de destino');
    await waitFor(() => expect(screen.getByRole('option', { name: 'Canteiro Norte' })).toBeInTheDocument());
    const grupos = Array.from(seletor.querySelectorAll('optgroup')).map((g) => g.label);
    expect(grupos).toEqual(['Matriz', 'Filial Norte']);
  });

  it('envio fica bloqueado até escolher o depósito, mesmo com arquivo', async () => {
    stubFetch(semUltima);
    const user = userEvent.setup();
    renderComponente();
    await screen.findByRole('option', { name: 'Canteiro Norte' });

    await user.upload(screen.getByLabelText('Planilha (.xlsx)'), new File(['x'], 'planilha.xlsx'));
    expect(screen.getByRole('button', { name: 'Importar planilha' })).toBeDisabled();
    await escolherDeposito(user, 'Canteiro Norte');
    expect(screen.getByRole('button', { name: 'Importar planilha' })).toBeEnabled();
  });

  it('sem depósitos: mostra a mensagem e bloqueia o envio', async () => {
    estoquesDeTeste = [];
    stubFetch(semUltima);
    const user = userEvent.setup();
    renderComponente();

    expect(
      await screen.findByText('Nenhum depósito cadastrado. Peça a um gestor para cadastrar em Cadastros > Filiais.'),
    ).toBeInTheDocument();
    await user.upload(screen.getByLabelText('Planilha (.xlsx)'), new File(['x'], 'planilha.xlsx'));
    expect(screen.getByRole('button', { name: 'Importar planilha' })).toBeDisabled();
  });

  it('erro no envio preserva o arquivo e a escolha do depósito', async () => {
    stubFetch((url, init) => {
      if (url === '/api/importacoes/ultima') return jsonOk(SEM_IMPORTACAO_ANTERIOR);
      if (url === '/api/importacoes' && init?.method === 'POST') {
        return Promise.resolve({
          ok: false,
          status: 400,
          json: async () => ({ error: { code: 'VALIDATION_ERROR', message: 'cabeçalho da planilha inválido' } }),
        });
      }
      throw new Error(`URL inesperada: ${url}`);
    });
    const user = userEvent.setup();
    renderComponente();
    await user.upload(screen.getByLabelText('Planilha (.xlsx)'), new File(['x'], 'planilha.xlsx'));
    await escolherDeposito(user, 'Canteiro Norte');
    await user.click(screen.getByRole('button', { name: 'Importar planilha' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('cabeçalho da planilha inválido');
    expect(screen.getByLabelText('Depósito de destino')).toHaveValue('est-2');
    expect(screen.getByRole('button', { name: 'Importar planilha' })).toBeEnabled();
  });

  it('relatório mostra as linhas preenchidas com 0 e as que mantiveram o valor atual', async () => {
    stubFetch((url, init) => {
      if (url === '/api/importacoes/ultima') return jsonOk(SEM_IMPORTACAO_ANTERIOR);
      if (url === '/api/importacoes' && init?.method === 'POST') {
        return jsonOk({
          importacao: { id: 'imp-9', status: 'concluida', total_linhas: 3, estoque_nome: 'Canteiro Norte', filial_nome: 'Filial Norte' },
          relatorio: {
            criados: 1,
            atualizados: 2,
            rejeitados: 0,
            preenchidas_com_zero: 1,
            mantiveram_valor_atual: 2,
            linhas_rejeitadas: [],
          },
        });
      }
      throw new Error(`URL inesperada: ${url}`);
    });
    const user = userEvent.setup();
    renderComponente();
    await user.upload(screen.getByLabelText('Planilha (.xlsx)'), new File(['x'], 'planilha.xlsx'));
    await escolherDeposito(user, 'Canteiro Norte');
    await user.click(screen.getByRole('button', { name: 'Importar planilha' }));

    expect(await screen.findByText(/1 linha\(s\) com quantidade ou preço vazio foram preenchidas com 0/)).toBeInTheDocument();
    expect(screen.getByText(/2 linha\(s\) com quantidade ou preço mantidos/)).toBeInTheDocument();
    expect(screen.getByText('Depósito: Canteiro Norte (Filial Norte)')).toBeInTheDocument();
  });
});

describe('ImportacaoProdutosSection — depósitos: falha, agrupamento e depósito excluído', () => {
  it('falha em GET /api/estoques: mostra erro e "Tentar de novo" recarrega', async () => {
    let falhar = true;
    const fetchMock = vi.fn((url: string) => {
      if (url === '/api/importacoes/ultima') return jsonOk(SEM_IMPORTACAO_ANTERIOR);
      if (url === '/api/filiais') return jsonOk({ filiais: filiaisDeTeste });
      if (url === '/api/estoques') {
        if (falhar) return Promise.resolve({ ok: false, status: 500, json: async () => ({}) });
        return jsonOk({ estoques: estoquesDeTeste });
      }
      throw new Error(`URL inesperada: ${url}`);
    });
    vi.stubGlobal('fetch', fetchMock);
    const user = userEvent.setup();
    renderComponente();

    expect(await screen.findByText('Não foi possível carregar os depósitos agora.')).toBeInTheDocument();
    falhar = false;
    await user.click(screen.getByRole('button', { name: 'Tentar de novo' }));
    expect(await screen.findByRole('option', { name: 'Canteiro Norte' })).toBeInTheDocument();
    expect(screen.queryByText('Não foi possível carregar os depósitos agora.')).not.toBeInTheDocument();
  });

  it('agrupamento: cada depósito sob a sua Filial e legado em "Sem filial"', async () => {
    estoquesDeTeste = [
      { id: 'est-1', nome: 'Almoxarifado Central', filial_id: 'fil-1' },
      { id: 'est-2', nome: 'Canteiro Norte', filial_id: 'fil-2' },
      { id: 'est-3', nome: 'Depósito Legado', filial_id: null },
    ];
    stubFetch((url) => {
      if (url === '/api/importacoes/ultima') return jsonOk(SEM_IMPORTACAO_ANTERIOR);
      throw new Error(`URL inesperada: ${url}`);
    });
    renderComponente();

    const seletor = await screen.findByLabelText('Depósito de destino');
    await screen.findByRole('option', { name: 'Depósito Legado' });
    const mapa = Object.fromEntries(
      Array.from(seletor.querySelectorAll('optgroup')).map((g) => [
        g.label,
        Array.from(g.querySelectorAll('option')).map((o) => o.textContent),
      ]),
    );
    expect(mapa).toEqual({
      Matriz: ['Almoxarifado Central'],
      'Filial Norte': ['Canteiro Norte'],
      'Sem filial': ['Depósito Legado'],
    });
  });

  it('por_deposito=true com depósito nulo: resumo mostra "Depósito: excluído"', async () => {
    stubFetch((url, init) => {
      if (url === '/api/importacoes/ultima') return jsonOk(SEM_IMPORTACAO_ANTERIOR);
      if (url === '/api/importacoes' && init?.method === 'POST') {
        return jsonOk({
          importacao: { id: 'imp-x', status: 'concluida', total_linhas: 1, estoque_id: null, estoque_nome: null, por_deposito: true },
          relatorio: { criados: 0, atualizados: 0, rejeitados: 1, linhas_rejeitadas: [{ linha: 2, erro: 'depósito de destino da importação não existe mais' }] },
        });
      }
      throw new Error(`URL inesperada: ${url}`);
    });
    const user = userEvent.setup();
    renderComponente();
    await user.upload(screen.getByLabelText('Planilha (.xlsx)'), new File(['x'], 'planilha.xlsx'));
    await escolherDeposito(user);
    await user.click(screen.getByRole('button', { name: 'Importar planilha' }));

    expect(await screen.findByText(/Depósito: excluído/)).toBeInTheDocument();
  });
});

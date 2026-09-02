import { describe, expect, it, vi, afterEach } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { DuplicatasSection } from './DuplicatasSection';

const toastSuccess = vi.hoisted(() => vi.fn());
vi.mock('sonner', () => ({ toast: { success: toastSuccess } }));

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

const GRUPOS = [
  {
    produtos: [
      {
        id: 'p-1',
        nome: 'Tubo PVC 25mm',
        dimensoes: {
          comprimento: null,
          largura: null,
          diametro: { valor: 25, unidade: 'mm' },
          altura: null,
          espessura: null,
        },
      },
      {
        id: 'p-2',
        nome: 'Tubo PVC 25mm',
        dimensoes: {
          comprimento: null,
          largura: null,
          diametro: { valor: 2.5, unidade: 'cm' },
          altura: null,
          espessura: null,
        },
      },
    ],
  },
];

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

async function analisarEObterGrupo(user: ReturnType<typeof userEvent.setup>) {
  render(<DuplicatasSection />);
  await user.click(screen.getByRole('button', { name: 'Analisar duplicatas' }));
  await screen.findAllByText('Tubo PVC 25mm');
}

describe('DuplicatasSection', () => {
  it('não busca nada no mount por padrão — só ao clicar em "Analisar duplicatas"', () => {
    const fetchMock = stubFetch(() => jsonOk({ grupos: [] }));
    render(<DuplicatasSection />);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('clique dispara o fetch e renderiza os grupos', async () => {
    const user = userEvent.setup();
    const fetchMock = stubFetch((url) => {
      if (url === '/api/normalizacao/duplicatas') return jsonOk({ grupos: GRUPOS });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<DuplicatasSection />);
    await user.click(screen.getByRole('button', { name: 'Analisar duplicatas' }));

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/normalizacao/duplicatas',
      expect.objectContaining({ headers: { Authorization: 'Bearer token-de-teste' } }),
    );

    const nomes = await screen.findAllByText('Tubo PVC 25mm');
    expect(nomes).toHaveLength(2);
    expect(screen.getByRole('columnheader', { name: 'Produto' })).toBeInTheDocument();
    expect(screen.getByRole('columnheader', { name: 'Dimensões' })).toBeInTheDocument();
  });

  it('autoAnalisar dispara a análise uma vez ao montar, sem exigir clique', async () => {
    const fetchMock = stubFetch((url) => {
      if (url === '/api/normalizacao/duplicatas') return jsonOk({ grupos: GRUPOS });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<DuplicatasSection autoAnalisar />);

    const nomes = await screen.findAllByText('Tubo PVC 25mm');
    expect(nomes).toHaveLength(2);
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('lista vazia mostra "Nenhuma duplicata encontrada."', async () => {
    const user = userEvent.setup();
    stubFetch((url) => {
      if (url === '/api/normalizacao/duplicatas') return jsonOk({ grupos: [] });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<DuplicatasSection />);
    await user.click(screen.getByRole('button', { name: 'Analisar duplicatas' }));

    expect(await screen.findByText('Nenhuma duplicata encontrada.')).toBeInTheDocument();
  });

  it('falha de rede mostra o alerta e nenhuma tabela', async () => {
    const user = userEvent.setup();
    stubFetch(() => Promise.reject(new Error('network down')));

    render(<DuplicatasSection />);
    await user.click(screen.getByRole('button', { name: 'Analisar duplicatas' }));

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível analisar os produtos. Tente novamente em instantes.',
    );
    expect(screen.queryByRole('table')).not.toBeInTheDocument();
  });

  it('resposta não-ok (ex. 500) mostra o mesmo alerta', async () => {
    const user = userEvent.setup();
    stubFetch(() => Promise.resolve({ ok: false, status: 500, json: async () => ({}) }));

    render(<DuplicatasSection />);
    await user.click(screen.getByRole('button', { name: 'Analisar duplicatas' }));

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível analisar os produtos. Tente novamente em instantes.',
    );
    expect(screen.queryByRole('table')).not.toBeInTheDocument();
  });

  // --- Mesclagem (Story 6.4, spec-6-4) --------------------------------------

  it('botão "Mesclar" começa desabilitado e habilita só depois de selecionar um rádio "manter"', async () => {
    const user = userEvent.setup();
    stubFetch((url) => {
      if (url === '/api/normalizacao/duplicatas') return jsonOk({ grupos: GRUPOS });
      throw new Error(`URL inesperada: ${url}`);
    });

    await analisarEObterGrupo(user);

    const botaoMesclar = screen.getByRole('button', { name: 'Mesclar' });
    expect(botaoMesclar).toBeDisabled();

    const radios = screen.getAllByRole('radio', { name: 'Manter Tubo PVC 25mm' });
    await user.click(radios[0]);
    expect(botaoMesclar).not.toBeDisabled();
  });

  it('seleciona um rádio por grupo (mutuamente exclusivo) e abre o ConfirmDialog citando mantido e removidos', async () => {
    const user = userEvent.setup();
    stubFetch((url) => {
      if (url === '/api/normalizacao/duplicatas') return jsonOk({ grupos: GRUPOS });
      throw new Error(`URL inesperada: ${url}`);
    });

    await analisarEObterGrupo(user);

    const radios = screen.getAllByRole('radio', { name: 'Manter Tubo PVC 25mm' });
    expect(radios).toHaveLength(2);
    await user.click(radios[0]);
    expect(radios[0]).toBeChecked();
    expect(radios[1]).not.toBeChecked();

    await user.click(radios[1]);
    expect(radios[0]).not.toBeChecked();
    expect(radios[1]).toBeChecked();

    await user.click(screen.getByRole('button', { name: 'Mesclar' }));
    const dialog = await screen.findByRole('alertdialog');
    expect(within(dialog).getByText('Esta ação não pode ser desfeita.', { exact: false })).toBeInTheDocument();
  });

  it('confirmar a mesclagem chama POST /api/normalizacao/mesclar e remove o grupo da lista em sucesso', async () => {
    const user = userEvent.setup();
    const fetchMock = stubFetch((url, init) => {
      if (url === '/api/normalizacao/duplicatas') return jsonOk({ grupos: GRUPOS });
      if (url === '/api/normalizacao/mesclar' && init?.method === 'POST') {
        return jsonOk({ produtoMantidoId: 'p-1', produtosRemovidosIds: ['p-2'], quantidadeConsolidada: 8 });
      }
      throw new Error(`URL inesperada: ${url}`);
    });

    await analisarEObterGrupo(user);

    const radios = screen.getAllByRole('radio', { name: 'Manter Tubo PVC 25mm' });
    await user.click(radios[0]); // mantém p-1

    await user.click(screen.getByRole('button', { name: 'Mesclar' }));
    const dialog = await screen.findByRole('alertdialog');
    await user.click(within(dialog).getByRole('button', { name: 'Mesclar' }));

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/normalizacao/mesclar',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({ produtoMantidoId: 'p-1', produtoRemovidoIds: ['p-2'] }),
      }),
    );

    await vi.waitFor(() => {
      expect(screen.queryByText('Tubo PVC 25mm')).not.toBeInTheDocument();
    });
    expect(await screen.findByText('Nenhuma duplicata encontrada.')).toBeInTheDocument();
    expect(toastSuccess).toHaveBeenCalledWith(
      expect.stringContaining('8'),
    );
  });

  it('409 na confirmação mostra alerta e MANTÉM o grupo na lista', async () => {
    const user = userEvent.setup();
    stubFetch((url, init) => {
      if (url === '/api/normalizacao/duplicatas') return jsonOk({ grupos: GRUPOS });
      if (url === '/api/normalizacao/mesclar' && init?.method === 'POST') {
        return Promise.resolve({
          ok: false,
          status: 409,
          json: async () => ({ error: { code: 'CONFLICT', message: 'grupo inválido' } }),
        });
      }
      throw new Error(`URL inesperada: ${url}`);
    });

    await analisarEObterGrupo(user);

    const radios = screen.getAllByRole('radio', { name: 'Manter Tubo PVC 25mm' });
    await user.click(radios[0]);
    await user.click(screen.getByRole('button', { name: 'Mesclar' }));
    const dialog = await screen.findByRole('alertdialog');
    await user.click(within(dialog).getByRole('button', { name: 'Mesclar' }));

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Este grupo mudou desde a última análise',
    );
    // O grupo continua na lista — o Almoxarife pode reabrir a análise.
    const nomes = await screen.findAllByText('Tubo PVC 25mm');
    expect(nomes).toHaveLength(2);
    expect(toastSuccess).not.toHaveBeenCalled();
  });

  it('cancelar o ConfirmDialog não dispara POST e mantém a seleção', async () => {
    const user = userEvent.setup();
    const fetchMock = stubFetch((url) => {
      if (url === '/api/normalizacao/duplicatas') return jsonOk({ grupos: GRUPOS });
      throw new Error(`URL inesperada: ${url}`);
    });

    await analisarEObterGrupo(user);

    const radios = screen.getAllByRole('radio', { name: 'Manter Tubo PVC 25mm' });
    await user.click(radios[0]);
    await user.click(screen.getByRole('button', { name: 'Mesclar' }));
    const dialog = await screen.findByRole('alertdialog');
    await user.click(within(dialog).getByRole('button', { name: 'Cancelar' }));

    expect(
      fetchMock.mock.calls.filter(([url]) => url === '/api/normalizacao/mesclar'),
    ).toHaveLength(0);
    expect(radios[0]).toBeChecked();
  });
});

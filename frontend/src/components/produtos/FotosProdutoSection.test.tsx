import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { FotosProdutoSection } from './FotosProdutoSection';

vi.mock('@/lib/session', () => ({ getAccessToken: () => 'token-de-teste' }));
const toastSuccess = vi.hoisted(() => vi.fn());
vi.mock('sonner', () => ({ toast: { success: toastSuccess, info: vi.fn() } }));

type Resposta = { ok: boolean; status: number; json: () => Promise<unknown>; blob: () => Promise<Blob> };

function resposta(ok: boolean, body: unknown, status = ok ? 200 : 500): Promise<Resposta> {
  return Promise.resolve({ ok, status, json: async () => body, blob: async () => new Blob(['x']) });
}

// Servidor em memória: `fotos` é a lista vigente; POST acrescenta, DELETE tira.
function servidor(inicial: string[], opcoes: { falharPost?: boolean } = {}) {
  let fotos = [...inicial];
  let seq = 0;
  const fn = vi.fn((url: string, init?: RequestInit) => {
    if (url === '/api/produtos/p1/fotos' && !init?.method) {
      return resposta(true, { fotos: fotos.map((nome) => ({ nome, url: `/api/produtos/p1/fotos/${nome}` })) });
    }
    if (url.startsWith('/api/produtos/p1/fotos/') && !init?.method) return resposta(true, {});
    if (url === '/api/produtos/p1/fotos' && init?.method === 'POST') {
      if (opcoes.falharPost) return resposta(false, { error: { message: 'formato de imagem não suportado' } }, 400);
      const nome = `p1-179000000${seq++}.jpg`;
      fotos.push(nome);
      return resposta(true, { foto: { nome } }, 201);
    }
    if (url.startsWith('/api/produtos/p1/fotos/') && init?.method === 'DELETE') {
      const nome = url.split('/').pop()!;
      fotos = fotos.filter((f) => f !== nome);
      return resposta(true, {}, 204);
    }
    throw new Error(`URL inesperada: ${init?.method ?? 'GET'} ${url}`);
  });
  vi.stubGlobal('fetch', fn);
  return { fn, fotos: () => fotos };
}

const arquivo = () => new File(['conteudo'], 'nova.jpg', { type: 'image/jpeg' });

beforeEach(() => {
  vi.stubGlobal('URL', Object.assign(URL, { createObjectURL: vi.fn(() => 'blob:x'), revokeObjectURL: vi.fn() }));
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe('FotosProdutoSection', () => {
  it('produto sem foto: mostra o aviso e adiciona uma foto', async () => {
    const srv = servidor([]);
    const onAlterado = vi.fn();
    render(<FotosProdutoSection produtoId="p1" onAlterado={onAlterado} />);

    expect(await screen.findByText('Este produto ainda não tem foto.')).toBeInTheDocument();
    const enviar = screen.getByRole('button', { name: 'Enviar foto' });
    expect(enviar).toBeDisabled();

    await userEvent.upload(screen.getByLabelText('Adicionar foto'), arquivo());
    await userEvent.click(enviar);

    await waitFor(() => expect(screen.getAllByRole('img')).toHaveLength(1));
    expect(srv.fotos()).toHaveLength(1);
    expect(toastSuccess).toHaveBeenCalledWith('Foto adicionada.');
    expect(onAlterado).toHaveBeenCalledTimes(1);
  });

  it('remove uma foto só depois de confirmar', async () => {
    const srv = servidor(['p1-1790000001.jpg', 'p1-1790000002.jpg']);
    render(<FotosProdutoSection produtoId="p1" />);

    await userEvent.click(await screen.findByRole('button', { name: 'Remover foto 1' }));
    const confirmacao = await screen.findByRole('alertdialog');
    expect(srv.fn).not.toHaveBeenCalledWith(expect.anything(), expect.objectContaining({ method: 'DELETE' }));
    await userEvent.click(within(confirmacao).getByRole('button', { name: 'Remover' }));

    await waitFor(() => expect(srv.fotos()).toEqual(['p1-1790000002.jpg']));
    await waitFor(() => expect(screen.getAllByRole('img')).toHaveLength(1));
    expect(toastSuccess).toHaveBeenCalledWith('Foto removida.');
  });

  it('troca: envia a nova e só então remove a antiga', async () => {
    const srv = servidor(['p1-1790000001.jpg']);
    render(<FotosProdutoSection produtoId="p1" />);

    await userEvent.click(await screen.findByRole('button', { name: 'Trocar foto 1' }));
    await userEvent.upload(screen.getByTestId('fotos-trocar-input'), arquivo());

    await waitFor(() => expect(toastSuccess).toHaveBeenCalledWith('Foto trocada.'));
    expect(srv.fotos()).toHaveLength(1);
    expect(srv.fotos()[0]).not.toBe('p1-1790000001.jpg');
    const metodos = srv.fn.mock.calls.map(([, init]) => (init as RequestInit | undefined)?.method).filter(Boolean);
    expect(metodos).toEqual(['POST', 'DELETE']);
  });

  it('troca com envio recusado: mostra o erro e mantém a foto antiga', async () => {
    const srv = servidor(['p1-1790000001.jpg'], { falharPost: true });
    render(<FotosProdutoSection produtoId="p1" />);

    await userEvent.click(await screen.findByRole('button', { name: 'Trocar foto 1' }));
    await userEvent.upload(screen.getByTestId('fotos-trocar-input'), arquivo());

    expect(await screen.findByRole('alert')).toHaveTextContent('formato de imagem não suportado');
    expect(srv.fotos()).toEqual(['p1-1790000001.jpg']);
    expect(srv.fn).not.toHaveBeenCalledWith(expect.anything(), expect.objectContaining({ method: 'DELETE' }));
    expect(toastSuccess).not.toHaveBeenCalled();
  });
});

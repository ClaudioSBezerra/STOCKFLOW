import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { LogoEmpresaSection } from './LogoEmpresaSection';

const toastSuccess = vi.hoisted(() => vi.fn());
vi.mock('sonner', () => ({ toast: { success: toastSuccess } }));
vi.mock('@/lib/session', () => ({ getAccessToken: () => 'token-de-teste' }));

type Resp = { ok: boolean; status: number; json: () => Promise<unknown>; blob: () => Promise<Blob> };
const blobOk = (): Promise<Resp> =>
  Promise.resolve({ ok: true, status: 200, json: async () => ({}), blob: async () => new Blob(['x'], { type: 'image/png' }) });
const naoEncontrada = (): Promise<Resp> =>
  Promise.resolve({ ok: false, status: 404, json: async () => ({}), blob: async () => new Blob() });
const jsonOk = (): Promise<Resp> =>
  Promise.resolve({ ok: true, status: 200, json: async () => ({}), blob: async () => new Blob() });
const semConteudo = (): Promise<Resp> =>
  Promise.resolve({ ok: true, status: 204, json: async () => ({}), blob: async () => new Blob() });
const erro400 = (message: string): Promise<Resp> =>
  Promise.resolve({
    ok: false,
    status: 400,
    json: async () => ({ error: { code: 'VALIDATION_ERROR', message } }),
    blob: async () => new Blob(),
  });

const metodo = (init?: RequestInit) => init?.method ?? 'GET';
let contador = 0;

beforeEach(() => {
  contador = 0;
  vi.stubGlobal('URL', Object.assign(URL, {
    createObjectURL: vi.fn(() => `blob:fake-${++contador}`),
    revokeObjectURL: vi.fn(),
  }));
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

function arquivo(nome: string, tipo: string) {
  return new File(['conteudo'], nome, { type: tipo });
}

describe('LogoEmpresaSection', () => {
  it('sem logo (404): mostra o estado vazio, as instruções e só o botão Enviar', async () => {
    vi.stubGlobal('fetch', vi.fn(() => naoEncontrada()));
    render(<LogoEmpresaSection />);
    expect(await screen.findByText('Nenhuma logo cadastrada.')).toBeInTheDocument();
    expect(screen.getByText(/até 512 KB/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Enviar logo' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Remover logo' })).not.toBeInTheDocument();
  });

  it('com logo: mostra a imagem e oferece Trocar e Remover', async () => {
    const fetchMock = vi.fn(() => blobOk());
    vi.stubGlobal('fetch', fetchMock);
    render(<LogoEmpresaSection />);
    expect(await screen.findByAltText('Logo atual da Empresa')).toHaveAttribute('src', 'blob:fake-1');
    expect(screen.getByRole('button', { name: 'Trocar logo' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Remover logo' })).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith('/api/empresa/logo', expect.objectContaining({ cache: 'no-cache' }));
  });

  it('pré-visualiza antes de confirmar, envia multipart `logo` e recarrega', async () => {
    let logoGravada = false;
    const fetchMock = vi.fn((_url: string, init?: RequestInit) => {
      if (metodo(init) === 'PUT') {
        logoGravada = true;
        return jsonOk();
      }
      return logoGravada ? blobOk() : naoEncontrada();
    });
    vi.stubGlobal('fetch', fetchMock);
    const user = userEvent.setup();
    render(<LogoEmpresaSection />);
    await screen.findByText('Nenhuma logo cadastrada.');

    await user.upload(screen.getByLabelText('Arquivo da logo'), arquivo('logo.png', 'image/png'));
    expect(screen.getByAltText('Pré-visualização da nova logo')).toBeInTheDocument();
    // Nada foi enviado ainda.
    expect(fetchMock.mock.calls.some(([, i]) => metodo(i) === 'PUT')).toBe(false);

    await user.click(screen.getByRole('button', { name: 'Confirmar envio' }));
    const put = fetchMock.mock.calls.find(([, i]) => metodo(i) === 'PUT');
    expect(put).toBeTruthy();
    const corpo = put![1]!.body as FormData;
    expect((corpo.get('logo') as File).name).toBe('logo.png');
    expect(await screen.findByAltText('Logo atual da Empresa')).toBeInTheDocument();
    expect(screen.queryByAltText('Pré-visualização da nova logo')).not.toBeInTheDocument();
    expect(toastSuccess).toHaveBeenCalled();
  });

  it('cancelar descarta a pré-visualização sem enviar', async () => {
    const fetchMock = vi.fn(() => naoEncontrada());
    vi.stubGlobal('fetch', fetchMock);
    const user = userEvent.setup();
    render(<LogoEmpresaSection />);
    await screen.findByText('Nenhuma logo cadastrada.');
    await user.upload(screen.getByLabelText('Arquivo da logo'), arquivo('logo.png', 'image/png'));
    await user.click(screen.getByRole('button', { name: 'Cancelar' }));
    expect(screen.queryByAltText('Pré-visualização da nova logo')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Enviar logo' })).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('mostra a mensagem do servidor quando recusa o arquivo e mantém a logo atual', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn((_u: string, init?: RequestInit) =>
        metodo(init) === 'PUT' ? erro400('Dimensões inválidas (10x10 px): largura e altura devem ficar entre 32 e 4000 px.') : blobOk(),
      ),
    );
    const user = userEvent.setup();
    render(<LogoEmpresaSection />);
    await screen.findByAltText('Logo atual da Empresa');
    await user.upload(screen.getByLabelText('Arquivo da logo'), arquivo('logo.png', 'image/png'));
    await user.click(screen.getByRole('button', { name: 'Confirmar envio' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Dimensões inválidas');
    expect(toastSuccess).not.toHaveBeenCalled();
    expect(screen.getByAltText('Pré-visualização da nova logo')).toBeInTheDocument();
  });

  it('remover pede confirmação, chama DELETE e volta ao estado vazio', async () => {
    const fetchMock = vi.fn((_u: string, init?: RequestInit) => (metodo(init) === 'DELETE' ? semConteudo() : blobOk()));
    vi.stubGlobal('fetch', fetchMock);
    const user = userEvent.setup();
    render(<LogoEmpresaSection />);
    await screen.findByAltText('Logo atual da Empresa');
    await user.click(screen.getByRole('button', { name: 'Remover logo' }));
    expect(fetchMock.mock.calls.some(([, i]) => metodo(i) === 'DELETE')).toBe(false);
    await user.click(await screen.findByRole('button', { name: 'Remover' }));
    await waitFor(() => expect(fetchMock.mock.calls.some(([, i]) => metodo(i) === 'DELETE')).toBe(true));
    expect(await screen.findByText('Nenhuma logo cadastrada.')).toBeInTheDocument();
  });

  it('erro ao carregar: mostra o aviso, sai do "Carregando…" e deixa tentar de novo', async () => {
    let falhar = true;
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        falhar
          ? Promise.resolve({ ok: false, status: 500, json: async () => ({}), blob: async () => new Blob() })
          : blobOk(),
      ),
    );
    const user = userEvent.setup();
    render(<LogoEmpresaSection />);
    expect(await screen.findByRole('alert')).toHaveTextContent('Não foi possível carregar a logo');
    expect(screen.queryByText('Carregando…')).not.toBeInTheDocument();
    falhar = false;
    await user.click(screen.getByRole('button', { name: 'Tentar de novo' }));
    expect(await screen.findByAltText('Logo atual da Empresa')).toBeInTheDocument();
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('valida tipo e tamanho no navegador, sem enviar, com as mensagens do servidor', async () => {
    const fetchMock = vi.fn(() => naoEncontrada());
    vi.stubGlobal('fetch', fetchMock);
    const user = userEvent.setup({ applyAccept: false });
    render(<LogoEmpresaSection />);
    await screen.findByText('Nenhuma logo cadastrada.');
    const input = screen.getByLabelText('Arquivo da logo') as HTMLInputElement;

    await user.upload(input, arquivo('logo.svg', 'image/svg+xml'));
    expect(screen.getByRole('alert')).toHaveTextContent('Formato inválido: use PNG ou JPEG.');
    expect(screen.queryByAltText('Pré-visualização da nova logo')).not.toBeInTheDocument();
    expect(input.value).toBe('');

    const grande = new File([new Uint8Array(512 * 1024 + 1)], 'grande.png', { type: 'image/png' });
    await user.upload(input, grande);
    expect(screen.getByRole('alert')).toHaveTextContent('A logo é grande demais: o limite é 512 KB.');
    expect(screen.queryByRole('button', { name: 'Confirmar envio' })).not.toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledTimes(1); // só a busca inicial
  });

  it('limpa o campo de arquivo ao cancelar, para escolher o mesmo arquivo de novo', async () => {
    vi.stubGlobal('fetch', vi.fn(() => naoEncontrada()));
    const user = userEvent.setup();
    render(<LogoEmpresaSection />);
    await screen.findByText('Nenhuma logo cadastrada.');
    const input = screen.getByLabelText('Arquivo da logo') as HTMLInputElement;
    const f = arquivo('logo.png', 'image/png');
    await user.upload(input, f);
    await user.click(screen.getByRole('button', { name: 'Cancelar' }));
    expect(input.value).toBe('');
    await user.upload(input, f);
    expect(screen.getByAltText('Pré-visualização da nova logo')).toBeInTheDocument();
  });

  it('a ajuda e o aviso de sucesso dizem que a logo vale para todas as Pré-Notas, inclusive as emitidas', async () => {
    vi.stubGlobal('fetch', vi.fn((_u: string, init?: RequestInit) => (metodo(init) === 'PUT' ? jsonOk() : naoEncontrada())));
    const user = userEvent.setup();
    render(<LogoEmpresaSection />);
    await screen.findByText('Nenhuma logo cadastrada.');
    expect(screen.getByText(/inclusive as já emitidas/)).toBeInTheDocument();
    expect(screen.getByText(/4 milhões de pixels/)).toBeInTheDocument();
    await user.upload(screen.getByLabelText('Arquivo da logo'), arquivo('logo.png', 'image/png'));
    await user.click(screen.getByRole('button', { name: 'Confirmar envio' }));
    await waitFor(() =>
      expect(toastSuccess).toHaveBeenCalledWith('Logo salva. A logo vale para todas as Pré-Notas, inclusive as já emitidas.'),
    );
  });
});

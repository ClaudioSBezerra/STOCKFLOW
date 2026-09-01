import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom';
import { ScannerProdutoFab } from './ScannerProdutoFab';

/**
 * Cobre as linhas 8-13 da matriz de spec-4-5:
 *  - decodificação OK -> câmera fecha, `navigate('/produtos/<id>')`;
 *  - código lido inexistente (`404`) -> toast + `aoFalharLeitura`;
 *  - `NotAllowedError` -> mensagem de permissão + `aoFalharLeitura`;
 *  - `NotFoundError` -> mensagem "nenhuma câmera" + `aoFalharLeitura`;
 *  - `window.isSecureContext === false` -> mensagem HTTPS, `criarLeitorCodigo`
 *    NUNCA chamado;
 *  - Cancelar / desmontar durante a leitura -> `parar()`, sem `navigate`.
 */

const iniciarMock = vi.hoisted(() => vi.fn());
const pararLeituraMock = vi.hoisted(() => vi.fn());
vi.mock('@/lib/scanner/leitor', () => ({
  criarLeitorCodigo: () => ({ iniciar: iniciarMock }),
}));

const toastError = vi.hoisted(() => vi.fn());
vi.mock('sonner', () => ({ toast: { error: toastError } }));

vi.mock('@/lib/session', () => ({
  getAccessToken: () => 'token-de-teste',
}));

let aoLerCapturado: (texto: string) => void;

function definirContextoSeguro(valor: boolean) {
  Object.defineProperty(window, 'isSecureContext', { configurable: true, value: valor });
}

function LocationDisplay() {
  const location = useLocation();
  return <div data-testid="location">{location.pathname}</div>;
}

function renderFab() {
  const aoFalharLeitura = vi.fn();
  render(
    <MemoryRouter initialEntries={['/']}>
      <Routes>
        <Route
          path="/"
          element={
            <>
              <ScannerProdutoFab aoFalharLeitura={aoFalharLeitura} />
              <LocationDisplay />
            </>
          }
        />
        <Route path="/produtos/:id" element={<LocationDisplay />} />
      </Routes>
    </MemoryRouter>,
  );
  return { aoFalharLeitura };
}

const abrirCameraLabel = 'Escanear código do produto';

beforeEach(() => {
  definirContextoSeguro(true);
  iniciarMock.mockReset();
  pararLeituraMock.mockReset();
  toastError.mockReset();
  iniciarMock.mockImplementation((_video: HTMLVideoElement, aoLer: (texto: string) => void) => {
    aoLerCapturado = aoLer;
    return Promise.resolve({ parar: pararLeituraMock });
  });
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe('ScannerProdutoFab — FAB', () => {
  it('renderiza o botão flutuante com aria-label de scanner', () => {
    renderFab();
    expect(screen.getByRole('button', { name: abrirCameraLabel })).toBeInTheDocument();
  });
});

describe('ScannerProdutoFab — decodificação OK', () => {
  it('lê um código de Produto existente: fecha a câmera, resolve pelo endpoint e navega para /produtos/<id>', async () => {
    const fetchMock = vi.fn(() =>
      Promise.resolve({ ok: true, status: 200, json: async () => ({ produto: { id: 'p42' } }) }),
    );
    vi.stubGlobal('fetch', fetchMock);

    const user = userEvent.setup();
    renderFab();
    await user.click(screen.getByRole('button', { name: abrirCameraLabel }));
    await waitFor(() => expect(iniciarMock).toHaveBeenCalledTimes(1));

    await act(async () => {
      aoLerCapturado('CAB-004');
    });

    await waitFor(() => expect(screen.getByTestId('location').textContent).toBe('/produtos/p42'));
    expect(fetchMock).toHaveBeenCalledWith('/api/produtos/por-codigo?codigo=CAB-004', {
      headers: { Authorization: 'Bearer token-de-teste' },
    });
    expect(pararLeituraMock).toHaveBeenCalled();
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('só o primeiro código lido é resolvido — quadros seguintes são ignorados (guarda de disparo duplo)', async () => {
    const fetchMock = vi.fn(() =>
      Promise.resolve({ ok: true, status: 200, json: async () => ({ produto: { id: 'p1' } }) }),
    );
    vi.stubGlobal('fetch', fetchMock);

    const user = userEvent.setup();
    renderFab();
    await user.click(screen.getByRole('button', { name: abrirCameraLabel }));
    await waitFor(() => expect(iniciarMock).toHaveBeenCalledTimes(1));

    await act(async () => {
      aoLerCapturado('CAB-004');
      aoLerCapturado('OUTRO-999');
    });

    await waitFor(() => expect(screen.getByTestId('location').textContent).toBe('/produtos/p1'));
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });
});

describe('ScannerProdutoFab — falhas de leitura', () => {
  it('código lido sem Produto correspondente (404): toast "não reconhecido", aoFalharLeitura, sem navegar', async () => {
    const fetchMock = vi.fn(() =>
      Promise.resolve({ ok: false, status: 404, json: async () => ({}) }),
    );
    vi.stubGlobal('fetch', fetchMock);

    const user = userEvent.setup();
    const { aoFalharLeitura } = renderFab();
    await user.click(screen.getByRole('button', { name: abrirCameraLabel }));
    await waitFor(() => expect(iniciarMock).toHaveBeenCalledTimes(1));

    await act(async () => {
      aoLerCapturado('NAO-EXISTE');
    });

    await waitFor(() =>
      expect(toastError).toHaveBeenCalledWith(
        'Código não reconhecido: nenhum produto com esse código. Use a busca por texto.',
      ),
    );
    expect(aoFalharLeitura).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId('location').textContent).toBe('/');
    expect(pararLeituraMock).toHaveBeenCalled();
  });

  it('resposta inesperada do endpoint (500): toast genérico "não foi possível abrir o produto"', async () => {
    const fetchMock = vi.fn(() =>
      Promise.resolve({ ok: false, status: 500, json: async () => ({}) }),
    );
    vi.stubGlobal('fetch', fetchMock);

    const user = userEvent.setup();
    renderFab();
    await user.click(screen.getByRole('button', { name: abrirCameraLabel }));
    await waitFor(() => expect(iniciarMock).toHaveBeenCalledTimes(1));

    await act(async () => {
      aoLerCapturado('CAB-004');
    });

    await waitFor(() =>
      expect(toastError).toHaveBeenCalledWith('Não foi possível abrir o produto agora. Tente novamente.'),
    );
  });

  it('permissão de câmera negada (NotAllowedError): mensagem de permissão, aoFalharLeitura, câmera não fica aberta', async () => {
    iniciarMock.mockRejectedValueOnce(
      Object.assign(new Error('denied'), { name: 'NotAllowedError' }),
    );

    const user = userEvent.setup();
    const { aoFalharLeitura } = renderFab();
    await user.click(screen.getByRole('button', { name: abrirCameraLabel }));

    await waitFor(() =>
      expect(toastError).toHaveBeenCalledWith(
        'Permissão de câmera negada. Libere o acesso à câmera nas configurações do navegador ou use a busca por texto.',
      ),
    );
    expect(aoFalharLeitura).toHaveBeenCalledTimes(1);
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
  });

  it('sem câmera / hardware (NotFoundError): mensagem "nenhuma câmera disponível", aoFalharLeitura', async () => {
    iniciarMock.mockRejectedValueOnce(
      Object.assign(new Error('no device'), { name: 'NotFoundError' }),
    );

    const user = userEvent.setup();
    const { aoFalharLeitura } = renderFab();
    await user.click(screen.getByRole('button', { name: abrirCameraLabel }));

    await waitFor(() =>
      expect(toastError).toHaveBeenCalledWith(
        'Nenhuma câmera disponível. Use a busca por texto para encontrar o produto.',
      ),
    );
    expect(aoFalharLeitura).toHaveBeenCalledTimes(1);
  });

  it('erro de câmera não mapeado: mensagem genérica de câmera', async () => {
    iniciarMock.mockRejectedValueOnce(
      Object.assign(new Error('weird'), { name: 'AbortError' }),
    );

    const user = userEvent.setup();
    renderFab();
    await user.click(screen.getByRole('button', { name: abrirCameraLabel }));

    await waitFor(() =>
      expect(toastError).toHaveBeenCalledWith('Não foi possível abrir a câmera. Use a busca por texto.'),
    );
  });
});

describe('ScannerProdutoFab — contexto inseguro (HTTP)', () => {
  it('window.isSecureContext === false: mensagem HTTPS, criarLeitorCodigo NUNCA chamado, câmera não abre', async () => {
    definirContextoSeguro(false);
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);

    const user = userEvent.setup();
    const { aoFalharLeitura } = renderFab();
    await user.click(screen.getByRole('button', { name: abrirCameraLabel }));

    expect(toastError).toHaveBeenCalledWith(
      'O scanner exige uma conexão segura (HTTPS). A câmera do navegador não está disponível aqui.',
    );
    expect(iniciarMock).not.toHaveBeenCalled();
    expect(fetchMock).not.toHaveBeenCalled();
    expect(aoFalharLeitura).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });
});

describe('ScannerProdutoFab — cancelar / desmontar', () => {
  it('Cancelar durante a leitura: parar() é chamado, sem navegar, e o foco volta à busca', async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);

    const user = userEvent.setup();
    const { aoFalharLeitura } = renderFab();
    await user.click(screen.getByRole('button', { name: abrirCameraLabel }));
    await waitFor(() => expect(iniciarMock).toHaveBeenCalledTimes(1));
    await act(async () => {});

    await user.click(screen.getByRole('button', { name: 'Cancelar' }));

    await waitFor(() => expect(pararLeituraMock).toHaveBeenCalled());
    expect(fetchMock).not.toHaveBeenCalled();
    expect(screen.getByTestId('location').textContent).toBe('/');
    // Review pass (Intent Alignment Auditor): "incapaz de reconhecer o
    // código" (AC2) não produz um erro discreto do decoder numa leitura
    // contínua — Cancelar é o jeito real de desistir, e também devolve o
    // foco à busca, mesmo padrão das falhas "duras".
    await waitFor(() => expect(aoFalharLeitura).toHaveBeenCalledTimes(1));
  });

  it('desmontar o componente durante a leitura chama parar()', async () => {
    const user = userEvent.setup();
    const { unmount } = render(
      <MemoryRouter>
        <ScannerProdutoFab aoFalharLeitura={vi.fn()} />
      </MemoryRouter>,
    );
    await user.click(screen.getByRole('button', { name: abrirCameraLabel }));
    await waitFor(() => expect(iniciarMock).toHaveBeenCalledTimes(1));
    await act(async () => {});

    unmount();

    expect(pararLeituraMock).toHaveBeenCalled();
  });
});

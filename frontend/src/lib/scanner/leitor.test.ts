import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { criarLeitorCodigo } from './leitor';

/**
 * Mock de `@zxing/browser` (Story 4.5, spec-4-5) — jsdom não tem
 * `getUserMedia` nem um decoder real. `decodeFromConstraintsMock` captura o
 * callback de decodificação e as constraints passadas, e devolve um objeto
 * `controls` cujo `.stop()` é um spy, para que os testes exerçam:
 *   - o encaminhamento `result.getText()` -> `aoLer(texto)`;
 *   - `parar()` -> `controls.stop()` + parada de todas as tracks de
 *     `video.srcObject`.
 */
const decodeFromConstraintsMock = vi.hoisted(() => vi.fn());
const stopControlsMock = vi.hoisted(() => vi.fn());

vi.mock('@zxing/browser', () => ({
  BrowserMultiFormatReader: class {
    decodeFromConstraints = decodeFromConstraintsMock;
  },
}));

function criarVideoComTracks(qtdTracks: number) {
  const tracks = Array.from({ length: qtdTracks }, () => ({ stop: vi.fn() }));
  const video = document.createElement('video');
  Object.defineProperty(video, 'srcObject', {
    writable: true,
    value: { getTracks: () => tracks },
  });
  return { video, tracks };
}

beforeEach(() => {
  decodeFromConstraintsMock.mockReset();
  stopControlsMock.mockReset();
  decodeFromConstraintsMock.mockResolvedValue({ stop: stopControlsMock });
});

afterEach(() => {
  vi.clearAllMocks();
});

describe('criarLeitorCodigo — iniciar', () => {
  it('abre a câmera traseira (facingMode: environment) e liga o callback ao <video> dado', async () => {
    const { video } = criarVideoComTracks(1);
    const aoLer = vi.fn();

    await criarLeitorCodigo().iniciar(video, aoLer);

    expect(decodeFromConstraintsMock).toHaveBeenCalledTimes(1);
    const [constraints, videoArg] = decodeFromConstraintsMock.mock.calls[0];
    expect(constraints).toEqual({ video: { facingMode: 'environment' } });
    expect(videoArg).toBe(video);
  });

  it('cada quadro decodificado chama aoLer com result.getText()', async () => {
    const { video } = criarVideoComTracks(1);
    const aoLer = vi.fn();

    await criarLeitorCodigo().iniciar(video, aoLer);
    const callback = decodeFromConstraintsMock.mock.calls[0][2] as (r: unknown) => void;

    callback({ getText: () => 'CAB-004' });
    callback({ getText: () => 'PAR-001' });

    expect(aoLer).toHaveBeenNthCalledWith(1, 'CAB-004');
    expect(aoLer).toHaveBeenNthCalledWith(2, 'PAR-001');
  });

  it('um callback sem resultado (só erro de "não decodificou este quadro") não chama aoLer', async () => {
    const { video } = criarVideoComTracks(1);
    const aoLer = vi.fn();

    await criarLeitorCodigo().iniciar(video, aoLer);
    const callback = decodeFromConstraintsMock.mock.calls[0][2] as (r: unknown) => void;

    callback(undefined);

    expect(aoLer).not.toHaveBeenCalled();
  });

  it('propaga a rejeição de getUserMedia (não engole o erro)', async () => {
    const { video } = criarVideoComTracks(0);
    const erro = Object.assign(new Error('denied'), { name: 'NotAllowedError' });
    decodeFromConstraintsMock.mockRejectedValueOnce(erro);

    await expect(criarLeitorCodigo().iniciar(video, vi.fn())).rejects.toBe(erro);
  });
});

describe('criarLeitorCodigo — parar', () => {
  it('para os controls do zxing E todas as tracks de video.srcObject, e zera srcObject', async () => {
    const { video, tracks } = criarVideoComTracks(2);

    const leitura = await criarLeitorCodigo().iniciar(video, vi.fn());
    leitura.parar();

    expect(stopControlsMock).toHaveBeenCalledTimes(1);
    for (const track of tracks) {
      expect(track.stop).toHaveBeenCalledTimes(1);
    }
    expect(video.srcObject).toBeNull();
  });

  it('é idempotente — uma segunda chamada não repara nada', async () => {
    const { video, tracks } = criarVideoComTracks(1);

    const leitura = await criarLeitorCodigo().iniciar(video, vi.fn());
    leitura.parar();
    leitura.parar();

    expect(stopControlsMock).toHaveBeenCalledTimes(1);
    expect(tracks[0].stop).toHaveBeenCalledTimes(1);
  });
});

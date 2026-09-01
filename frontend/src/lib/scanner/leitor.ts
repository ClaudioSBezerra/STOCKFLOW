/**
 * Wrapper de leitura de QR Code / código de barras (Story 4.5, spec-4-5,
 * FR-35) sobre `@zxing/browser`. Isolado num módulo próprio por dois
 * motivos:
 *
 * 1. O `await import('@zxing/browser')` acontece dentro de `iniciar()` — o
 *    bundle do decoder (grande) só é baixado quando o Usuário toca o FAB do
 *    scanner, nunca no carregamento inicial do Catálogo.
 * 2. `ScannerProdutoFab` fala só com esta interface pequena
 *    (`criarLeitorCodigo`/`LeituraAtiva`), então os testes do componente
 *    mockam toda a mecânica de câmera com `vi.mock('@/lib/scanner/leitor')`
 *    — mesmo espírito do wrapper `lib/realtime/client.ts`.
 *
 * `iniciar` NÃO engole o erro de `getUserMedia`: contexto inseguro,
 * permissão negada, ausência de câmera ou constraint impossível fazem a
 * promise REJEITAR (um `DOMException` com `name` significativo), e quem
 * chama (`ScannerProdutoFab`) mapeia `err.name` para a mensagem certa.
 *
 * `parar()` é idempotente e encerra as duas pontas: os `controls` do zxing
 * (para o loop de decodificação) E todas as `MediaStreamTrack` presas em
 * `video.srcObject` (apaga o indicador de câmera ligada do dispositivo).
 */

export interface LeituraAtiva {
  parar(): void;
}

export interface LeitorCodigo {
  iniciar(video: HTMLVideoElement, aoLer: (texto: string) => void): Promise<LeituraAtiva>;
}

function pararTracksDoVideo(video: HTMLVideoElement): void {
  const fonte = video.srcObject;
  if (fonte && typeof (fonte as MediaStream).getTracks === 'function') {
    for (const track of (fonte as MediaStream).getTracks()) {
      track.stop();
    }
  }
  video.srcObject = null;
}

export function criarLeitorCodigo(): LeitorCodigo {
  return {
    async iniciar(video, aoLer) {
      const { BrowserMultiFormatReader } = await import('@zxing/browser');
      const leitor = new BrowserMultiFormatReader();

      // decodeFromConstraints abre a câmera traseira (`facingMode:
      // 'environment'`) via getUserMedia e chama o callback a cada quadro
      // decodificado com sucesso. A rejeição de getUserMedia propaga para
      // fora deste `await` — nunca é capturada aqui.
      const controls = await leitor.decodeFromConstraints(
        { video: { facingMode: 'environment' } },
        video,
        (resultado) => {
          if (resultado) {
            aoLer(resultado.getText());
          }
        },
      );

      let parado = false;
      return {
        parar() {
          if (parado) return;
          parado = true;
          controls.stop();
          pararTracksDoVideo(video);
        },
      };
    },
  };
}

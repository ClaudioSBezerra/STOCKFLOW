import { useCallback, useEffect, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { ScanLine } from 'lucide-react';
import { toast } from 'sonner';
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog';
import { getAccessToken } from '@/lib/session';
import { criarLeitorCodigo, type LeituraAtiva } from '@/lib/scanner/leitor';

/**
 * `fab-scanner` (Story 4.5, spec-4-5, FR-35, UX-DR4) — botão flutuante do
 * Catálogo que abre a câmera em tela cheia, decodifica um QR Code / código
 * de barras físico com `@zxing/browser` (carregado sob demanda em
 * `@/lib/scanner/leitor`), resolve o valor lido pelo endpoint de match exato
 * `GET /api/produtos/por-codigo?codigo=<valor>` e navega para
 * `/produtos/{id}` (detalhe da Story 4.4).
 *
 * O FAB só existe onde este componente é montado (hoje: `CatalogoPage`) —
 * satisfaz "nunca em telas administrativas" sem lógica de rota. Quando a
 * tela de Carrinho (outro épico) existir, ela monta o mesmo componente
 * trocando o `navigate` por um callback de adicionar-ao-carrinho.
 *
 * Toda falha (contexto inseguro, permissão negada, ausência de câmera,
 * hardware, código não reconhecido) cai numa mensagem clara e específica
 * (`toast.error`, `aria-live` nativo do `Toaster` global) e devolve o foco
 * ao campo de busca por texto (Story 4.1) via `aoFalharLeitura()` — chamado
 * de dentro de `onCloseAutoFocus` do `Dialog`, o único momento em que a
 * `FocusScope` do Radix já soltou o resto da página; chamar `.focus()`
 * antes disso (ex. direto em `falhar()`) é ignorado pelo navegador, porque
 * o restante da árvore ainda está `aria-hidden`/preso no `Dialog` enquanto
 * ele não terminou de desmontar. O scanner nunca é a única forma de achar
 * um Produto.
 *
 * `window.isSecureContext` é checado ANTES de qualquer `getUserMedia`: em
 * HTTP puro (`http://<ip-da-lan>` local) a mensagem de HTTPS é distinta das
 * falhas de permissão/hardware e `criarLeitorCodigo` nunca é chamado.
 *
 * O leitor é iniciado por um callback `ref` no `<video>` (não por um efeito
 * reagindo ao estado de abertura): o `<video>` vive dentro do portal do
 * `Dialog`, que pode montar depois do commit que abre a câmera — o callback
 * `ref` dispara exatamente quando o nó existe. O mesmo callback, recebendo
 * `null` no desmonte do `<video>` (Cancelar, `Esc`, troca de rota),
 * encerra a câmera (`parar()`).
 */

const MSG_HTTPS =
  'O scanner exige uma conexão segura (HTTPS). A câmera do navegador não está disponível aqui.';
const MSG_PERMISSAO =
  'Permissão de câmera negada. Libere o acesso à câmera nas configurações do navegador ou use a busca por texto.';
const MSG_SEM_CAMERA = 'Nenhuma câmera disponível. Use a busca por texto para encontrar o produto.';
const MSG_CAMERA_GENERICA = 'Não foi possível abrir a câmera. Use a busca por texto.';
const MSG_CODIGO_NAO_RECONHECIDO =
  'Código não reconhecido: nenhum produto com esse código. Use a busca por texto.';
const MSG_ABRIR_PRODUTO_FALHOU = 'Não foi possível abrir o produto agora. Tente novamente.';

function authHeaders(): Record<string, string> {
  const token = getAccessToken();
  return token ? { Authorization: `Bearer ${token}` } : {};
}

function nomeDoErro(err: unknown): string {
  if (err instanceof DOMException) return err.name;
  if (typeof err === 'object' && err !== null && 'name' in err) {
    return String((err as { name: unknown }).name);
  }
  return '';
}

function mensagemDeFalhaDaCamera(err: unknown): string {
  const nome = nomeDoErro(err);
  if (nome === 'NotAllowedError' || nome === 'SecurityError') return MSG_PERMISSAO;
  if (nome === 'NotFoundError' || nome === 'OverconstrainedError' || nome === 'DevicesNotFoundError') {
    return MSG_SEM_CAMERA;
  }
  return MSG_CAMERA_GENERICA;
}

interface ScannerProdutoFabProps {
  aoFalharLeitura: () => void;
}

export function ScannerProdutoFab({ aoFalharLeitura }: ScannerProdutoFabProps) {
  const navigate = useNavigate();
  const [cameraAberta, setCameraAberta] = useState(false);
  const leituraRef = useRef<LeituraAtiva | null>(null);
  // cancelarInicioRef marca uma chamada de `iniciar` ainda pendente para ser
  // descartada (o `<video>` desmontou antes de a câmera abrir).
  const cancelarInicioRef = useRef<(() => void) | null>(null);
  // resolvendoRef guarda contra disparo duplo: `@zxing/browser` chama o
  // callback a cada quadro decodificado — o primeiro código lido vence, os
  // seguintes são ignorados até uma nova abertura do scanner.
  const resolvendoRef = useRef(false);
  // devolverFocoRef marca que o fechamento em andamento NÃO veio de uma
  // decodificação bem-sucedida (falha "dura" — permissão/hardware/código
  // não reconhecido — OU "Cancelar"/Esc manual) — consumido por
  // `onCloseAutoFocus`, o único momento em que a `FocusScope` do Radix já
  // soltou o resto da página (`aria-hidden`) e um `.focus()` no campo de
  // busca realmente "pega". Chamar `aoFalharLeitura()` direto em
  // `falhar()`/`fecharCamera()` (antes do Dialog desmontar) não funciona: a
  // página ainda está marcada `aria-hidden`/presa no `FocusScope` enquanto
  // `cameraAberta` não terminou de virar `false` no React.
  const devolverFocoRef = useRef(false);

  const parar = useCallback(() => {
    cancelarInicioRef.current?.();
    cancelarInicioRef.current = null;
    leituraRef.current?.parar();
    leituraRef.current = null;
  }, []);

  const falhar = useCallback(
    (msg: string) => {
      parar();
      devolverFocoRef.current = true;
      setCameraAberta(false);
      toast.error(msg);
    },
    [parar],
  );

  const resolverCodigo = useCallback(
    async (texto: string) => {
      if (resolvendoRef.current) return;
      resolvendoRef.current = true;
      parar();
      setCameraAberta(false);
      try {
        const res = await fetch(
          `/api/produtos/por-codigo?codigo=${encodeURIComponent(texto.trim())}`,
          { headers: authHeaders() },
        );
        if (res.ok) {
          const dados = (await res.json()) as { produto: { id: string } };
          navigate(`/produtos/${dados.produto.id}`);
          return;
        }
        if (res.status === 404) {
          falhar(MSG_CODIGO_NAO_RECONHECIDO);
          return;
        }
        falhar(MSG_ABRIR_PRODUTO_FALHOU);
      } catch {
        falhar(MSG_ABRIR_PRODUTO_FALHOU);
      }
    },
    [navigate, parar, falhar],
  );

  // Callback `ref` do `<video>`: `node` presente -> abre a câmera e liga o
  // decoder; `node === null` (desmonte do `<video>`) -> encerra a câmera.
  const aoMontarVideo = useCallback(
    (node: HTMLVideoElement | null) => {
      if (!node) {
        parar();
        return;
      }
      let vivo = true;
      cancelarInicioRef.current = () => {
        vivo = false;
      };
      criarLeitorCodigo()
        .iniciar(node, (texto) => {
          void resolverCodigo(texto);
        })
        .then((ativa) => {
          if (!vivo) {
            ativa.parar();
            return;
          }
          leituraRef.current = ativa;
        })
        .catch((err: unknown) => {
          if (!vivo) return;
          falhar(mensagemDeFalhaDaCamera(err));
        });
    },
    [parar, resolverCodigo, falhar],
  );

  // Desmontar o componente também encerra a câmera (spec-4-5, matriz:
  // "Cancelar / desmontar durante leitura").
  useEffect(() => () => parar(), [parar]);

  function aoTocarFab() {
    if (!window.isSecureContext) {
      // A câmera nunca chega a abrir aqui — não existe `Dialog`/`FocusScope`
      // prendendo o resto da página, então `aoFalharLeitura()` direto
      // funciona (ao contrário do caminho de `falhar()`, que precisa
      // esperar `onCloseAutoFocus` — ver comentário de `devolverFocoRef`).
      toast.error(MSG_HTTPS);
      aoFalharLeitura();
      return;
    }
    resolvendoRef.current = false;
    setCameraAberta(true);
  }

  function fecharCamera() {
    // "Cancelar"/Esc também devolve o foco à busca (review pass, Intent
    // Alignment Auditor): AC2 lista "incapaz de reconhecer o código" como
    // motivo de falha ao lado de permissão/hardware, mas uma leitura
    // ótica que nunca reconhece nada não produz um evento de erro discreto
    // do decoder — o único jeito de "desistir" hoje é fechar a câmera
    // manualmente. Tratar esse fechamento como qualquer outra falha (foco
    // de volta ao campo de busca) cobre esse caso sem custo nem risco: o
    // scanner nunca deixa o Usuário sem um caminho óbvio de volta à busca
    // por texto.
    parar();
    devolverFocoRef.current = true;
    setCameraAberta(false);
  }

  return (
    <>
      <button
        type="button"
        onClick={aoTocarFab}
        aria-label="Escanear código do produto"
        className="fixed right-fab-margin bottom-fab-offset-mobile z-40 flex size-fab-size items-center justify-center rounded-full bg-primary text-primary-foreground shadow-lg md:bottom-fab-margin"
      >
        <ScanLine aria-hidden="true" className="size-6" />
      </button>

      <Dialog
        open={cameraAberta}
        onOpenChange={(aberta) => {
          if (!aberta) fecharCamera();
        }}
      >
        <DialogContent
          onCloseAutoFocus={(event) => {
            // `devolverFocoRef` (setado em `falhar()` E em `fecharCamera()`
            // — toda saída da câmera SEM sucesso) sobrescreve o foco
            // pós-fechamento padrão do Radix (que devolveria ao próprio
            // FAB, o gatilho): o campo de busca (Story 4.1) recebe o foco
            // em vez disso. Só o caminho de SUCESSO (a navegação leva
            // embora) deixa esse ref como está (`false`) e o Radix restaura
            // o foco normalmente — sem efeito prático, já que a rota muda.
            if (devolverFocoRef.current) {
              devolverFocoRef.current = false;
              event.preventDefault();
              aoFalharLeitura();
            }
          }}
          className="flex h-full max-h-none w-full max-w-none flex-col gap-4 rounded-none border-0 bg-background p-6 sm:max-w-none"
        >
          <DialogTitle>Escanear código do produto</DialogTitle>
          <video
            ref={aoMontarVideo}
            playsInline
            muted
            autoPlay
            aria-label="Câmera do scanner"
            className="min-h-0 w-full flex-1 rounded-md bg-black object-cover"
          />
          <p className="text-body text-muted-foreground">
            Aponte a câmera para o QR Code ou o código de barras do produto.
          </p>
          <button
            type="button"
            onClick={fecharCamera}
            className="min-h-touch-target-min self-end rounded-md border border-border px-4"
          >
            Cancelar
          </button>
        </DialogContent>
      </Dialog>
    </>
  );
}

export default ScannerProdutoFab;

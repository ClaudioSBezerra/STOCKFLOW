import { useState } from 'react';
import { CircleHelp } from 'lucide-react';
import { toast } from 'sonner';
import { cn } from '@/lib/utils';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog';
import { useAuth } from '@/lib/auth';
import { buscarManual, buscarManualPDFBlob, type SecaoManual } from '@/lib/manual';

const touchTarget = 'min-h-touch-target-min min-w-touch-target-min';

const MENSAGEM_ERRO_CARREGAR = 'Não foi possível carregar o manual agora. Tente novamente em instantes.';

/**
 * "Ajuda" (feedback Ferreira Costa, direção, 2026-10-22): deixa de ser um
 * diálogo estático (Story 17.1) e passa a carregar o Manual Online de
 * acordo com o papel de quem está logado — `GET /api/manual`, filtrado pelo
 * servidor (`services.SecoesManualParaPapel`); o `adm` recebe o manual
 * completo e ganha o botão "Baixar manual em PDF" (`GET /api/manual/pdf`,
 * molde EXATO de "Baixar recibo" em FilaPedidosSection: `Blob` ->
 * `URL.createObjectURL` -> `<a download>` criado/clicado/removido ->
 * `URL.revokeObjectURL`, sem tela intermediária).
 *
 * Busca as seções só na PRIMEIRA abertura do Dialog nesta montagem (nunca a
 * cada clique) — `secoes` fica em cache local enquanto o componente não
 * desmonta; fechar e reabrir o Dialog não refaz o GET.
 */
export function ManualDialog() {
  const { usuario } = useAuth();
  const ehAdm = usuario?.papel === 'adm';

  const [aberto, setAberto] = useState(false);
  const [secoes, setSecoes] = useState<SecaoManual[] | null>(null);
  const [carregando, setCarregando] = useState(false);
  const [erro, setErro] = useState<string | null>(null);
  const [baixandoPDF, setBaixandoPDF] = useState(false);

  async function abrir(abrindo: boolean) {
    setAberto(abrindo);
    if (!abrindo || secoes !== null || carregando) {
      return;
    }
    setCarregando(true);
    setErro(null);
    try {
      const dados = await buscarManual();
      setSecoes(dados);
    } catch {
      setErro(MENSAGEM_ERRO_CARREGAR);
    } finally {
      setCarregando(false);
    }
  }

  async function baixarPDF() {
    if (baixandoPDF) {
      return;
    }
    setBaixandoPDF(true);
    try {
      const blob = await buscarManualPDFBlob();
      const href = URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = href;
      link.download = 'manual-stockflow.pdf';
      document.body.appendChild(link);
      link.click();
      link.remove();
      URL.revokeObjectURL(href);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Não foi possível gerar o manual em PDF agora.');
    } finally {
      setBaixandoPDF(false);
    }
  }

  return (
    <Dialog open={aberto} onOpenChange={(v) => void abrir(v)}>
      <DialogTrigger asChild>
        <Button variant="ghost" className={cn('gap-2', touchTarget)}>
          <CircleHelp className="size-5" aria-hidden="true" />
          Ajuda
        </Button>
      </DialogTrigger>
      <DialogContent className="flex max-h-[85vh] flex-col sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>Manual do stockflow</DialogTitle>
          <DialogDescription>
            {ehAdm
              ? 'Manual completo — como administrador, você vê todas as seções.'
              : 'Seções disponíveis para o seu papel de acesso.'}
          </DialogDescription>
        </DialogHeader>

        {ehAdm && secoes !== null && (
          <Button
            type="button"
            variant="outline"
            className="self-start"
            disabled={baixandoPDF}
            onClick={() => void baixarPDF()}
          >
            {baixandoPDF ? 'Gerando PDF...' : 'Baixar manual em PDF'}
          </Button>
        )}

        <div className="flex-1 overflow-y-auto">
          {carregando && <p className="text-body text-muted-foreground">Carregando manual...</p>}
          {erro && (
            <p role="alert" className="text-body text-destructive">
              {erro}
            </p>
          )}
          {secoes && (
            <div className="flex flex-col gap-6">
              {secoes.map((secao) => (
                <section key={secao.id} className="flex flex-col gap-2">
                  <h3 className="text-heading-md">{secao.titulo}</h3>
                  {secao.paragrafos.map((paragrafo, indice) => (
                    <p key={indice} className="text-body text-muted-foreground">
                      {paragrafo}
                    </p>
                  ))}
                </section>
              ))}
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}

export default ManualDialog;

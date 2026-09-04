import { useRef } from 'react';
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog';

export interface ConfirmDialogProps {
  /** Controla a visibilidade do diálogo. */
  open: boolean;
  /**
   * Chamado quando o diálogo muda de estado aberto/fechado (ex.: `Esc`,
   * clique fora, confirmar ou cancelar). Obrigatório — o diálogo é
   * totalmente controlado por `open`, então sem isto o caller nunca fecha
   * o diálogo em resposta a `Esc`/clique fora.
   */
  onOpenChange: (open: boolean) => void;
  /** Chamado exatamente uma vez quando o usuário confirma a ação. */
  onConfirm: () => void;
  /** Chamado quando o usuário cancela ou fecha o diálogo sem confirmar. */
  onCancel?: () => void;
  title: string;
  description?: string;
  confirmLabel?: string;
  cancelLabel?: string;
  /**
   * Variante visual do botão de confirmar. `'destructive'` para ações
   * irreversíveis/destrutivas (anonimização, exclusão). Default `'default'`
   * — retrocompatível: nenhum caller existente muda de comportamento.
   */
  confirmVariant?: 'default' | 'destructive';
}

/**
 * Wrapper reutilizável sobre o `AlertDialog` do shadcn para toda confirmação
 * de ação destrutiva/irreversível do stockflow — nunca `window.confirm()`
 * (ver DESIGN.md Do's and Don'ts, EXPERIENCE.md Interaction Primitives).
 */
export function ConfirmDialog({
  open,
  onOpenChange,
  onConfirm,
  onCancel,
  title,
  description,
  confirmLabel = 'Confirmar',
  cancelLabel = 'Cancelar',
  confirmVariant = 'default',
}: ConfirmDialogProps) {
  // `AlertDialogAction` é um `DialogPrimitive.Close` por baixo — clicar em
  // "Confirmar" também dispara `onOpenChange(false)` no `AlertDialog`. Sem
  // este ref, esse fechamento seria indistinguível de um cancelamento real
  // (Esc/clique fora/"Cancelar") e `onCancel` dispararia em toda confirmação.
  const confirmedRef = useRef(false);
  // `AlertDialogCancel` também é um `DialogPrimitive.Close`: um duplo clique
  // rápido antes do diálogo fechar pode disparar `onOpenChange(false)` mais
  // de uma vez para o mesmo fechamento (mesma classe de bug já corrigida
  // para "Confirmar"). Este guard processa só a primeira transição para
  // fechado, e é rearmado quando o diálogo reabre.
  const closeHandledRef = useRef(false);

  return (
    <AlertDialog
      open={open}
      onOpenChange={(next) => {
        if (next) {
          closeHandledRef.current = false;
        } else if (!closeHandledRef.current) {
          closeHandledRef.current = true;
          if (confirmedRef.current) {
            confirmedRef.current = false;
          } else {
            onCancel?.();
          }
        }
        onOpenChange(next);
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{title}</AlertDialogTitle>
          {description ? (
            <AlertDialogDescription>{description}</AlertDialogDescription>
          ) : null}
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel className="min-h-touch-target-min min-w-touch-target-min">
            {cancelLabel}
          </AlertDialogCancel>
          <AlertDialogAction
            variant={confirmVariant}
            className="min-h-touch-target-min min-w-touch-target-min"
            onClick={() => {
              // Guarda contra duplo clique disparando `onConfirm` mais de
              // uma vez antes do diálogo fechar, e restaura o ref se
              // `onConfirm` lançar, para não mascarar um cancelamento real
              // subsequente.
              if (confirmedRef.current) return;
              confirmedRef.current = true;
              try {
                onConfirm();
              } catch (error) {
                confirmedRef.current = false;
                throw error;
              }
            }}
          >
            {confirmLabel}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

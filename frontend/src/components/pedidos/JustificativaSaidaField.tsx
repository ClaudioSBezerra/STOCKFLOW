import { Label } from '@/components/ui/label';
import { MAX_JUSTIFICATIVA_SAIDA, MENSAGEM_JUSTIFICATIVA_LONGA } from '@/lib/motivosSaida';

interface JustificativaSaidaFieldProps {
  id: string;
  valor: string;
  onChange: (valor: string) => void;
  placeholder?: string;
}

/** True quando a justificativa (após trim) passa do limite do servidor. */
export function justificativaLonga(valor: string): boolean {
  return Array.from(valor.trim()).length > MAX_JUSTIFICATIVA_SAIDA;
}

/**
 * Campo obrigatório "Justificativa" do Pedido de Outras Saídas, com limite de
 * 1000 caracteres (`maxLength`) e contador "N/1000" — mesmas regras e
 * mensagem do servidor.
 */
export function JustificativaSaidaField({ id, valor, onChange, placeholder }: JustificativaSaidaFieldProps) {
  return (
    <div className="flex flex-col gap-2">
      <Label htmlFor={id}>Justificativa</Label>
      <textarea
        id={id}
        required
        maxLength={MAX_JUSTIFICATIVA_SAIDA}
        placeholder={placeholder}
        className="min-h-16 w-full min-w-0 rounded-md border border-input bg-transparent px-3 py-2 text-base shadow-xs outline-none placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 md:text-sm dark:bg-input/30"
        value={valor}
        onChange={(event) => onChange(event.target.value)}
      />
      <p className="text-label tabular-nums text-muted-foreground" data-testid={`${id}-contador`}>
        {Array.from(valor).length}/{MAX_JUSTIFICATIVA_SAIDA}
      </p>
      {justificativaLonga(valor) && (
        <p role="alert" className="text-body text-destructive">
          {MENSAGEM_JUSTIFICATIVA_LONGA}
        </p>
      )}
    </div>
  );
}

export default JustificativaSaidaField;

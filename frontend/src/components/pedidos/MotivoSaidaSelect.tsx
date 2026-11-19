import { Label } from '@/components/ui/label';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { MOTIVOS_SAIDA, type MotivoSaida } from '@/lib/motivosSaida';

interface MotivoSaidaSelectProps {
  /** Prefixo de id (a mesma tela pode ter mais de um formulário). */
  idBase: string;
  valor: MotivoSaida | '';
  onChange: (valor: MotivoSaida) => void;
}

/**
 * Campo obrigatório "Motivo da saída" do Pedido de Outras Saídas
 * (spec-pedido-outras-saidas) — usado na tela própria e no diálogo
 * "Registrar baixa" do Detalhe do Produto.
 */
export function MotivoSaidaSelect({ idBase, valor, onChange }: MotivoSaidaSelectProps) {
  return (
    <div className="flex flex-col gap-2">
      <Label htmlFor={`${idBase}-motivo`}>Motivo da saída</Label>
      <Select value={valor} onValueChange={(v) => onChange(v as MotivoSaida)}>
        <SelectTrigger id={`${idBase}-motivo`} aria-label="Motivo da saída">
          <SelectValue placeholder="Selecione o motivo" />
        </SelectTrigger>
        <SelectContent>
          {MOTIVOS_SAIDA.map((m) => (
            <SelectItem key={m.valor} value={m.valor}>
              {m.rotulo}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <p className="text-label text-muted-foreground">
        O motivo sai em destaque no Termo de Baixa, impresso quando o Gestor aprova.
      </p>
    </div>
  );
}

export default MotivoSaidaSelect;

import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { UNIDADES, type DimensaoEstado } from '@/lib/produtos';

export function DimensaoField({
  label,
  idPrefix,
  dimensao,
  onChange,
}: {
  label: string;
  idPrefix: string;
  dimensao: DimensaoEstado;
  onChange: (d: DimensaoEstado) => void;
}) {
  return (
    <div className="flex flex-col gap-2">
      <Label htmlFor={`${idPrefix}-valor`}>{label}</Label>
      <div className="flex gap-2">
        <Input
          id={`${idPrefix}-valor`}
          type="number"
          inputMode="decimal"
          value={dimensao.valor}
          onChange={(event) => onChange({ ...dimensao, valor: event.target.value })}
          className="flex-1"
        />
        <Select
          value={dimensao.unidade}
          onValueChange={(unidade) => onChange({ ...dimensao, unidade })}
        >
          <SelectTrigger id={`${idPrefix}-unidade`} aria-label={`Unidade de ${label}`} className="w-20">
            <SelectValue placeholder="Un." />
          </SelectTrigger>
          <SelectContent>
            {UNIDADES.map((unidade) => (
              <SelectItem key={unidade} value={unidade}>
                {unidade}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
    </div>
  );
}


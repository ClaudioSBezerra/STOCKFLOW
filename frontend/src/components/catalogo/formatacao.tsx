import { PackageCheck, PackageX } from 'lucide-react';

/**
 * Formatação compartilhada do Catálogo (Story 4.3, spec-4-3; extraída para cá
 * pela Story 4.4, spec-4-4, sem mudança de comportamento) — reusada por
 * `CatalogoListagem.tsx` (grade/tabela) e `ProdutoDetalhePage.tsx` (Story
 * 4.4), evitando divergir a mesma formatação em dois lugares (a Story 4.3 já
 * corrigiu uma divergência equivalente entre grade e tabela).
 */

export interface DimensaoValor {
  valor: number;
  unidade: string;
}

export interface Dimensoes {
  comprimento: DimensaoValor | null;
  largura: DimensaoValor | null;
  diametro: DimensaoValor | null;
  altura: DimensaoValor | null;
  espessura: DimensaoValor | null;
}

const ROTULO_DIMENSAO: Record<keyof Dimensoes, string> = {
  comprimento: 'C',
  largura: 'L',
  diametro: '⌀',
  altura: 'A',
  espessura: 'E',
};

// resumirDimensoes monta o texto curto das dimensões preenchidas ("C 6m · ⌀
// 10cm"); todas nulas -> "—". O valor usa a mesma formatação pt-BR de
// formatarQuantidade (decimal com vírgula), para não misturar "6,5" e "6.5m"
// na mesma tela.
export function resumirDimensoes(dimensoes: Dimensoes): string {
  const partes = (Object.keys(ROTULO_DIMENSAO) as (keyof Dimensoes)[])
    .map((chave) => {
      const dim = dimensoes[chave];
      return dim ? `${ROTULO_DIMENSAO[chave]} ${formatarQuantidade(dim.valor)}${dim.unidade}` : null;
    })
    .filter((parte): parte is string => parte !== null);
  return partes.length > 0 ? partes.join(' · ') : '—';
}

// formatarQuantidade serializa a quantidade para exibição. O backend manda um
// NUMERIC(10,3) já decodificado como number pelo JSON.parse, então `15.000`
// chega como `15`; `toLocaleString('pt-BR')` só agrupa milhar quando houver.
export function formatarQuantidade(valor: number): string {
  return valor.toLocaleString('pt-BR');
}

export function IndicadorDisponibilidade({ disponivel }: { disponivel: boolean }) {
  const Icone = disponivel ? PackageCheck : PackageX;
  return (
    <span
      data-status={disponivel ? 'disponivel' : 'sem-estoque'}
      className={`inline-flex items-center gap-1 text-label ${
        disponivel ? 'text-success' : 'text-muted-foreground'
      }`}
    >
      <Icone aria-hidden="true" className="h-4 w-4" />
      {disponivel ? 'Disponível' : 'Sem estoque'}
    </span>
  );
}

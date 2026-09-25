import type { ReactNode } from 'react';

/**
 * FaixaIndicadores — faixa de KPIs do padrão de página de lista (Story 17.3).
 *
 * Componente de display puro: recebe `indicadores` (array de {rotulo, valor,
 * alerta?}) e o slot `acoes` (botões Exportar/Cadastrar), nunca faz fetch.
 *
 * Regras:
 *  - `valor === null` → exibe "—" (quando o fetch de indicadores falhou).
 *  - `alerta === true && valor > 0` → valor em `text-destructive` (vermelho FC).
 *  - `acoes` fica à direita (flex justify-between).
 */

interface Indicador {
  rotulo: string;
  valor: number | null;
  alerta?: boolean;
}

interface FaixaIndicadoresProps {
  indicadores: Indicador[];
  acoes?: ReactNode;
}

export function FaixaIndicadores({ indicadores, acoes }: FaixaIndicadoresProps) {
  return (
    <div className="flex items-center justify-between border-b border-border pb-3">
      <div className="flex gap-6">
        {indicadores.map((ind) => {
          const valorTexto = ind.valor === null ? '—' : String(ind.valor);
          const destrutivo = ind.alerta === true && ind.valor !== null && ind.valor > 0;
          return (
            <div key={ind.rotulo} className="flex flex-col">
              <span className="text-label text-muted-foreground">{ind.rotulo}</span>
              <span
                className={`text-heading-lg font-bold${destrutivo ? ' text-destructive' : ''}`}
              >
                {valorTexto}
              </span>
            </div>
          );
        })}
      </div>
      {acoes && <div className="flex gap-2">{acoes}</div>}
    </div>
  );
}

export default FaixaIndicadores;

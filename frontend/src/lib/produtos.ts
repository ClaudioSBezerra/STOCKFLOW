// Constantes/helpers compartilhados entre o cadastro e a edição de Produto.

export interface DimensaoEstado {
  valor: string;
  unidade: string;
}

export const DIMENSAO_VAZIA: DimensaoEstado = { valor: '', unidade: '' };
export const UNIDADES = ['mm', 'cm', 'm'] as const;

// UNIDADES_MEDIDA (Story 10.3, spec-10-3, addendum.md §F): os 12 valores do
// enum fechado `unidade_medida_produto` (migration 000038), mesma grafia e
// ordem do backend (`unidadesMedidaValidas`, backend/services/produtos.go) —
// inclusive os 3 com caracteres não-ASCII ("m²", "m³", "kg/m²").
export const UNIDADES_MEDIDA = [
  'un',
  'm',
  'm²',
  'm³',
  'kg',
  'L',
  'cx',
  'rolo',
  'barra',
  'mm',
  'cm',
  'kg/m²',
] as const;

/**
 * Converte o estado local de uma dimensão para o par aceito por
 * `POST /api/produtos`: os dois campos em branco -> `undefined` (chave
 * omitida do JSON, dimensão não informada); só um preenchido -> objeto
 * parcial, deixando o servidor rejeitar citando o campo (AD-9) — o cliente
 * não duplica essa validação.
 */
export function montarDimensao(d: DimensaoEstado): { valor?: number; unidade?: string } | undefined {
  if (d.valor.trim() === '' && d.unidade === '') {
    return undefined;
  }
  const payload: { valor?: number; unidade?: string } = {};
  if (d.valor.trim() !== '') {
    payload.valor = Number(d.valor);
  }
  if (d.unidade !== '') {
    payload.unidade = d.unidade;
  }
  return payload;
}


export function dimensaoParaEstado(d: { valor: number; unidade: string } | null | undefined): DimensaoEstado {
  return d ? { valor: String(d.valor), unidade: d.unidade } : { valor: '', unidade: '' };
}

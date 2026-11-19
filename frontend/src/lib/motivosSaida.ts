/**
 * Motivos de um Pedido de Outras Saídas (`tipo: 'baixa'`) —
 * spec-pedido-outras-saidas. Módulo próprio (sem `fetch`) para telas e
 * componentes importarem sem puxar o cliente HTTP de `lib/pedidos`.
 */
export type MotivoSaida = 'perda_roubo' | 'vencido_avariado' | 'utilizacao_obra';

/** Opções do select (rótulo completo) e rótulo curto das listas. */
export const MOTIVOS_SAIDA: { valor: MotivoSaida; rotulo: string; curto: string }[] = [
  { valor: 'perda_roubo', rotulo: 'Baixa de Estoque por Perda ou Roubo', curto: 'Perda ou roubo' },
  { valor: 'vencido_avariado', rotulo: 'Produto Vencido ou Avariado', curto: 'Vencido ou avariado' },
  { valor: 'utilizacao_obra', rotulo: 'Por Utilização na Obra da Empresa', curto: 'Utilização na obra' },
];

/** Rótulo curto; baixa antiga sem motivo -> "Motivo não informado". */
export function rotuloCurtoMotivo(motivo?: MotivoSaida | null): string {
  return MOTIVOS_SAIDA.find((m) => m.valor === motivo)?.curto ?? 'Motivo não informado';
}

/** Rótulo completo; baixa antiga sem motivo -> "Motivo não informado". */
export function rotuloCompletoMotivo(motivo?: MotivoSaida | null): string {
  return MOTIVOS_SAIDA.find((m) => m.valor === motivo)?.rotulo ?? 'Motivo não informado';
}

/** Limite (em caracteres, após trim) da justificativa — o servidor valida o mesmo. */
export const MAX_JUSTIFICATIVA_SAIDA = 1000;
export const MENSAGEM_JUSTIFICATIVA_LONGA = 'A justificativa deve ter no máximo 1000 caracteres.';

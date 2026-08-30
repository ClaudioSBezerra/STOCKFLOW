import type { Papel } from '@/components/shell/nav-items';

/**
 * Espelho MÍNIMO da regra de promoção de papel do backend
 * (`services.proximoPapelPromocao` / `services.RankPapel`, spec-1-7). A
 * duplicação entre Go e TS é deliberada e documentada — mesmo caso de
 * `rankPapel` (nav-items.ts) e `senha.ts`. A AUTORIDADE é sempre o servidor:
 * `POST /api/promocoes` deriva o alvo do papel atual do solicitante e ignora
 * qualquer valor do cliente. Aqui só decidimos o rótulo do botão e se ele
 * aparece.
 */

const PROXIMO_PAPEL: Partial<Record<Papel, Papel>> = {
  usuario: 'almoxarife',
  almoxarife: 'gestor',
};

/**
 * Papel imediatamente acima na hierarquia (`usuario -> almoxarife`,
 * `almoxarife -> gestor`). `gestor`, `adm` e qualquer valor desconhecido não
 * têm promoção disponível -> `null`.
 */
export function proximoPapel(papel: string): Papel | null {
  return PROXIMO_PAPEL[papel as Papel] ?? null;
}

const PAPEL_ABAIXO: Partial<Record<Papel, Papel>> = {
  gestor: 'almoxarife',
  almoxarife: 'usuario',
};

/**
 * Papel imediatamente abaixo na hierarquia (`gestor -> almoxarife`,
 * `almoxarife -> usuario`) — inverso exato de `proximoPapel`, espelho de
 * `services.papelImediatamenteAbaixo` (spec-1-8). `usuario`, `adm` e qualquer
 * valor desconhecido não têm papel abaixo aplicável a rebaixamento -> `null`.
 * Só decide o rótulo/visibilidade do botão "Rebaixar"; a AUTORIDADE é sempre o
 * servidor, que deriva o alvo do papel atual do alvo e ignora o cliente.
 */
export function papelAbaixo(papel: string): Papel | null {
  return PAPEL_ABAIXO[papel as Papel] ?? null;
}

const ROTULO_PAPEL: Record<Papel, string> = {
  usuario: 'Usuário',
  almoxarife: 'Almoxarife',
  gestor: 'Gestor',
  adm: 'Adm',
};

/** Rótulo humano de um papel; valor desconhecido volta como veio. */
export function rotuloPapel(papel: string): string {
  return ROTULO_PAPEL[papel as Papel] ?? papel;
}

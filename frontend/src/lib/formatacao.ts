/**
 * Formatadores de documentos brasileiros reutilizados pelas telas (Empresas
 * da área do Dono, Filiais...). Funções puras, sem estado.
 */

/**
 * Maiúsculas só para a-z (ASCII): `toUpperCase()` também transformaria 'ß' em
 * 'SS'. Usada ao digitar o CNPJ.
 */
export function maiusculasAscii(valor: string): string {
  return valor.replace(/[a-z]/g, (c) => c.toUpperCase());
}

/**
 * Tira a máscara (". / -" e brancos) e põe a-z em maiúsculas, mantendo o resto
 * (a validação é do servidor; símbolos inválidos NÃO são descartados aqui por
 * `formatarCNPJ`, que devolve a entrada como veio).
 */
export function limparCNPJ(valor: string): string {
  return maiusculasAscii(valor.replace(/[.\/\-\s]/g, ''));
}

/**
 * "12345678000195" -> "12.345.678/0001-95"; "12ABC34501DE35" ->
 * "12.ABC.345/01DE-35"; entrada com símbolo inválido ou outro tamanho volta
 * como veio.
 */
export function formatarCNPJ(cnpj: string): string {
  if (/[^0-9A-Za-z.\/\-\s]/.test(cnpj)) {
    return cnpj;
  }
  const d = limparCNPJ(cnpj);
  if (d.length !== 14) {
    return cnpj;
  }
  return `${d.slice(0, 2)}.${d.slice(2, 5)}.${d.slice(5, 8)}/${d.slice(8, 12)}-${d.slice(12)}`;
}

/** "50780000" -> "50780-000"; outro formato volta como veio. */
export function formatarCEP(cep: string): string {
  const d = cep.replace(/\D/g, '');
  if (d.length !== 8) {
    return cep;
  }
  return `${d.slice(0, 5)}-${d.slice(5)}`;
}

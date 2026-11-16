/**
 * Formatadores de documentos brasileiros reutilizados pelas telas (Empresas
 * da área do Dono, Filiais...). Funções puras, sem estado.
 */

/** "12345678000195" -> "12.345.678/0001-95"; outro formato volta como veio. */
export function formatarCNPJ(cnpj: string): string {
  const d = cnpj.replace(/\D/g, '');
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

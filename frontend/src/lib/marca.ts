/**
 * Nome da marca do ambiente exibido no topo do menu lateral: "Suprimentos" no
 * ambiente da Ferreira Costa (host `suprimentos.*`), "stockflow" nos demais.
 * Função pura — o chamador passa `window.location.hostname`.
 */
export function nomeDaMarca(hostname: string): 'Suprimentos' | 'stockflow' {
  return hostname.toLowerCase().startsWith('suprimentos.') ? 'Suprimentos' : 'stockflow';
}

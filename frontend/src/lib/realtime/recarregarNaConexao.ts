/**
 * Decide se uma tela deve recarregar quando o SSE reporta 'conectado'.
 *
 * As telas carregam na montagem, sem esperar o SSE (proxies podem segurar o
 * `open` do EventSource por ~15s). Por isso o 1º 'conectado' só recarrega
 * quando há motivo:
 * - a última carga falhou (a conexão serve de nova tentativa); ou
 * - a conexão demorou mais que `LIMIAR_CONEXAO_LENTA_MS` desde a montagem —
 *   eventos ocorridos entre o snapshot da carga e a assinatura se perderiam.
 * Reconexões posteriores sempre recarregam (recuperam eventos perdidos).
 */
export const LIMIAR_CONEXAO_LENTA_MS = 2000;

export interface ParametrosRecarregarNaConexao {
  primeiraConexao: boolean;
  msDesdeMontagem: number;
  ultimaCargaFalhou: boolean;
}

export function deveRecarregarNaConexao({
  primeiraConexao,
  msDesdeMontagem,
  ultimaCargaFalhou,
}: ParametrosRecarregarNaConexao): boolean {
  if (!primeiraConexao) return true;
  return ultimaCargaFalhou || msDesdeMontagem > LIMIAR_CONEXAO_LENTA_MS;
}

import { apiUrl, authHeaders } from '@/lib/api';

/**
 * Manual Online (feedback Ferreira Costa, direção, 2026-10-22): `GET
 * /api/manual` devolve só as seções visíveis ao papel de quem chama — o
 * servidor é a autoridade (`services.SecoesManualParaPapel`, mesma regra de
 * `filtrarNavPorPapel`); este módulo nunca filtra de novo no cliente. `GET
 * /api/manual/pdf` devolve o manual COMPLETO em PDF, como `Blob` — molde
 * EXATO de `buscarReciboPedidoBlob` (SEM lógica de DOM; quem cria/clica/
 * remove o `<a download>` é o componente chamador).
 */

export interface SecaoManual {
  id: string;
  titulo: string;
  papelMinimo: string;
  paragrafos: string[];
}

const MENSAGEM_ERRO_MANUAL = 'Não foi possível carregar o manual agora. Tente novamente em instantes.';
const MENSAGEM_ERRO_MANUAL_PDF = 'Não foi possível gerar o manual em PDF agora. Tente novamente em instantes.';

export async function buscarManual(): Promise<SecaoManual[]> {
  const res = await fetch(apiUrl('/api/manual'), { headers: authHeaders() });
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as { error?: { message?: string } };
    throw new Error(body.error?.message ?? MENSAGEM_ERRO_MANUAL);
  }
  const body = (await res.json()) as { secoes: SecaoManual[] };
  return body.secoes ?? [];
}

export async function buscarManualPDFBlob(): Promise<Blob> {
  const res = await fetch(apiUrl('/api/manual/pdf'), { headers: authHeaders() });
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as { error?: { message?: string } };
    throw new Error(body.error?.message ?? MENSAGEM_ERRO_MANUAL_PDF);
  }
  return res.blob();
}

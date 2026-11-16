import { describe, expect, it } from 'vitest';
import { deveRecarregarNaConexao, LIMIAR_CONEXAO_LENTA_MS } from './recarregarNaConexao';

describe('deveRecarregarNaConexao', () => {
  it('1ª conexão rápida com carga ok não recarrega', () => {
    expect(
      deveRecarregarNaConexao({ primeiraConexao: true, msDesdeMontagem: 300, ultimaCargaFalhou: false }),
    ).toBe(false);
  });

  it('exatamente no limiar ainda não recarrega; acima dele recarrega', () => {
    expect(
      deveRecarregarNaConexao({
        primeiraConexao: true,
        msDesdeMontagem: LIMIAR_CONEXAO_LENTA_MS,
        ultimaCargaFalhou: false,
      }),
    ).toBe(false);
    expect(
      deveRecarregarNaConexao({
        primeiraConexao: true,
        msDesdeMontagem: LIMIAR_CONEXAO_LENTA_MS + 1,
        ultimaCargaFalhou: false,
      }),
    ).toBe(true);
  });

  it('1ª conexão com a última carga falha recarrega, mesmo rápida', () => {
    expect(
      deveRecarregarNaConexao({ primeiraConexao: true, msDesdeMontagem: 10, ultimaCargaFalhou: true }),
    ).toBe(true);
  });

  it('reconexão sempre recarrega', () => {
    expect(
      deveRecarregarNaConexao({ primeiraConexao: false, msDesdeMontagem: 0, ultimaCargaFalhou: false }),
    ).toBe(true);
  });
});

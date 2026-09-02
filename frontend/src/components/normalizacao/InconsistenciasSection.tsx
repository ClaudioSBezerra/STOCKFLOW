import { useCallback, useMemo, useState } from 'react';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader } from '@/components/ui/card';
import { Checkbox } from '@/components/ui/checkbox';
import { getAccessToken } from '@/lib/session';
import { formatarQuantidade } from '@/components/catalogo/formatacao';

/**
 * Seção "Inconsistências" da página `/normalizacao` (Story 6.1, spec-6-1 +
 * Story 6.2, spec-6-2). Botão "Analisar todos os produtos" dispara `GET
 * /api/normalizacao/inconsistencias` SÓ AO CLIQUE — ao contrário de
 * MovimentacoesSection (auto-load-on-mount + `conectarRealtime`), esta seção
 * NÃO carrega nada no mount: a análise varre o catálogo inteiro sob demanda,
 * sem canal de tempo real (nenhum estado persistido para notificar).
 *
 * Story 6.2 acrescenta as 3 ações de correção — nenhuma usa modal (UX
 * pattern do épico, ver epic-6-context.md):
 *  - `Checkbox` por linha + "Aplicar selecionadas": lote via seleção — 1
 *    item marcado é "individual", N itens do mesmo produto é "lote por
 *    produto", todos marcados é "lote geral" — os 3 modos são só tamanhos
 *    diferentes da mesma seleção, decisão inteiramente do usuário aqui.
 *  - "Aceitar" inline por linha: atalho para aplicar só aquela sugestão,
 *    sem precisar marcar o checkbox primeiro.
 *  - "Ignorar" inline por linha: grava a tupla exata como ignorada — a
 *    sugestão nunca mais reaparece para ESSE valor (POST
 *    /api/normalizacao/ignoradas).
 *
 * Em qualquer sucesso, só as linhas confirmadas pelo servidor somem da
 * tabela — `aplicadas` da resposta de POST /correcoes, ou a própria linha no
 * caso de ignorar — nunca um reload/re-análise completa.
 */

interface Sugestao {
  produtoId: string;
  produtoNome: string;
  campo: string;
  valorSugerido: { valor: number; unidade: string };
  origem: string;
}

interface CorrecaoAplicada {
  produtoId: string;
  campo: string;
}

function authHeaders(): Record<string, string> {
  const token = getAccessToken();
  return token ? { Authorization: `Bearer ${token}` } : {};
}

/** chave é o identificador estável de uma linha (produto+campo) — usado
 * tanto para a seleção de checkboxes quanto para remover da tabela só o que
 * o servidor confirmou. */
function chave(produtoId: string, campo: string): string {
  return `${produtoId}::${campo}`;
}

const MENSAGEM_ERRO_ANALISAR =
  'Não foi possível analisar os produtos. Tente novamente em instantes.';
const MENSAGEM_ERRO_APLICAR =
  'Não foi possível aplicar a(s) correção(ões) selecionada(s). Tente novamente em instantes.';
const MENSAGEM_ERRO_IGNORAR =
  'Não foi possível ignorar a sugestão. Tente novamente em instantes.';

const ROTULO_CAMPO: Record<string, string> = {
  comprimento: 'Comprimento',
  largura: 'Largura',
  diametro: 'Diâmetro',
  altura: 'Altura',
  espessura: 'Espessura',
};

const ROTULO_ORIGEM: Record<string, string> = {
  migracao: 'Migração',
  nome: 'Nome',
};

export function InconsistenciasSection() {
  const [sugestoes, setSugestoes] = useState<Sugestao[] | null>(null);
  const [carregando, setCarregando] = useState(false);
  const [erro, setErro] = useState<string | null>(null);

  const [selecionadas, setSelecionadas] = useState<Set<string>>(new Set());
  const [processando, setProcessando] = useState(false);
  const [erroAcao, setErroAcao] = useState<string | null>(null);

  const analisar = useCallback(async () => {
    setCarregando(true);
    setErro(null);
    setErroAcao(null);
    setSelecionadas(new Set());
    // Limpa a tabela anterior ANTES de tentar de novo — sem isso, um
    // segundo clique que falha deixaria a tabela de uma corrida bem-sucedida
    // anterior visível ao mesmo tempo que o alerta de erro novo, uma
    // combinação enganosa (dado velho aparentando ser a resposta atual).
    setSugestoes(null);
    try {
      const res = await fetch('/api/normalizacao/inconsistencias', { headers: authHeaders() });
      if (!res.ok) {
        setErro(MENSAGEM_ERRO_ANALISAR);
        return;
      }
      const body = (await res.json()) as { sugestoes: Sugestao[] };
      setSugestoes(body.sugestoes ?? []);
    } catch {
      setErro(MENSAGEM_ERRO_ANALISAR);
    } finally {
      setCarregando(false);
    }
  }, []);

  // aplicar chama POST /api/normalizacao/correcoes para `alvo` (1 item =
  // "Aceitar" inline, N itens = "Aplicar selecionadas") e remove da tabela
  // só as linhas que a resposta `aplicadas` confirma — um item obsoleto
  // (campo já preenchido por outra ação enquanto a lista estava aberta)
  // simplesmente permanece na tabela, sem erro.
  const aplicar = useCallback(async (alvo: Sugestao[]) => {
    if (alvo.length === 0) return;
    setErroAcao(null);
    setProcessando(true);
    try {
      const res = await fetch('/api/normalizacao/correcoes', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', ...authHeaders() },
        body: JSON.stringify({
          correcoes: alvo.map((s) => ({
            produtoId: s.produtoId,
            campo: s.campo,
            valorSugerido: s.valorSugerido,
          })),
        }),
      });
      if (!res.ok) {
        setErroAcao(MENSAGEM_ERRO_APLICAR);
        return;
      }
      const body = (await res.json()) as { aplicadas: CorrecaoAplicada[] };
      const aplicadasChaves = new Set(body.aplicadas.map((a) => chave(a.produtoId, a.campo)));
      setSugestoes((atual) => (atual ?? []).filter((s) => !aplicadasChaves.has(chave(s.produtoId, s.campo))));
      setSelecionadas((atual) => {
        const novo = new Set(atual);
        for (const k of aplicadasChaves) novo.delete(k);
        return novo;
      });
    } catch {
      setErroAcao(MENSAGEM_ERRO_APLICAR);
    } finally {
      setProcessando(false);
    }
  }, []);

  // ignorar chama POST /api/normalizacao/ignoradas para uma única sugestão
  // — em sucesso, some da tabela imediatamente (o servidor já gravou a
  // tupla, não reaparece numa próxima análise).
  const ignorar = useCallback(async (s: Sugestao) => {
    setErroAcao(null);
    setProcessando(true);
    try {
      const res = await fetch('/api/normalizacao/ignoradas', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', ...authHeaders() },
        body: JSON.stringify({
          produtoId: s.produtoId,
          campo: s.campo,
          valorSugerido: s.valorSugerido,
        }),
      });
      if (!res.ok) {
        setErroAcao(MENSAGEM_ERRO_IGNORAR);
        return;
      }
      const chaveLinha = chave(s.produtoId, s.campo);
      setSugestoes((atual) => (atual ?? []).filter((x) => chave(x.produtoId, x.campo) !== chaveLinha));
      setSelecionadas((atual) => {
        const novo = new Set(atual);
        novo.delete(chaveLinha);
        return novo;
      });
    } catch {
      setErroAcao(MENSAGEM_ERRO_IGNORAR);
    } finally {
      setProcessando(false);
    }
  }, []);

  const alternarSelecao = useCallback((chaveLinha: string) => {
    setSelecionadas((atual) => {
      const novo = new Set(atual);
      if (novo.has(chaveLinha)) {
        novo.delete(chaveLinha);
      } else {
        novo.add(chaveLinha);
      }
      return novo;
    });
  }, []);

  const todasSelecionadas = useMemo(
    () => (sugestoes ?? []).length > 0 && (sugestoes ?? []).every((s) => selecionadas.has(chave(s.produtoId, s.campo))),
    [sugestoes, selecionadas],
  );

  const alternarSelecaoTodas = useCallback(() => {
    setSelecionadas((atual) => {
      const lista = sugestoes ?? [];
      const todasJaSelecionadas = lista.length > 0 && lista.every((s) => atual.has(chave(s.produtoId, s.campo)));
      if (todasJaSelecionadas) return new Set();
      return new Set(lista.map((s) => chave(s.produtoId, s.campo)));
    });
  }, [sugestoes]);

  const aplicarSelecionadas = useCallback(() => {
    const lista = (sugestoes ?? []).filter((s) => selecionadas.has(chave(s.produtoId, s.campo)));
    void aplicar(lista);
  }, [sugestoes, selecionadas, aplicar]);

  return (
    <Card>
      <CardHeader>
        <h2 className="text-heading-md">Inconsistências</h2>
        <CardDescription>
          Sugestões de correção dimensional a partir da migração do legado e de nomes de Produto com
          valor implícito ainda não estruturado. Marque as linhas desejadas para aplicar em lote, ou use
          as ações "Aceitar"/"Ignorar" de cada linha.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <Button onClick={() => void analisar()} disabled={carregando || processando} className="self-start">
          {carregando ? 'Analisando...' : 'Analisar todos os produtos'}
        </Button>

        {erro && (
          <p role="alert" className="text-body text-destructive">
            {erro}
          </p>
        )}

        {erroAcao && (
          <p role="alert" className="text-body text-destructive">
            {erroAcao}
          </p>
        )}

        {!erro && sugestoes !== null && sugestoes.length === 0 && (
          <p className="text-body text-muted-foreground">Nenhuma inconsistência encontrada.</p>
        )}

        {sugestoes !== null && sugestoes.length > 0 && (
          <>
            <Button
              onClick={aplicarSelecionadas}
              disabled={processando || selecionadas.size === 0}
              variant="secondary"
              className="self-start"
            >
              {processando ? 'Aplicando...' : `Aplicar selecionadas (${selecionadas.size})`}
            </Button>

            <div className="overflow-x-auto">
              <table className="w-full text-body">
                <thead>
                  <tr className="text-label text-muted-foreground">
                    <th className="py-2 pr-4 text-left font-medium">
                      <Checkbox
                        aria-label="Selecionar todas"
                        checked={todasSelecionadas}
                        onCheckedChange={alternarSelecaoTodas}
                        disabled={processando}
                      />
                    </th>
                    <th className="py-2 pr-4 text-left font-medium">Produto</th>
                    <th className="py-2 pr-4 text-left font-medium">Campo</th>
                    <th className="py-2 pr-4 text-left font-medium">Valor sugerido</th>
                    <th className="py-2 pr-4 text-left font-medium">Origem</th>
                    <th className="py-2 text-left font-medium">Ações</th>
                  </tr>
                </thead>
                <tbody>
                  {sugestoes.map((s) => {
                    const chaveLinha = chave(s.produtoId, s.campo);
                    const rotuloCampo = ROTULO_CAMPO[s.campo] ?? s.campo;
                    return (
                      <tr key={chaveLinha} className="border-t border-border">
                        <td className="py-2 pr-4">
                          <Checkbox
                            aria-label={`Selecionar ${s.produtoNome} - ${rotuloCampo}`}
                            checked={selecionadas.has(chaveLinha)}
                            onCheckedChange={() => alternarSelecao(chaveLinha)}
                            disabled={processando}
                          />
                        </td>
                        <td className="py-2 pr-4">{s.produtoNome}</td>
                        <td className="py-2 pr-4">{rotuloCampo}</td>
                        <td className="py-2 pr-4">
                          {formatarQuantidade(s.valorSugerido.valor)}
                          {s.valorSugerido.unidade}
                        </td>
                        <td className="py-2 pr-4">{ROTULO_ORIGEM[s.origem] ?? s.origem}</td>
                        <td className="flex gap-2 py-2">
                          <Button
                            size="sm"
                            variant="outline"
                            aria-label={`Aceitar ${s.produtoNome} - ${rotuloCampo}`}
                            disabled={processando}
                            onClick={() => void aplicar([s])}
                          >
                            Aceitar
                          </Button>
                          <Button
                            size="sm"
                            variant="ghost"
                            aria-label={`Ignorar ${s.produtoNome} - ${rotuloCampo}`}
                            disabled={processando}
                            onClick={() => void ignorar(s)}
                          >
                            Ignorar
                          </Button>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          </>
        )}
      </CardContent>
    </Card>
  );
}

export default InconsistenciasSection;

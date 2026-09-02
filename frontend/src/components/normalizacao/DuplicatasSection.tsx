import { useCallback, useEffect, useRef, useState } from 'react';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader } from '@/components/ui/card';
import { getAccessToken } from '@/lib/session';
import { resumirDimensoes, type Dimensoes } from '@/components/catalogo/formatacao';

/**
 * Seção "Duplicatas" da página `/normalizacao` (Story 6.3, spec-6-3). Molde
 * de `InconsistenciasSection.tsx` (Story 6.1): botão dispara `GET
 * /api/normalizacao/duplicatas` SÓ AO CLIQUE por padrão — a análise varre o
 * catálogo inteiro sob demanda, sem canal de tempo real (nenhum estado
 * persistido para notificar, mesmo padrão de Inconsistências).
 *
 * `autoAnalisar` (Intent Contract, spec-6-3): quando `true`, dispara a
 * análise UMA VEZ ao montar — usado quando a página chega via CTA "Verificar
 * duplicatas agora" (Story 3.4, `ImportacaoProdutosSection.tsx`) com
 * `?verificarDuplicatas=1`, para que o Almoxarife caia direto na aba
 * Duplicatas com a análise já em andamento, sem precisar de um segundo
 * clique. Idioma de `VerificarEmailPage.tsx` (`useRef` para nunca disparar
 * duas vezes DENTRO da mesma montagem), mas aqui disparando uma AÇÃO em vez
 * de decidir o estado inicial a partir de um token da URL.
 *
 * O guard `useRef` local só protege contra disparo duplo dentro de UMA
 * montagem (ex. Strict Mode). Ele NÃO sobrevive a uma remontagem — e
 * `TabsContent` (Radix, `NormalizacaoPage.tsx`) desmonta o conteúdo da aba
 * inativa por padrão, então trocar de aba e voltar para "Duplicatas" remonta
 * este componente do zero. Por isso `onAutoAnalisado` (chamado uma única vez,
 * junto do disparo automático) devolve o controle para `NormalizacaoPage`,
 * que é quem NÃO desmonta ao trocar de aba e por isso é o dono correto do
 * guard "já disparei a análise automática nesta visita à página" — ver
 * comentário de `NormalizacaoPage.tsx`.
 *
 * Rota só-leitura (Never, spec-6-3): nenhuma ação de mesclagem/exclusão
 * nesta tela — isso é Story 6.4. Cada grupo só é exibido (produtos +
 * dimensões), sem nenhum botão de ação.
 */

interface ProdutoDuplicata {
  id: string;
  nome: string;
  dimensoes: Dimensoes;
}

interface GrupoDuplicata {
  produtos: ProdutoDuplicata[];
}

function authHeaders(): Record<string, string> {
  const token = getAccessToken();
  return token ? { Authorization: `Bearer ${token}` } : {};
}

const MENSAGEM_ERRO_ANALISAR =
  'Não foi possível analisar os produtos. Tente novamente em instantes.';

interface DuplicatasSectionProps {
  autoAnalisar?: boolean;
  /**
   * Chamado uma única vez, no exato momento em que `autoAnalisar` dispara a
   * análise automática — permite que `NormalizacaoPage` registre "já
   * disparei" no seu próprio estado (que sobrevive à remontagem deste
   * componente ao trocar de aba), para nunca mais passar `autoAnalisar=true`
   * de novo nesta visita à página.
   */
  onAutoAnalisado?: () => void;
}

export function DuplicatasSection({ autoAnalisar = false, onAutoAnalisado }: DuplicatasSectionProps) {
  const [grupos, setGrupos] = useState<GrupoDuplicata[] | null>(null);
  const [carregando, setCarregando] = useState(false);
  const [erro, setErro] = useState<string | null>(null);

  const analisar = useCallback(async () => {
    setCarregando(true);
    setErro(null);
    // Limpa os grupos da corrida anterior ANTES de tentar de novo — mesmo
    // cuidado de InconsistenciasSection: sem isso, um segundo clique que
    // falha deixaria os grupos de uma corrida bem-sucedida anterior visíveis
    // ao mesmo tempo que o alerta de erro novo.
    setGrupos(null);
    try {
      const res = await fetch('/api/normalizacao/duplicatas', { headers: authHeaders() });
      if (!res.ok) {
        setErro(MENSAGEM_ERRO_ANALISAR);
        return;
      }
      const body = (await res.json()) as { grupos: GrupoDuplicata[] };
      setGrupos(body.grupos ?? []);
    } catch {
      setErro(MENSAGEM_ERRO_ANALISAR);
    } finally {
      setCarregando(false);
    }
  }, []);

  const jaDisparouAuto = useRef(false);
  useEffect(() => {
    if (!autoAnalisar || jaDisparouAuto.current) return;
    jaDisparouAuto.current = true;
    onAutoAnalisado?.();
    void analisar();
  }, [autoAnalisar, analisar, onAutoAnalisado]);

  return (
    <Card>
      <CardHeader>
        <h2 className="text-heading-md">Duplicatas</h2>
        <CardDescription>
          Grupos de Produtos candidatos a duplicata: mesmo nome, dimensões equivalentes (considerando
          conversão de unidade) e ao menos um local em comum. A revisão e a mesclagem acontecem em outra
          tela.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <Button onClick={() => void analisar()} disabled={carregando} className="self-start">
          {carregando ? 'Analisando...' : 'Analisar duplicatas'}
        </Button>

        {erro && (
          <p role="alert" className="text-body text-destructive">
            {erro}
          </p>
        )}

        {!erro && grupos !== null && grupos.length === 0 && (
          <p className="text-body text-muted-foreground">Nenhuma duplicata encontrada.</p>
        )}

        {grupos !== null && grupos.length > 0 && (
          <div className="flex flex-col gap-4">
            {grupos.map((grupo) => {
              const chaveGrupo = grupo.produtos.map((p) => p.id).join('|');
              return (
                <div key={chaveGrupo} className="overflow-x-auto rounded-md border border-border">
                  <table className="w-full text-body">
                    <thead>
                      <tr className="border-b border-border text-label text-muted-foreground">
                        <th className="py-2 pr-4 pl-4 text-left font-medium">Produto</th>
                        <th className="py-2 pr-4 text-left font-medium">Dimensões</th>
                      </tr>
                    </thead>
                    <tbody>
                      {grupo.produtos.map((produto) => (
                        <tr key={produto.id} className="border-t border-border first:border-t-0">
                          <td className="py-2 pr-4 pl-4">{produto.nome}</td>
                          <td className="py-2 pr-4">{resumirDimensoes(produto.dimensoes)}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              );
            })}
          </div>
        )}
      </CardContent>
    </Card>
  );
}

export default DuplicatasSection;

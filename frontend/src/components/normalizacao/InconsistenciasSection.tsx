import { useCallback, useState } from 'react';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader } from '@/components/ui/card';
import { getAccessToken } from '@/lib/session';
import { formatarQuantidade } from '@/components/catalogo/formatacao';

/**
 * Seção "Inconsistências" da página `/normalizacao` (Story 6.1, spec-6-1).
 * Botão "Analisar todos os produtos" dispara `GET
 * /api/normalizacao/inconsistencias` SÓ AO CLIQUE — ao contrário de
 * MovimentacoesSection (auto-load-on-mount + `conectarRealtime`), esta seção
 * NÃO carrega nada no mount: a análise varre o catálogo inteiro sob demanda,
 * sem canal de tempo real (nenhum estado persistido para notificar).
 *
 * Tabela SOMENTE-LEITURA (Produto · Campo · Valor sugerido · Origem) ou
 * "Nenhuma inconsistência encontrada." quando a lista vem vazia. Aplicar/
 * ignorar uma sugestão é Story 6.2 — nenhuma ação em nenhuma linha aqui.
 * Falha de rede/servidor vira `<p role="alert">` (molde de
 * MovimentacoesSection/LogAcessoSection).
 */

interface Sugestao {
  produtoId: string;
  produtoNome: string;
  campo: string;
  valorSugerido: { valor: number; unidade: string };
  origem: string;
}

function authHeaders(): Record<string, string> {
  const token = getAccessToken();
  return token ? { Authorization: `Bearer ${token}` } : {};
}

const MENSAGEM_ERRO_ANALISAR =
  'Não foi possível analisar os produtos. Tente novamente em instantes.';

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

  const analisar = useCallback(async () => {
    setCarregando(true);
    setErro(null);
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

  return (
    <Card>
      <CardHeader>
        <h2 className="text-heading-md">Inconsistências</h2>
        <CardDescription>
          Sugestões de correção dimensional a partir da migração do legado e de nomes de Produto com
          valor implícito ainda não estruturado. Somente leitura — nenhuma alteração é aplicada aqui.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <Button onClick={() => void analisar()} disabled={carregando} className="self-start">
          {carregando ? 'Analisando...' : 'Analisar todos os produtos'}
        </Button>

        {erro && (
          <p role="alert" className="text-body text-destructive">
            {erro}
          </p>
        )}

        {!erro && sugestoes !== null && sugestoes.length === 0 && (
          <p className="text-body text-muted-foreground">Nenhuma inconsistência encontrada.</p>
        )}

        {sugestoes !== null && sugestoes.length > 0 && (
          <div className="overflow-x-auto">
            <table className="w-full text-body">
              <thead>
                <tr className="text-label text-muted-foreground">
                  <th className="py-2 pr-4 text-left font-medium">Produto</th>
                  <th className="py-2 pr-4 text-left font-medium">Campo</th>
                  <th className="py-2 pr-4 text-left font-medium">Valor sugerido</th>
                  <th className="py-2 text-left font-medium">Origem</th>
                </tr>
              </thead>
              <tbody>
                {sugestoes.map((s, i) => (
                  <tr key={`${s.produtoId}-${s.campo}-${i}`} className="border-t border-border">
                    <td className="py-2 pr-4">{s.produtoNome}</td>
                    <td className="py-2 pr-4">{ROTULO_CAMPO[s.campo] ?? s.campo}</td>
                    <td className="py-2 pr-4">
                      {formatarQuantidade(s.valorSugerido.valor)}
                      {s.valorSugerido.unidade}
                    </td>
                    <td className="py-2">{ROTULO_ORIGEM[s.origem] ?? s.origem}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

export default InconsistenciasSection;

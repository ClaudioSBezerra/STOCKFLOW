import { useCallback, useEffect, useState, type FormEvent } from 'react';
import { toast } from 'sonner';
import { Link } from 'react-router-dom';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { getAccessToken } from '@/lib/session';

/**
 * Seção "Importação" da `CatalogoPage` (Story 3.3, spec-3-3; Story 3.4,
 * spec-3-4 acrescenta `atualizados` e o CTA de duplicatas), visível só a
 * `almoxarife`+ (gate na própria página, mesmo padrão de
 * `CadastroProdutoSection`). Um `Card` com:
 *
 * - Banner de retomada (`GET /api/importacoes/ultima` no mount e depois de
 *   cada envio/continuação): quando a importação mais recente ainda está
 *   `em_andamento`, mostra "Última importação parou na linha N de M.
 *   Continuar de onde parou?" com um botão que chama
 *   `POST /api/importacoes/{id}/continuar`. `N` é `proxima_linha_pendente`
 *   devolvido pelo backend (o número REAL da linha na planilha, cabeçalho =
 *   1) — NUNCA `criados+atualizados+rejeitados`: `numero_linha` começa em 2 e
 *   tem gaps (linhas em branco descartadas antes de gravar), então uma
 *   contagem de processadas nunca aponta pra célula certa ao reabrir o
 *   arquivo original.
 * - Formulário de upload: `<input type="file" accept=".xlsx">` + submit
 *   envia `FormData` (campo `planilha`) para `POST /api/importacoes` com
 *   header `Authorization` — SEM `Content-Type` manual, o browser define o
 *   boundary do multipart sozinho.
 * - Relatório do envio mais recente: contagem de criados/atualizados/
 *   rejeitados, um CTA "Verificar duplicatas agora" (`/normalizacao`, rota já
 *   cadastrada no nav — hoje `PlaceholderPage`, Epic 6 ainda não existe) e,
 *   se houver, uma tabela "linha/erro" das linhas rejeitadas.
 *
 * "Importar planilha" e "Continuar importação" são mutuamente exclusivos —
 * os dois escrevem no mesmo estado de relatório exibido, então os dois
 * botões desabilitam sempre que QUALQUER uma das duas operações está em
 * andamento (`operacaoEmAndamento = enviando || continuando`), não só a sua
 * própria.
 *
 * Sem barra de progresso incremental — o processamento é uma única
 * requisição síncrona (sem SSE, mesmo precedente das Stories 3.1/3.2); só um
 * estado de carregamento/desabilitado durante o envio.
 */

interface ImportacaoResumo {
  id: string;
  status: string;
  total_linhas: number;
  // Número REAL da linha (cabeçalho = 1) mais antiga ainda pendente/
  // processando, ou `null` quando não sobra nenhuma.
  // `criados+atualizados+rejeitados` NUNCA é um substituto válido para este
  // valor: `numero_linha` começa em 2 e tem gaps (linhas em branco são
  // descartadas antes de gravar), então uma simples contagem de processadas
  // nunca aponta pra célula certa da planilha original.
  proxima_linha_pendente: number | null;
}

interface LinhaRejeitada {
  linha: number;
  erro: string;
}

interface RelatorioImportacao {
  criados: number;
  // Linhas cujo código já casou com um Produto existente (Story 3.4,
  // spec-3-4): o Produto é atualizado, não duplicado — nunca soma em
  // `criados`.
  atualizados: number;
  rejeitados: number;
  linhas_rejeitadas: LinhaRejeitada[];
}

interface RespostaImportacao {
  importacao: ImportacaoResumo;
  relatorio: RelatorioImportacao;
}

function authHeaders(): Record<string, string> {
  const token = getAccessToken();
  return token ? { Authorization: `Bearer ${token}` } : {};
}

const MENSAGEM_ERRO_IMPORTAR =
  'Não foi possível importar a planilha agora. Tente novamente em instantes.';
const MENSAGEM_ERRO_CONTINUAR =
  'Não foi possível continuar a importação agora. Tente novamente em instantes.';

export function ImportacaoProdutosSection() {
  // `components/ui/input.tsx` não encaminha `ref` (função sem forwardRef) —
  // o input de arquivo é resetado depois de um envio bem-sucedido trocando
  // `inputKey` (força o React a desmontar/remontar o elemento, limpando a
  // seleção nativa do browser), não via `ref.current.value = ''`.
  const [inputKey, setInputKey] = useState(0);
  const [arquivo, setArquivo] = useState<File | null>(null);
  const [enviando, setEnviando] = useState(false);
  const [continuando, setContinuando] = useState(false);
  const [erro, setErro] = useState<string | null>(null);
  const [relatorio, setRelatorio] = useState<RelatorioImportacao | null>(null);

  const [ultima, setUltima] = useState<ImportacaoResumo | null>(null);

  const consultarUltima = useCallback(async () => {
    try {
      const res = await fetch('/api/importacoes/ultima', { headers: authHeaders() });
      if (!res.ok) {
        return;
      }
      const body = (await res.json()) as { importacao: ImportacaoResumo | null };
      setUltima(body.importacao ?? null);
    } catch {
      // Falha isolada ao consultar a última importação: não bloqueia o
      // formulário de upload, só deixa de mostrar o banner de retomada.
    }
  }, []);

  useEffect(() => {
    void (async () => {
      await consultarUltima();
    })();
  }, [consultarUltima]);

  // `proxima_linha_pendente` só vem `null` quando a importação está
  // `concluida` (ou, momentaneamente, quando outra chamada concorrente já
  // reivindicou a última linha pendente) — em qualquer um dos dois casos não
  // há nada de útil pra mostrar no banner de retomada.
  const emAndamento = ultima?.status === 'em_andamento' && ultima.proxima_linha_pendente != null;
  // Nenhuma das duas ações assíncronas desta seção (enviar/continuar) pode
  // rodar simultaneamente com a outra — as duas escrevem no mesmo estado de
  // relatório exibido, e disparar as duas ao mesmo tempo deixaria uma
  // resposta pisar na outra.
  const operacaoEmAndamento = enviando || continuando;

  async function enviar(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (operacaoEmAndamento || !arquivo) {
      return;
    }
    setErro(null);
    setEnviando(true);
    try {
      const formData = new FormData();
      formData.append('planilha', arquivo);
      const res = await fetch('/api/importacoes', {
        method: 'POST',
        headers: authHeaders(),
        body: formData,
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => ({}))) as { error?: { message?: string } };
        setErro(body.error?.message ?? MENSAGEM_ERRO_IMPORTAR);
        return;
      }
      const body = (await res.json()) as RespostaImportacao;
      setRelatorio(body.relatorio);
      toast.success(
        `Importação concluída: ${body.relatorio.criados} criado(s), ${body.relatorio.atualizados} atualizado(s), ${body.relatorio.rejeitados} rejeitado(s).`,
      );
      setArquivo(null);
      setInputKey((k) => k + 1);
      await consultarUltima();
    } catch {
      setErro(MENSAGEM_ERRO_IMPORTAR);
    } finally {
      setEnviando(false);
    }
  }

  async function continuarImportacao() {
    if (operacaoEmAndamento || !ultima) {
      return;
    }
    setErro(null);
    setContinuando(true);
    try {
      const res = await fetch(`/api/importacoes/${ultima.id}/continuar`, {
        method: 'POST',
        headers: authHeaders(),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => ({}))) as { error?: { message?: string } };
        setErro(body.error?.message ?? MENSAGEM_ERRO_CONTINUAR);
        return;
      }
      const body = (await res.json()) as RespostaImportacao;
      setRelatorio(body.relatorio);
      toast.success(
        `Importação concluída: ${body.relatorio.criados} criado(s), ${body.relatorio.atualizados} atualizado(s), ${body.relatorio.rejeitados} rejeitado(s).`,
      );
      await consultarUltima();
    } catch {
      setErro(MENSAGEM_ERRO_CONTINUAR);
    } finally {
      setContinuando(false);
    }
  }

  return (
    <Card>
      <CardHeader>
        <h2 className="text-heading-lg">Importar Produtos</h2>
        <CardDescription>Importação em massa via planilha padronizada (.xlsx).</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {emAndamento && ultima && (
          <div className="flex flex-col gap-2 rounded-md border border-border bg-muted p-4 sm:flex-row sm:items-center sm:justify-between">
            <p className="text-body">
              Última importação parou na linha {ultima.proxima_linha_pendente} de{' '}
              {ultima.total_linhas}. Continuar de onde parou?
            </p>
            <Button
              type="button"
              variant="outline"
              disabled={operacaoEmAndamento}
              onClick={() => void continuarImportacao()}
              className="self-start sm:self-auto"
            >
              {continuando ? 'Continuando...' : 'Continuar importação'}
            </Button>
          </div>
        )}

        <form onSubmit={enviar} className="flex flex-col gap-4" noValidate>
          <div className="flex flex-col gap-2">
            <Label htmlFor="importacao-arquivo">Planilha (.xlsx)</Label>
            <Input
              key={inputKey}
              id="importacao-arquivo"
              type="file"
              accept=".xlsx"
              onChange={(event) => setArquivo(event.target.files?.[0] ?? null)}
            />
          </div>

          {erro && (
            <p role="alert" className="text-body text-destructive">
              {erro}
            </p>
          )}

          <Button type="submit" disabled={operacaoEmAndamento || !arquivo} className="self-start">
            {enviando ? 'Importando...' : 'Importar planilha'}
          </Button>
        </form>

        {relatorio && (
          <div className="flex flex-col gap-2">
            <p className="text-body">
              {relatorio.criados} criado(s), {relatorio.atualizados} atualizado(s),{' '}
              {relatorio.rejeitados} rejeitado(s).
            </p>
            {/*
              CTA "Verificar duplicatas agora" (Story 3.4, spec-3-4; ligado à
              Detecção de duplicatas pela Story 6.3, spec-6-3):
              `?verificarDuplicatas=1` leva `/normalizacao` direto para a aba
              Duplicatas com a análise já em andamento — `NormalizacaoPage`
              lê o parâmetro e passa `autoAnalisar` para `DuplicatasSection`,
              sem exigir um segundo clique do Almoxarife.
            */}
            <Button asChild variant="outline" className="self-start">
              <Link to="/normalizacao?verificarDuplicatas=1">Verificar duplicatas agora</Link>
            </Button>
            {relatorio.linhas_rejeitadas.length > 0 && (
              <div className="overflow-x-auto">
                <table className="w-full text-body">
                  <thead>
                    <tr className="border-b border-border text-left">
                      <th className="py-2 pr-4 font-medium">Linha</th>
                      <th className="py-2 font-medium">Erro</th>
                    </tr>
                  </thead>
                  <tbody>
                    {relatorio.linhas_rejeitadas.map((linha) => (
                      <tr key={linha.linha} className="border-b border-border last:border-0">
                        <td className="py-2 pr-4 font-mono">{linha.linha}</td>
                        <td className="py-2">{linha.erro}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        )}
      </CardContent>
    </Card>
  );
}

export default ImportacaoProdutosSection;

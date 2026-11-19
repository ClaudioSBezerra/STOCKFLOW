import { useCallback, useEffect, useState, type FormEvent } from 'react';
import { toast } from 'sonner';
import { Link } from 'react-router-dom';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { apiUrl, authHeaders } from '@/lib/api';

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
 * - Seletor "Depósito de destino" (spec-importacao-por-deposito): UM depósito
 *   por importação, obrigatório, agrupado por Filial (`GET /api/estoques` +
 *   `GET /api/filiais`). A coluna "Estoque" da planilha é ignorada. Sem
 *   depósitos cadastrados: mensagem orientando e envio bloqueado.
 * - Formulário de upload: `<input type="file" accept=".xlsx">` + submit
 *   envia `FormData` (campos `planilha` e `estoque_id`) para `POST /api/importacoes` com
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
  // Depósito de destino da importação (nulo em importação legada ou se o
  // depósito foi excluído depois).
  estoque_id?: string | null;
  estoque_nome?: string | null;
  filial_nome?: string | null;
  por_deposito?: boolean;
}

interface Deposito {
  id: string;
  nome: string;
  filial_id?: string | null;
}

interface Filial {
  id: string;
  nome: string;
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
  // Produto novo com quantidade/preço vazio ou 0 gravado como 0.
  preenchidas_com_zero?: number;
  // Atualização com quantidade/preço vazio ou 0: o valor atual foi mantido.
  mantiveram_valor_atual?: number;
  linhas_rejeitadas: LinhaRejeitada[];
}

interface RespostaImportacao {
  importacao: ImportacaoResumo;
  relatorio: RelatorioImportacao;
}

const MENSAGEM_ERRO_IMPORTAR =
  'Não foi possível importar a planilha agora. Tente novamente em instantes.';
const MENSAGEM_SEM_DEPOSITOS =
  'Nenhum depósito cadastrado. Peça a um gestor para cadastrar em Cadastros > Filiais.';
const MENSAGEM_DEPOSITO_EXCLUIDO = 'Depósito: excluído (o depósito de destino não existe mais)';
const MENSAGEM_ERRO_DEPOSITOS = 'Não foi possível carregar os depósitos agora.';
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
  // Resumo (com o depósito usado) do envio/continuação mais recente.
  const [resumo, setResumo] = useState<ImportacaoResumo | null>(null);

  const [depositos, setDepositos] = useState<Deposito[]>([]);
  const [filiais, setFiliais] = useState<Filial[]>([]);
  const [carregouDepositos, setCarregouDepositos] = useState(false);
  const [erroDepositos, setErroDepositos] = useState<string | null>(null);
  const [estoqueId, setEstoqueId] = useState('');

  const carregarDepositos = useCallback(async () => {
    setErroDepositos(null);
    try {
      const [resEstoques, resFiliais] = await Promise.all([
        fetch(apiUrl('/api/estoques'), { headers: authHeaders() }),
        fetch(apiUrl('/api/filiais'), { headers: authHeaders() }),
      ]);
      if (!resEstoques.ok) {
        setErroDepositos(MENSAGEM_ERRO_DEPOSITOS);
        return;
      }
      const bodyEstoques = (await resEstoques.json()) as { estoques?: Deposito[] };
      setDepositos(bodyEstoques.estoques ?? []);
      if (resFiliais.ok) {
        const bodyFiliais = (await resFiliais.json()) as { filiais?: Filial[] };
        setFiliais(bodyFiliais.filiais ?? []);
      }
      setCarregouDepositos(true);
    } catch {
      setErroDepositos(MENSAGEM_ERRO_DEPOSITOS);
    }
  }, []);

  useEffect(() => {
    void carregarDepositos();
  }, [carregarDepositos]);

  // Grupos por Filial; depósito legado sem Filial conhecida cai em "Sem filial".
  const idsFiliais = new Set(filiais.map((f) => f.id));
  const grupos = [
    ...filiais.map((f) => ({
      chave: f.id,
      titulo: f.nome,
      itens: depositos.filter((d) => d.filial_id === f.id),
    })),
    {
      chave: 'sem-filial',
      titulo: 'Sem filial',
      itens: depositos.filter((d) => !d.filial_id || !idsFiliais.has(d.filial_id)),
    },
  ].filter((g) => g.itens.length > 0);
  const semDepositos = carregouDepositos && depositos.length === 0;

  const consultarUltima = useCallback(async () => {
    try {
      const res = await fetch(apiUrl('/api/importacoes/ultima'), { headers: authHeaders() });
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
    if (operacaoEmAndamento || !arquivo || !estoqueId) {
      return;
    }
    setErro(null);
    setEnviando(true);
    try {
      const formData = new FormData();
      formData.append('planilha', arquivo);
      formData.append('estoque_id', estoqueId);
      const res = await fetch(apiUrl('/api/importacoes'), {
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
      setResumo(body.importacao);
      toast.success(
        `Importação concluída: ${body.relatorio.criados} criado(s), ${body.relatorio.atualizados} atualizado(s), ${body.relatorio.rejeitados} rejeitado(s).`,
      );
      // A escolha do depósito é preservada entre envios; só o arquivo é limpo.
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
      const res = await fetch(apiUrl(`/api/importacoes/${ultima.id}/continuar`), {
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
      setResumo(body.importacao);
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
            <Label htmlFor="importacao-deposito">Depósito de destino</Label>
            {semDepositos ? (
              <p className="text-body text-muted-foreground">{MENSAGEM_SEM_DEPOSITOS}</p>
            ) : (
              <select
                id="importacao-deposito"
                value={estoqueId}
                onChange={(event) => setEstoqueId(event.target.value)}
                className="text-body min-h-touch-target-min rounded-md border border-border bg-background px-3"
              >
                <option value="">Escolha o depósito</option>
                {grupos.map((g) => (
                  <optgroup key={g.chave} label={g.titulo}>
                    {g.itens.map((d) => (
                      <option key={d.id} value={d.id}>
                        {d.nome}
                      </option>
                    ))}
                  </optgroup>
                ))}
              </select>
            )}
            {erroDepositos && (
              <div className="flex flex-wrap items-center gap-2">
                <p role="alert" className="text-body text-destructive">
                  {erroDepositos}
                </p>
                <Button type="button" variant="outline" onClick={() => void carregarDepositos()}>
                  Tentar de novo
                </Button>
              </div>
            )}
            <p className="text-body text-muted-foreground">
              Toda a planilha entra neste depósito. A coluna "Estoque" da planilha é ignorada
              (pode ficar vazia). Quantidade ou preço vazio vale 0; em produto que já existe,
              vazio ou 0 mantém o valor atual.
            </p>
          </div>

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

          <Button
            type="submit"
            disabled={operacaoEmAndamento || !arquivo || !estoqueId}
            className="self-start"
          >
            {enviando ? 'Importando...' : 'Importar planilha'}
          </Button>
        </form>

        {relatorio && (
          <div className="flex flex-col gap-2">
            {resumo?.estoque_nome ? (
              <p className="text-body">
                Depósito: {resumo.estoque_nome}
                {resumo.filial_nome ? ` (${resumo.filial_nome})` : ''}
              </p>
            ) : (
              resumo?.por_deposito && <p className="text-body">{MENSAGEM_DEPOSITO_EXCLUIDO}</p>
            )}
            <p className="text-body">
              {relatorio.criados} criado(s), {relatorio.atualizados} atualizado(s),{' '}
              {relatorio.rejeitados} rejeitado(s).
            </p>
            {(relatorio.preenchidas_com_zero ?? 0) > 0 && (
              <p className="text-body text-muted-foreground">
                {relatorio.preenchidas_com_zero} linha(s) com quantidade ou preço vazio foram
                preenchidas com 0.
              </p>
            )}
            {(relatorio.mantiveram_valor_atual ?? 0) > 0 && (
              <p className="text-body text-muted-foreground">
                {relatorio.mantiveram_valor_atual} linha(s) com quantidade ou preço mantidos (vazio
                ou 0 em produto existente mantém o valor atual).
              </p>
            )}
            {/*
              CTA "Verificar duplicatas agora" (Story 3.4, spec-3-4; ligado à
              Detecção de duplicatas pela Story 6.3, spec-6-3):
              `?verificarDuplicatas=1` leva direto a `/normalizacao/duplicatas` com
              a análise já em andamento — `DuplicatasPage`
              lê o parâmetro e passa `autoAnalisar` para `DuplicatasSection`,
              sem exigir um segundo clique do Almoxarife.
            */}
            <Button asChild variant="outline" className="self-start">
              <Link to="/normalizacao/duplicatas?verificarDuplicatas=1">Verificar duplicatas agora</Link>
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

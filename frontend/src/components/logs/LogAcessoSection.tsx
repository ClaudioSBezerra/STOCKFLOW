import { useCallback, useEffect, useRef, useState } from 'react';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { useAuth } from '@/lib/auth';
import { getAccessToken } from '@/lib/session';
import { rankPapel } from '@/components/shell/nav-items';

/**
 * Seção "Log de Acesso" (`/configuracoes`, Story 1.12, spec-1-12). Montada só
 * para `adm` (mesmo padrão de gate de `GestaoUsuariosSection` — o item de
 * navegação da EXPERIENCE.md IA fica DENTRO de Configurações, nunca no rail;
 * a AC de "o item de navegação nem aparece" é atendida por esta seção não ser
 * renderizada para não-`adm`).
 *
 * Tabela somente-leitura de `GET /api/logs-acesso` (Data/Hora · Usuário ·
 * E-mail informado · IP · Método · Resultado): toda tentativa de login por
 * senha ou SSO, sucesso ou falha, em ordem decrescente por data. NENHUMA ação
 * de edição/exclusão em nenhuma linha — a trilha é append-only e o servidor
 * não expõe rota de escrita.
 *
 * Filtro por período: dois `<input type="date">` (início/fim) + "Filtrar". O
 * valor do date picker é uma data no calendário LOCAL do operador; ele é
 * convertido para um instante RFC3339 explícito dos limites do dia local
 * (`limiteLocalParaISO`) antes de virar query param, porque o backend
 * interpreta um `AAAA-MM-DD` cru como UTC — o que desalinharia o filtro em
 * ~3h para pt-BR (UTC-3). A carga inicial no mount vai sem filtro. Falha de
 * carga vira `<p role="alert">` inline (molde de `GestaoUsuariosSection`).
 */

interface LogAcesso {
  id: string;
  usuarioId: string | null;
  usuarioNome: string | null;
  emailInformado: string;
  metodo: string;
  sucesso: boolean;
  ip: string;
  criadoEm: string;
}

function authHeaders(): Record<string, string> {
  const token = getAccessToken();
  return token ? { Authorization: `Bearer ${token}` } : {};
}

const MENSAGEM_ERRO_CARREGAR =
  'Não foi possível carregar o log de acesso. Recarregue a página.';

// MAX_LOGS espelha services.maxLogsAcessoPorConsulta (backend): cada consulta
// devolve no máximo as 500 linhas mais recentes do período. Quando a resposta
// bate nesse teto a tela avisa do limite — sem afirmar que há linhas ocultas
// (pode haver exatamente 500 no período), só que a consulta não vai além disso.
const MAX_LOGS = 500;

/**
 * Converte o valor de um `<input type="date">` (data no calendário LOCAL) num
 * instante RFC3339 do limite do dia local — início às 00:00:00, fim às
 * 23:59:59.999. Valor vazio -> `''`. `Invalid Date` -> devolve o valor cru
 * (o backend aceita data pura como fallback).
 */
function limiteLocalParaISO(valor: string, ehFim: boolean): string {
  if (!valor) {
    return '';
  }
  const d = new Date(`${valor}T${ehFim ? '23:59:59.999' : '00:00:00'}`);
  return Number.isNaN(d.getTime()) ? valor : d.toISOString();
}

export function LogAcessoSection() {
  const { usuario } = useAuth();
  const podeVer = rankPapel(usuario?.papel ?? '') >= rankPapel('adm');

  const [logs, setLogs] = useState<LogAcesso[]>([]);
  const [inicio, setInicio] = useState('');
  const [fim, setFim] = useState('');
  const [erroCarregar, setErroCarregar] = useState<string | null>(null);
  const [carregando, setCarregando] = useState(true);

  // Contador de sequência: cada `carregar` captura seu id; respostas de uma
  // chamada mais antiga que chegam DEPOIS de uma nova são descartadas (uma
  // busca lenta do mount não pode sobrescrever uma busca filtrada rápida).
  const seqRef = useRef(0);

  // `carregar` recebe o período por argumento (não lê `inicio`/`fim` do
  // estado) para ficar estável entre renders — o mount carrega sem filtro e
  // só o clique em "Filtrar" repassa os valores atuais dos campos.
  const carregar = useCallback(async (filtroInicio: string, filtroFim: string) => {
    const seq = ++seqRef.current;
    setCarregando(true);
    try {
      const params = new URLSearchParams();
      const isoInicio = limiteLocalParaISO(filtroInicio, false);
      const isoFim = limiteLocalParaISO(filtroFim, true);
      if (isoInicio) {
        params.set('inicio', isoInicio);
      }
      if (isoFim) {
        params.set('fim', isoFim);
      }
      const qs = params.toString();
      const res = await fetch(`/api/logs-acesso${qs ? `?${qs}` : ''}`, {
        headers: authHeaders(),
      });
      if (seq !== seqRef.current) {
        return;
      }
      if (!res.ok) {
        setErroCarregar(MENSAGEM_ERRO_CARREGAR);
        return;
      }
      const body = (await res.json()) as { logs: LogAcesso[] };
      if (seq !== seqRef.current) {
        return;
      }
      setLogs(body.logs ?? []);
      setErroCarregar(null);
    } catch {
      if (seq === seqRef.current) {
        setErroCarregar(MENSAGEM_ERRO_CARREGAR);
      }
    } finally {
      if (seq === seqRef.current) {
        setCarregando(false);
      }
    }
  }, []);

  useEffect(() => {
    void (async () => {
      if (podeVer) {
        await carregar('', '');
      }
    })();
  }, [podeVer, carregar]);

  if (!podeVer) {
    return null;
  }

  const noLimite = logs.length === MAX_LOGS;

  return (
    <Card>
      <CardHeader>
        <h2 className="text-heading-md">Log de Acesso</h2>
        <CardDescription>
          Toda tentativa de login (por senha ou SSO, com sucesso ou falha). Somente leitura.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <div className="flex flex-wrap items-end gap-3">
          <div className="flex flex-col gap-1">
            <Label htmlFor="log-inicio">Início</Label>
            <Input
              id="log-inicio"
              type="date"
              value={inicio}
              onChange={(event) => setInicio(event.target.value)}
            />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="log-fim">Fim</Label>
            <Input
              id="log-fim"
              type="date"
              value={fim}
              onChange={(event) => setFim(event.target.value)}
            />
          </div>
          <Button type="button" onClick={() => void carregar(inicio, fim)}>
            Filtrar
          </Button>
        </div>

        {erroCarregar && (
          <p role="alert" className="text-body text-destructive">
            {erroCarregar}
          </p>
        )}

        {!erroCarregar && !carregando && logs.length === 0 && (
          <p className="text-body text-muted-foreground">Nenhum registro no período.</p>
        )}

        {!erroCarregar && noLimite && (
          <p className="text-body text-muted-foreground">
            Cada consulta mostra no máximo 500 registros (os mais recentes do período). Refine o
            período se precisar ver além disso.
          </p>
        )}

        {!erroCarregar && logs.length > 0 && (
          <div className="overflow-x-auto">
            <table className="w-full text-body">
              <thead>
                <tr className="text-label text-muted-foreground">
                  <th className="py-2 pr-4 text-left font-medium">Data/Hora</th>
                  <th className="py-2 pr-4 text-left font-medium">Usuário</th>
                  <th className="py-2 pr-4 text-left font-medium">E-mail informado</th>
                  <th className="py-2 pr-4 text-left font-medium">IP</th>
                  <th className="py-2 pr-4 text-left font-medium">Método</th>
                  <th className="py-2 text-left font-medium">Resultado</th>
                </tr>
              </thead>
              <tbody>
                {logs.map((log) => (
                  <tr key={log.id} className="border-t border-border">
                    <td className="py-2 pr-4">
                      {new Date(log.criadoEm).toLocaleString('pt-BR')}
                    </td>
                    <td className="py-2 pr-4">{log.usuarioNome ?? '—'}</td>
                    <td className="py-2 pr-4">{log.emailInformado}</td>
                    <td className="py-2 pr-4">{log.ip}</td>
                    <td className="py-2 pr-4">{log.metodo === 'sso' ? 'SSO' : 'Senha'}</td>
                    <td className="py-2">{log.sucesso ? 'Sucesso' : 'Falha'}</td>
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

export default LogAcessoSection;

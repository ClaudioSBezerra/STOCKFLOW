import { useCallback, useEffect, useMemo, useState } from 'react';
import { User } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { FaixaIndicadores } from '@/components/lista/FaixaIndicadores';
import { PilhulaStatus } from '@/components/lista/PilhulaStatus';
import { Input } from '@/components/ui/input';
import { ConfirmDialog } from '@/components/ConfirmDialog';
import { useAuth } from '@/lib/auth';
import { rankPapel } from '@/components/shell/nav-items';
import { papelAbaixo, rotuloPapel } from '@/lib/promocao';
import { apiUrl, authHeaders } from '@/lib/api';

/**
 * Seção "Gestão de Usuários" (Story 1.8; página `/admin/usuarios` e padrão de
 * lista na 17.5: busca, filtros de papel/situação, faixa Ativos/Sem MFA), só para `gestor`/`adm` (mesmo
 * gate de "Decidir promoções"). Lista `GET /api/usuarios` — o recorte de escopo
 * é do servidor: um `gestor` só recebe contas `usuario`/`almoxarife`, um `adm`
 * recebe todas.
 *
 * Ações por linha, exceto a do próprio ator:
 *  - "Desativar"/"Reativar" conforme `ativo` -> `POST /api/usuarios/{id}/desativacao`
 *    com `{ "ativo": false | true }`.
 *  - "Rebaixar para {papel}" quando existe papel abaixo -> `POST
 *    /api/usuarios/{id}/rebaixamento` (sem corpo; o alvo é derivado no servidor).
 *
 *  - "Resetar MFA" (Story 14.4) só para o ator `adm`, em conta com
 *    `mfaHabilitado` e papel abaixo de `adm` -> `POST /api/usuarios/{id}/mfa-reset`
 *    (sem corpo). O servidor revoga as sessões da conta e audita.
 *
 * "Desativar", "Rebaixar" e "Resetar MFA" reduzem acesso: passam por um `ConfirmDialog` único
 * (nunca `window.confirm()`). "Reativar" é direto. Falha de carga da lista e
 * falha de ação viram mensagem inline `role="alert"` (sem toast, molde de
 * `ConfiguracoesPage`); toda ação — sucesso OU falha — refaz o
 * `GET /api/usuarios`.
 */

interface UsuarioResumo {
  id: string;
  nome: string;
  email: string;
  papel: string;
  ativo: boolean;
  mfaHabilitado?: boolean;
}

type TipoAcao = 'desativar' | 'reativar' | 'rebaixar' | 'resetar-mfa';

interface AcaoPendente {
  id: string;
  tipo: 'desativar' | 'rebaixar' | 'resetar-mfa';
  nome: string;
  alvoRotulo?: string;
}

type SituacaoFiltro = 'todas' | 'ativas' | 'inativas';
const PAPEIS_ORDEM = ['usuario', 'almoxarife', 'gestor', 'adm'];

const MENSAGEM_ERRO_CARREGAR = 'Não foi possível carregar a lista de contas. Recarregue a página.';
const MENSAGEM_ERRO_ACAO = 'Não foi possível concluir a ação na conta.';

export function GestaoUsuariosSection() {
  const { usuario } = useAuth();
  const atorId = usuario?.id ?? '';
  const podeGerir = rankPapel(usuario?.papel ?? '') >= rankPapel('gestor');
  const atorEhAdm = usuario?.papel === 'adm';

  const [contas, setContas] = useState<UsuarioResumo[]>([]);
  const [carregou, setCarregou] = useState(false);
  const [erroCarregar, setErroCarregar] = useState<string | null>(null);
  const [erroAcao, setErroAcao] = useState<string | null>(null);
  const [acaoEmCurso, setAcaoEmCurso] = useState(false);
  const [acaoPendente, setAcaoPendente] = useState<AcaoPendente | null>(null);
  const [busca, setBusca] = useState('');
  const [filtroPapel, setFiltroPapel] = useState('');
  const [situacao, setSituacao] = useState<SituacaoFiltro>('todas');

  // Indicadores sobre a lista COMPLETA carregada (não a filtrada).
  const totalAtivos = contas.filter((c) => c.ativo).length;
  const totalSemMfa = contas.filter((c) => c.ativo && !c.mfaHabilitado).length;
  const papeisPresentes = useMemo(
    () => PAPEIS_ORDEM.filter((p) => contas.some((c) => c.papel === p)),
    [contas],
  );
  // Papel filtrado que sumiu da lista após recarga vale como "todos".
  const papelEfetivo = papeisPresentes.includes(filtroPapel) ? filtroPapel : '';
  const contasFiltradas = useMemo(() => {
    const termo = busca.trim().toLowerCase();
    return contas.filter(
      (c) =>
        (termo === '' ||
          c.nome.toLowerCase().includes(termo) ||
          c.email.toLowerCase().includes(termo)) &&
        (papelEfetivo === '' || c.papel === papelEfetivo) &&
        (situacao === 'todas' || (situacao === 'ativas' ? c.ativo : !c.ativo)),
    );
  }, [contas, busca, papelEfetivo, situacao]);

  function limparFiltros() {
    setBusca('');
    setFiltroPapel('');
    setSituacao('todas');
  }

  const carregar = useCallback(async () => {
    try {
      const res = await fetch(apiUrl('/api/usuarios'), {
        headers: authHeaders(),
      });
      if (!res.ok) {
        setErroCarregar(MENSAGEM_ERRO_CARREGAR);
        return;
      }
      const body = (await res.json()) as { usuarios: UsuarioResumo[] };
      setContas(body.usuarios ?? []);
      setCarregou(true);
      setErroCarregar(null);
    } catch {
      setErroCarregar(MENSAGEM_ERRO_CARREGAR);
    }
  }, []);

  useEffect(() => {
    void (async () => {
      if (podeGerir) {
        await carregar();
      }
    })();
  }, [podeGerir, carregar]);

  async function executar(id: string, tipo: TipoAcao) {
    if (acaoEmCurso) {
      return;
    }
    setErroAcao(null);
    setAcaoEmCurso(true);
    try {
      const url =
        tipo === 'rebaixar'
          ? `/api/usuarios/${id}/rebaixamento`
          : tipo === 'resetar-mfa'
            ? `/api/usuarios/${id}/mfa-reset`
            : `/api/usuarios/${id}/desativacao`;
      const init: RequestInit =
        tipo === 'rebaixar' || tipo === 'resetar-mfa'
          ? { method: 'POST', headers: authHeaders() }
          : {
              method: 'POST',
              headers: { 'Content-Type': 'application/json', ...authHeaders() },
              body: JSON.stringify({ ativo: tipo === 'reativar' }),
            };
      const res = await fetch(apiUrl(url), init);
      if (!res.ok) {
        setErroAcao(MENSAGEM_ERRO_ACAO);
      }
    } catch {
      setErroAcao(MENSAGEM_ERRO_ACAO);
    } finally {
      setAcaoEmCurso(false);
      // Sucesso OU falha: a lista é refeita para a linha refletir o estado
      // real (ou a linha obsoleta cair após um 404/409).
      await carregar();
    }
  }

  function confirmarAcao() {
    if (!acaoPendente) {
      return;
    }
    const { id, tipo } = acaoPendente;
    setAcaoPendente(null);
    void executar(id, tipo);
  }

  if (!podeGerir) {
    return null;
  }

  const selectClasse =
    'text-body min-h-touch-target-min rounded-md border border-border bg-background px-3';

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-2">
        <Input
          type="search"
          aria-label="Buscar por nome ou e-mail"
          placeholder="Buscar por nome ou e-mail"
          value={busca}
          onChange={(event) => setBusca(event.target.value)}
          className="max-w-xs"
        />
        <select
          aria-label="Papel"
          value={papelEfetivo}
          onChange={(event) => setFiltroPapel(event.target.value)}
          className={selectClasse}
        >
          <option value="">Todos os papéis</option>
          {papeisPresentes.map((p) => (
            <option key={p} value={p}>
              {rotuloPapel(p)}
            </option>
          ))}
        </select>
        <select
          aria-label="Situação"
          value={situacao}
          onChange={(event) => setSituacao(event.target.value as SituacaoFiltro)}
          className={selectClasse}
        >
          <option value="todas">Todas</option>
          <option value="ativas">Ativas</option>
          <option value="inativas">Inativas</option>
        </select>
        <Button type="button" variant="outline" size="sm" onClick={limparFiltros}>
          Limpar filtros
        </Button>
      </div>

      <FaixaIndicadores
        indicadores={[
          { rotulo: 'Ativos', valor: carregou && !erroCarregar ? totalAtivos : null },
          {
            rotulo: 'Sem MFA',
            valor: carregou && !erroCarregar ? totalSemMfa : null,
            alerta: usuario?.empresa?.mfaObrigatorio === true,
          },
        ]}
      />

      {erroAcao && (
        <p role="alert" className="text-body text-destructive">
          {erroAcao}
        </p>
      )}
      {erroCarregar && (
        <p role="alert" className="text-body text-destructive">
          {erroCarregar}
        </p>
      )}
      {!erroCarregar && carregou && contas.length === 0 && (
        <p className="text-body text-muted-foreground">Nenhuma conta para gerir.</p>
      )}
      {!erroCarregar && carregou && contas.length > 0 && contasFiltradas.length === 0 && (
        <p className="text-body text-muted-foreground">
          Nenhuma conta encontrada com esses filtros.
        </p>
      )}
      {!erroCarregar && contasFiltradas.length > 0 && (
        <ul className="flex flex-col">
          {contasFiltradas.map((c) => {
            const abaixo = papelAbaixo(c.papel);
            const ehAtor = c.id === atorId;
            const podeResetarMfa =
              atorEhAdm && c.mfaHabilitado === true && rankPapel(c.papel) < rankPapel('adm');
            return (
              <li
                key={c.id}
                className="flex min-h-[60px] flex-wrap items-center gap-3 border-b border-border"
              >
                <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-muted">
                  <User aria-hidden="true" className="h-4 w-4 text-muted-foreground" />
                </div>
                <div className="flex min-w-0 flex-1 flex-col">
                  <span className="text-body font-medium">{c.nome}</span>
                  <span className="text-label text-muted-foreground">{c.email}</span>
                </div>
                <span className="flex shrink-0 items-center gap-2">
                  <PilhulaStatus status={rotuloPapel(c.papel)} />
                  {!c.ativo && <PilhulaStatus status="Inativa" />}
                </span>
                {!ehAtor && (
                  <div className="flex flex-wrap gap-2">
                    {c.ativo ? (
                      <Button
                        type="button"
                        size="sm"
                        variant="destructive"
                        aria-label={`Desativar conta de ${c.nome}`}
                        onClick={() =>
                          setAcaoPendente({
                            id: c.id,
                            tipo: 'desativar',
                            nome: c.nome,
                          })
                        }
                        disabled={acaoEmCurso}
                      >
                        Desativar
                      </Button>
                    ) : (
                      <Button
                        type="button"
                        size="sm"
                        aria-label={`Reativar conta de ${c.nome}`}
                        onClick={() => void executar(c.id, 'reativar')}
                        disabled={acaoEmCurso}
                      >
                        Reativar
                      </Button>
                    )}
                    {abaixo && (
                      <Button
                        type="button"
                        size="sm"
                        variant="outline"
                        aria-label={`Rebaixar ${c.nome} para ${rotuloPapel(abaixo)}`}
                        onClick={() =>
                          setAcaoPendente({
                            id: c.id,
                            tipo: 'rebaixar',
                            nome: c.nome,
                            alvoRotulo: rotuloPapel(abaixo),
                          })
                        }
                        disabled={acaoEmCurso}
                      >
                        Rebaixar para {rotuloPapel(abaixo)}
                      </Button>
                    )}
                    {podeResetarMfa && (
                      <Button
                        type="button"
                        size="sm"
                        variant="outline"
                        aria-label={`Resetar MFA de ${c.nome}`}
                        onClick={() =>
                          setAcaoPendente({
                            id: c.id,
                            tipo: 'resetar-mfa',
                            nome: c.nome,
                          })
                        }
                        disabled={acaoEmCurso}
                      >
                        Resetar MFA
                      </Button>
                    )}
                  </div>
                )}
              </li>
            );
          })}
        </ul>
      )}

      <ConfirmDialog
        open={acaoPendente !== null}
        onOpenChange={(aberto) => {
          if (!aberto) {
            setAcaoPendente(null);
          }
        }}
        onConfirm={confirmarAcao}
        title={
          acaoPendente?.tipo === 'rebaixar'
            ? `Rebaixar ${acaoPendente.nome} para ${acaoPendente.alvoRotulo}?`
            : acaoPendente?.tipo === 'resetar-mfa'
              ? `Resetar a dupla autenticação de ${acaoPendente.nome}?`
              : `Desativar a conta de ${acaoPendente?.nome ?? ''}?`
        }
        description={
          acaoPendente?.tipo === 'rebaixar'
            ? 'A conta continua entrando, mas com menos privilégio já na próxima requisição.'
            : acaoPendente?.tipo === 'resetar-mfa'
              ? 'As sessões da conta são encerradas e ela precisará configurar um novo MFA se a Empresa exigir.'
              : 'A conta perde o acesso imediatamente e as sessões ativas são encerradas.'
        }
        confirmLabel={
          acaoPendente?.tipo === 'rebaixar'
            ? 'Rebaixar'
            : acaoPendente?.tipo === 'resetar-mfa'
              ? 'Resetar MFA'
              : 'Desativar'
        }
      />
    </div>
  );
}

export default GestaoUsuariosSection;

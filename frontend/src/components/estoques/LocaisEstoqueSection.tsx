import { useCallback, useEffect, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { MapPin } from 'lucide-react';
import { FaixaIndicadores } from '@/components/lista/FaixaIndicadores';
import { rankPapel } from '@/components/shell/nav-items';
import { apiUrl, authHeaders } from '@/lib/api';
import { useAuth } from '@/lib/auth';

/**
 * Seção "Depósitos" da página `/estoques` (spec-depositos-na-filial): SÓ
 * CONSULTA. O cadastro e a exclusão de Depósitos moram em Cadastros > Filiais
 * (`DepositosDaFilial`, gestor+). Mostra a faixa de indicadores
 * (`/api/estoques/indicadores`, "—" se falhar; `seqIndRef` descarta respostas
 * fora de ordem) e a lista de `GET /api/estoques` agrupada por Filial
 * (`GET /api/filiais`; Filial como título do grupo, depósito legado sem
 * Filial em "Sem filial"), com `totalItens` por depósito. O texto de apoio
 * depende do papel: gestor+ recebe o link para `/cadastros/filiais`;
 * almoxarife, a orientação de pedir a um gestor.
 *
 * O nome do arquivo/componente e a rota `/estoques` foram mantidos (só os
 * textos visíveis mudaram de "Local" para "Depósito").
 */

interface Deposito {
  id: string;
  nome: string;
  filial_id?: string | null;
  filial_nome?: string | null;
  totalItens?: number;
}

interface IndicadoresDepositos {
  locais: number;
  itensEmEstoque: number;
}

interface Filial {
  id: string;
  nome: string;
}

interface Grupo {
  chave: string;
  titulo: string;
  depositos: Deposito[];
}

const MENSAGEM_ERRO_CARREGAR =
  'Não foi possível carregar a lista de depósitos. Recarregue a página.';
const MENSAGEM_ERRO_CARREGAR_FILIAIS = 'Não foi possível carregar as filiais. Recarregue a página.';

export function LocaisEstoqueSection() {
  const { usuario } = useAuth();
  const ehGestor = rankPapel(usuario?.papel ?? '') >= rankPapel('gestor');
  const [depositos, setDepositos] = useState<Deposito[]>([]);
  const [carregouDepositos, setCarregouDepositos] = useState(false);
  const [filiais, setFiliais] = useState<Filial[]>([]);
  const [filiaisCarregadas, setFiliaisCarregadas] = useState(false);
  const [erroCarregar, setErroCarregar] = useState<string | null>(null);
  const [erroFiliais, setErroFiliais] = useState<string | null>(null);
  const [indicadores, setIndicadores] = useState<IndicadoresDepositos | null>(null);

  const carregar = useCallback(async () => {
    try {
      const res = await fetch(apiUrl('/api/estoques'), {
        headers: authHeaders(),
      });
      if (!res.ok) {
        setErroCarregar(MENSAGEM_ERRO_CARREGAR);
        return;
      }
      const body = (await res.json()) as { estoques: Deposito[] };
      setDepositos(body.estoques ?? []);
      setErroCarregar(null);
      setCarregouDepositos(true);
    } catch {
      setErroCarregar(MENSAGEM_ERRO_CARREGAR);
    }
  }, []);

  const carregarFiliais = useCallback(async () => {
    try {
      const res = await fetch(apiUrl('/api/filiais'), {
        headers: authHeaders(),
      });
      if (!res.ok) {
        setErroFiliais(MENSAGEM_ERRO_CARREGAR_FILIAIS);
        return;
      }
      const body = (await res.json()) as { filiais?: Filial[] };
      setFiliais(body.filiais ?? []);
      setErroFiliais(null);
      setFiliaisCarregadas(true);
    } catch {
      setErroFiliais(MENSAGEM_ERRO_CARREGAR_FILIAIS);
    }
  }, []);

  const seqIndRef = useRef(0);
  const carregarIndicadores = useCallback(async () => {
    const seq = ++seqIndRef.current;
    try {
      const res = await fetch(apiUrl('/api/estoques/indicadores'), {
        headers: authHeaders(),
      });
      if (seq !== seqIndRef.current) return;
      if (!res.ok) {
        setIndicadores(null);
        return;
      }
      const corpo = (await res.json()) as IndicadoresDepositos;
      if (seq !== seqIndRef.current) return;
      setIndicadores(corpo);
    } catch {
      if (seq === seqIndRef.current) setIndicadores(null);
    }
  }, []);

  useEffect(() => {
    void (async () => {
      await Promise.all([carregar(), carregarFiliais(), carregarIndicadores()]);
    })();
  }, [carregar, carregarFiliais, carregarIndicadores]);

  // Sem as Filiais não dá para agrupar: tudo cairia em "Sem filial", atribuído
  // errado. Com a carga de Filiais falha, mostra a lista plana (com o aviso).
  const agrupar = erroFiliais === null;
  const grupos: Grupo[] = filiais.map((f) => ({
    chave: f.id,
    titulo: f.nome,
    depositos: depositos.filter((d) => d.filial_id === f.id),
  }));
  const idsConhecidos = new Set(filiais.map((f) => f.id));
  const semFilial = depositos.filter((d) => !d.filial_id || !idsConhecidos.has(d.filial_id));
  if (semFilial.length > 0) {
    grupos.push({
      chave: 'sem-filial',
      titulo: 'Sem filial',
      depositos: semFilial,
    });
  }

  const tudoCarregado = carregouDepositos && (filiaisCarregadas || erroFiliais !== null);
  const semNenhumaFilial = filiaisCarregadas && filiais.length === 0 && semFilial.length === 0;

  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-heading-lg">Depósitos</h1>

      <FaixaIndicadores
        indicadores={[
          { rotulo: 'Depósitos', valor: indicadores?.locais ?? null },
          {
            rotulo: 'Itens em estoque',
            valor: indicadores?.itensEmEstoque ?? null,
          },
        ]}
      />

      <p className="text-body text-muted-foreground">
        {ehGestor ? (
          <>
            Para cadastrar ou excluir depósitos, use{' '}
            <Link to="/cadastros/filiais" className="underline">
              Cadastros &gt; Filiais
            </Link>
            .
          </>
        ) : (
          'Peça a um gestor ou administrador para cadastrar ou excluir depósitos.'
        )}
      </p>

      {erroCarregar && (
        <p role="alert" className="text-body text-destructive">
          {erroCarregar}
        </p>
      )}
      {erroFiliais && (
        <p role="alert" className="text-body text-destructive">
          {erroFiliais}
        </p>
      )}
      {!erroCarregar && semNenhumaFilial && (
        <p className="text-body text-muted-foreground">
          Nenhuma filial cadastrada. Primeiro cadastre uma filial (Cadastros &gt; Filiais); os
          depósitos ficam dentro dela.
        </p>
      )}
      {!erroCarregar && tudoCarregado && depositos.length === 0 && !semNenhumaFilial && (
        <p className="text-body text-muted-foreground">Nenhum depósito cadastrado ainda.</p>
      )}
      {!erroCarregar && !agrupar && depositos.length > 0 && (
        <ul className="flex flex-col">
          {depositos.map((d) => (
            <li
              key={d.id}
              className="text-body flex min-h-[60px] flex-wrap items-center gap-3 border-b border-border"
            >
              <MapPin aria-hidden="true" className="h-4 w-4 text-muted-foreground" />
              <span className="min-w-0 flex-1 break-words font-medium">{d.nome}</span>
              <span className="shrink-0 tabular-nums" aria-label={`${d.totalItens ?? 0} itens`}>
                {d.totalItens ?? 0}
              </span>
            </li>
          ))}
        </ul>
      )}
      {!erroCarregar &&
        agrupar &&
        grupos
          .filter((g) => g.depositos.length > 0)
          .map((g) => (
            <section key={g.chave} aria-label={`Filial ${g.titulo}`} className="flex flex-col">
              <h2 className="text-heading-md border-b border-border py-2">{g.titulo}</h2>
              <ul className="flex flex-col">
                {g.depositos.map((d) => (
                  <li
                    key={d.id}
                    className="text-body flex min-h-[60px] flex-wrap items-center gap-3 border-b border-border"
                  >
                    <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-muted">
                      <MapPin aria-hidden="true" className="h-4 w-4 text-muted-foreground" />
                    </div>
                    <span className="min-w-0 flex-1 break-words font-medium">{d.nome}</span>
                    <span
                      className="shrink-0 text-body tabular-nums"
                      aria-label={`${d.totalItens ?? 0} itens`}
                    >
                      {d.totalItens ?? 0}
                    </span>
                  </li>
                ))}
              </ul>
            </section>
          ))}
    </div>
  );
}

export default LocaisEstoqueSection;

import { useCallback, useEffect, useState } from 'react';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader } from '@/components/ui/card';
import { rotuloPapel } from '@/lib/promocao';
import { apiUrl, authHeaders } from '@/lib/api';

/**
 * "Decidir promoções" (extraída de `ConfiguracoesPage` na Story 17.2).
 * Lista `GET /api/promocoes` com "Aprovar"/"Recusar" por item, chamando
 * `POST /api/promocoes/{id}/decisao`. O gate de papel (`gestor`+) fica na
 * página que a monta; o servidor segue como autoridade.
 */

interface SolicitacaoPendente {
  id: string;
  solicitante_nome: string;
  solicitante_email: string;
  papel_atual: string;
  papel_alvo: string;
  criado_em: string;
}

const MENSAGEM_ERRO_DECISAO = 'Não foi possível concluir a decisão.';

export function DecidirPromocoesSection() {
  const [pendentes, setPendentes] = useState<SolicitacaoPendente[]>([]);
  const [decidindoId, setDecidindoId] = useState<string | null>(null);
  const [erroDecisao, setErroDecisao] = useState<string | null>(null);
  const [avisoDecisao, setAvisoDecisao] = useState<string | null>(null);
  const [erroCarregarPendentes, setErroCarregarPendentes] = useState<string | null>(null);

  const carregarPendentes = useCallback(async () => {
    try {
      const res = await fetch(apiUrl('/api/promocoes'), { headers: authHeaders() });
      if (!res.ok) {
        // Sem este alerta, uma falha de carga deixaria o gestor/adm olhando
        // "Nenhuma solicitação pendente." — um falso "nada a fazer" que
        // esconde promoções reais aguardando decisão.
        setErroCarregarPendentes('Não foi possível carregar as solicitações pendentes.');
        return;
      }
      const body = (await res.json()) as { solicitacoes: SolicitacaoPendente[] };
      setPendentes(body.solicitacoes ?? []);
      setErroCarregarPendentes(null);
    } catch {
      setErroCarregarPendentes('Não foi possível carregar as solicitações pendentes.');
    }
  }, []);

  useEffect(() => {
    void (async () => {
      await carregarPendentes();
    })();
  }, [carregarPendentes]);

  async function decidir(id: string, aprovar: boolean) {
    if (decidindoId !== null) {
      return;
    }
    setErroDecisao(null);
    setAvisoDecisao(null);
    setDecidindoId(id);
    try {
      const res = await fetch(apiUrl(`/api/promocoes/${id}/decisao`), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', ...authHeaders() },
        body: JSON.stringify({ aprovar }),
      });
      if (!res.ok) {
        setErroDecisao(MENSAGEM_ERRO_DECISAO);
        // Uma decisão que falha por 404/409 (o item já foi decidido por outro
        // gestor) deixaria a linha na lista para ser retentada sem fim —
        // recarrega a fila para a linha obsoleta cair.
        await carregarPendentes();
        return;
      }
      setAvisoDecisao(aprovar ? 'Promoção aprovada.' : 'Promoção recusada.');
      await carregarPendentes();
    } catch {
      setErroDecisao(MENSAGEM_ERRO_DECISAO);
      // Mesma razão do ramo `!res.ok`: uma falha aqui pode ter coincidido com
      // o item já sendo decidido por outro gestor — recarrega a fila para a
      // linha obsoleta cair em vez de ser retentada sem fim.
      await carregarPendentes();
    } finally {
      setDecidindoId(null);
    }
  }

  return (
      <Card>
      <CardHeader>
        <h2 className="text-heading-md">Decidir promoções</h2>
        <CardDescription>Solicitações de promoção aguardando sua decisão.</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {avisoDecisao && <output className="text-body">{avisoDecisao}</output>}
        {erroDecisao && (
          <p role="alert" className="text-body text-destructive">
            {erroDecisao}
          </p>
        )}
        {erroCarregarPendentes && (
          <p role="alert" className="text-body text-destructive">
            {erroCarregarPendentes}
          </p>
        )}
        {!erroCarregarPendentes && pendentes.length === 0 && (
          <p className="text-body text-muted-foreground">Nenhuma solicitação pendente.</p>
        )}
        {!erroCarregarPendentes && pendentes.length > 0 && (
          <ul className="flex flex-col gap-3">
            {pendentes.map((p) => (
              <li
                key={p.id}
                className="flex flex-col gap-2 border-b border-border pb-3 last:border-b-0 last:pb-0"
              >
                <div className="flex flex-col">
                  <span className="text-body">{p.solicitante_nome}</span>
                  <span className="text-label text-muted-foreground">
                    {p.solicitante_email}
                  </span>
                  <span className="text-label text-muted-foreground">
                    {rotuloPapel(p.papel_atual)} &rarr; {rotuloPapel(p.papel_alvo)}
                  </span>
                </div>
                <div className="flex gap-2">
                  <Button
                    type="button"
                    size="sm"
                    aria-label={`Aprovar promoção de ${p.solicitante_nome}`}
                    onClick={() => decidir(p.id, true)}
                    disabled={decidindoId !== null}
                  >
                    Aprovar
                  </Button>
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    aria-label={`Recusar promoção de ${p.solicitante_nome}`}
                    onClick={() => decidir(p.id, false)}
                    disabled={decidindoId !== null}
                  >
                    Recusar
                  </Button>
                </div>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}

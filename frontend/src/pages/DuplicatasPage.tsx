import { useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { useAuth } from '@/lib/auth';
import { rankPapel } from '@/components/shell/nav-items';
import { DuplicatasSection } from '@/components/normalizacao/DuplicatasSection';

/**
 * Página "Duplicatas" (`/normalizacao/duplicatas`, Story 6.3; rota própria
 * desde a Story 17.1). `?verificarDuplicatas=1` (CTA "Verificar duplicatas
 * agora" da importação) passa `autoAnalisar` à `DuplicatasSection`, no MÁXIMO
 * uma vez por visita: a seção chama `onAutoAnalisado` ao disparar e o guard
 * `autoAnalisarConsumido` daqui impede repetir se a seção remontar.
 * Gate `almoxarife`+; o servidor é a autoridade.
 */
export function DuplicatasPage() {
  const { usuario } = useAuth();
  const podeGerir = rankPapel(usuario?.papel ?? '') >= rankPapel('almoxarife');
  const [searchParams] = useSearchParams();
  const [autoAnalisarConsumido, setAutoAnalisarConsumido] = useState(false);
  const autoAnalisar = searchParams.get('verificarDuplicatas') === '1' && !autoAnalisarConsumido;

  return (
    <div className="flex flex-col gap-6 p-6">
      {podeGerir ? (
        <>
          <h1 className="text-heading-lg">Duplicatas</h1>
          <DuplicatasSection
            autoAnalisar={autoAnalisar}
            onAutoAnalisado={() => setAutoAnalisarConsumido(true)}
          />
        </>
      ) : (
        <p className="text-body text-muted-foreground">
          Você não tem acesso à área de Normalização.
        </p>
      )}
    </div>
  );
}

export default DuplicatasPage;

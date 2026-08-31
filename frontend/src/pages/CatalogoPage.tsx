import { useAuth } from '@/lib/auth';
import { rankPapel } from '@/components/shell/nav-items';
import { CadastroProdutoSection } from '@/components/produtos/CadastroProdutoSection';

/**
 * Página "Catálogo" (`/`, item de nav "Catálogo", Story 3.1, spec-3-1) —
 * substitui a `PlaceholderPage` da rota índice. Renderizada dentro do
 * `AppShell`/`RotaProtegida`.
 *
 * Sempre mostra um aviso de "busca em breve" (Epic 4), para qualquer papel.
 * Quando `rankPapel(papel) >= rankPapel('almoxarife')`, empilha também a
 * seção de cadastro (`CadastroProdutoSection`) abaixo do aviso — mesma
 * simplificação deliberada de `ConfiguracoesPage`/`EstoquesPage`: sem abas
 * horizontais do `AppShell` ainda, mesmo a IA de EXPERIENCE.md descrevendo
 * Cadastro/Importação como abas do módulo Catálogo.
 *
 * Gate de papel espelhado do `nav-items.ts`/`EstoquesPage`: o servidor
 * continua sendo a autoridade real — `POST /api/produtos` responde 403 para
 * papéis abaixo de `almoxarife` mesmo em chamada direta à API; este espelho é
 * só de experiência.
 */
export function CatalogoPage() {
  const { usuario } = useAuth();
  const podeCadastrar = rankPapel(usuario?.papel ?? '') >= rankPapel('almoxarife');

  return (
    <div className="flex flex-col gap-6 p-6">
      <p className="text-body text-muted-foreground">
        Busca e visualização do catálogo chegam em breve.
      </p>
      {podeCadastrar && <CadastroProdutoSection />}
    </div>
  );
}

export default CatalogoPage;

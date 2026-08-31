import { useAuth } from '@/lib/auth';
import { rankPapel } from '@/components/shell/nav-items';
import { CadastroProdutoSection } from '@/components/produtos/CadastroProdutoSection';
import { ImportacaoProdutosSection } from '@/components/produtos/ImportacaoProdutosSection';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';

/**
 * Página "Catálogo" (`/`, item de nav "Catálogo", Story 3.1, spec-3-1) —
 * substitui a `PlaceholderPage` da rota índice. Renderizada dentro do
 * `AppShell`/`RotaProtegida`.
 *
 * Sempre mostra um aviso de "busca em breve" (Epic 4), para qualquer papel.
 * Quando `rankPapel(papel) >= rankPapel('almoxarife')`, mostra também
 * `Tabs` ("Cadastro"/"Importação", Story 3.3, spec-3-3) envolvendo
 * `CadastroProdutoSection`/`ImportacaoProdutosSection` — resolve o que antes
 * era uma simplificação deliberada (empilhamento simples, sem abas), agora
 * que a Story 3.3 entrega o segundo fluxo que faz as abas valerem a pena.
 *
 * Gate de papel espelhado do `nav-items.ts`/`EstoquesPage`: o servidor
 * continua sendo a autoridade real — `POST /api/produtos`/`POST
 * /api/importacoes` respondem 403 para papéis abaixo de `almoxarife` mesmo em
 * chamada direta à API; este espelho é só de experiência.
 */
export function CatalogoPage() {
  const { usuario } = useAuth();
  const podeCadastrar = rankPapel(usuario?.papel ?? '') >= rankPapel('almoxarife');

  return (
    <div className="flex flex-col gap-6 p-6">
      <p className="text-body text-muted-foreground">
        Busca e visualização do catálogo chegam em breve.
      </p>
      {podeCadastrar && (
        <Tabs defaultValue="cadastro">
          <TabsList>
            <TabsTrigger value="cadastro">Cadastro</TabsTrigger>
            <TabsTrigger value="importacao">Importação</TabsTrigger>
          </TabsList>
          <TabsContent value="cadastro">
            <CadastroProdutoSection />
          </TabsContent>
          <TabsContent value="importacao">
            <ImportacaoProdutosSection />
          </TabsContent>
        </Tabs>
      )}
    </div>
  );
}

export default CatalogoPage;

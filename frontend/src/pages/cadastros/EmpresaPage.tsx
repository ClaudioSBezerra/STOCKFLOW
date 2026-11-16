import { useAuth } from '@/lib/auth';
import { rankPapel } from '@/components/shell/nav-items';
import { LogoEmpresaSection } from '@/components/empresa/LogoEmpresaSection';

/**
 * Página "Empresa" (spec-logo-da-empresa). Gate `gestor`+ espelhado de
 * `nav-items.ts`; sem o papel, mostra a recusa e não monta a seção (nenhum
 * fetch). Hoje só cuida da logo impressa na Pré-Nota.
 */
export function EmpresaPage() {
  const { usuario } = useAuth();
  const permitido = rankPapel(usuario?.papel ?? '') >= rankPapel('gestor');

  return (
    <div className="flex flex-col gap-6 p-6">
      {permitido ? (
        <>
          <h1 className="text-heading-lg">Empresa</h1>
          <LogoEmpresaSection />
        </>
      ) : (
        <p className="text-body text-muted-foreground">Você não tem acesso a Empresa.</p>
      )}
    </div>
  );
}

export default EmpresaPage;

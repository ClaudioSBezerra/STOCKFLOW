/**
 * Página mínima só para hospedar o `AppShell` até a Story 3.x/4.x existir
 * (ver spec-1-2, Never: "Construir telas reais de produto").
 */
export function PlaceholderPage() {
  return (
    <div className="flex flex-col gap-2 p-6">
      <h1 className="text-heading-lg">Em construção</h1>
      <p className="text-body text-muted-foreground">
        Esta tela ainda não existe. O shell de navegação está pronto para as próximas stories.
      </p>
    </div>
  );
}

export default PlaceholderPage;

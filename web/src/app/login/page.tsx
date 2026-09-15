import type { Metadata } from "next";
import { Suspense } from "react";
import { Card, CardContent } from "@/components/ui/card";
import { FormularioLogin } from "./formulario";

export const metadata: Metadata = { title: "Entrar · Travus Plataforma" };

export default function PaginaLogin() {
  return (
    <main className="flex flex-1 items-center justify-center bg-muted/40 px-4 py-12">
      <div className="w-full max-w-sm">
        <div className="mb-6 text-center">
          <h1 className="text-2xl font-semibold tracking-tight">Travus Plataforma</h1>
          <p className="text-sm text-muted-foreground">Entre com seu e-mail e senha</p>
        </div>
        <Card>
          <CardContent>
            {/* useSearchParams (destino depois do login) precisa de Suspense. */}
            <Suspense>
              <FormularioLogin />
            </Suspense>
          </CardContent>
        </Card>
      </div>
    </main>
  );
}

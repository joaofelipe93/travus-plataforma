import type { Metadata } from "next";
import { Suspense } from "react";
import { Marca } from "@/components/marca";
import { FormularioLogin } from "./formulario";

export const metadata: Metadata = { title: "Entrar · Travus Plataforma" };

// Sem cartão nem texto de apresentação: quem abre esta tela já sabe onde está. A marca e o
// botão Entrar (ouro) são as únicas cores.
export default function PaginaLogin() {
  return (
    <main className="flex flex-1 items-center justify-center px-4 py-12">
      <div className="w-full max-w-xs">
        <h1 className="mb-10 text-center">
          <Marca className="text-4xl" />
          <span className="sr-only">Travus Plataforma: entrar</span>
        </h1>
        {/* useSearchParams (destino depois do login) precisa de Suspense. */}
        <Suspense>
          <FormularioLogin />
        </Suspense>
      </div>
    </main>
  );
}

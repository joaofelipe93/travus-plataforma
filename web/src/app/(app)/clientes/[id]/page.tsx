"use client";

import { useParams } from "next/navigation";
import { EspacoClientes } from "@/components/espaco-clientes";

// Mesma tela da lista, com o cliente do endereço já aberto no painel.
export default function PaginaCliente() {
  const { id } = useParams<{ id: string }>();
  const numero = Number(id);
  return <EspacoClientes selecionadoId={Number.isSafeInteger(numero) && numero > 0 ? numero : undefined} />;
}

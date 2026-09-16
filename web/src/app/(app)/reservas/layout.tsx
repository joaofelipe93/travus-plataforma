import { EspacoServico } from "@/components/espaco-servico";

export default function LayoutReservas({ children }: LayoutProps<"/reservas">) {
  return <EspacoServico id="reservas">{children}</EspacoServico>;
}

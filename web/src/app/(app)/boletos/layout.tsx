import { EspacoServico } from "@/components/espaco-servico";

export default function LayoutBoletos({ children }: LayoutProps<"/boletos">) {
  return <EspacoServico id="boletos">{children}</EspacoServico>;
}

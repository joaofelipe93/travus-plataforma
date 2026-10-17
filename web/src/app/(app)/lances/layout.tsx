import { EspacoServico } from "@/components/espaco-servico";

export default function LayoutLances({ children }: LayoutProps<"/lances">) {
  return <EspacoServico id="lances">{children}</EspacoServico>;
}

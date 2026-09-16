import { EspacoServico } from "@/components/espaco-servico";

export default function LayoutCanopus({ children }: LayoutProps<"/canopus">) {
  return <EspacoServico id="canopus">{children}</EspacoServico>;
}

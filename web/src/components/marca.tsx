import { cn } from "@/lib/utils";

// Nome da marca em Poppins, no ouro do logo da Travus Capital. Provisório até o logo em SVG.
export function Marca({ className }: { className?: string }) {
  return (
    <span className={cn("font-heading text-xl font-semibold tracking-tight text-ouro", className)}>
      travus
    </span>
  );
}

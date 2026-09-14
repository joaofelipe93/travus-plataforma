import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "Travus Plataforma",
  description: "Automação de credenciamento de lances e serviços da Travus",
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html lang="pt-BR" className="h-full antialiased">
      <body className="min-h-full flex flex-col">{children}</body>
    </html>
  );
}

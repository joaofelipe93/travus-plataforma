import type { Metadata } from "next";
import localFont from "next/font/local";
import "./globals.css";
import { Providers } from "./providers";

// Fontes da marca empacotadas no build (sem requisição ao Google).
const inter = localFont({
  src: "../../node_modules/@fontsource-variable/inter/files/inter-latin-wght-normal.woff2",
  variable: "--fonte-inter",
  weight: "100 900",
  display: "swap",
});
const poppins = localFont({
  src: [
    { path: "../../node_modules/@fontsource/poppins/files/poppins-latin-500-normal.woff2", weight: "500" },
    { path: "../../node_modules/@fontsource/poppins/files/poppins-latin-600-normal.woff2", weight: "600" },
  ],
  variable: "--fonte-poppins",
  display: "swap",
});

export const metadata: Metadata = {
  title: "Travus Plataforma",
  description: "Serviços internos da Travus Capital",
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    // "dark": tema único escuro (as variantes dark: dos componentes valem sempre).
    <html lang="pt-BR" className={`dark h-full antialiased font-sans ${inter.variable} ${poppins.variable}`}>
      <body className="min-h-full flex flex-col">
        <Providers>{children}</Providers>
      </body>
    </html>
  );
}

"use client";

import Markdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";

// Markdown das respostas do assistente. O react-markdown não interpreta HTML cru: o texto do
// modelo (que pode repetir dados do PMS) nunca vira marcação na página.
const componentes: Components = {
  p: ({ children }) => <p className="leading-relaxed [&:not(:first-child)]:mt-3">{children}</p>,
  strong: ({ children }) => <strong className="font-semibold text-foreground">{children}</strong>,
  ul: ({ children }) => <ul className="mt-2 list-disc space-y-1 pl-5 marker:text-muted-foreground">{children}</ul>,
  ol: ({ children }) => <ol className="mt-2 list-decimal space-y-1 pl-5 marker:text-muted-foreground">{children}</ol>,
  li: ({ children }) => <li className="leading-relaxed">{children}</li>,
  h1: ({ children }) => <h3 className="mt-4 text-base font-semibold first:mt-0">{children}</h3>,
  h2: ({ children }) => <h3 className="mt-4 text-base font-semibold first:mt-0">{children}</h3>,
  h3: ({ children }) => <h3 className="mt-4 text-sm font-semibold first:mt-0">{children}</h3>,
  a: ({ children, href }) => (
    <a href={href} target="_blank" rel="noopener noreferrer" className="text-ouro underline underline-offset-2 hover:text-ouro-forte">
      {children}
    </a>
  ),
  code: ({ children }) => <code className="rounded bg-secondary px-1 py-0.5 font-mono text-[0.85em]">{children}</code>,
  pre: ({ children }) => (
    <pre className="mt-3 overflow-x-auto rounded-md bg-secondary p-3 font-mono text-xs [&_code]:bg-transparent [&_code]:p-0">{children}</pre>
  ),
  blockquote: ({ children }) => <blockquote className="mt-3 border-l-2 pl-3 text-muted-foreground">{children}</blockquote>,
  hr: () => <hr className="my-4" />,
  table: ({ children }) => (
    <div className="mt-3 overflow-x-auto rounded-md border">
      <table className="w-full text-sm">{children}</table>
    </div>
  ),
  thead: ({ children }) => <thead className="bg-secondary/60 text-left text-xs text-muted-foreground">{children}</thead>,
  tr: ({ children }) => <tr className="border-b last:border-0">{children}</tr>,
  th: ({ children }) => <th className="px-3 py-2 font-medium whitespace-nowrap">{children}</th>,
  td: ({ children }) => <td className="px-3 py-2 align-top">{children}</td>,
};

export function RespostaMarkdown({ texto }: { texto: string }) {
  return (
    <div className="text-sm text-foreground/90">
      <Markdown remarkPlugins={[remarkGfm]} components={componentes}>
        {texto}
      </Markdown>
    </div>
  );
}

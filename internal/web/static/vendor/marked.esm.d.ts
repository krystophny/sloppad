declare class Renderer {
  code(token: { text?: string; lang?: string; [key: string]: unknown }): string;
  heading(token: { depth?: number; [key: string]: unknown }): string;
  paragraph(token: { [key: string]: unknown }): string;
  blockquote(token: { [key: string]: unknown }): string;
  list(token: { ordered?: boolean; [key: string]: unknown }): string;
  listitem(token: { [key: string]: unknown }): string;
  table(token: { [key: string]: unknown }): string;
  hr(token: { [key: string]: unknown }): string;
}

interface MarkedOptions {
  breaks?: boolean;
  renderer?: Renderer;
  [key: string]: unknown;
}

interface Token {
  type: string;
  raw: string;
  items?: Token[];
  [key: string]: unknown;
}

export const marked: {
  Renderer: new () => Renderer;
  setOptions(options: MarkedOptions): void;
  parse(src: string): string;
  lexer(src: string): Token[];
  parser(tokens: Token[]): string;
};

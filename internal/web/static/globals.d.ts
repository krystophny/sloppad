interface HighlightResult {
  value: string;
}

interface HljsAPI {
  highlight(code: string, options: { language: string; ignoreIllegals?: boolean }): HighlightResult;
  highlightAuto(code: string): HighlightResult;
  getLanguage(name: string): unknown;
}

interface MathJaxStartup {
  promise: Promise<void>;
}

interface MathJaxAPI {
  typesetPromise(elements: Element[]): Promise<void>;
  typesetClear?(elements: Element[]): void;
  texReset?(): void;
  startup?: MathJaxStartup;
}

interface VadMicVADInstance {
  start(): void;
  pause(): void;
  destroy(): void;
}

interface VadNamespace {
  MicVAD: { new: (options: Record<string, unknown>) => Promise<VadMicVADInstance> };
}

declare global {
  const hljs: HljsAPI | undefined;
  const vad: VadNamespace | undefined;
  const MathJax: MathJaxAPI | undefined;
  interface Window {
    [key: string]: any;
  }
}

export {};

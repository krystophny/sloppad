export const GlobalWorkerOptions: {
  workerSrc: string;
};

interface PDFPageViewport {
  width: number;
  height: number;
  scale: number;
  clone(params?: { dontFlip?: boolean }): PDFPageViewport;
}

interface PDFRenderTask {
  promise: Promise<void>;
  cancel(): void;
}

interface PDFTextContent {
  items: unknown[];
}

interface PDFPage {
  getViewport(params: { scale: number }): PDFPageViewport;
  render(params: {
    canvasContext: CanvasRenderingContext2D;
    viewport: PDFPageViewport;
    annotationMode?: number;
  }): PDFRenderTask;
  getTextContent(params?: { includeMarkedContent?: boolean }): Promise<PDFTextContent>;
  getAnnotations(params?: { intent?: string }): Promise<unknown[]>;
  cleanup?(): void;
}

interface PDFDocument {
  numPages: number;
  annotationStorage: unknown;
  getPage(pageNumber: number): Promise<PDFPage>;
  destroy(): Promise<void>;
}

interface PDFLoadingTask {
  promise: Promise<PDFDocument>;
  destroy(): Promise<void>;
}

export function getDocument(params: {
  url: string;
  withCredentials?: boolean;
  standardFontDataUrl?: string;
  useSystemFonts?: boolean;
  isEvalSupported?: boolean;
}): PDFLoadingTask;

export class TextLayer {
  constructor(params: {
    textContentSource: PDFTextContent;
    container: HTMLElement;
    viewport: PDFPageViewport;
  });
  render(): Promise<void>;
  cancel(): void;
}

export class AnnotationLayer {
  constructor(params: {
    div: HTMLElement;
    accessibilityManager: unknown;
    annotationCanvasMap: unknown;
    annotationEditorUIManager: unknown;
    page: PDFPage;
    viewport: PDFPageViewport;
    structTreeLayer: unknown;
  });
  render(params: {
    annotations: unknown[];
    div: HTMLElement;
    page: PDFPage;
    viewport: PDFPageViewport;
    linkService: unknown;
    annotationStorage: unknown;
    renderForms?: boolean;
    enableScripting?: boolean;
  }): Promise<void>;
}

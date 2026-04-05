import { Root } from 'mdast';
export interface HeadingNode {
    depth: number;
    text: string;
    children: HeadingNode[];
}
export interface NormalizedDoc {
    path: string;
    title: string;
    headings: HeadingNode[];
    wordCount: number;
    frontmatter?: Record<string, unknown>;
    ast: Root;
}
export declare function normalize(content: string, filePath: string): Promise<NormalizedDoc>;
//# sourceMappingURL=normalizer.d.ts.map
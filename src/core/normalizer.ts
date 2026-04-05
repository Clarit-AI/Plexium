import { unified } from 'unified';
import remarkParse from 'remark-parse';
import remarkGfm from 'remark-gfm';
import { Root, Heading } from 'mdast';

function extractFrontmatter(content: string): Record<string, unknown> | undefined {
  const yamlRegex = /^---\s*\n([\s\S]*?)\n---/m;
  const match = content.match(yamlRegex);
  
  if (!match) return undefined;
  
  const yamlContent = match[1];
  const result: Record<string, unknown> = {};
  
  for (const line of yamlContent.split('\n')) {
    const colonIndex = line.indexOf(':');
    if (colonIndex === -1) continue;
    
    const key = line.slice(0, colonIndex).trim();
    const value = line.slice(colonIndex + 1).trim();
    
    if (key) {
      result[key] = value;
    }
  }
  
  return Object.keys(result).length > 0 ? result : undefined;
}

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

function extractText(node: unknown): string {
  if (node === null || node === undefined) return '';
  if (typeof node === 'string') return node;
  if (typeof node === 'number') return String(node);
  
  const n = node as { type?: string; value?: string; children?: unknown[] };
  
  if (n.type === 'text' || n.type === 'inlineCode') {
    return n.value || '';
  }
  
  if (n.children) {
    return n.children.map(extractText).join('');
  }
  
  return '';
}

function buildHeadingTree(headings: Heading[]): HeadingNode[] {
  const result: HeadingNode[] = [];
  const stack: { depth: number; node: HeadingNode }[] = [];
  
  for (const heading of headings) {
    const node: HeadingNode = {
      depth: heading.depth,
      text: extractText(heading),
      children: [],
    };
    
    while (stack.length > 0 && stack[stack.length - 1].depth >= heading.depth) {
      stack.pop();
    }
    
    if (stack.length === 0) {
      result.push(node);
    } else {
      stack[stack.length - 1].node.children.push(node);
    }
    
    stack.push({ depth: heading.depth, node });
  }
  
  return result;
}

function countWords(text: string): number {
  const words = text.trim().split(/\s+/).filter(word => word.length > 0);
  return words.length;
}

function extractAllTextFromAst(ast: Root): string {
  const texts: string[] = [];
  
  function walk(node: unknown): void {
    if (node === null || node === undefined) return;
    
    const n = node as { type?: string; value?: string; children?: unknown[] };
    
    if (n.type === 'text' || n.type === 'inlineCode' || n.type === 'code') {
      if (n.value) texts.push(n.value);
    }
    
    if (n.children) {
      for (const child of n.children) {
        walk(child);
      }
    }
  }
  
  for (const child of ast.children) {
    walk(child);
  }
  
  return texts.join(' ');
}

export async function normalize(content: string, filePath: string): Promise<NormalizedDoc> {
  const processor = unified()
    .use(remarkParse)
    .use(remarkGfm);
  
  let ast: Root;
  let frontmatter: Record<string, unknown> | undefined;
  
  try {
    const result = processor.parse(content);
    ast = result as Root;
    frontmatter = extractFrontmatter(content);
  } catch (error) {
    console.warn(`Warning: Failed to parse markdown in ${filePath}:`, error);
    ast = { type: 'root', children: [] };
  }
  
  const allHeadings = ast.children.filter(
    (node): node is Heading => node.type === 'heading'
  );
  
  const titleNode = allHeadings.find(h => h.depth === 1);
  const title = titleNode ? extractText(titleNode) : '';
  
  const headings = buildHeadingTree(allHeadings);
  
  const textContent = extractAllTextFromAst(ast);
  const wordCount = countWords(textContent);
  
  return {
    path: filePath,
    title,
    headings,
    wordCount,
    frontmatter,
    ast,
  };
}
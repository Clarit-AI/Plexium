import { unified } from 'unified';
import remarkParse from 'remark-parse';
import remarkGfm from 'remark-gfm';
function extractFrontmatter(content) {
    const yamlRegex = /^---\s*\n([\s\S]*?)\n---/m;
    const match = content.match(yamlRegex);
    if (!match)
        return undefined;
    const yamlContent = match[1];
    const result = {};
    for (const line of yamlContent.split('\n')) {
        const colonIndex = line.indexOf(':');
        if (colonIndex === -1)
            continue;
        const key = line.slice(0, colonIndex).trim();
        const value = line.slice(colonIndex + 1).trim();
        if (key) {
            result[key] = value;
        }
    }
    return Object.keys(result).length > 0 ? result : undefined;
}
function extractText(node) {
    if (node === null || node === undefined)
        return '';
    if (typeof node === 'string')
        return node;
    if (typeof node === 'number')
        return String(node);
    const n = node;
    if (n.type === 'text' || n.type === 'inlineCode') {
        return n.value || '';
    }
    if (n.children) {
        return n.children.map(extractText).join('');
    }
    return '';
}
function buildHeadingTree(headings) {
    const result = [];
    const stack = [];
    for (const heading of headings) {
        const node = {
            depth: heading.depth,
            text: extractText(heading),
            children: [],
        };
        while (stack.length > 0 && stack[stack.length - 1].depth >= heading.depth) {
            stack.pop();
        }
        if (stack.length === 0) {
            result.push(node);
        }
        else {
            stack[stack.length - 1].node.children.push(node);
        }
        stack.push({ depth: heading.depth, node });
    }
    return result;
}
function countWords(text) {
    const words = text.trim().split(/\s+/).filter(word => word.length > 0);
    return words.length;
}
function extractAllTextFromAst(ast) {
    const texts = [];
    function walk(node) {
        if (node === null || node === undefined)
            return;
        const n = node;
        if (n.type === 'text' || n.type === 'inlineCode' || n.type === 'code') {
            if (n.value)
                texts.push(n.value);
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
export async function normalize(content, filePath) {
    const processor = unified()
        .use(remarkParse)
        .use(remarkGfm);
    let ast;
    let frontmatter;
    try {
        const result = processor.parse(content);
        ast = result;
        frontmatter = extractFrontmatter(content);
    }
    catch (error) {
        console.warn(`Warning: Failed to parse markdown in ${filePath}:`, error);
        ast = { type: 'root', children: [] };
    }
    const allHeadings = ast.children.filter((node) => node.type === 'heading');
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
//# sourceMappingURL=normalizer.js.map
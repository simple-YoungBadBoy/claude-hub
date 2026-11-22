import { Fragment, type ReactNode } from "react";

// A small Markdown renderer that builds React elements (never HTML strings),
// enough for Claude's replies and README-style files: headings, paragraphs,
// lists, quotes, code fences, tables, rules, `code`, **bold**, *em*, links.

const inlineRe = /(`+)([^`]|[^`][\s\S]*?[^`])\1(?!`)|\*\*([^*]+)\*\*|__([^_]+)__|\*([^*\s][^*]*)\*|\[([^\]]+)\]\(([^)\s]+)\)|(https?:\/\/[^\s<>()]+[^\s<>().,;:!?'"，。）])/g;

export function inline(text: string): ReactNode[] {
  const out: ReactNode[] = [];
  let last = 0;
  let k = 0;
  for (const m of text.matchAll(inlineRe)) {
    if (m.index! > last) out.push(text.slice(last, m.index));
    if (m[2] !== undefined) out.push(<code key={k++}>{m[2]}</code>);
    else if (m[3] !== undefined || m[4] !== undefined) out.push(<strong key={k++}>{inline(m[3] ?? m[4])}</strong>);
    else if (m[5] !== undefined) out.push(<em key={k++}>{inline(m[5])}</em>);
    else if (m[6] !== undefined) out.push(link(m[7], inline(m[6]), k++));
    else if (m[8] !== undefined) out.push(link(m[8], m[8], k++));
    last = m.index! + m[0].length;
  }
  if (last < text.length) out.push(text.slice(last));
  return out;
}

function link(href: string, body: ReactNode, key: number) {
  // only web links are clickable: no javascript:, data: or relative paths
  if (!/^https?:\/\//i.test(href)) return <span key={key} className="md-link-off">{body}</span>;
  return (
    <a key={key} href={href} target="_blank" rel="noopener noreferrer">
      {body}
    </a>
  );
}

const isRow = (l: string) => /^\s*\|.*\|\s*$/.test(l);
const isSep = (l: string) => /^\s*\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?\s*$/.test(l);
const cells = (l: string) => l.trim().replace(/^\|/, "").replace(/\|$/, "").split("|").map((c) => c.trim());
const listRe = /^(\s*)([-*+]|\d+[.)])\s+(.*)$/;

export default function Markdown({ text }: { text: string }) {
  const lines = text.replace(/\r\n?/g, "\n").split("\n");
  const blocks: ReactNode[] = [];
  let i = 0;
  let k = 0;
  while (i < lines.length) {
    const l = lines[i];
    const fence = l.match(/^\s*(```+|~~~+)\s*([\w+-]*)/);
    if (fence) {
      const body: string[] = [];
      i++;
      while (i < lines.length && !lines[i].trim().startsWith(fence[1])) body.push(lines[i++]);
      i++;
      blocks.push(
        <pre key={k++} className="md-code" data-lang={fence[2] || undefined}>
          <code>{body.join("\n")}</code>
        </pre>,
      );
      continue;
    }
    // a line that is only an HTML tag (README layout like <div align="center">): skip it;
    // HTML is never rendered, and as text it is just noise
    if (!l.trim() || /^\s*<\/?[a-zA-Z][^>]*>\s*$/.test(l)) {
      i++;
      continue;
    }
    const h = l.match(/^(#{1,6})\s+(.*?)\s*#*\s*$/);
    if (h) {
      const level = Math.min(h[1].length, 4);
      const Tag = `h${level + 2}` as "h3";
      blocks.push(<Tag key={k++} className="md-h">{inline(h[2])}</Tag>);
      i++;
      continue;
    }
    if (/^\s*([-*_])(\s*\1){2,}\s*$/.test(l)) {
      blocks.push(<hr key={k++} />);
      i++;
      continue;
    }
    if (isRow(l) && i + 1 < lines.length && isSep(lines[i + 1])) {
      const head = cells(l);
      const rows: string[][] = [];
      i += 2;
      while (i < lines.length && isRow(lines[i])) rows.push(cells(lines[i++]));
      blocks.push(
        <div key={k++} className="md-table">
          <table>
            <thead>
              <tr>{head.map((c, j) => <th key={j}>{inline(c)}</th>)}</tr>
            </thead>
            <tbody>
              {rows.map((r, n) => (
                <tr key={n}>{r.map((c, j) => <td key={j}>{inline(c)}</td>)}</tr>
              ))}
            </tbody>
          </table>
        </div>,
      );
      continue;
    }
    if (/^\s*>/.test(l)) {
      const body: string[] = [];
      while (i < lines.length && /^\s*>/.test(lines[i])) body.push(lines[i++].replace(/^\s*>\s?/, ""));
      blocks.push(
        <blockquote key={k++}>
          <Markdown text={body.join("\n")} />
        </blockquote>,
      );
      continue;
    }
    const li = l.match(listRe);
    if (li) {
      const ordered = /\d/.test(li[2]);
      const items: { depth: number; text: string }[] = [];
      const base = li[1].length;
      while (i < lines.length) {
        const m = lines[i].match(listRe);
        if (m) {
          items.push({ depth: Math.min(Math.floor(Math.max(0, m[1].length - base) / 2), 4), text: m[3] });
          i++;
        } else if (lines[i].trim() && /^\s{2,}/.test(lines[i]) && items.length) {
          items[items.length - 1].text += " " + lines[i++].trim(); // continuation line
        } else break;
      }
      const Tag = ordered ? "ol" : "ul";
      blocks.push(
        <Tag key={k++} className="md-list">
          {items.map((it, j) => (
            <li key={j} style={it.depth ? { marginLeft: it.depth * 18 } : undefined}>
              {inline(it.text)}
            </li>
          ))}
        </Tag>,
      );
      continue;
    }
    const para: string[] = [];
    while (
      i < lines.length &&
      lines[i].trim() &&
      !/^\s*<\/?[a-zA-Z][^>]*>\s*$/.test(lines[i]) &&
      !/^\s*(```|~~~|#{1,6}\s|>)/.test(lines[i]) &&
      !listRe.test(lines[i]) &&
      !(isRow(lines[i]) && i + 1 < lines.length && isSep(lines[i + 1]))
    )
      para.push(lines[i++]);
    if (!para.length) para.push(lines[i++]); // never stall
    blocks.push(
      <p key={k++}>
        {para.map((t, j) => (
          <Fragment key={j}>
            {j > 0 && <br />}
            {inline(t)}
          </Fragment>
        ))}
      </p>,
    );
  }
  return <div className="md">{blocks}</div>;
}

import type { ILinkProvider, Terminal } from "@xterm/xterm";

// File paths in the terminal (src/App.tsx, ./a/b.go:12, /abs/x.md) become
// links that open the read-only file viewer. URLs are left to WebLinksAddon.
const pathRe = /(^|[\s'"`(\[{<:：，、])((?:\.{1,2}\/|\/)?[\w@.+-]+(?:\/[\w@.+-]+)*\.[A-Za-z][A-Za-z0-9]{0,7})(?::(\d+))?(?=$|[\s'"`)\]}>,;:：，。、）])/g;

export function findPaths(text: string): { start: number; end: number; path: string }[] {
  const out: { start: number; end: number; path: string }[] = [];
  for (const m of text.matchAll(pathRe)) {
    const p = m[2];
    if (/^\d+(\.\d+)+$/.test(p) || /^[\d.]+[a-z]{1,2}$/i.test(p)) continue; // versions, 1.5s, 2.0x
    if (!p.includes("/") && !/\.(md|markdown|txt|json|ya?ml|toml|go|ts|tsx|js|jsx|mjs|py|rs|sh|css|html|sql|swift|kt|java|rb|c|h|cpp)$/i.test(p)) continue;
    const start = m.index! + m[1].length;
    out.push({ start, end: start + p.length + (m[3] ? m[3].length + 1 : 0), path: p });
  }
  return out;
}

export function pathLinks(term: Terminal, open: (path: string) => void): ILinkProvider {
  return {
    provideLinks(y, cb) {
      const line = term.buffer.active.getLine(y - 1);
      if (!line) return cb(undefined);
      // map string index -> cell column (wide CJK cells take two columns)
      let text = "";
      const col: number[] = [];
      for (let x = 0; x < line.length; x++) {
        const cell = line.getCell(x);
        if (!cell || cell.getWidth() === 0) continue;
        const ch = cell.getChars() || " ";
        for (let i = 0; i < ch.length; i++) col.push(x);
        text += ch;
      }
      col.push(line.length);
      const links = findPaths(text).map((f) => ({
        range: { start: { x: col[f.start] + 1, y }, end: { x: col[f.end - 1] + 1, y } },
        text: f.path,
        decorations: { underline: true, pointerCursor: true },
        activate: (e: MouseEvent) => {
          // plain clicks select text in a terminal; open with ⌘/Ctrl-click, or a tap on phones
          if (e.metaKey || e.ctrlKey || matchMedia("(pointer: coarse)").matches) open(f.path);
        },
      }));
      cb(links.length ? links : undefined);
    },
  };
}

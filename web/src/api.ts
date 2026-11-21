export class Unauthorized extends Error {}

export async function api<T = unknown>(path: string, body?: unknown, method?: string): Promise<T> {
  const init: RequestInit = { method: method ?? (body === undefined ? "GET" : "POST"), credentials: "same-origin" };
  if (init.method !== "GET") {
    init.headers = { "X-Hub": "1", "Content-Type": "application/json" };
    init.body = JSON.stringify(body ?? {});
  }
  const res = await fetch(path, init);
  if (res.status === 401) throw new Unauthorized("unauthorized");
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error((data as { error?: string }).error || res.statusText);
  return data as T;
}

export async function upload(session: string, files: File[]): Promise<string[]> {
  const fd = new FormData();
  for (const f of files) fd.append("file", f, f.name || "image.png");
  const res = await fetch(`/api/upload/${encodeURIComponent(session)}`, {
    method: "POST",
    headers: { "X-Hub": "1" },
    body: fd,
    credentials: "same-origin",
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.error || res.statusText);
  return data.paths;
}

export function wsURL(path: string) {
  return `${location.protocol === "https:" ? "wss" : "ws"}://${location.host}${path}`;
}

type Handler = (type: string, data: any) => void;

/** Keeps /ws/events open, reconnecting with backoff. */
export function connectEvents(onMsg: Handler, onConn: (up: boolean) => void) {
  let ws: WebSocket | null = null;
  let stopped = false;
  let delay = 500;
  let timer: number | undefined;
  const open = () => {
    ws = new WebSocket(wsURL("/ws/events"));
    ws.onopen = () => {
      delay = 500;
      onConn(true);
    };
    ws.onmessage = (e) => {
      try {
        const m = JSON.parse(e.data);
        onMsg(m.type, m.data);
      } catch {
        /* ignore */
      }
    };
    ws.onclose = () => {
      onConn(false);
      if (stopped) return;
      timer = window.setTimeout(open, delay);
      delay = Math.min(delay * 2, 8000);
    };
  };
  open();
  return () => {
    stopped = true;
    clearTimeout(timer);
    ws?.close();
  };
}

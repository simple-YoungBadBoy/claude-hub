// Native notification inside the Tauri shell, Web Notification in a browser.
declare global {
  interface Window {
    __TAURI__?: any;
  }
}

let asked = false;

export async function notify(title: string, body: string) {
  const t = window.__TAURI__?.notification;
  if (t) {
    try {
      let ok = await t.isPermissionGranted();
      if (!ok) ok = (await t.requestPermission()) === "granted";
      if (ok) t.sendNotification({ title, body });
      return;
    } catch {
      /* fall through */
    }
  }
  if (!("Notification" in window)) return;
  if (Notification.permission === "default" && !asked) {
    asked = true;
    await Notification.requestPermission();
  }
  if (Notification.permission === "granted") new Notification(title, { body });
}

let ctx: AudioContext | null = null;
/** Short two-note chime. */
export function chime(kind: "done" | "ask") {
  try {
    ctx ??= new AudioContext();
    const notes = kind === "done" ? [660, 880] : [880, 660];
    notes.forEach((f, i) => {
      const o = ctx!.createOscillator();
      const g = ctx!.createGain();
      o.frequency.value = f;
      o.type = "sine";
      const t0 = ctx!.currentTime + i * 0.12;
      g.gain.setValueAtTime(0.0001, t0);
      g.gain.exponentialRampToValueAtTime(0.15, t0 + 0.02);
      g.gain.exponentialRampToValueAtTime(0.0001, t0 + 0.25);
      o.connect(g).connect(ctx!.destination);
      o.start(t0);
      o.stop(t0 + 0.3);
    });
  } catch {
    /* ignore */
  }
}

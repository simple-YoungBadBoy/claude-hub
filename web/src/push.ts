import { api } from "./api";

// Web Push: lock-screen notifications. iPhone needs the page added to the
// home screen and opened over HTTPS (tailscale serve).
export type PushState = "unsupported" | "need-https" | "need-home-screen" | "denied" | "off" | "on";

const ios = /iPhone|iPad|iPod/.test(navigator.userAgent);
const standalone = matchMedia("(display-mode: standalone)").matches || (navigator as { standalone?: boolean }).standalone === true;

function b64ToBytes(s: string) {
  const b = atob(s.replace(/-/g, "+").replace(/_/g, "/") + "=".repeat((4 - (s.length % 4)) % 4));
  return Uint8Array.from(b, (c) => c.charCodeAt(0));
}

async function registration() {
  return navigator.serviceWorker.register("/sw.js");
}

export async function pushState(): Promise<PushState> {
  if (!window.isSecureContext) return "need-https";
  if (ios && !standalone) return "need-home-screen";
  if (!("serviceWorker" in navigator) || !("PushManager" in window) || !("Notification" in window)) return "unsupported";
  if (Notification.permission === "denied") return "denied";
  const reg = await navigator.serviceWorker.getRegistration("/");
  const sub = await reg?.pushManager.getSubscription();
  if (!sub) return "off";
  const r = await api<{ subscribed?: boolean }>(`/api/push?endpoint=${encodeURIComponent(sub.endpoint)}`);
  return r.subscribed ? "on" : "off";
}

/** Must run from a tap (the permission prompt needs a user gesture). */
export async function enablePush() {
  const info = await api<{ available: boolean; key?: string }>("/api/push");
  if (!info.available || !info.key) throw new Error("服务器没能启用推送");
  if ((await Notification.requestPermission()) !== "granted") throw new Error("没有通知权限，请在系统设置里允许");
  const reg = await registration();
  await navigator.serviceWorker.ready;
  let sub = await reg.pushManager.getSubscription();
  if (!sub) sub = await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: b64ToBytes(info.key) });
  await api("/api/push/subscribe", sub.toJSON());
}

export async function disablePush() {
  const reg = await navigator.serviceWorker.getRegistration("/");
  const sub = await reg?.pushManager.getSubscription();
  if (sub) {
    await api("/api/push/unsubscribe", { endpoint: sub.endpoint });
    await sub.unsubscribe();
  }
}

export const pushHint: Record<PushState, string> = {
  unsupported: "",
  "need-https": "锁屏通知需要用 HTTPS 地址打开（tailscale serve）",
  "need-home-screen": "锁屏通知：先用 Safari 分享 → 添加到主屏幕，再从主屏幕打开",
  denied: "通知被拒绝了，请到系统设置里允许 Claude Hub 通知",
  off: "",
  on: "",
};

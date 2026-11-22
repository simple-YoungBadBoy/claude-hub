// Service worker: only shows Web Push notifications (no caching, so a
// deploy is never hidden behind a stale copy).
self.addEventListener("push", (e) => {
  let m = {};
  try {
    m = e.data ? e.data.json() : {};
  } catch {
    m = { title: "Claude Hub", body: e.data ? e.data.text() : "" };
  }
  e.waitUntil(
    self.registration.showNotification(m.title || "Claude Hub", {
      body: m.body || "",
      tag: m.tag || m.session || "hub",
      data: { session: m.session || "" },
      icon: "/icon-192.png",
      badge: "/icon-192.png",
    }),
  );
});

self.addEventListener("notificationclick", (e) => {
  e.notification.close();
  const session = (e.notification.data && e.notification.data.session) || "";
  const url = session ? `/?s=${encodeURIComponent(session)}` : "/";
  e.waitUntil(
    self.clients.matchAll({ type: "window", includeUncontrolled: true }).then((list) => {
      for (const c of list) {
        if ("focus" in c) {
          c.postMessage({ open: session });
          return c.focus();
        }
      }
      return self.clients.openWindow(url);
    }),
  );
});

self.addEventListener("activate", (e) => e.waitUntil(self.clients.claim()));

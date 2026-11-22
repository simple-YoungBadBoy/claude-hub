import { useEffect, useState } from "react";
import { api } from "../api";
import { disablePush, enablePush, pushHint, pushState, type PushState } from "../push";

/** Lock-screen notifications (Web Push) for this device. */
export default function PushToggle({ onToast }: { onToast: (msg: string, err?: boolean) => void }) {
  const [st, setSt] = useState<PushState | "loading">("loading");
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    pushState()
      .then(setSt)
      .catch(() => setSt("unsupported"));
  }, []);
  if (st === "loading" || st === "unsupported") return null;
  if (pushHint[st]) return <div className="push-hint">🔕 {pushHint[st]}</div>;
  const run = async (fn: () => Promise<void>, ok: string) => {
    setBusy(true);
    try {
      await fn();
      setSt(await pushState());
      onToast(ok);
    } catch (e) {
      onToast((e as Error).message, true);
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="push-toggle">
      {st === "on" ? (
        <>
          <span title="Claude 完成、要你确认、出错时，推送到这台设备（正在看网页时不推）">🔔 锁屏通知已开</span>
          <button className="ghost" disabled={busy} onClick={() => run(() => api("/api/push/test", {}).then(() => undefined), "已发送测试通知")}>
            测试
          </button>
          <button className="ghost" disabled={busy} onClick={() => run(disablePush, "已关闭锁屏通知")}>
            关闭
          </button>
        </>
      ) : (
        <button className="ghost" disabled={busy} title="Claude 完成、要你确认、出错时推送到这台设备" onClick={() => run(enablePush, "锁屏通知已开启")}>
          🔔 开启锁屏通知
        </button>
      )}
    </div>
  );
}

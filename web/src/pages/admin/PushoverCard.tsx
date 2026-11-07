import { useEffect, useState } from "react";
import { Send } from "lucide-react";
import { api, ApiError } from "@/lib/api";
import { usePushover, useSavePushover } from "@/lib/queries";

/** Pushover keys (stored encrypted), device filter, link base URL and a test button. */
export function PushoverCard() {
  const { data } = usePushover();
  const save = useSavePushover();
  const [userKey, setUserKey] = useState("");
  const [appToken, setAppToken] = useState("");
  const [device, setDevice] = useState("");
  const [baseUrl, setBaseUrl] = useState("");
  const [enabled, setEnabled] = useState(true);
  const [test, setTest] = useState<{ ok: boolean; text: string } | null>(null);
  const [testing, setTesting] = useState(false);

  useEffect(() => {
    if (!data) return;
    setDevice(data.device);
    setBaseUrl(data.baseUrl || window.location.origin);
    setEnabled(data.configured ? data.enabled : true);
  }, [data]);

  const submit = () =>
    save.mutate(
      { enabled, device, baseUrl, userKey: userKey || undefined, appToken: appToken || undefined },
      {
        onSuccess: () => {
          setUserKey("");
          setAppToken("");
          setTest(null);
        },
      },
    );

  const sendTest = async () => {
    setTesting(true);
    setTest(null);
    try {
      await api.post("/api/integrations/pushover/test");
      setTest({ ok: true, text: "Test notification sent — check your phone." });
    } catch (e) {
      setTest({ ok: false, text: e instanceof ApiError ? e.message : "Failed" });
    } finally {
      setTesting(false);
    }
  };

  const row = "grid grid-cols-[220px_1fr] items-center gap-4";
  return (
    <div className="grid gap-4">
      <p className="text-xs text-muted">
        Create an application at <span className="font-mono text-fg">pushover.net/apps/build</span> to get an API token. Your user key is on the Pushover dashboard.
        Keys are stored encrypted with LABDASH_SECRET.
      </p>
      <div className={row}>
        <div className="text-sm">User key</div>
        <input
          className="input max-w-sm font-mono"
          type="password"
          autoComplete="off"
          value={userKey}
          onChange={(e) => setUserKey(e.target.value)}
          placeholder={data?.userKeyHint ? `saved (${data.userKeyHint}) — leave blank to keep` : "30-character user key"}
        />
      </div>
      <div className={row}>
        <div className="text-sm">Application token</div>
        <input
          className="input max-w-sm font-mono"
          type="password"
          autoComplete="off"
          value={appToken}
          onChange={(e) => setAppToken(e.target.value)}
          placeholder={data?.appTokenHint ? `saved (${data.appTokenHint}) — leave blank to keep` : "30-character API token"}
        />
      </div>
      <div className={row}>
        <div>
          <div className="text-sm">Device</div>
          <div className="text-xs text-faint">Optional — blank sends to all devices</div>
        </div>
        <input className="input max-w-sm" value={device} onChange={(e) => setDevice(e.target.value)} placeholder="iphone" />
      </div>
      <div className={row}>
        <div>
          <div className="text-sm">LabDash URL</div>
          <div className="text-xs text-faint">Alerts link back to the status page</div>
        </div>
        <input className="input max-w-sm font-mono" value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)} placeholder="https://labdash.home.example.com" />
      </div>
      <div className={row}>
        <div className="text-sm">Send alerts</div>
        <label className="flex items-center gap-2 text-sm text-muted">
          <input type="checkbox" className="accent-accent" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
          Enabled
        </label>
      </div>
      <div className="flex items-center gap-3">
        <button className="btn-primary" onClick={submit} disabled={save.isPending}>
          Save
        </button>
        <button className="btn-outline" onClick={sendTest} disabled={testing || !data?.configured}>
          <Send size={14} /> {testing ? "Sending…" : "Send test"}
        </button>
        {save.error && <span className="text-sm text-down">{save.error.message}</span>}
        {save.isSuccess && !test && <span className="text-sm text-up">Saved</span>}
        {test && <span className={`text-sm ${test.ok ? "text-up" : "text-down"}`}>{test.text}</span>}
      </div>
    </div>
  );
}

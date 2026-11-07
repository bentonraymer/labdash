import { useEffect, useState } from "react";
import { Send } from "lucide-react";
import { api, ApiError } from "@/lib/api";
import { useLaunchpad, usePatchSettings, usePushover, useSavePushover } from "@/lib/queries";

/** Pushover keys (stored encrypted), device filter, link base URL and a test button. */
export function PushoverCard() {
  const { data } = usePushover();
  const { data: lp } = useLaunchpad();
  const patch = usePatchSettings();
  const st = lp?.settings;
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
      {st && (
        <div className="grid gap-3 rounded-lg border border-line p-4">
          <div className="text-xs font-semibold text-muted">Spam protection</div>
          <div className={row}>
            <div>
              <div className="text-sm">Group alerts</div>
              <div className="text-xs text-faint">Outages close together arrive as one message</div>
            </div>
            <select className="input max-w-sm" value={st.alert_group_seconds} onChange={(e) => patch.mutate({ alert_group_seconds: Number(e.target.value) })}>
              <option value={0}>Off: send each alert immediately</option>
              <option value={15}>Within 15 seconds</option>
              <option value={30}>Within 30 seconds</option>
              <option value={60}>Within 1 minute</option>
              <option value={120}>Within 2 minutes</option>
            </select>
          </div>
          <div className={row}>
            <div>
              <div className="text-sm">Hourly limit</div>
              <div className="text-xs text-faint">Extra alerts are held back and counted</div>
            </div>
            <select className="input max-w-sm" value={st.alert_max_per_hour} onChange={(e) => patch.mutate({ alert_max_per_hour: Number(e.target.value) })}>
              {[5, 10, 20, 50].map((n) => (
                <option key={n} value={n}>
                  At most {n} messages per hour
                </option>
              ))}
              <option value={0}>Unlimited</option>
            </select>
          </div>
          <div className={row}>
            <div>
              <div className="text-sm">Flapping protection</div>
              <div className="text-xs text-faint">Mute a monitor that keeps going up and down</div>
            </div>
            <label className="flex items-center gap-2 text-sm text-muted">
              <input type="checkbox" className="accent-accent" checked={st.alert_flap_protection} onChange={(e) => patch.mutate({ alert_flap_protection: e.target.checked })} />
              After 3 down/up cycles in 30 min, send one notice and stay quiet until it's stable for 15 min
            </label>
          </div>
        </div>
      )}
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

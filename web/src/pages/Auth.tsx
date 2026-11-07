import { useState, type FormEvent, type ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "@/lib/api";
import { keys } from "@/lib/queries";

function AuthCard({ title, subtitle, children }: { title: string; subtitle: string; children: ReactNode }) {
  return (
    <div className="relative flex min-h-full items-center justify-center overflow-hidden px-4">
      <div
        className="absolute inset-0"
        style={{
          background:
            "radial-gradient(900px 500px at 20% 0%, rgba(91,140,255,0.18), transparent 60%), radial-gradient(800px 600px at 90% 100%, rgba(167,139,250,0.14), transparent 60%), #07080c",
        }}
      />
      <div className="glass relative w-full max-w-sm rounded-2xl p-8">
        <div className="mb-6 flex items-center gap-3">
          <img src="/favicon.svg" alt="" className="size-9" />
          <div>
            <h1 className="text-lg font-semibold">{title}</h1>
            <p className="text-xs text-muted">{subtitle}</p>
          </div>
        </div>
        {children}
      </div>
    </div>
  );
}

export function LoginPage() {
  const qc = useQueryClient();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setErr("");
    try {
      await api.post("/api/auth/login", { username, password });
      await qc.invalidateQueries({ queryKey: keys.me });
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : "Login failed");
    } finally {
      setBusy(false);
    }
  };

  return (
    <AuthCard title="LabDash" subtitle="Sign in to your homelab">
      <form onSubmit={submit} className="grid gap-4">
        <div>
          <label className="label">Username</label>
          <input className="input" autoFocus autoComplete="username" value={username} onChange={(e) => setUsername(e.target.value)} />
        </div>
        <div>
          <label className="label">Password</label>
          <input className="input" type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} />
        </div>
        {err && <p className="text-sm text-down">{err}</p>}
        <button className="btn-primary w-full" disabled={busy || !username || !password}>
          Sign in
        </button>
      </form>
    </AuthCard>
  );
}

export function SetupPage() {
  const qc = useQueryClient();
  const [username, setUsername] = useState("admin");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [domain, setDomain] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (password !== confirm) return setErr("Passwords don't match");
    setBusy(true);
    setErr("");
    try {
      await api.post("/api/setup", { username, password, dnsDomain: domain.trim() });
      await qc.invalidateQueries();
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : "Setup failed");
    } finally {
      setBusy(false);
    }
  };

  return (
    <AuthCard title="Welcome to LabDash" subtitle="Create your admin account to get started">
      <form onSubmit={submit} className="grid gap-4">
        <div>
          <label className="label">Username</label>
          <input className="input" value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" />
        </div>
        <div>
          <label className="label">Password</label>
          <input className="input" type="password" autoFocus value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="new-password" placeholder="At least 8 characters" />
        </div>
        <div>
          <label className="label">Confirm password</label>
          <input className="input" type="password" value={confirm} onChange={(e) => setConfirm(e.target.value)} autoComplete="new-password" />
        </div>
        <div>
          <label className="label">Internal DNS domain</label>
          <input className="input font-mono" value={domain} onChange={(e) => setDomain(e.target.value)} placeholder="home.example.com" />
          <p className="mt-1 text-xs text-faint">Used to suggest URLs like https://jellyfin.&lt;domain&gt;. You can change it later.</p>
        </div>
        {err && <p className="text-sm text-down">{err}</p>}
        <button className="btn-primary w-full" disabled={busy || !username || password.length < 8}>
          Create account
        </button>
      </form>
    </AuthCard>
  );
}

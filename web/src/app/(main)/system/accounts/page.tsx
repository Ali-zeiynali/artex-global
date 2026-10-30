"use client";

import * as React from "react";

import Link from "next/link";

import {
  ArrowRight,
  Check,
  CheckCircle2,
  CircleAlert,
  KeyRound,
  Loader2,
  Mail,
  Plus,
  RefreshCw,
  Send,
  ShieldCheck,
  Trash2,
  Unplug,
  Users,
} from "lucide-react";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { http } from "@/lib/api";

type Provider = "gmail" | "telegram" | "atomicmail";

const providerName = (provider: string) =>
  ({ gmail: "Gmail", telegram: "Telegram", atomicmail: "Atomic Mail" })[provider] ?? provider;

type Account = {
  id: string;

  provider: Provider;

  label: string;

  identity: string;

  enabled: boolean;

  status: string;

  mcp_name: string;

  tool_count: number;

  created_at: number;

  checked_at?: number;

  error?: string;
};

type Connection = {
  id: string;

  kind: "mcp" | "llm" | "search";

  name: string;

  enabled: boolean;

  configured: boolean;

  managed: boolean;

  endpoint?: string;

  needs_endpoint: boolean;

  fields: { key: string; configured: boolean }[];

  note?: string;
};

type Login = {
  challenge_id?: string;

  step: "phone" | "code" | "password" | "connected";

  delivery?: string;

  account?: Account;
};

const post = <T,>(path: string, value: unknown, method = "POST") =>
  http<T>(path, { method, body: JSON.stringify(value) });

const message = (error: unknown) => (error instanceof Error ? error.message : "The operation could not be completed.");

export default function AccountsPage() {
  const [tab, setTab] = React.useState("gmail");

  const [accounts, setAccounts] = React.useState<Account[]>([]);

  const [connections, setConnections] = React.useState<Connection[]>([]);

  const [telegramApp, setTelegramApp] = React.useState(false);

  const [loading, setLoading] = React.useState(true);

  const [loadError, setLoadError] = React.useState("");

  const [busy, setBusy] = React.useState("");

  const [dialog, setDialog] = React.useState<Provider | null>(null);

  const [edit, setEdit] = React.useState<Account | null>(null);

  const [remove, setRemove] = React.useState<Account | null>(null);

  const [revoke, setRevoke] = React.useState(false);

  const [connection, setConnection] = React.useState<Connection | null>(null);

  const [formError, setFormError] = React.useState("");

  const [form, setForm] = React.useState({
    label: "",

    email: "",

    password: "",

    phone: "",

    code: "",

    api_id: "",

    api_hash: "",
  });

  const [login, setLogin] = React.useState<Login>({ step: "phone" });

  const [tokens, setTokens] = React.useState<Record<string, string>>({});

  const [endpoint, setEndpoint] = React.useState("");

  const [filter, setFilter] = React.useState("");

  const load = React.useCallback(async () => {
    setLoadError("");

    const result = await Promise.allSettled([
      http<{ accounts: Account[]; telegram_app_configured: boolean }>("/accounts"),

      http<{ connections: Connection[] }>("/accounts/connections"),
    ]);

    if (result[0].status === "fulfilled") {
      setAccounts(result[0].value.accounts);

      setTelegramApp(result[0].value.telegram_app_configured);
    } else setLoadError(message(result[0].reason));

    if (result[1].status === "fulfilled") setConnections(result[1].value.connections);
    else setLoadError(message(result[1].reason));

    setLoading(false);
  }, []);

  React.useEffect(() => {
    void load();
  }, [load]);

  React.useEffect(() => {
    const timer = window.setInterval(() => {
      if (!document.hidden && !busy) void load();
    }, 30000);

    return () => window.clearInterval(timer);
  }, [load, busy]);

  function begin(provider: Provider, account?: Account) {
    setEdit(account ?? null);

    setDialog(provider);

    setFormError("");

    setLogin({ step: "phone" });

    setForm({
      label: account?.label ?? "",

      email: account?.identity ?? "",

      password: "",

      phone: "",

      code: "",

      api_id: "",

      api_hash: "",
    });
  }

  async function close() {
    if (busy) return;

    if (login.challenge_id && login.step !== "connected") {
      try {
        await post("/accounts/telegram/cancel", { challenge_id: login.challenge_id });
      } catch {
        toast.error("Could not cancel immediately. This login expires automatically after 10 minutes.");
      }
    }

    setDialog(null);

    setEdit(null);

    setLogin({ step: "phone" });

    setFormError("");

    setForm({ label: "", email: "", password: "", phone: "", code: "", api_id: "", api_hash: "" });
  }

  async function save(event: React.FormEvent) {
    event.preventDefault();

    setBusy("form");

    setFormError("");

    try {
      if (edit) {
        await post(
          `/accounts/${edit.id}`,

          {
            label: form.label,
            ...(form.password ? { [dialog === "atomicmail" ? "api_key" : "password"]: form.password } : {}),
          },

          "PATCH",
        );

        toast.success("Account updated");

        setDialog(null);

        setEdit(null);
      } else if (dialog === "atomicmail") {
        await post("/accounts/atomicmail", { label: form.label, email: form.email, api_key: form.password });

        toast.success("Atomic Mail MCP connected to agents");

        setDialog(null);
      } else if (dialog === "gmail") {
        await post("/accounts/gmail", { label: form.label, email: form.email, password: form.password });

        toast.success("Gmail verified and connected to agents");

        setDialog(null);
      } else if (login.step === "phone") {
        const result = await post<Login>("/accounts/telegram/start", {
          label: form.label,

          phone: form.phone,

          ...(form.api_id ? { api_id: Number(form.api_id), api_hash: form.api_hash } : {}),
        });

        setLogin(result);

        setTelegramApp(true);
      } else {
        const result = await post<Login>(`/accounts/telegram/${login.step === "code" ? "verify" : "password"}`, {
          challenge_id: login.challenge_id,

          ...(login.step === "code" ? { code: form.code } : { password: form.password }),
        });

        setLogin(result);

        if (result.step === "connected") {
          toast.success("Telegram session created and connected to agents");

          setDialog(null);
        }
      }

      setForm((value) => ({ ...value, password: "", code: "", api_hash: "" }));

      await load();
    } catch (error) {
      setFormError(message(error));

      await load();
    } finally {
      setBusy("");
    }
  }

  async function action(account: Account, mode: "test" | "toggle") {
    setBusy(account.id);

    try {
      if (mode === "test") await post(`/accounts/${account.id}/test`, {});
      else await post(`/accounts/${account.id}`, { enabled: !account.enabled }, "PATCH");

      toast.success(
        mode === "test"
          ? "Account and MCP connection verified"
          : account.enabled
            ? "Account disconnected"
            : "Account reconnected",
      );
    } catch (error) {
      toast.error(message(error));
    } finally {
      await load();

      setBusy("");
    }
  }

  async function saveConnection(event: React.FormEvent) {
    event.preventDefault();

    if (!connection) return;

    setBusy("connection");

    setFormError("");

    try {
      const result = await post<{ ok: boolean; message: string }>("/accounts/connections", {
        kind: connection.kind,

        id: connection.id,

        fields: tokens,

        endpoint,
      });

      setTokens({});

      if (!result.ok) {
        setFormError(result.message);

        toast.warning("Saved, but connection needs attention");
      } else {
        toast.success(result.message);

        setConnection(null);
      }

      await load();
    } catch (error) {
      setFormError(message(error));
    } finally {
      setBusy("");
    }
  }

  const ready = accounts.filter((account) => account.enabled && account.status === "connected").length;

  const shown = accounts.filter(
    (account) =>
      account.provider === tab && `${account.label} ${account.identity}`.toLowerCase().includes(filter.toLowerCase()),
  );

  const loginSteps = ["phone", "code", "password"];

  const stepNumber = Math.max(0, loginSteps.indexOf(login.step));

  return (
    <div className="flex flex-col gap-6 p-4 md:p-6">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <div className="mb-2 flex items-center gap-2 text-muted-foreground text-sm">
            <Users className="size-4" /> Connected services
          </div>
          <h1 className="font-semibold text-3xl tracking-tight">Accounts</h1>
          <p className="mt-2 max-w-2xl text-muted-foreground text-sm">
            Connect your accounts once. Give your agents the right tools, without passing credentials through chat.
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" onClick={() => void load()} disabled={loading || Boolean(busy)}>
            <RefreshCw className={loading ? "animate-spin" : ""} /> Refresh
          </Button>
          {tab !== "connections" && (
            <Button onClick={() => begin(tab as Provider)}>
              <Plus /> Add {providerName(tab)}
            </Button>
          )}
        </div>
      </div>
      <div className="grid gap-3 sm:grid-cols-3">
        {[
          { label: "Connected accounts", value: `${ready} / ${accounts.length}`, icon: CheckCircle2 },

          {
            label: "Account tools",

            value: accounts

              .filter((a) => a.enabled && a.status === "connected")

              .reduce((sum, a) => sum + a.tool_count, 0),

            icon: Send,
          },

          {
            label: "Configured API connections",

            value: connections.filter((c) => c.configured && !c.managed).length,

            icon: KeyRound,
          },
        ].map((stat) => (
          <Card key={stat.label} className="gap-0 py-4">
            <CardContent className="flex items-center justify-between">
              <div>
                <p className="text-muted-foreground text-xs">{stat.label}</p>
                <p className="mt-1 font-semibold text-2xl tabular-nums">{stat.value}</p>
              </div>
              <stat.icon className="size-5 text-muted-foreground" />
            </CardContent>
          </Card>
        ))}
      </div>
      {loadError && (
        <div
          role="alert"
          className="flex items-start gap-3 rounded-xl border border-destructive/30 bg-destructive/5 p-4 text-sm"
        >
          <CircleAlert className="mt-0.5 size-4 shrink-0 text-destructive" />
          <div>
            {loadError}
            <Button variant="link" onClick={() => void load()}>
              Try again
            </Button>
          </div>
        </div>
      )}
      <Tabs
        value={tab}
        onValueChange={(value) => {
          setTab(value);

          setFilter("");
        }}
      >
        <div className="flex flex-wrap items-center justify-between gap-3">
          <TabsList className="h-auto flex-wrap">
            <TabsTrigger value="gmail">
              <Mail /> Gmail
            </TabsTrigger>
            <TabsTrigger value="telegram">
              <Send /> Telegram
            </TabsTrigger>
            <TabsTrigger value="atomicmail">
              <Mail /> Atomic Mail
            </TabsTrigger>
            <TabsTrigger value="connections">
              <KeyRound /> API Connections
            </TabsTrigger>
          </TabsList>
          <Input
            className="w-full sm:w-64"
            aria-label="Filter accounts and connections"
            placeholder="Search accounts or connections…"
            value={filter}
            onChange={(event) => setFilter(event.target.value)}
          />
        </div>
        {(["gmail", "telegram", "atomicmail"] as const).map((provider) => (
          <TabsContent key={provider} value={provider} className="mt-5 space-y-4">
            {loading ? (
              <Card>
                <CardContent className="flex items-center justify-center gap-2 py-12 text-muted-foreground">
                  <Loader2 className="size-5 animate-spin" /> Loading accounts…
                </CardContent>
              </Card>
            ) : shown.length === 0 ? (
              <Card className="border-dashed">
                <CardContent className="flex flex-col items-center py-14 text-center">
                  <div className="mb-4 rounded-2xl bg-muted p-4">
                    {provider !== "telegram" ? <Mail className="size-7" /> : <Send className="size-7" />}
                  </div>
                  <h2 className="font-semibold text-lg">
                    {filter ? "No matching accounts" : `Connect your first ${providerName(provider)} account`}
                  </h2>
                  <p className="mt-2 mb-5 max-w-md text-muted-foreground text-sm">
                    {provider === "atomicmail"
                      ? "Official hosted MCP for Atomic Mail for Agents. Add an inbox and its API key from dashboard.atomicmail.ai. Consumer atomicmail.io mailboxes are a separate service."
                      : provider === "gmail"
                        ? "Use an app password to connect Gmail. Your existing account is kept, and you can add more."
                        : "Create a Telegram user session with your phone number, verification code and two-step password when needed."}
                  </p>
                  <Button onClick={() => begin(provider)}>
                    <Plus /> Connect account
                  </Button>
                </CardContent>
              </Card>
            ) : (
              <div className="grid gap-4 lg:grid-cols-2 xl:grid-cols-3">
                {shown.map((account) => (
                  <Card key={account.id}>
                    <CardHeader>
                      <div className="flex items-start justify-between gap-2">
                        <div
                          className={`flex size-11 items-center justify-center rounded-xl ${provider !== "telegram" ? "bg-emerald-500/10 text-emerald-600" : "bg-sky-500/10 text-sky-600"}`}
                        >
                          {provider !== "telegram" ? <Mail /> : <Send />}
                        </div>
                        <Badge variant={account.status === "connected" && account.enabled ? "default" : "secondary"}>
                          {account.status === "connected" && account.enabled ? <Check className="size-3" /> : null}
                          {account.status}
                        </Badge>
                      </div>
                      <CardTitle className="mt-2 truncate">{account.label}</CardTitle>
                      <CardDescription className="break-all">{account.identity}</CardDescription>
                    </CardHeader>
                    <CardContent className="space-y-4">
                      <div className="flex items-center justify-between rounded-lg bg-muted/50 p-3 text-xs">
                        <span className="text-muted-foreground">Agent tools</span>
                        <span className="font-medium">{account.tool_count} available</span>
                      </div>
                      <p className="break-all font-mono text-muted-foreground text-xs">{account.mcp_name}</p>
                      {account.error && (
                        <p className="text-destructive text-xs" role="alert">
                          {account.error}
                        </p>
                      )}
                      <p className="text-muted-foreground text-xs">
                        {account.checked_at
                          ? `Last checked ${new Date(account.checked_at * 1000).toLocaleString()}`
                          : "Not checked yet"}
                      </p>
                      <div className="flex flex-wrap gap-2">
                        <Button
                          variant="outline"
                          size="sm"
                          disabled={Boolean(busy) || !account.enabled}
                          onClick={() => void action(account, "test")}
                        >
                          {busy === account.id ? <Loader2 className="animate-spin" /> : <RefreshCw />} Test
                        </Button>
                        <Button
                          variant="outline"
                          size="sm"
                          disabled={Boolean(busy)}
                          onClick={() => begin(provider, account)}
                        >
                          Manage
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          disabled={Boolean(busy)}
                          onClick={() => void action(account, "toggle")}
                        >
                          <Unplug /> {account.enabled ? "Disconnect" : "Reconnect"}
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          aria-label={`Remove ${account.label}`}
                          disabled={Boolean(busy)}
                          onClick={() => setRemove(account)}
                        >
                          <Trash2 className="text-destructive" />
                        </Button>
                      </div>
                    </CardContent>
                  </Card>
                ))}
              </div>
            )}
            <div className="flex gap-3 rounded-xl border bg-muted/30 p-4 text-muted-foreground text-sm">
              <ShieldCheck className="mt-0.5 size-4 shrink-0" />
              <p>
                {provider === "atomicmail"
                  ? "Inbox keys are encrypted on the server. Connection tests discover tools without reading or sending email. The entered address labels the connected inbox; use that inbox’s own key."
                  : provider === "gmail"
                    ? "App passwords stay on the server. Tests do not send mail or read message bodies."
                    : "One shared MCP connection per account serves your agents. Login codes and two-step passwords are not stored."}{" "}
                New accounts become available to agents on their next run.
              </p>
            </div>
          </TabsContent>
        ))}
        <TabsContent value="connections" className="mt-5 space-y-6">
          <div className="rounded-xl border bg-muted/30 p-4 text-sm">
            <p className="font-medium">One place for API credentials</p>
            <p className="mt-1 text-muted-foreground">
              Keys are never displayed. MCP connections are tested before activation. For model ordering and advanced
              search settings, use{" "}
              <Link className="underline" href="/system/llm">
                LLM settings
              </Link>{" "}
              and{" "}
              <Link className="underline" href="/system/settings">
                System settings
              </Link>
              .
            </p>
          </div>
          {(["mcp", "llm", "search"] as const).map((kind) => (
            <section key={kind} className="space-y-3">
              <h2 className="font-semibold text-base">
                {kind === "mcp" ? "MCP credentials" : kind === "llm" ? "Model API keys" : "Web search providers"}
              </h2>
              <div className="grid gap-3 lg:grid-cols-2 xl:grid-cols-3">
                {connections

                  .filter((c) => c.kind === kind && c.name.toLowerCase().includes(filter.toLowerCase()))

                  .map((item) => (
                    <Card key={`${item.kind}-${item.id}`} className="gap-3">
                      <CardHeader className="gap-2">
                        <div className="flex items-center justify-between gap-2">
                          <KeyRound className="size-4 text-muted-foreground" />
                          <Badge variant={item.configured ? "secondary" : "outline"}>
                            {item.managed ? "Managed" : item.configured ? "Credential saved" : "Needs credentials"}
                          </Badge>
                        </div>
                        <CardTitle className="break-all text-base">{item.name}</CardTitle>
                        <CardDescription>{item.note}</CardDescription>
                      </CardHeader>
                      <CardContent className="space-y-3">
                        <div className="flex flex-wrap gap-2">
                          <Badge variant="outline">
                            {item.kind === "llm"
                              ? item.enabled
                                ? "Primary"
                                : "Fallback"
                              : item.enabled
                                ? "Active"
                                : "Inactive"}
                          </Badge>
                          {item.needs_endpoint && <Badge variant="outline">Endpoint needed</Badge>}
                        </div>
                        <Button
                          variant="outline"
                          className="w-full"
                          disabled={item.managed || Boolean(busy)}
                          onClick={() => {
                            setConnection(item);

                            setTokens({});

                            setEndpoint(item.endpoint ?? "");

                            setFormError("");
                          }}
                        >
                          {item.managed
                            ? "Managed automatically"
                            : item.configured
                              ? "Update credentials"
                              : "Configure connection"}
                          <ArrowRight />
                        </Button>
                      </CardContent>
                    </Card>
                  ))}
              </div>
            </section>
          ))}
        </TabsContent>
      </Tabs>
      <Sheet
        open={dialog !== null}
        onOpenChange={(open) => {
          if (!open) void close();
        }}
      >
        <SheetContent className="overflow-y-auto sm:max-w-lg">
          <SheetHeader>
            <SheetTitle>{edit ? "Manage account" : `Connect ${providerName(dialog ?? "")}`}</SheetTitle>
            <SheetDescription>
              {edit
                ? "Update the label or replace the saved credential without creating another account."
                : dialog === "atomicmail"
                  ? "Official Atomic Mail for Agents MCP. Use an inbox API key, not your email password."
                  : dialog === "gmail"
                    ? "Connect with your Google app password. No OAuth project is needed."
                    : "Authorize your own account directly with Telegram."}
            </SheetDescription>
          </SheetHeader>
          <form onSubmit={(event) => void save(event)} className="space-y-5 px-4 pb-6">
            {dialog === "telegram" && !edit && (
              <ol className="flex gap-2 text-xs">
                {["Phone", "Login code", "Two-step verification"].map((label, index) => (
                  <li
                    key={label}
                    className={`flex flex-1 items-center gap-2 rounded-lg border p-2 ${index === stepNumber ? "border-primary bg-primary/5 font-medium" : "text-muted-foreground"}`}
                  >
                    <span>{index < stepNumber ? <Check className="size-3" /> : index + 1}</span>
                    {label}
                  </li>
                ))}
              </ol>
            )}
            {(edit || dialog === "gmail" || login.step === "phone") && (
              <div className="space-y-2">
                <Label htmlFor="account-label">Account label</Label>
                <Input
                  id="account-label"
                  placeholder="Personal, work, support…"
                  maxLength={80}
                  value={form.label}
                  onChange={(event) => setForm({ ...form, label: event.target.value })}
                />
              </div>
            )}
            {dialog === "atomicmail" && (
              <>
                <div className="space-y-2">
                  <Label htmlFor="atomic-address">Inbox address</Label>
                  <Input
                    id="atomic-address"
                    type="email"
                    required
                    readOnly={Boolean(edit)}
                    value={form.email}
                    onChange={(event) => setForm({ ...form, email: event.target.value })}
                    placeholder="you@atomicmail.ai"
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="atomic-key">{edit ? "New inbox API key (optional)" : "Inbox API key"}</Label>
                  <Input
                    id="atomic-key"
                    type="password"
                    autoComplete="new-password"
                    required={!edit}
                    value={form.password}
                    onChange={(event) => setForm({ ...form, password: event.target.value })}
                    placeholder={edit ? "Leave empty to keep the current key" : "Paste the inbox API key"}
                  />
                  <p className="text-muted-foreground text-xs">
                    Create or select an inbox in{" "}
                    <a className="underline" href="https://dashboard.atomicmail.ai" target="_blank" rel="noreferrer">
                      Atomic Mail for Agents
                    </a>
                    , then open Connect and copy its API key. This does not connect consumer atomicmail.io mailboxes.{" "}
                    <a
                      className="underline"
                      href="https://docs.atomicmail.ai/mcp-remote"
                      target="_blank"
                      rel="noreferrer"
                    >
                      Official MCP documentation
                    </a>
                    .
                  </p>
                </div>
              </>
            )}
            {dialog === "gmail" && (
              <>
                <div className="space-y-2">
                  <Label htmlFor="gmail-address">Gmail address</Label>
                  <Input
                    id="gmail-address"
                    type="email"
                    autoComplete="username"
                    required
                    readOnly={Boolean(edit)}
                    placeholder="you@gmail.com"
                    value={form.email}
                    onChange={(event) => setForm({ ...form, email: event.target.value })}
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="gmail-password">{edit ? "New app password (optional)" : "App password"}</Label>
                  <Input
                    id="gmail-password"
                    type="password"
                    autoComplete="new-password"
                    required={!edit}
                    placeholder={edit ? "Leave empty to keep the current password" : "16-character Google app password"}
                    value={form.password}
                    onChange={(event) => setForm({ ...form, password: event.target.value })}
                  />
                  <p className="text-muted-foreground text-xs">
                    Create an app password at{" "}
                    <a
                      className="underline"
                      href="https://myaccount.google.com/apppasswords"
                      target="_blank"
                      rel="noreferrer"
                    >
                      Google Account
                    </a>
                    . This is different from your normal Google password.
                  </p>
                </div>
              </>
            )}
            {dialog === "telegram" && !edit && login.step === "phone" && (
              <>
                <div className="space-y-2">
                  <Label htmlFor="telegram-phone">Phone number</Label>
                  <Input
                    id="telegram-phone"
                    type="tel"
                    autoComplete="tel"
                    required
                    placeholder="+98 912 123 4567"
                    value={form.phone}
                    onChange={(event) => setForm({ ...form, phone: event.target.value })}
                  />
                  <p className="text-muted-foreground text-xs">
                    Include the country code. Telegram chooses whether to deliver the code in the app or by SMS.
                  </p>
                </div>
                <details open={!telegramApp} className="rounded-xl border p-4">
                  <summary className="cursor-pointer font-medium text-sm">
                    {telegramApp ? "Use a different Telegram application" : "Telegram application credentials"}
                  </summary>
                  <p className="mt-3 text-muted-foreground text-xs">
                    Required once. Create your application at{" "}
                    <a href="https://my.telegram.org/apps" target="_blank" rel="noreferrer" className="underline">
                      my.telegram.org/apps
                    </a>
                    . A bot token cannot create a user session.
                  </p>
                  <div className="mt-3 space-y-2">
                    <Label htmlFor="telegram-api-id">API ID</Label>
                    <Input
                      id="telegram-api-id"
                      inputMode="numeric"
                      required={!telegramApp}
                      value={form.api_id}
                      onChange={(event) => setForm({ ...form, api_id: event.target.value })}
                    />
                    <Label htmlFor="telegram-api-hash">API hash</Label>
                    <Input
                      id="telegram-api-hash"
                      type="password"
                      autoComplete="new-password"
                      required={!telegramApp || Boolean(form.api_id)}
                      value={form.api_hash}
                      onChange={(event) => setForm({ ...form, api_hash: event.target.value })}
                    />
                  </div>
                </details>
              </>
            )}
            {dialog === "telegram" && login.step === "code" && (
              <div className="space-y-2">
                <Label htmlFor="telegram-code">Telegram login code</Label>
                <Input
                  id="telegram-code"
                  autoComplete="one-time-code"
                  inputMode="numeric"
                  required
                  placeholder="12345"
                  maxLength={12}
                  className="h-12 font-mono text-xl tracking-[0.4em]"
                  value={form.code}
                  onChange={(event) => setForm({ ...form, code: event.target.value.replace(/\s/g, "") })}
                />
                <p className="text-muted-foreground text-xs">
                  Delivery: {login.delivery || "Telegram"}. Keep this page open; the login expires after 10 minutes. Use
                  the code only in this form.
                </p>
              </div>
            )}
            {dialog === "telegram" && login.step === "password" && (
              <div className="space-y-2">
                <Label htmlFor="telegram-2fa">Two-step verification password</Label>
                <Input
                  id="telegram-2fa"
                  type="password"
                  autoComplete="off"
                  required
                  value={form.password}
                  onChange={(event) => setForm({ ...form, password: event.target.value })}
                />
                <p className="text-muted-foreground text-xs">
                  Telegram requires your account’s two-step password. It is used for this login and is not saved.
                </p>
              </div>
            )}
            {formError && (
              <p role="alert" className="rounded-lg bg-destructive/10 p-3 text-destructive text-sm">
                {formError}
              </p>
            )}
            <div className="flex justify-end gap-2 border-t pt-4">
              <Button type="button" variant="outline" onClick={() => void close()} disabled={Boolean(busy)}>
                Cancel
              </Button>
              <Button type="submit" disabled={Boolean(busy)}>
                {busy === "form" ? <Loader2 className="animate-spin" /> : <ShieldCheck />}
                {busy === "form"
                  ? "Connecting…"
                  : edit
                    ? "Save changes"
                    : dialog !== "telegram"
                      ? "Verify & connect"
                      : login.step === "phone"
                        ? "Send login code"
                        : login.step === "code"
                          ? "Verify code"
                          : "Create session"}
              </Button>
            </div>
          </form>
        </SheetContent>
      </Sheet>
      <Sheet
        open={connection !== null}
        onOpenChange={(open) => {
          if (!open && !busy) {
            setConnection(null);

            setTokens({});
          }
        }}
      >
        <SheetContent className="overflow-y-auto sm:max-w-lg">
          <SheetHeader>
            <SheetTitle>{connection?.name}</SheetTitle>
            <SheetDescription>
              Credentials remain on the server. Existing secrets are never returned to this page.
            </SheetDescription>
          </SheetHeader>
          <form className="space-y-5 px-4 pb-6" onSubmit={(event) => void saveConnection(event)}>
            {connection?.fields.map((field) => (
              <div key={field.key} className="space-y-2">
                <Label htmlFor={`credential-${field.key}`}>{field.key}</Label>
                <Input
                  id={`credential-${field.key}`}
                  type="password"
                  autoComplete="new-password"
                  required={!field.configured}
                  placeholder={field.configured ? "Saved securely — enter a replacement" : "Enter token or API key"}
                  value={tokens[field.key] ?? ""}
                  onChange={(event) => setTokens({ ...tokens, [field.key]: event.target.value })}
                />
              </div>
            ))}
            {connection?.needs_endpoint && (
              <div className="space-y-2">
                <Label htmlFor="mcp-endpoint">MCP endpoint URL</Label>
                <Input
                  id="mcp-endpoint"
                  type="url"
                  required
                  placeholder="https://service.example/mcp"
                  value={endpoint}
                  onChange={(event) => setEndpoint(event.target.value)}
                />
                <p className="text-muted-foreground text-xs">
                  This saved connection has no endpoint yet. A token alone is not enough to connect it.
                </p>
              </div>
            )}
            {formError && (
              <p role="alert" className="rounded-lg bg-destructive/10 p-3 text-destructive text-sm">
                {formError}
              </p>
            )}
            <Button type="submit" className="w-full" disabled={Boolean(busy)}>
              {busy === "connection" ? <Loader2 className="animate-spin" /> : <CheckCircle2 />}
              {connection?.kind === "llm" ? "Save replacement key" : "Save, test & activate"}
            </Button>
          </form>
        </SheetContent>
      </Sheet>
      <Sheet
        open={remove !== null}
        onOpenChange={(open) => {
          if (!open && !busy) setRemove(null);
        }}
      >
        <SheetContent>
          <SheetHeader>
            <SheetTitle>Remove {remove?.label}?</SheetTitle>
            <SheetDescription>
              This disconnects the account from ARTEX and removes its locally stored credentials and session. It does
              not delete your Gmail or Telegram account.
            </SheetDescription>
          </SheetHeader>
          {remove?.provider === "telegram" && (
            <label htmlFor="revoke-session" className="mx-4 flex items-start gap-2 rounded-lg border p-3 text-sm">
              <input
                id="revoke-session"
                type="checkbox"
                checked={revoke}
                onChange={(event) => setRevoke(event.target.checked)}
              />
              <span>Also revoke this Telegram login. Other devices stay connected.</span>
            </label>
          )}
          <div className="flex gap-2 px-4">
            <Button variant="outline" disabled={Boolean(busy)} onClick={() => setRemove(null)}>
              Keep account
            </Button>
            <Button
              variant="destructive"
              disabled={Boolean(busy)}
              onClick={async () => {
                if (!remove) return;

                setBusy("remove");

                try {
                  await post(`/accounts/${remove.id}`, { revoke }, "DELETE");

                  setRemove(null);

                  setRevoke(false);

                  toast.success("Account removed");

                  await load();
                } catch (error) {
                  toast.error(message(error));
                } finally {
                  setBusy("");
                }
              }}
            >
              {busy === "remove" ? <Loader2 className="animate-spin" /> : <Trash2 />} Remove account
            </Button>
          </div>
        </SheetContent>
      </Sheet>
    </div>
  );
}
